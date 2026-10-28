package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
	"gkfeed/api/internal/db"
)

// Both applications deliberately use user 1 and the same username. Constructing
// the second handler must not redirect the first handler's persistence.
func TestAuthPersistenceIsIsolatedBetweenHandlers(t *testing.T) {
	t.Parallel()
	firstDB, cfg := browserFixture(t)
	first := newHandler(cfg, firstDB)
	secondDB, _ := browserFixture(t)
	if _, err := secondDB.Exec("UPDATE users SET hashed_password=$1 WHERE id=1", testHash(t, "second-secret")); err != nil {
		t.Fatal(err)
	}
	second := newHandler(cfg, secondDB)

	call := func(handler http.Handler, method, path, body, password string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if password != "" {
			r.SetBasicAuth("reader", password)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	assertStatus := func(w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body.String())
		}
	}
	type tokenPair struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	login := func(handler http.Handler, password string) tokenPair {
		t.Helper()
		body := `{"username":"reader","password":"` + password + `"}`
		w := call(handler, "POST", "/api/v1/auth/login", body, "")
		assertStatus(w, 200)
		var pair tokenPair
		if err := json.Unmarshal(w.Body.Bytes(), &pair); err != nil || pair.AccessToken == "" || pair.RefreshToken == "" {
			t.Fatalf("login = %s, %v", w.Body.String(), err)
		}
		return pair
	}

	// Both handlers remain alive while requests alternate between their pools.
	for _, test := range []struct {
		handler                 http.Handler
		password, otherPassword string
	}{
		{first, "secret", "second-secret"},
		{second, "second-secret", "secret"},
	} {
		assertStatus(call(test.handler, "GET", "/api/v1/auth/me", "", test.password), 200)
		assertStatus(call(test.handler, "GET", "/api/v1/auth/me", "", test.otherPassword), 401)
	}

	firstTokens := login(first, "secret")
	secondTokens := login(second, "second-secret")
	refreshBody := `{"refresh_token":"` + firstTokens.RefreshToken + `"}`
	assertStatus(call(second, "POST", "/api/v1/auth/refresh", refreshBody, ""), 401)
	assertStatus(call(first, "POST", "/api/v1/auth/refresh", refreshBody, ""), 200)

	firstCookie := browserLogin(t, first)
	secondCookie := browserLoginCookie(t, browserRequest(second, "POST", "/api/v2/sessions", `{"username":"reader","password":"second-secret"}`, browserOrigin, nil))
	assertStatus(browserRequest(second, "GET", "/api/v2/me", "", "", firstCookie), 401)
	assertStatus(browserRequest(second, "DELETE", "/api/v2/sessions/current", "", browserOrigin, firstCookie), 204)
	assertStatus(browserRequest(first, "GET", "/api/v2/me", "", "", firstCookie), 200)

	for _, fixture := range []struct {
		database *sql.DB
		name     string
	}{
		{firstDB, "first-key"}, {secondDB, "second-key"},
	} {
		credential := webauthn.Credential{ID: []byte(fixture.name)}
		if err := db.NewCredentialRepository(fixture.database).AddWebAuthnCredential(t.Context(), 1, credential, fixture.name); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		handler        http.Handler
		password, name string
	}{
		{first, "secret", "first-key"}, {second, "second-secret", "second-key"},
	} {
		w := call(test.handler, "GET", "/api/v1/auth/credentials", "", test.password)
		assertStatus(w, 200)
		var credentials []struct{ Name string }
		if err := json.Unmarshal(w.Body.Bytes(), &credentials); err != nil || len(credentials) != 1 || credentials[0].Name != test.name {
			t.Fatalf("credentials = %s, %v", w.Body.String(), err)
		}
	}
	credentialPath := "/api/v1/auth/credentials/" + base64.RawURLEncoding.EncodeToString([]byte("first-key"))
	assertStatus(call(second, "DELETE", credentialPath, "", "second-secret"), 404)
	assertStatus(call(first, "DELETE", credentialPath, "", "secret"), 204)

	// Revoking user 1 in one pool must leave the other application's refresh,
	// browser and in-memory access sessions intact.
	assertStatus(call(first, "POST", "/api/v1/auth/logout-all", "", "secret"), 204)
	assertStatus(browserRequest(first, "GET", "/api/v2/me", "", "", firstCookie), 401)
	assertStatus(browserRequest(second, "GET", "/api/v2/me", "", "", secondCookie), 200)
	refreshBody = `{"refresh_token":"` + secondTokens.RefreshToken + `"}`
	assertStatus(call(second, "POST", "/api/v1/auth/refresh", refreshBody, ""), 200)
	r := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	r.Header.Set("Authorization", "Bearer "+secondTokens.AccessToken)
	w := httptest.NewRecorder()
	second.ServeHTTP(w, r)
	assertStatus(w, 200)

}
