package handlers

import (
	"encoding/xml"
	"net/http"
	"time"

	"gkfeed/api/internal/services/rss"
)

// @Summary      Get RSS feed
// @Description  Returns all user items as an RSS 2.0 XML feed.
// @Tags         feeds
// @Produce      application/rss+xml
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200
// @Failure      401
// @Failure      500
// @Router       /api/v1/feed [get]
func (h *LibraryHandler) HandleRSSFeed(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	items, err := h.service.ListItems(r.Context(), user.ID)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}

	rssItems := make([]rss.Item, 0, len(items))
	for _, item := range items {
		rssItems = append(rssItems, rss.Item{
			ID:          item.ID,
			Title:       item.Title,
			Link:        item.Link,
			Description: item.Text,
			PubDate:     item.Date.Format(time.RFC1123),
		})
	}
	rssFeed := rss.GenerateRSS(rssItems)

	w.Header().Set("Content-Type", "application/rss+xml")
	if err := xml.NewEncoder(w).Encode(rssFeed); err != nil {
		http.Error(w, "Failed to encode RSS feed", http.StatusInternalServerError)
		return
	}
}
