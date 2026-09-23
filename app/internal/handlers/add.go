package handlers

import (
	"net/http"
	"strings"

	"gkfeed/api/internal/library"
)

// @Summary      Add feed
// @Description  Adds a new RSS/YouTube/TikTok feed for the authenticated user.
// @Tags         feeds
// @Accept       json
// @Produce      json
// @Param        feed  body      createFeedDTO  true  "Feed to add (title, type, url)"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200   {object}  feedMutationResponse
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/add [post]
func (h *LibraryHandler) HandleAddFeed(w http.ResponseWriter, r *http.Request) {
	h.addFeed(w, r, false)
}

// @Summary      Create feed
// @Description  Creates a feed with explicit title, type, and URL. Repeated creation returns the existing feed.
// @Tags         feeds
// @Accept       json
// @Produce      json
// @Param        feed body createFeedDTO true "Feed to create (title, type, url required)"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200 {object} feedMutationResponse
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v2/feeds [post]
func (h *LibraryHandler) HandleCreateFeed(w http.ResponseWriter, r *http.Request) {
	h.addFeed(w, r, true)
}

func (h *LibraryHandler) addFeed(w http.ResponseWriter, r *http.Request, requireFields bool) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	var feedInput createFeedDTO
	if !decodeJSON(w, r, &feedInput) {
		return
	}
	if requireFields && (strings.TrimSpace(feedInput.Title) == "" || strings.TrimSpace(feedInput.Type) == "" || strings.TrimSpace(feedInput.URL) == "") {
		http.Error(w, "title, type, and url are required", http.StatusBadRequest)
		return
	}

	feed, err := h.service.AddFeed(r.Context(), user.ID, library.CreateFeedInput{
		Title: feedInput.Title, Type: feedInput.Type, URL: feedInput.URL,
	})
	if err != nil {
		writeInternalServerError(w, err)
		return
	}

	writeJSON(w, feedMutationResponse{Created: feed.Created, Item: toFeedDTO(feed.Feed)})
}
