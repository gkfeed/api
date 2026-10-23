package handlers

import (
	"gkfeed/api/internal/library"
	"net/http"
)

// @Summary      List feed types
// @Description  Returns all feed type identifiers supported by the parser.
// @Tags         feeds
// @Produce      json
// @Success      200  {array}  string
// @Router       /api/v1/feed_types [get]
func HandleListFeedTypes(w http.ResponseWriter, _ *http.Request) {
	types := make([]string, 0)
	for _, option := range library.FeedTypeOptions() {
		types = append(types, string(option.Type))
	}
	writeJSON(w, types)
}
