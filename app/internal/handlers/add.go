package handlers

import (
	"net/http"

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
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	var feedInput createFeedDTO
	if !decodeJSON(w, r, &feedInput) {
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
