package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/config"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/testschema"
)

const browserOrigin = "https://feed.example"

func browserFixture(t *testing.T) (*sql.DB, config.Config) {
	t.Helper()
	database, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	db.Configure(database)
	t.Cleanup(func() { db.Configure(nil); database.Close() })
	testschema.Init(t, database)
	if _, err := database.Exec("INSERT INTO users (id,name,hashed_password) VALUES (1,'reader',$1)", testHash(t, "secret")); err != nil {
		t.Fatal(err)
	}
	return database, config.Config{AllowedOrigins: []string{browserOrigin}, WebAuthnRPID: "feed.example", WebAuthnRPOrigin: browserOrigin, WebAuthnRPDisplay: "GKFeed"}
}

func browserRequest(handler http.Handler, method, path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func browserLogin(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	w := browserRequest(handler, http.MethodPost, "/api/v2/sessions", `{"username":"reader","password":"secret"}`, browserOrigin, nil)
	return browserLoginCookie(t, w)
}

func browserLoginCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	if w.Code != http.StatusCreated {
		t.Fatalf("login = %d, %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 || body["id"] != float64(1) || body["name"] != "reader" {
		t.Fatalf("login body = %#v", body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %#v", cookies)
	}
	c := cookies[0]
	if c.Name != auth.BrowserSessionCookie || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.MaxAge != int(auth.BrowserSessionIdleTTL/time.Second) {
		t.Fatalf("cookie = %#v", c)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("login can be cached")
	}
	return c
}

func TestBrowserSessionLifecycle(t *testing.T) {
	database, cfg := browserFixture(t)
	handler := newHandler(cfg)
	first := browserLogin(t, handler)
	second := browserLogin(t, handler)
	var storedHash []byte
	if err := database.QueryRow("SELECT token_hash FROM browser_sessions WHERE token_hash=$1", auth.DigestToken(first.Value)).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedHash, auth.DigestToken(first.Value)) || bytes.Contains(storedHash, []byte(first.Value)) {
		t.Fatal("raw session token stored")
	}
	// Rebuilding the handler replaces in-memory stores while the database stays.
	handler = newHandler(cfg)
	for _, path := range []string{"/api/v2/me", "/api/v1/list", "/api/v1/auth/me", "/api/v2/feeds", "/api/v2/items", "/api/v2/items/sync"} {
		w := browserRequest(handler, http.MethodGet, path, "", "", first)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("restored %s = %d, %s", path, w.Code, w.Body.String())
		}
	}
	for range 2 {
		w := browserRequest(handler, http.MethodDelete, "/api/v2/sessions/current", "", browserOrigin, first)
		if w.Code != 204 || w.Body.Len() != 0 {
			t.Fatalf("logout = %d, %s", w.Code, w.Body.String())
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" || !cookies[0].Secure || !cookies[0].HttpOnly {
			t.Fatalf("logout cookie = %#v", cookies)
		}
	}
	if w := browserRequest(handler, http.MethodGet, "/api/v2/me", "", "", first); w.Code != 401 {
		t.Fatalf("revoked = %d", w.Code)
	}
	if w := browserRequest(handler, http.MethodGet, "/api/v2/me", "", "", second); w.Code != 200 {
		t.Fatalf("other device = %d", w.Code)
	}
	if w := browserRequest(handler, http.MethodDelete, "/api/v2/sessions/current", "", browserOrigin, nil); w.Code != 204 {
		t.Fatalf("anonymous logout = %d", w.Code)
	}
	// v2 /me restores only cookie sessions, never Basic/Bearer credentials.
	r := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	r.SetBasicAuth("reader", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("v2 me accepted Basic: %d", w.Code)
	}
}

