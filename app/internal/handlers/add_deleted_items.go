package handlers

import (
	"net/http"
)

// @Summary      Deprecated item hide endpoint
// @Description  Gone. Use DELETE /api/v1/items/{id} instead.
// @Tags         items
// @Produce      json,plain
// @Security     BasicAuth
// @Security     BearerAuth
// @Security     BrowserSession
// @Failure      410  {object} object{error=string,replacement=string}
// @Failure      401
// @Failure      403 {string} string "Browser-session Origin policy rejected the request; response is text/plain"
// @Router       /api/v1/add_deleted_items [post]
func (h *LibraryHandler) HandleAddDeletedItems(w http.ResponseWriter, r *http.Request) {
	_, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	writeJSON(w, struct {
		Error       string `json:"error"`
		Replacement string `json:"replacement"`
	}{Error: "endpoint_gone", Replacement: "DELETE /api/v1/items/{id}"})
}
