package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/webauthn"
	"gkfeed/api/internal/db"
)

const passkeyBodyLimit = 1 << 20

type testPasskey struct {
	key       *ecdsa.PrivateKey
	id        []byte
	publicKey []byte
}

func newTestPasskey(t *testing.T) testPasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: key.X.FillBytes(make([]byte, 32)), -3: key.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	return testPasskey{key: key, id: []byte("http-test-passkey"), publicKey: publicKey}
}

func (p testPasskey) store(t *testing.T) {
	t.Helper()
	if err := db.AddWebAuthnCredential(1, webauthn.Credential{ID: p.id, PublicKey: p.publicKey}, "test"); err != nil {
		t.Fatal(err)
	}
}

func passkeyChallenge(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	requirePasskeyStatus(t, w, http.StatusOK)
	var options struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &options); err != nil {
		t.Fatal(err)
	}
	if options.PublicKey.Challenge == "" {
		t.Fatal("missing challenge")
	}
	return options.PublicKey.Challenge
}

func (p testPasskey) response(t *testing.T, challenge, origin string, registration bool) string {
	t.Helper()
	ceremonyType := "webauthn.get"
	if registration {
		ceremonyType = "webauthn.create"
	}
	clientData, err := json.Marshal(map[string]string{"type": ceremonyType, "challenge": challenge, "origin": origin})
	if err != nil {
		t.Fatal(err)
	}
	rpHash := sha256.Sum256([]byte("feed.example"))
	flags := byte(5) // User present and verified.
	if registration {
		flags |= 64 // Attested credential data.
	}
	authData := append(rpHash[:], flags, 0, 0, 0, 1)
	encode := base64.RawURLEncoding.EncodeToString
	response := map[string]string{"clientDataJSON": encode(clientData)}
	if registration {
		authData = append(authData, make([]byte, 16)...) // AAGUID.
		authData = binary.BigEndian.AppendUint16(authData, uint16(len(p.id)))
		authData = append(authData, p.id...)
		authData = append(authData, p.publicKey...)
		attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "authData": authData, "attStmt": map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		response["attestationObject"] = encode(attestation)
	} else {
		clientHash := sha256.Sum256(clientData)
		signed := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
		signature, err := ecdsa.SignASN1(rand.Reader, p.key, signed[:])
		if err != nil {
			t.Fatal(err)
		}
		response["authenticatorData"] = encode(authData)
		response["signature"] = encode(signature)
		response["userHandle"] = encode(binary.BigEndian.AppendUint64(nil, 1))
	}
	body, err := json.Marshal(map[string]any{"id": encode(p.id), "rawId": encode(p.id), "type": "public-key", "response": response})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func requirePasskeyStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d: %s", w.Code, status, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("auth response can be cached")
	}
	if status >= 400 {
		if strings.Contains(w.Body.String(), "token") || strings.Contains(w.Body.String(), "sqlite") || strings.Contains(w.Body.String(), "database") {
			t.Fatalf("error exposed auth/storage details: %s", w.Body.String())
		}
	}
}

func TestPasskeyMalformedHTTPBodies(t *testing.T) {
	database, cfg := browserFixture(t)
	handler := newHandler(cfg)
	cookie := browserLogin(t, handler)
	for _, path := range []string{
		"/api/v1/auth/register/begin", "/api/v1/auth/register/finish",
		"/api/v1/auth/login/finish", "/api/v2/sessions/passkey/finish",
	} {
		for name, body := range map[string]string{
			"malformed":            `{"name":"work",`,
			"wrong type":           `[]`,
			"wrong name type":      `{"name":42}`,
			"trailing value":       `{} {}`,
			"trailing garbage":     `{} garbage`,
			"oversized value":      `{"name":"` + strings.Repeat("a", passkeyBodyLimit) + `"}`,
			"oversized whitespace": `{}` + strings.Repeat(" ", passkeyBodyLimit),
		} {
			t.Run(path+"/"+name, func(t *testing.T) {
				// Unknown Content-Length also exercises streaming requests.
				r := httptest.NewRequest("POST", path, strings.NewReader(body))
				r.ContentLength = -1
				r.Header.Set("Origin", browserOrigin)
				r.AddCookie(cookie)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				requirePasskeyStatus(t, w, http.StatusBadRequest)
			})
		}
	}
	for _, body := range []string{"", " \n\t", `{}`, `null`, `{"name":"work"}`, `{"name":"work","extension":true}`, `{}` + strings.Repeat(" ", passkeyBodyLimit-2)} {
		t.Run("optional", func(t *testing.T) {
			w := browserRequest(handler, "POST", "/api/v1/auth/register/begin", body, browserOrigin, cookie)
			passkeyChallenge(t, w)
		})
	}
	var count int
	if err := database.QueryRow("SELECT count(*) FROM webauthn_credentials").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid bodies created %d credentials", count)
	}
}

