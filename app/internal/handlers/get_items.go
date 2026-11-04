package handlers

import (
	"net/http"
	"strconv"

	"gkfeed/api/internal/library"
)

const defaultItemsLimit = 100

type getItemsResponse struct {
	Items      []itemDTO `json:"items"`
	NextCursor *int      `json:"next_cursor,omitempty"`
}

type getItemSummariesResponse struct {
	Items      []itemSummaryDTO `json:"items"`
	NextCursor *int             `json:"next_cursor,omitempty"`
}

// @Summary      Get items
// @Description  Returns paginated items for the authenticated user. Supports cursor-based pagination. The default full view includes text; view=summary omits text and does not load it from storage. Fetch a full item separately with GET /api/v2/items/{id} or GET /api/v1/item?id={id}.
// @Tags         items
// @Produce      json
// @Param        limit   query     int  false  "Items per page" default(100) minimum(1) maximum(500)
// @Param        cursor  query     int  false  "Pagination cursor (item ID)"
// @Param        view    query     string false "Item representation (summary omits text)" Enums(full, summary) default(full)
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200     {object}  getItemsResponse "Full items by default; summary items omit text"
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/get_items [get]
// @Router       /api/v2/items [get]
func (h *LibraryHandler) HandleGetItems(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	limit, cursor, ok := itemsPageParameters(w, r)
	if !ok {
		return
	}

	view := "full"
	if values, present := r.URL.Query()["view"]; present {
		if len(values) != 1 || (values[0] != "full" && values[0] != "summary") {
			http.Error(w, "Invalid view", http.StatusBadRequest)
			return
		}
		view = values[0]
	}
	if view == "summary" {
		page, err := h.service.ListItemSummariesPage(r.Context(), user.ID, cursor, limit)
		if err != nil {
			writeInternalServerError(w, err)
			return
		}
		writeJSON(w, getItemSummariesResponse{Items: itemSummaryDTOs(page.Items), NextCursor: page.NextCursor})
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
	if limit > library.MaxItemsPageLimit {
		http.Error(w, "Invalid limit", http.StatusBadRequest)
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
