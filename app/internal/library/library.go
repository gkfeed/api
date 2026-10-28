package library

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("library resource not found")

// MaxItemsPageLimit bounds ordinary item pages, matching the sync API's limit.
// The repository reads one additional item to determine whether a page follows.
const MaxItemsPageLimit = 500

type FeedType string

type Feed struct {
	ID     int
	Title  string
	Type   FeedType
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

// ItemSummary contains the fields needed to choose an item without loading its text.
type ItemSummary struct {
	ID     int
	FeedID int
	Title  string
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

type SummaryPage struct {
	Items      []ItemSummary
	NextCursor *int
}

// SyncPage is anchored to one visible change sequence and item ID ceiling.
type SyncPage struct {
	Items    []Item
	LastID   int
	MaxID    int
	Sequence int64
	HasMore  bool
}

type Change struct {
	Sequence int64
	ItemID   int
	Item     *Item
}

type ChangesPage struct {
	Changes  []Change
	Sequence int64
	HasMore  bool
}

type CreateFeedInput struct {
	Title string
	Type  FeedType
	URL   string
}

// CreateFeedsInput selects subscriptions for one source URL. Type and Types
// are mutually exclusive; nil Types means automatic selection is allowed.
type CreateFeedsInput struct {
	Title string
	Type  FeedType
	Types []FeedType
	URL   string
}

type FeedRepository interface {
	List(ctx context.Context, userID int) ([]Feed, error)
	Get(ctx context.Context, userID, feedID int) (Feed, error)
	Add(ctx context.Context, userID int, input CreateFeedInput) (AddFeedResult, error)
	AddBatch(ctx context.Context, userID int, inputs []CreateFeedInput) ([]AddFeedResult, error)
	Delete(ctx context.Context, userID, feedID int) error
}

type ItemRepository interface {
	Get(ctx context.Context, userID, itemID int) (ItemDetails, error)
	List(ctx context.Context, userID int) ([]Item, error)
	ListPage(ctx context.Context, userID int, cursor *int, limit int) ([]Item, error)
	ListSummaryPage(ctx context.Context, userID int, cursor *int, limit int) ([]ItemSummary, error)
	Delete(ctx context.Context, userID, itemID int) error
	SyncPage(ctx context.Context, userID, afterID, maxID, limit int) (SyncPage, error)
	Changes(ctx context.Context, userID int, afterSequence int64, limit int) (ChangesPage, error)
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
	if limit < 1 || limit > MaxItemsPageLimit || (cursor != nil && *cursor < 0) {
		return Page{}, errors.New("invalid item pagination")
	}

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

func (s *Service) ListItemSummariesPage(ctx context.Context, userID int, cursor *int, limit int) (SummaryPage, error) {
	items, err := s.items.ListSummaryPage(ctx, userID, cursor, limit+1)
	if err != nil {
		return SummaryPage{}, err
	}
	if items == nil {
		items = []ItemSummary{}
	}
	if len(items) <= limit {
		return SummaryPage{Items: items}, nil
	}
	nextCursor := items[limit-1].ID
	return SummaryPage{Items: items[:limit], NextCursor: &nextCursor}, nil
}

func (s *Service) SyncItemsPage(ctx context.Context, userID, afterID, maxID, limit int) (SyncPage, error) {
	return s.items.SyncPage(ctx, userID, afterID, maxID, limit)
}

func (s *Service) ItemChanges(ctx context.Context, userID int, afterSequence int64, limit int) (ChangesPage, error) {
	return s.items.Changes(ctx, userID, afterSequence, limit)
}
