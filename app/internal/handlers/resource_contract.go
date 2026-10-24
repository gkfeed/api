package handlers

import "net/http"

// ResourceError is the error body for v2 feed reads/deletes and item reads/deletes.
type ResourceError struct {
	Error   string `json:"error" validate:"required" enums:"invalid_id,unauthorized,not_found,internal_error,method_not_allowed" example:"not_found"`
	Message string `json:"message" validate:"required" example:"Resource not found"`
}

// @Summary      List feeds
// @Description  Returns the authenticated user's feeds. An empty list is returned as [].
// @Tags         feeds
// @Produce      json
// @Security     BasicAuth
// @Security     BearerAuth
// @Security     BrowserSession
// @Success      200 {array} feedDTO
// @Failure      401 {object} ResourceError "unauthorized: missing or invalid credentials"
// @Failure      405 {object} ResourceError "method_not_allowed: use GET or POST on this path"
// @Failure      500 {object} ResourceError "internal_error: request failed"
// @Header       401 {string} WWW-Authenticate "Basic authentication challenge for realm Restricted"
// @Header       405 {string} Allow "GET, POST"
// @Router       /api/v2/feeds [get]
func (h *LibraryHandler) HandleListResourceFeeds(w http.ResponseWriter, r *http.Request) {
	h.HandleListOfFeeds(w, r)
}

// @Summary      Delete item
// @Description  Permanently deletes an owned item. Missing, foreign, and previously deleted IDs all return 404. Authentication is checked before the ID.
// @Tags         items
// @Produce      json,plain
// @Param        id path int true "Positive item ID" minimum(1)
// @Security     BasicAuth
// @Security     BearerAuth
// @Security     BrowserSession
// @Success      204 "Deleted; response body is empty"
// @Failure      400 {object} ResourceError "invalid_id: ID must be a positive integer within the server's integer range"
// @Failure      401 {object} ResourceError "unauthorized: missing or invalid credentials"
// @Failure      403 {string} string "Browser-session Origin policy rejected the request; response is text/plain"
// @Failure      404 {object} ResourceError "not_found: item is missing or belongs to another user"
// @Failure      405 {object} ResourceError "method_not_allowed: use GET or DELETE on this path"
// @Failure      500 {object} ResourceError "internal_error: request failed"
// @Header       401 {string} WWW-Authenticate "Basic authentication challenge for realm Restricted"
// @Header       405 {string} Allow "GET, DELETE"
// @Router       /api/v2/items/{id} [delete]
func (h *LibraryHandler) HandleDeleteResourceItem(w http.ResponseWriter, r *http.Request) {
	h.HandleDeleteItem(w, r)
}
