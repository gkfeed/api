package handlers

import (
	"net/http"
)

// @Summary      Delete feed
// @Description  Deletes a feed by ID. Only the feed owner can delete it.
// @Tags         feeds
// @Produce      json,plain
// @Param        id   query     int  true  "Feed ID"
// @Security     BasicAuth
// @Security     BearerAuth
// @Security     BrowserSession
// @Success      204
// @Failure      400
// @Failure      401
// @Failure      403 {string} string "Browser-session Origin policy rejected the request; response is text/plain"
// @Failure      404
// @Failure      500
// @Router       /api/v1/delete [delete]
func (h *LibraryHandler) HandleDeleteFeed(w http.ResponseWriter, r *http.Request) {
	h.deleteFeed(w, r, queryID)
}

// @Summary      Delete feed by ID
// @Description  Deletes an owned feed and all its items. Missing, foreign, and previously deleted IDs all return 404. Authentication is checked before the ID.
// @Tags         feeds
// @Produce      json,plain
// @Param        id path int true "Positive feed ID" minimum(1)
// @Security     BasicAuth
// @Security     BearerAuth
// @Security     BrowserSession
// @Success      204 "Deleted; response body is empty"
// @Failure      400 {object} ResourceError "invalid_id: ID must be a positive integer within the server's integer range"
// @Failure      401 {object} ResourceError "unauthorized: missing or invalid credentials"
// @Failure      403 {string} string "Browser-session Origin policy rejected the request; response is text/plain"
// @Failure      404 {object} ResourceError "not_found: feed is missing or belongs to another user"
// @Failure      405 {object} ResourceError "method_not_allowed: use GET or DELETE on this path"
// @Failure      500 {object} ResourceError "internal_error: request failed"
// @Header       401 {string} WWW-Authenticate "Basic authentication challenge for realm Restricted"
// @Header       405 {string} Allow "GET, DELETE"
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

	if err := h.feeds.Delete(r.Context(), user.ID, id); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
