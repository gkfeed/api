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
	listItems  []library.Item
	page       library.Page
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
