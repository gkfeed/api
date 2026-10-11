package handlers

import (
	"net/http"
)

type feedMutationResponse struct {
	Created bool    `json:"created"`
	Item    feedDTO `json:"item"`
}

// @Summary      Add feed by URL
// @Description  Creates a feed by parsing the URL and detecting the source type (YouTube, TikTok, Rezka, etc.).
// @Tags         feeds
// @Accept       json
// @Produce      json
// @Param        feed  body      object{url=string}  true  "URL to create feed from"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200   {object}  feedMutationResponse
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/add_lazy [post]
func (h *LibraryHandler) HandleAddFeedByURL(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	var input struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}

	feedInput, err := h.resolver.Resolve(r.Context(), input.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	feed, err := h.feeds.Add(r.Context(), user.ID, feedInput)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}

	writeJSON(w, feedMutationResponse{Created: feed.Created, Item: toFeedDTO(feed.Feed)})
}
