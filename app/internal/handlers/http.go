package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/models"
)

const maxRequestBodySize = 1 << 20

type LibraryService interface {
	ListFeeds(context.Context, int) ([]library.Feed, error)
	AddFeed(context.Context, int, library.CreateFeedInput) (library.AddFeedResult, error)
	DeleteFeed(context.Context, int, int) error
	GetItem(context.Context, int, int) (library.ItemDetails, error)
	ListItems(context.Context, int) ([]library.Item, error)
	ListItemsPage(context.Context, int, *int, int) (library.Page, error)
	DeleteItem(context.Context, int, int) error
}

type FeedResolver interface {
	Resolve(context.Context, string) (library.CreateFeedInput, error)
}

type LibraryHandler struct {
	service  LibraryService
	resolver FeedResolver
}

func NewLibraryHandler(service LibraryService, resolver FeedResolver) *LibraryHandler {
	return &LibraryHandler{service: service, resolver: resolver}
}

func authenticatedUser(w http.ResponseWriter, r *http.Request) (models.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "No authentication provided", http.StatusUnauthorized)
		return models.User{}, false
	}

	return user, true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func writeInternalServerError(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func writeLibraryError(w http.ResponseWriter, err error) {
	if errors.Is(err, library.ErrNotFound) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	writeInternalServerError(w, err)
}

func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func queryID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, "Invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "Request body must contain one JSON object", http.StatusBadRequest)
		return false
	}
	return true
}
