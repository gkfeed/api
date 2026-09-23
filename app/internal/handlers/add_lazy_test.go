package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/models"
)

type fakeResolver struct{ input library.CreateFeedInput }

func (f fakeResolver) Resolve(context.Context, string) (library.CreateFeedInput, error) {
	return f.input, nil
}

func TestHandleAddFeedLazy(t *testing.T) {
	service := &fakeLibraryService{addFeed: func(_ context.Context, userID int, input library.CreateFeedInput) (library.AddFeedResult, error) {
		return library.AddFeedResult{Feed: library.Feed{ID: 1, UserID: userID, Title: input.Title, Type: input.Type, URL: input.URL}, Created: true}, nil
	}}
	handler := NewLibraryHandler(service, fakeResolver{library.CreateFeedInput{Title: "Test Feed", Type: "rss", URL: "https://example.com"}})
	request := httptest.NewRequest(http.MethodPost, "/add-lazy", bytes.NewBufferString(`{"url":"https://example.com"}`))
	request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: 1}))
	response := httptest.NewRecorder()
	handler.HandleAddFeedLazy(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var body feedMutationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Created || body.Item.ID != 1 || body.Item.UserID != 1 {
		t.Fatalf("response = %#v", body)
	}
}

func TestHandleAddFeedLazyReturnsServerErrorWhenInsertFails(t *testing.T) {
	service := &fakeLibraryService{addFeed: func(context.Context, int, library.CreateFeedInput) (library.AddFeedResult, error) {
		return library.AddFeedResult{}, errors.New("database unavailable")
	}}
	handler := NewLibraryHandler(service, fakeResolver{library.CreateFeedInput{URL: "https://example.com"}})
	request := httptest.NewRequest(http.MethodPost, "/add-lazy", bytes.NewBufferString(`{"url":"https://example.com"}`))
	request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: 1}))
	response := httptest.NewRecorder()
	handler.HandleAddFeedLazy(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}
