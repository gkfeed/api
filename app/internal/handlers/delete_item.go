package handlers

import "net/http"

// @Summary      Delete item
// @Description  Permanently deletes an item owned by the authenticated user.
// @Tags         items
// @Param        id path int true "Item ID"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      204
// @Failure      400
// @Failure      401
// @Failure      404
// @Failure      500
// @Router       /api/v1/items/{id} [delete]
func (h *LibraryHandler) HandleDeleteItem(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteItem(r.Context(), user.ID, id); err != nil {
		writeLibraryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
