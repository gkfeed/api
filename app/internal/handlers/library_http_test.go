package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/models"
)

func TestDeleteItemHTTPContract(t *testing.T) {
	for _, test := range []struct {
		name       string
		path       string
		serviceErr error
		wantStatus int
	}{
		{"success", "/items/42", nil, http.StatusNoContent},
		{"not numeric", "/items/nope", nil, http.StatusBadRequest},
		{"zero", "/items/0", nil, http.StatusBadRequest},
		{"negative", "/items/-1", nil, http.StatusBadRequest},
		{"not found", "/items/42", library.ErrNotFound, http.StatusNotFound},
		{"unexpected", "/items/42", errors.New("broken"), http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeLibraryService{deleteItem: func(context.Context, int, int) error { return test.serviceErr }}
			handler := NewLibraryHandler(service, fakeResolver{})
			router := mux.NewRouter()
			router.HandleFunc("/items/{id}", handler.HandleDeleteItem).Methods(http.MethodDelete)
			request := httptest.NewRequest(http.MethodDelete, test.path, nil)
			request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: 7}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestDeleteFeedHTTPContract(t *testing.T) {
	for _, test := range []struct {
		name       string
		query      string
		serviceErr error
		wantStatus int
	}{
		{"success", "?id=42", nil, http.StatusNoContent},
		{"invalid", "?id=0", nil, http.StatusBadRequest},
		{"not found", "?id=42", library.ErrNotFound, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeLibraryService{deleteFeed: func(context.Context, int, int) error { return test.serviceErr }}
			handler := NewLibraryHandler(service, fakeResolver{})
			request := httptest.NewRequest(http.MethodDelete, "/delete"+test.query, nil)
			request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: 7}))
			response := httptest.NewRecorder()
			handler.HandleDeleteFeed(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestDeprecatedDeletedItemsEndpoint(t *testing.T) {
	handler := NewLibraryHandler(&fakeLibraryService{}, fakeResolver{})
	unauthenticated := httptest.NewRecorder()
	handler.HandleAddDeletedItems(unauthenticated, httptest.NewRequest(http.MethodPost, "/add_deleted_items", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/add_deleted_items", nil)
	request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: 1}))
	response := httptest.NewRecorder()
	handler.HandleAddDeletedItems(response, request)
	if response.Code != http.StatusGone || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status = %d, content type = %q", response.Code, response.Header().Get("Content-Type"))
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "endpoint_gone" || body["replacement"] != "DELETE /api/v1/items/{id}" {
		t.Fatalf("body = %#v", body)
	}
}

func TestLibraryHandlersEncodeEmptyArrays(t *testing.T) {
	handler := NewLibraryHandler(&fakeLibraryService{}, fakeResolver{})
	for _, invoke := range []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
	}{
		{"feeds", handler.HandleListOfFeeds},
		{"items", handler.HandleGetItems},
	} {
		t.Run(invoke.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: 1}))
			response := httptest.NewRecorder()
			invoke.fn(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d", response.Code)
			}
			if invoke.name == "feeds" && response.Body.String() != "[]\n" {
				t.Fatalf("body = %q", response.Body.String())
			}
			if invoke.name == "items" && response.Body.String() != "{\"items\":[]}\n" {
				t.Fatalf("body = %q", response.Body.String())
			}
		})
	}
}
