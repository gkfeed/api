package main

import (
	"database/sql"
	"encoding/json"
	_ "github.com/mattn/go-sqlite3"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gkfeed/api/internal/config"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/passwordhash"
	"gkfeed/api/internal/testschema"
)

func TestDeleteRouteDoesNotAllowGet(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/delete?id=1", nil)
	response := httptest.NewRecorder()

	newHandler(config.Config{}).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/delete returned status %d; want %d", response.Code, http.StatusNotFound)
	}
}

func TestSwaggerRoutesAreUnderAPI(t *testing.T) {
	for _, path := range []string{"/api/swagger/index.html", "/api/swagger/doc.json"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()

		newHandler(config.Config{}).ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("GET %s returned status %d; want %d", path, response.Code, http.StatusOK)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	response := httptest.NewRecorder()
	newHandler(config.Config{}).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("GET /swagger/index.html returned status %d; want %d", response.Code, http.StatusNotFound)
	}
}

func TestMeRouteAcceptsBasicAuth(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "db.sqlite")
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer database.Close()
	db.Configure(database)
	t.Cleanup(func() { db.Configure(nil) })

	testschema.Init(t, database)

	if _, err := database.Exec(
		"INSERT INTO users (id, name, hashed_password) VALUES (?, ?, ?)",
		7, "reader", testHash(t, "secret"),
	); err != nil {
		t.Fatalf("insert test user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.SetBasicAuth("reader", "secret")
	response := httptest.NewRecorder()

	newHandler(config.Config{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/auth/me returned status %d; want %d", response.Code, http.StatusOK)
	}

	var user struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&user); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if user.ID != 7 || user.Name != "reader" {
		t.Fatalf("GET /api/v1/auth/me returned %#v; want reader with ID 7", user)
	}
}

func TestPasswordLoginRefreshesAndRevokesRotatedSessions(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "db.sqlite")
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	db.Configure(database)
	t.Cleanup(func() { db.Configure(nil) })
	testschema.Init(t, database)
	hash, err := passwordhash.HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword() returned error: %v", err)
	}
	if _, err := database.Exec("INSERT INTO users (id, name, hashed_password) VALUES (?, ?, ?)", 1, "reader", hash); err != nil {
		t.Fatalf("insert test user: %v", err)
	}

	handler := newHandler(config.Config{
		AccessTokenTTL:  30 * time.Minute,
		RefreshTokenTTL: 90 * 24 * time.Hour,
	})
	loginRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"username":"reader","password":"secret"}`),
	)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login returned status %d; want %d", loginResponse.Code, http.StatusOK)
	}
	if loginResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("login Cache-Control = %q; want no-store", loginResponse.Header().Get("Cache-Control"))
	}

	var tokens struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshExpiresIn int64  `json:"refresh_expires_in"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("login returned empty tokens: %#v", tokens)
	}
	if tokens.TokenType != "Bearer" || tokens.ExpiresIn != 1800 || tokens.RefreshExpiresIn != 7_776_000 {
		t.Fatalf("login token metadata = %#v", tokens)
	}
	var storedRefreshHash string
	if err := database.QueryRow(
		"SELECT id FROM refresh_tokens WHERE user_id = ? AND expires_at > CURRENT_TIMESTAMP",
		1,
	).Scan(&storedRefreshHash); err != nil {
		t.Fatalf("read stored refresh token hash: %v", err)
	}
	if strings.Contains(storedRefreshHash, tokens.RefreshToken) {
		t.Fatal("database stored the raw refresh token")
	}

	meRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meRequest.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	meResponse := httptest.NewRecorder()
	handler.ServeHTTP(meResponse, meRequest)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("authenticated request returned status %d; want %d", meResponse.Code, http.StatusOK)
	}

	refreshRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/refresh",
		strings.NewReader(`{"refresh_token":"`+tokens.RefreshToken+`"}`),
	)
	refreshResponse := httptest.NewRecorder()
	handler.ServeHTTP(refreshResponse, refreshRequest)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("refresh returned status %d; want %d", refreshResponse.Code, http.StatusOK)
	}
	var rotated struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(refreshResponse.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if rotated.AccessToken == "" || rotated.RefreshToken == tokens.RefreshToken {
		t.Fatalf("refresh did not rotate tokens: %#v", rotated)
	}

	reuseRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/refresh",
		strings.NewReader(`{"refresh_token":"`+tokens.RefreshToken+`"}`),
	)
	reuseResponse := httptest.NewRecorder()
	handler.ServeHTTP(reuseResponse, reuseRequest)
	if reuseResponse.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh token returned status %d; want %d", reuseResponse.Code, http.StatusUnauthorized)
	}

	rotatedMeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rotatedMeRequest.Header.Set("Authorization", "Bearer "+rotated.AccessToken)
	rotatedMeResponse := httptest.NewRecorder()
	handler.ServeHTTP(rotatedMeResponse, rotatedMeRequest)
	if rotatedMeResponse.Code != http.StatusUnauthorized {
		t.Fatalf("access token from revoked family returned status %d; want %d", rotatedMeResponse.Code, http.StatusUnauthorized)
	}
}

func TestItemRouteRequiresAuthenticationAndOwner(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "db.sqlite")
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer database.Close()
	db.Configure(database)
	t.Cleanup(func() { db.Configure(nil) })

	testschema.Init(t, database)

	for _, user := range []struct {
		id       int
		name     string
		password string
	}{
		{1, "owner", "owner-password"},
		{2, "other", "other-password"},
	} {
		if _, err := database.Exec("INSERT INTO users (id, name, hashed_password) VALUES (?, ?, ?)", user.id, user.name, testHash(t, user.password)); err != nil {
			t.Fatalf("insert test user %d: %v", user.id, err)
		}
	}
	if _, err := database.Exec(
		"INSERT INTO feed (id, title, url, type, user_id) VALUES (?, ?, ?, ?, ?)",
		11, "Owner feed", "https://example.com/feed", "web", 1,
	); err != nil {
		t.Fatalf("insert test feed: %v", err)
	}
	if _, err := database.Exec(
		"INSERT INTO item (id, feed_id, title, text, date, link) VALUES (?, ?, ?, ?, ?, ?)",
		22, 11, "Owner item", "private", "2026-01-01T00:00:00Z", "https://example.com/item",
	); err != nil {
		t.Fatalf("insert test item: %v", err)
	}

	tests := []struct {
		name       string
		username   string
		password   string
		wantStatus int
	}{
		{name: "requires authentication", wantStatus: http.StatusUnauthorized},
		{name: "allows owner", username: "owner", password: "owner-password", wantStatus: http.StatusOK},
		{name: "rejects another user", username: "other", password: "other-password", wantStatus: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/item?id=22", nil)
			if test.username != "" {
				request.SetBasicAuth(test.username, test.password)
			}
			response := httptest.NewRecorder()

			newHandler(config.Config{}).ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("GET /api/v1/item returned status %d; want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestFeedTypesRouteIsPublic(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/feed_types", nil)
	response := httptest.NewRecorder()

	newHandler(config.Config{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/feed_types returned status %d; want %d", response.Code, http.StatusOK)
	}

	var feedTypes []string
	if err := json.NewDecoder(response.Body).Decode(&feedTypes); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !slices.Contains(feedTypes, "web") || !slices.Contains(feedTypes, "spoti:playlist") {
		t.Fatalf("GET /api/v1/feed_types returned unexpected feed types: %#v", feedTypes)
	}
}

func testHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := passwordhash.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
