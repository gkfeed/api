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
// @Description  Returns an item and its parent feed for the authenticated user.
// @Tags         items
// @Produce      json
// @Param        id path int true "Item ID"
// @Success      200 {object} getItemResponse
// @Failure      400
// @Failure      401
// @Failure      404
// @Failure      500
// @Security     BasicAuth
// @Security     BearerAuth
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
