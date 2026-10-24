package handlers

import (
	"net/http"
)

type getItemResponse struct {
	Item itemDTO `json:"item"`
	Feed feedDTO `json:"feed"`
}

// @Summary      Get item by ID
// @Description  Returns a single item with its parent feed for the authenticated user.
// @Tags         items
// @Produce      json
// @Param        id   query     int  true  "Item ID"
// @Success      200  {object}  object{item=object,feed=object}
// @Failure      400
// @Failure      401
// @Failure      404
// @Security     BasicAuth
// @Security     BearerAuth
// @Router       /api/v1/item [get]
func (h *LibraryHandler) HandleGetItemByID(w http.ResponseWriter, r *http.Request) {
	h.getItem(w, r, queryID)
}

// @Summary      Get item by ID
// @Description  Returns an owned item and its parent feed. Missing and foreign IDs both return 404. Authentication is checked before the ID.
// @Tags         items
// @Produce      json
// @Param        id path int true "Positive item ID" minimum(1)
// @Success      200 {object} getItemResponse
// @Failure      400 {object} ResourceError "invalid_id: ID must be a positive integer within the server's integer range"
// @Failure      401 {object} ResourceError "unauthorized: missing or invalid credentials"
// @Failure      404 {object} ResourceError "not_found: item is missing or belongs to another user"
// @Failure      405 {object} ResourceError "method_not_allowed: use GET or DELETE on this path"
// @Failure      500 {object} ResourceError "internal_error: request failed"
// @Header       401 {string} WWW-Authenticate "Basic authentication challenge for realm Restricted"
// @Header       405 {string} Allow "GET, DELETE"
// @Security     BasicAuth
// @Security     BearerAuth
// @Security     BrowserSession
// @Router       /api/v2/items/{id} [get]
func (h *LibraryHandler) HandleGetItemByPathID(w http.ResponseWriter, r *http.Request) {
	h.getItem(w, r, pathID)
}

func (h *LibraryHandler) getItem(w http.ResponseWriter, r *http.Request, readID func(http.ResponseWriter, *http.Request) (int, bool)) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	itemID, ok := readID(w, r)
	if !ok {
		return
	}

	details, err := h.items.Get(r.Context(), user.ID, itemID)
	if err != nil {
		writeLibraryError(w, err)
		return
	}

	writeJSON(w, getItemResponse{Item: toItemDTO(details.Item), Feed: toFeedDTO(details.Feed)})
}
