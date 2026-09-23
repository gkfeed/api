package library

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("library resource not found")

type Feed struct {
	ID     int
	Title  string
	Type   string
	URL    string
	UserID int
}

type AddFeedResult struct {
	Feed
	Created bool
}

type Item struct {
	ID     int
	FeedID int
	Title  string
	Text   string
	Date   time.Time
	Link   string
}

type ItemDetails struct {
	Item Item
	Feed Feed
}

type Page struct {
	Items      []Item
	NextCursor *int
}

type CreateFeedInput struct {
	Title string
	Type  string
	URL   string
}

type FeedRepository interface {
	List(ctx context.Context, userID int) ([]Feed, error)
	Add(ctx context.Context, userID int, input CreateFeedInput) (AddFeedResult, error)
	Delete(ctx context.Context, userID, feedID int) error
}

type ItemRepository interface {
	Get(ctx context.Context, userID, itemID int) (ItemDetails, error)
	List(ctx context.Context, userID int) ([]Item, error)
	ListPage(ctx context.Context, userID int, cursor *int, limit int) ([]Item, error)
	Delete(ctx context.Context, userID, itemID int) error
}

type Service struct {
	feeds FeedRepository
	items ItemRepository
}

func NewService(feeds FeedRepository, items ItemRepository) *Service {
	return &Service{feeds: feeds, items: items}
}

func (s *Service) ListFeeds(ctx context.Context, userID int) ([]Feed, error) {
	feeds, err := s.feeds.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if feeds == nil {
		feeds = []Feed{}
	}
	return feeds, nil
}

func (s *Service) AddFeed(ctx context.Context, userID int, input CreateFeedInput) (AddFeedResult, error) {
	return s.feeds.Add(ctx, userID, input)
}

func (s *Service) DeleteFeed(ctx context.Context, userID, feedID int) error {
	return s.feeds.Delete(ctx, userID, feedID)
}

func (s *Service) GetItem(ctx context.Context, userID, itemID int) (ItemDetails, error) {
	return s.items.Get(ctx, userID, itemID)
}

func (s *Service) ListItems(ctx context.Context, userID int) ([]Item, error) {
	items, err := s.items.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []Item{}
	}
	return items, nil
}

func (s *Service) ListItemsPage(ctx context.Context, userID int, cursor *int, limit int) (Page, error) {
	items, err := s.items.ListPage(ctx, userID, cursor, limit+1)
	if err != nil {
		return Page{}, err
	}
	if items == nil {
		items = []Item{}
	}
	if len(items) <= limit {
		return Page{Items: items}, nil
	}

	nextCursor := items[limit-1].ID
	return Page{Items: items[:limit], NextCursor: &nextCursor}, nil
}

func (s *Service) DeleteItem(ctx context.Context, userID, itemID int) error {
	return s.items.Delete(ctx, userID, itemID)
}
