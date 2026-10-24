package handlers

import (
	"net/http"
)

// @Summary      List feeds
// @Description  Returns all feeds for the authenticated user.
// @Tags         feeds
// @Produce      json
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200  {array}   object
// @Failure      401
// @Failure      500
// @Router       /api/v1/list [get]
func (h *LibraryHandler) HandleListOfFeeds(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	feeds, err := h.service.ListFeeds(r.Context(), user.ID)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}
	writeJSON(w, feedDTOs(feeds))
}
