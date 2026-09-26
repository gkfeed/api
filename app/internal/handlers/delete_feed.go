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
	h.deleteFeed(w, r, queryID)
}

// @Summary      Delete feed by ID
// @Description  Deletes a feed and all its items. Only the owner can delete it.
// @Tags         feeds
// @Param        id path int true "Feed ID"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      204
// @Failure      400
// @Failure      401
// @Failure      404
// @Failure      500
// @Router       /api/v2/feeds/{id} [delete]
func (h *LibraryHandler) HandleDeleteFeedByID(w http.ResponseWriter, r *http.Request) {
	h.deleteFeed(w, r, pathID)
}

func (h *LibraryHandler) deleteFeed(w http.ResponseWriter, r *http.Request, readID func(http.ResponseWriter, *http.Request) (int, bool)) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	id, ok := readID(w, r)
	if !ok {
		return
	}

	if err := h.service.DeleteFeed(r.Context(), user.ID, id); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
