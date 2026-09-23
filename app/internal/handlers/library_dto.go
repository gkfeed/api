package handlers

import (
	"time"

	"gkfeed/api/internal/library"
)

type createFeedDTO struct {
	Title string `json:"title"`
	Type  string `json:"type"`
	URL   string `json:"url"`
}

type feedDTO struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
	URL    string `json:"url"`
	UserID int    `json:"userid"`
}

type itemDTO struct {
	ID     int       `json:"id"`
	FeedID int       `json:"feed_id"`
	Title  string    `json:"title"`
	Text   string    `json:"text"`
	Date   time.Time `json:"date"`
	Link   string    `json:"link"`
}

func toFeedDTO(feed library.Feed) feedDTO {
	return feedDTO{ID: feed.ID, Title: feed.Title, Type: feed.Type, URL: feed.URL, UserID: feed.UserID}
}

func feedDTOs(feeds []library.Feed) []feedDTO {
	result := make([]feedDTO, 0, len(feeds))
	for _, feed := range feeds {
		result = append(result, toFeedDTO(feed))
	}
	return result
}

func toItemDTO(item library.Item) itemDTO {
	return itemDTO{ID: item.ID, FeedID: item.FeedID, Title: item.Title, Text: item.Text, Date: item.Date, Link: item.Link}
}

func itemDTOs(items []library.Item) []itemDTO {
	result := make([]itemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, toItemDTO(item))
	}
	return result
}