func TestPasskeyLoginHTTPValidationAndLimits(t *testing.T) {
	for _, prefix := range []string{"/api/v1/auth/login", "/api/v2/sessions/passkey"} {
		t.Run(prefix, func(t *testing.T) {
			database, cfg := browserFixture(t)
			handler := newHandler(cfg)
			p := newTestPasskey(t)
			p.store(t)
			begin := func() string {
				return passkeyChallenge(t, browserRequest(handler, "POST", prefix+"/begin", "", browserOrigin, nil))
			}
			finish := func(body string) *httptest.ResponseRecorder {
				w := browserRequest(handler, "POST", prefix+"/finish", body, browserOrigin, nil)
				if w.Code >= 400 && len(w.Result().Cookies()) != 0 {
					t.Fatal("failed login issued cookie")
				}
				return w
			}
			for _, invalid := range []string{"unknown challenge", "wrong origin", "bad signature", "unknown credential", "wrong user handle"} {
				t.Run(invalid, func(t *testing.T) {
					challenge, origin := begin(), browserOrigin
					candidate := p
					switch invalid {
					case "unknown challenge":
						challenge = "unknown"
					case "wrong origin":
						origin = "https://evil.example"
					case "bad signature":
						candidate.key = newTestPasskey(t).key
					case "unknown credential":
						candidate.id = []byte("unknown")
					}
					body := candidate.response(t, challenge, origin, false)
					if invalid == "wrong user handle" {
						body = strings.Replace(body, `"userHandle":"AAAAAAAAAAE"`, `"userHandle":"AAAAAAAAAAI"`, 1)
					}
					requirePasskeyStatus(t, finish(body), http.StatusUnauthorized)
				})
			}
			// A valid assertion plus trailing whitespace must respect the same limit.
			body := p.response(t, begin(), browserOrigin, false)
			requirePasskeyStatus(t, finish(body+strings.Repeat(" ", passkeyBodyLimit+1-len(body))), http.StatusBadRequest)
			// Rejection during parsing must not consume the pending ceremony.
			w := finish(body + strings.Repeat(" ", passkeyBodyLimit-len(body)))
			if prefix == "/api/v2/sessions/passkey" {
				browserLoginCookie(t, w)
			} else {
				requirePasskeyStatus(t, w, http.StatusOK)
				var tokens struct {
					AccessToken      string `json:"access_token"`
					RefreshToken     string `json:"refresh_token"`
					CredentialID     string `json:"credential_id"`
					TokenType        string `json:"token_type"`
					ExpiresIn        int    `json:"expires_in"`
					RefreshExpiresIn int    `json:"refresh_expires_in"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
					t.Fatal(err)
				}
				if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.CredentialID != base64.RawURLEncoding.EncodeToString(p.id) || tokens.TokenType != "Bearer" || tokens.ExpiresIn <= 0 || tokens.RefreshExpiresIn <= 0 {
					t.Fatalf("v1 token contract = %s", w.Body.String())
				}
				if len(w.Result().Cookies()) != 0 {
					t.Fatal("v1 passkey login set cookie")
				}
				r := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
				r.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
				authenticated := httptest.NewRecorder()
				handler.ServeHTTP(authenticated, r)
				requirePasskeyStatus(t, authenticated, http.StatusOK)
			}
			requirePasskeyStatus(t, finish(body), http.StatusUnauthorized)
			table := "refresh_tokens"
			if prefix == "/api/v2/sessions/passkey" {
				table = "browser_sessions"
			}
			var count int
			if err := database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("rejected assertions created sessions: %d", count)
			}
		})
	}
}

func TestPasskeyRegistrationHTTPValidationAndLimits(t *testing.T) {
	database, cfg := browserFixture(t)
	handler := newHandler(cfg)
	cookie := browserLogin(t, handler)
	p := newTestPasskey(t)
	begin := func() string {
		return passkeyChallenge(t, browserRequest(handler, "POST", "/api/v1/auth/register/begin", `{"name":"work laptop"}`, browserOrigin, cookie))
	}
	finish := func(body string) *httptest.ResponseRecorder {
		return browserRequest(handler, "POST", "/api/v1/auth/register/finish", body, browserOrigin, cookie)
	}
	for _, tc := range []struct{ challenge, origin string }{{"unknown", browserOrigin}, {begin(), "https://evil.example"}} {
		// Cookie authentication renews a cookie even when the handler rejects a request.
		requirePasskeyStatus(t, finish(p.response(t, tc.challenge, tc.origin, true)), http.StatusBadRequest)
	}
	body := p.response(t, begin(), browserOrigin, true)
	requirePasskeyStatus(t, finish(body+strings.Repeat(" ", passkeyBodyLimit+1-len(body))), http.StatusBadRequest)
	w := finish(body + strings.Repeat(" ", passkeyBodyLimit-len(body)))
	requirePasskeyStatus(t, w, http.StatusOK)
	var registered struct{ ID, Name string }
	if err := json.Unmarshal(w.Body.Bytes(), &registered); err != nil {
		t.Fatal(err)
	}
	if registered.ID != base64.RawURLEncoding.EncodeToString(p.id) || registered.Name != "work laptop" {
		t.Fatalf("registration contract = %s", w.Body.String())
	}
	requirePasskeyStatus(t, finish(body), http.StatusBadRequest)
	var count int
	if err := database.QueryRow("SELECT count(*) FROM webauthn_credentials").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("registration persisted %d credentials", count)
	}
}

func TestPasskeyLoginHTTPStorageFailures(t *testing.T) {
	for _, prefix := range []string{"/api/v1/auth/login", "/api/v2/sessions/passkey"} {
		for _, stage := range []string{"credential lookup", "user lookup", "credential load", "credential update", "session insert"} {
			t.Run(prefix+"/"+stage, func(t *testing.T) {
				database, cfg := browserFixture(t)
				handler := newHandler(cfg)
				p := newTestPasskey(t)
				p.store(t)
				challenge := passkeyChallenge(t, browserRequest(handler, "POST", prefix+"/begin", "", browserOrigin, nil))
				var statement string
				switch stage {
				case "credential lookup":
					statement = "ALTER TABLE webauthn_credentials RENAME TO unavailable_credentials"
				case "user lookup":
					statement = "ALTER TABLE users RENAME TO unavailable_users"
				case "credential load":
					statement = `UPDATE webauthn_credentials SET credential='corrupt JSON'`
				case "credential update":
					statement = `CREATE TRIGGER reject_update BEFORE UPDATE ON webauthn_credentials BEGIN SELECT RAISE(ABORT,'injected write failure'); END`
				case "session insert":
					table := "refresh_tokens"
					if prefix == "/api/v2/sessions/passkey" {
						table = "browser_sessions"
					}
					statement = "CREATE TRIGGER reject_insert BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'injected session failure'); END"
				}
				if _, err := database.Exec(statement); err != nil {
					t.Fatal(err)
				}
				w := browserRequest(handler, "POST", prefix+"/finish", p.response(t, challenge, browserOrigin, false), browserOrigin, nil)
				requirePasskeyStatus(t, w, http.StatusInternalServerError)
				if len(w.Result().Cookies()) != 0 {
					t.Fatal("storage failure issued cookie")
				}
				if w.Body.String() != "Internal Server Error\n" {
					t.Fatalf("storage failure leaked details: %s", w.Body.String())
				}
			})
		}
	}
}

func TestPasskeyRegistrationHTTPStorageFailures(t *testing.T) {
	for _, stage := range []string{"begin credential load", "finish credential load", "credential insert"} {
		t.Run(stage, func(t *testing.T) {
			database, cfg := browserFixture(t)
			handler := newHandler(cfg)
			cookie := browserLogin(t, handler)
			p := newTestPasskey(t)
			path, body := "/api/v1/auth/register/begin", ""
			if stage != "begin credential load" {
				begin := browserRequest(handler, "POST", path, "", browserOrigin, cookie)
				body = p.response(t, passkeyChallenge(t, begin), browserOrigin, true)
				path = "/api/v1/auth/register/finish"
			}
			statement := "ALTER TABLE webauthn_credentials RENAME TO unavailable_credentials"
			if stage == "credential insert" {
				statement = `CREATE TRIGGER reject_insert BEFORE INSERT ON webauthn_credentials BEGIN SELECT RAISE(ABORT,'injected credential failure'); END`
			}
			if _, err := database.Exec(statement); err != nil {
				t.Fatal(err)
			}
			w := browserRequest(handler, "POST", path, body, browserOrigin, cookie)
			requirePasskeyStatus(t, w, http.StatusInternalServerError)
			if w.Body.String() != "Internal Server Error\n" {
				t.Fatalf("storage failure leaked details: %s", w.Body.String())
			}
		})
	}
}

func TestAuthHTTPErrorResponsesDisableCaching(t *testing.T) {
	_, cfg := browserFixture(t)
	handler := newHandler(cfg)
	for _, endpoint := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/api/v1/auth/login", 400},
		{"POST", "/api/v1/auth/refresh", 400},
		{"POST", "/api/v1/auth/logout", 400},
		{"POST", "/api/v1/auth/logout-all", 401},
		{"GET", "/api/v1/auth/me", 401},
		{"GET", "/api/v1/auth/credentials", 401},
		{"DELETE", "/api/v1/auth/credentials/missing", 401},
		{"POST", "/api/v1/auth/register/begin", 401},
		{"POST", "/api/v1/auth/register/finish", 401},
		{"POST", "/api/v2/sessions", 403},
		{"POST", "/api/v2/sessions/passkey/begin", 403},
		{"POST", "/api/v2/sessions/passkey/finish", 403},
		{"DELETE", "/api/v2/sessions/current", 403},
		{"GET", "/api/v2/me", 401},
	} {
		t.Run(endpoint.path, func(t *testing.T) {
			requirePasskeyStatus(t, browserRequest(handler, endpoint.method, endpoint.path, "", "", nil), endpoint.status)
		})
	}
}
