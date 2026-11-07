package main

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// These response types describe the wire contract independently of handler DTOs.
type apiFeed struct {
	ID     int    `json:"id"`
	UserID int    `json:"userid"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	URL    string `json:"url"`
}

type apiItem struct {
	ID     int    `json:"id"`
	FeedID int    `json:"feed_id"`
	Title  string `json:"title"`
	Text   string `json:"text"`
	Date   string `json:"date"`
	Link   string `json:"link"`
}

type apiFeedMutation struct {
	Created bool    `json:"created"`
	Item    apiFeed `json:"item"`
}

type apiItemPage struct {
	Items      []apiItem `json:"items"`
	NextCursor *int      `json:"next_cursor"`
}

func decodeResponse[T any](t *testing.T, response *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if response.Code != status || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %v %s, want JSON status %d", response.Code, response.Header(), response.Body.String(), status)
	}
	var result T
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, response.Body.String())
	}
	return result
}

func execFixture(t *testing.T, database *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatalf("fixture SQL: %v", err)
	}
}

func itemIDs(items []apiItem) []int {
	ids := make([]int, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}
