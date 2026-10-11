package handlers

import "net/http"

// @Summary      Get feed by ID
// @Description  Returns a feed owned by the authenticated user.
// @Tags         feeds
// @Produce      json
// @Param        id path int true "Feed ID"
// @Success      200 {object} feedDTO
// @Failure      400
// @Failure      401
// @Failure      404
// @Failure      500
// @Security     BasicAuth
// @Security     BearerAuth
// @Router       /api/v2/feeds/{id} [get]
func (h *LibraryHandler) HandleGetFeed(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	feed, err := h.feeds.Get(r.Context(), user.ID, id)
	if err != nil {
		writeLibraryError(w, err)
		return
	}
	writeJSON(w, toFeedDTO(feed))
}
