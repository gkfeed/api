package handlers

import (
	"net/http"
	"strconv"
)

const defaultItemsLimit = 100

type getItemsResponse struct {
	Items      []itemDTO `json:"items"`
	NextCursor *int      `json:"next_cursor,omitempty"`
}

// @Summary      Get items
// @Description  Returns paginated items for the authenticated user. Supports cursor-based pagination.
// @Tags         items
// @Produce      json
// @Param        limit   query     int  false  "Items per page (default 100)"
// @Param        cursor  query     int  false  "Pagination cursor (item ID)"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200     {object}  getItemsResponse
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/get_items [get]
func (h *LibraryHandler) HandleGetItems(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	limit, cursor, ok := itemsPageParameters(w, r)
	if !ok {
		return
	}

	page, err := h.service.ListItemsPage(r.Context(), user.ID, cursor, limit)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}

	writeJSON(w, getItemsResponse{Items: itemDTOs(page.Items), NextCursor: page.NextCursor})
}

func itemsPageParameters(w http.ResponseWriter, r *http.Request) (int, *int, bool) {
	limit, ok := positiveQueryInt(w, r, "limit", defaultItemsLimit, false)
	if !ok {
		return 0, nil, false
	}

	cursor, ok := positiveQueryInt(w, r, "cursor", 0, true)
	if !ok {
		return 0, nil, false
	}
	if r.URL.Query().Get("cursor") == "" {
		return limit, nil, true
	}

	return limit, &cursor, true
}

func positiveQueryInt(w http.ResponseWriter, r *http.Request, name string, fallback int, allowZero bool) (int, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback, true
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 || (!allowZero && parsed == 0) {
		http.Error(w, "Invalid "+name, http.StatusBadRequest)
		return 0, false
	}

	return parsed, true
}
