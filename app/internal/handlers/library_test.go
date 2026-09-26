package handlers

import (
	"context"

	"gkfeed/api/internal/library"
)

type fakeLibraryService struct {
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

func (f *fakeLibraryService) ListFeeds(context.Context, int) ([]library.Feed, error) {
	return f.listFeeds, nil
}
func (f *fakeLibraryService) GetFeed(ctx context.Context, userID, feedID int) (library.Feed, error) {
	if f.getFeed != nil {
		return f.getFeed(ctx, userID, feedID)
	}
	return library.Feed{}, library.ErrNotFound
}
func (f *fakeLibraryService) AddFeed(ctx context.Context, userID int, input library.CreateFeedInput) (library.AddFeedResult, error) {
	return f.addFeed(ctx, userID, input)
}
func (f *fakeLibraryService) DeleteFeed(ctx context.Context, userID, feedID int) error {
	if f.deleteFeed != nil {
		return f.deleteFeed(ctx, userID, feedID)
	}
	return nil
}
func (f *fakeLibraryService) GetItem(context.Context, int, int) (library.ItemDetails, error) {
	return library.ItemDetails{}, nil
}
func (f *fakeLibraryService) ListItems(context.Context, int) ([]library.Item, error) {
	return f.listItems, nil
}
func (f *fakeLibraryService) ListItemsPage(context.Context, int, *int, int) (library.Page, error) {
	return f.page, nil
}
func (f *fakeLibraryService) SyncItemsPage(ctx context.Context, userID, afterID, maxID, limit int) (library.SyncPage, error) {
	if f.syncPage != nil {
		return f.syncPage(ctx, userID, afterID, maxID, limit)
	}
	return library.SyncPage{Items: []library.Item{}}, nil
}
func (f *fakeLibraryService) ItemChanges(ctx context.Context, userID int, sequence int64, limit int) (library.ChangesPage, error) {
	if f.changes != nil {
		return f.changes(ctx, userID, sequence, limit)
	}
	return library.ChangesPage{Changes: []library.Change{}}, nil
}
func (f *fakeLibraryService) DeleteItem(ctx context.Context, userID, itemID int) error {
	if f.deleteItem != nil {
		return f.deleteItem(ctx, userID, itemID)
	}
	return nil
}