func TestBrowserSessionOriginProtection(t *testing.T) {
	_, cfg := browserFixture(t)
	handler := newHandler(cfg)
	cookie := browserLogin(t, handler)
	for _, endpoint := range []struct{ method, path, body string }{
		{"POST", "/api/v2/sessions", `{"username":"reader","password":"secret"}`},
		{"DELETE", "/api/v2/sessions/current", ""},
		{"POST", "/api/v2/sessions/passkey/begin", ""},
		{"POST", "/api/v2/sessions/passkey/finish", "{}"},
		{"DELETE", "/api/v1/items/123", ""},
		{"DELETE", "/api/v2/items/123", ""},
		{"POST", "/api/v2/feeds", "{}"},
		{"POST", "/api/v1/auth/logout-all", ""},
		{"POST", "/api/v1/auth/register/begin", "{}"},
	} {
		for _, origin := range []string{"", "null", "https://evil.example", browserOrigin + ".evil.example", browserOrigin + "/"} {
			w := browserRequest(handler, endpoint.method, endpoint.path, endpoint.body, origin, cookie)
			if w.Code != 403 {
				t.Fatalf("%s origin %q = %d, %s", endpoint.path, origin, w.Code, w.Body.String())
			}
		}
	}
	w := browserRequest(handler, "DELETE", "/api/v1/items/123", "", browserOrigin, cookie)
	if w.Code != 404 {
		t.Fatalf("allowed cookie mutation = %d, %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("DELETE", "/api/v1/items/123", nil)
	r.SetBasicAuth("reader", "secret")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("Basic compatibility = %d, %s", w.Code, w.Body.String())
	}
	// Failed Origin checks must not revoke or renew the session.
	if w := browserRequest(handler, "GET", "/api/v2/me", "", "", cookie); w.Code != 200 {
		t.Fatalf("session lost after rejected mutation = %d", w.Code)
	}
}

func TestBrowserSessionInvalidAndExpired(t *testing.T) {
	database, cfg := browserFixture(t)
	handler := newHandler(cfg)
	for _, body := range []string{`{}`, `{"username":"reader","password":"wrong"}`, `{"username":"missing","password":"secret"}`} {
		w := browserRequest(handler, "POST", "/api/v2/sessions", body, browserOrigin, nil)
		if w.Code != 400 && w.Code != 401 {
			t.Fatalf("bad login = %d", w.Code)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("failed login issued cookie")
		}
	}
	for _, value := range []string{"", "invalid", strings.Repeat("A", 43)} {
		w := browserRequest(handler, "GET", "/api/v2/me", "", "", &http.Cookie{Name: auth.BrowserSessionCookie, Value: value})
		if w.Code != 401 {
			t.Fatalf("invalid cookie = %d, %s", w.Code, w.Body.String())
		}
	}
	for _, column := range []string{"last_seen_at", "expires_at"} {
		cookie := browserLogin(t, handler)
		expiry := time.Now().UTC().Add(-time.Second)
		if column == "last_seen_at" {
			expiry = expiry.Add(-auth.BrowserSessionIdleTTL)
		}
		if _, err := database.Exec("UPDATE browser_sessions SET "+column+"=$1 WHERE token_hash=$2", expiry, auth.DigestToken(cookie.Value)); err != nil {
			t.Fatal(err)
		}
		w := browserRequest(handler, "GET", "/api/v2/me", "", "", cookie)
		if w.Code != 401 {
			t.Fatalf("expired %s = %d, %s", column, w.Code, w.Body.String())
		}
	}
	// Renewed cookie must not carry a lifetime beyond the absolute expiry.
	cookie := browserLogin(t, handler)
	if _, err := database.Exec("UPDATE browser_sessions SET expires_at=$1 WHERE token_hash=$2", time.Now().UTC().Add(time.Hour), auth.DigestToken(cookie.Value)); err != nil {
		t.Fatal(err)
	}
	w := browserRequest(handler, "GET", "/api/v2/me", "", "", cookie)
	if w.Code != 200 || w.Result().Cookies()[0].MaxAge > 3600 {
		t.Fatalf("absolute cookie limit = %d, %#v", w.Code, w.Result().Cookies())
	}
}

func TestBrowserPasskeySession(t *testing.T) {
	_, cfg := browserFixture(t)
	p := newTestPasskey(t)
	p.store(t)
	handler := newHandler(cfg)
	begin := browserRequest(handler, "POST", "/api/v2/sessions/passkey/begin", "", browserOrigin, nil)
	body := p.response(t, passkeyChallenge(t, begin), browserOrigin, false)
	finish := browserRequest(handler, "POST", "/api/v2/sessions/passkey/finish", body, browserOrigin, nil)
	cookie := browserLoginCookie(t, finish)
	if w := browserRequest(newHandler(cfg), "GET", "/api/v2/me", "", "", cookie); w.Code != 200 {
		t.Fatalf("passkey cookie = %d, %s", w.Code, w.Body.String())
	}
	if w := browserRequest(handler, "POST", "/api/v2/sessions/passkey/finish", body, browserOrigin, nil); w.Code != 401 {
		t.Fatalf("challenge replay = %d", w.Code)
	}
}

func TestBrowserSessionOriginAndCORSConfiguration(t *testing.T) {
	_, cfg := browserFixture(t)
	handler := newHandler(cfg)
	r := httptest.NewRequest("POST", "/api/v2/sessions", strings.NewReader(`{"username":"reader","password":"secret"}`))
	r.Header.Add("Origin", browserOrigin)
	r.Header.Add("Origin", browserOrigin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("duplicate Origin = %d", w.Code)
	}
	wildcard := cfg
	wildcard.AllowedOrigins = []string{"*"}
	if w := browserRequest(newHandler(wildcard), "POST", "/api/v2/sessions", `{"username":"reader","password":"secret"}`, browserOrigin, nil); w.Code != 403 {
		t.Fatalf("wildcard origin = %d", w.Code)
	}
	preflight := httptest.NewRequest("OPTIONS", "/api/v2/sessions/current", nil)
	preflight.Header.Set("Origin", browserOrigin)
	preflight.Header.Set("Access-Control-Request-Method", "DELETE")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, preflight)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Methods") != "DELETE" || w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("preflight = %d, %v", w.Code, w.Header())
	}
	noPasskeys := cfg
	noPasskeys.WebAuthnRPID = ""
	noPasskeys.WebAuthnRPOrigin = ""
	for _, path := range []string{"/api/v2/sessions/passkey/begin", "/api/v2/sessions/passkey/finish"} {
		if w := browserRequest(newHandler(noPasskeys), "POST", path, "{}", browserOrigin, nil); w.Code != 503 {
			t.Fatalf("unconfigured passkey %s = %d", path, w.Code)
		}
	}
}

