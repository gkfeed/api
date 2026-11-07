package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/models"
)

func TestHandleAddFeedRejectsInvalidPayloadBeforeStorage(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{"null", `null`},
		{"empty object", `{}`},
		{"missing title", `{"type":"rss","url":"https://example.com"}`},
		{"missing type", `{"title":"Feed","url":"https://example.com"}`},
		{"missing url", `{"title":"Feed","type":"rss"}`},
		{"empty title", `{"title":"","type":"rss","url":"https://example.com"}`},
		{"empty type", `{"title":"Feed","type":"","url":"https://example.com"}`},
		{"empty url", `{"title":"Feed","type":"rss","url":""}`},
		{"blank title", `{"title":" \t\n\u00a0","type":"rss","url":"https://example.com"}`},
		{"blank type", `{"title":"Feed","type":" \t\n\u00a0","url":"https://example.com"}`},
		{"blank url", `{"title":"Feed","type":"rss","url":" \t\n\u00a0"}`},
		{"null title", `{"title":null,"type":"rss","url":"https://example.com"}`},
		{"null type", `{"title":"Feed","type":null,"url":"https://example.com"}`},
		{"null url", `{"title":"Feed","type":"rss","url":null}`},
		{"array", `[]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &fakeLibraryState{addFeed: func(context.Context, int, library.CreateFeedInput) (library.AddFeedResult, error) {
				t.Fatal("repository Add called for invalid payload")
				return library.AddFeedResult{}, nil
			}}
			handler := newTestLibraryHandler(state, nil)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/add", strings.NewReader(test.body))
			request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: 7}))
			response := httptest.NewRecorder()
			handler.HandleAddFeed(response, request)
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHandleAddFeedPreservesLegacySuccessContract(t *testing.T) {
	const url = "https://WWW.YouTube.com:443/@Example/streams/?si=share#top"
	wantInput := library.CreateFeedInput{Title: "My feed", Type: "author.today", URL: url}
	stored := library.Feed{ID: 42, UserID: 7, Title: "Stored title", Type: wantInput.Type, URL: url}
	state := &fakeLibraryState{addFeed: func(_ context.Context, userID int, input library.CreateFeedInput) (library.AddFeedResult, error) {
		if userID != 7 || input != wantInput {
			t.Fatalf("Add(%d, %#v), want Add(7, %#v)", userID, input, wantInput)
		}
		return library.AddFeedResult{Created: false, Feed: stored}, nil
	}}
	handler := newTestLibraryHandler(state, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/add", strings.NewReader(
		`{"title":" \tMy feed\u00a0","type":" author.today ","url":" \n`+url+`\t"}`))
	request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: 7}))
	response := httptest.NewRecorder()
	handler.HandleAddFeed(response, request)
	var result feedMutationResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if result.Created || result.Item != toFeedDTO(stored) {
		t.Fatalf("unexpected legacy response: %#v", result)
	}
}
