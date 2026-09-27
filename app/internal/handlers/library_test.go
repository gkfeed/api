package handlers

import (
	"context"

	"gkfeed/api/internal/library"
)

type fakeLibraryState struct {
	addFeed    func(context.Context, int, library.CreateFeedInput) (library.AddFeedResult, error)
	deleteFeed func(context.Context, int, int) error
	deleteItem func(context.Context, int, int) error
	listFeeds  []library.Feed
	getFeed    func(context.Context, int, int) (library.Feed, error)
	listItems  []library.Item
	page       library.Page
	syncPage   func(context.Context, int, int, int, int) (library.SyncPage, error)
	changes    func(context.Context, int, int64, int) (library.ChangesPage, error)
}

type fakeFeedRepository struct{ state *fakeLibraryState }

func (r fakeFeedRepository) List(context.Context, int) ([]library.Feed, error) {
	return r.state.listFeeds, nil
}
func (r fakeFeedRepository) Add(ctx context.Context, userID int, input library.CreateFeedInput) (library.AddFeedResult, error) {
	return r.state.addFeed(ctx, userID, input)
}
func (r fakeFeedRepository) Delete(ctx context.Context, userID, feedID int) error {
	if r.state.deleteFeed != nil {
		return r.state.deleteFeed(ctx, userID, feedID)
	}
	return nil
}

type fakeItemRepository struct{ state *fakeLibraryState }

func (r fakeItemRepository) Get(context.Context, int, int) (library.ItemDetails, error) {
	return library.ItemDetails{}, nil
}
func (r fakeItemRepository) List(context.Context, int) ([]library.Item, error) {
	return r.state.listItems, nil
}
func (r fakeItemRepository) ListPage(context.Context, int, *int, int) ([]library.Item, error) {
	return r.state.page.Items, nil
}
func (r fakeItemRepository) Delete(ctx context.Context, userID, itemID int) error {
	if r.state.deleteItem != nil {
		return r.state.deleteItem(ctx, userID, itemID)
	}
	return nil
}

func newTestLibraryHandler(state *fakeLibraryState, resolver FeedResolver) *LibraryHandler {
	return NewLibraryHandler(fakeFeedRepository{state}, fakeItemRepository{state}, resolver)
}

func (r fakeFeedRepository) Get(ctx context.Context, userID, feedID int) (library.Feed, error) {
	if r.state.getFeed != nil {
		return r.state.getFeed(ctx, userID, feedID)
	}
	return library.Feed{}, library.ErrNotFound
}
func (r fakeItemRepository) SyncPage(ctx context.Context, userID, afterID, maxID, limit int) (library.SyncPage, error) {
	if r.state.syncPage != nil {
		return r.state.syncPage(ctx, userID, afterID, maxID, limit)
	}
	return library.SyncPage{Items: []library.Item{}}, nil
}
func (r fakeItemRepository) Changes(ctx context.Context, userID int, sequence int64, limit int) (library.ChangesPage, error) {
	if r.state.changes != nil {
		return r.state.changes(ctx, userID, sequence, limit)
	}
	return library.ChangesPage{Changes: []library.Change{}}, nil
}
