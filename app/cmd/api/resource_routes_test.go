package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gkfeed/api/internal/config"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/testschema"
)

func TestV2ResourceRoutesAndV1Compatibility(t *testing.T) {
	database, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	db.Configure(database)
	t.Cleanup(func() { db.Configure(nil) })
	testschema.Init(t, database)
	for _, user := range []struct {
		id   int
		name string
	}{
		{1, "owner"}, {2, "other"},
	} {
		if _, err := database.Exec("INSERT INTO users (id, name, hashed_password) VALUES (?, ?, ?)", user.id, user.name, testHash(t, "secret")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec("INSERT INTO feed (id, title, url, type, user_id) VALUES (?, ?, ?, ?, ?)", 11, "Owner feed", "https://example.com/feed", "web", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO item (id, feed_id, title, text, date, link) VALUES (?, ?, ?, ?, ?, ?)", 22, 11, "Item", "Text", "2026-01-01T00:00:00Z", "https://example.com/item"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO item (id, feed_id, title, text, date, link) VALUES (?, ?, ?, ?, ?, ?)", 23, 11, "Delete me", "Text", "2026-01-01T00:00:00Z", "https://example.com/delete"); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(config.Config{})
	call := func(method, path, username, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		if username != "" {
			request.SetBasicAuth(username, "secret")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	for _, test := range []struct {
		method, path, username, body string
		status                       int
	}{
		{http.MethodGet, "/api/v2/feeds", "", "", 401},
		{http.MethodGet, "/api/v2/feeds", "owner", "", 200},
		{http.MethodGet, "/api/v2/feeds/11", "owner", "", 200},
		{http.MethodGet, "/api/v2/feeds/11", "other", "", 404},
		{http.MethodGet, "/api/v2/feeds/0", "owner", "", 400},
		{http.MethodGet, "/api/v2/items?limit=1&cursor=0", "owner", "", 200},
		{http.MethodGet, "/api/v2/items/22", "owner", "", 200},
		{http.MethodGet, "/api/v2/items/22", "other", "", 404},
		{http.MethodGet, "/api/v1/item?id=22", "owner", "", 200},
		{http.MethodGet, "/api/v1/list", "owner", "", 200},
		{http.MethodPost, "/api/v2/feeds", "owner", `{"url":"https://new"}`, 400},
		{http.MethodPatch, "/api/v2/items/22", "owner", "", 405},
	} {
		response := call(test.method, test.path, test.username, test.body)
		if response.Code != test.status {
			t.Errorf("%s %s as %s: status %d, want %d: %s", test.method, test.path, test.username, response.Code, test.status, response.Body.String())
		}
	}

	feedResponse := call(http.MethodGet, "/api/v2/feeds/11", "owner", "")
	var feed struct {
		ID     int `json:"id"`
		UserID int `json:"userid"`
	}
	if err := json.Unmarshal(feedResponse.Body.Bytes(), &feed); err != nil || feed.ID != 11 || feed.UserID != 1 {
		t.Fatalf("feed = %#v, %v", feed, err)
	}
	itemResponse := call(http.MethodGet, "/api/v2/items/22", "owner", "")
	var item struct {
		Item struct {
			ID int `json:"id"`
		} `json:"item"`
		Feed struct {
			ID int `json:"id"`
		} `json:"feed"`
	}
	if err := json.Unmarshal(itemResponse.Body.Bytes(), &item); err != nil || item.Item.ID != 22 || item.Feed.ID != 11 {
		t.Fatalf("item = %#v, %v", item, err)
	}
	if response := call(http.MethodDelete, "/api/v2/items/23", "other", ""); response.Code != 404 {
		t.Fatalf("foreign item delete status = %d", response.Code)
	}
	if response := call(http.MethodDelete, "/api/v2/items/23", "owner", ""); response.Code != 204 {
		t.Fatalf("item delete status = %d: %s", response.Code, response.Body.String())
	}
	if response := call(http.MethodGet, "/api/v2/items/23", "owner", ""); response.Code != 404 {
		t.Fatalf("deleted item status = %d", response.Code)
	}
	createBody := `{"title":"New","type":"web","url":"https://new"}`
	first := call(http.MethodPost, "/api/v2/feeds", "owner", createBody)
	repeat := call(http.MethodPost, "/api/v2/feeds", "owner", createBody)
	if first.Code != 200 || repeat.Code != 200 || !strings.Contains(first.Body.String(), `"created":true`) || !strings.Contains(repeat.Body.String(), `"created":false`) {
		t.Fatalf("create = %d %s, repeat = %d %s", first.Code, first.Body.String(), repeat.Code, repeat.Body.String())
	}
	if response := call(http.MethodDelete, "/api/v2/feeds/11", "other", ""); response.Code != 404 {
		t.Fatalf("foreign delete status = %d", response.Code)
	}
	if response := call(http.MethodDelete, "/api/v2/feeds/11", "owner", ""); response.Code != 204 {
		t.Fatalf("delete status = %d: %s", response.Code, response.Body.String())
	}
	if response := call(http.MethodGet, "/api/v2/items/22", "owner", ""); response.Code != 404 {
		t.Fatalf("deleted feed item status = %d", response.Code)
	}
}
