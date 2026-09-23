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
	listItems  []library.Item
	page       library.Page
}

func (f *fakeLibraryService) ListFeeds(context.Context, int) ([]library.Feed, error) {
	return f.listFeeds, nil
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
func (f *fakeLibraryService) DeleteItem(ctx context.Context, userID, itemID int) error {
	if f.deleteItem != nil {
		return f.deleteItem(ctx, userID, itemID)
	}
	return nil
}