func TestBrowserSyncCursorAndSessionAfterRestart(t *testing.T) {
	database, cfg := browserFixture(t)
	cfg.SyncCursorSecret = strings.Repeat("s", 32)
	if _, err := database.Exec("INSERT INTO feed (id,title,url,type,user_id) VALUES (1,'test','https://example.com','rss',1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO item (id,feed_id,title,text,date,link) VALUES (1,1,'test','','2026-01-01T00:00:00Z','')"); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(cfg)
	cookie := browserLogin(t, handler)
	page := browserRequest(handler, "GET", "/api/v2/items/sync", "", "", cookie)
	var response struct {
		SyncCursor string `json:"sync_cursor"`
	}
	if err := json.Unmarshal(page.Body.Bytes(), &response); err != nil || page.Code != 200 || response.SyncCursor == "" {
		t.Fatalf("sync page = %d, %s, %v", page.Code, page.Body.String(), err)
	}
	path := "/api/v2/items/changes?cursor=" + response.SyncCursor
	if w := browserRequest(newHandler(cfg), "GET", path, "", "", cookie); w.Code != 200 {
		t.Fatalf("restored sync = %d, %s", w.Code, w.Body.String())
	}
	cfg.SyncCursorSecret = strings.Repeat("r", 32)
	restarted := newHandler(cfg)
	if w := browserRequest(restarted, "GET", path, "", "", cookie); w.Code != 400 {
		t.Fatalf("rotated key accepted old cursor = %d", w.Code)
	}
	if w := browserRequest(restarted, "GET", "/api/v2/me", "", "", cookie); w.Code != 200 {
		t.Fatalf("sync key rotation ended browser session = %d", w.Code)
	}
}
