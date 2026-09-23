package handlers

import (
	"net/http"
)

// @Summary      Delete feed
// @Description  Deletes a feed by ID. Only the feed owner can delete it.
// @Tags         feeds
// @Produce      json
// @Param        id   query     int  true  "Feed ID"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      204
// @Failure      400
// @Failure      401
// @Failure      404
// @Failure      500
// @Router       /api/v1/delete [delete]
func (h *LibraryHandler) HandleDeleteFeed(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	id, ok := queryID(w, r)
	if !ok {
		return
	}

	if err := h.service.DeleteFeed(r.Context(), user.ID, id); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
