package handlers

import "net/http"

// @Summary      Get feed by ID
// @Description  Returns an owned feed with the same fields as a feed list entry. Missing and foreign IDs both return 404. Authentication is checked before the ID.
// @Tags         feeds
// @Produce      json
// @Param        id path int true "Positive feed ID" minimum(1)
// @Success      200 {object} feedDTO
// @Failure      400 {object} ResourceError "invalid_id: ID must be a positive integer within the server's integer range"
// @Failure      401 {object} ResourceError "unauthorized: missing or invalid credentials"
// @Failure      404 {object} ResourceError "not_found: feed is missing or belongs to another user"
// @Failure      405 {object} ResourceError "method_not_allowed: use GET or DELETE on this path"
// @Failure      500 {object} ResourceError "internal_error: request failed"
// @Header       401 {string} WWW-Authenticate "Basic authentication challenge for realm Restricted"
// @Header       405 {string} Allow "GET, DELETE"
// @Security     BasicAuth
// @Security     BearerAuth
// @Security     BrowserSession
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
