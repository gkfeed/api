package library

import (
	"context"
	"errors"
	"testing"
)

type fakeFeeds struct {
	list []Feed
	err  error
}

func (f fakeFeeds) List(context.Context, int) ([]Feed, error) { return f.list, f.err }
func (fakeFeeds) Add(context.Context, int, CreateFeedInput) (AddFeedResult, error) {
	return AddFeedResult{}, nil
}
func (fakeFeeds) Delete(context.Context, int, int) error { return nil }

type fakeItems struct {
	list      []Item
	page      []Item
	getErr    error
	deleteErr error
	gotLimit  int
}

func (f *fakeItems) Get(context.Context, int, int) (ItemDetails, error) {
	return ItemDetails{}, f.getErr
}
func (f *fakeItems) List(context.Context, int) ([]Item, error) { return f.list, nil }
func (f *fakeItems) ListPage(_ context.Context, _ int, _ *int, limit int) ([]Item, error) {
	f.gotLimit = limit
	return f.page, nil
}
func (f *fakeItems) Delete(context.Context, int, int) error { return f.deleteErr }

func TestServiceShapesPagination(t *testing.T) {
	items := &fakeItems{page: []Item{{ID: 3}, {ID: 2}, {ID: 1}}}
	service := NewService(fakeFeeds{}, items)
	page, err := service.ListItemsPage(t.Context(), 7, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if items.gotLimit != 3 || len(page.Items) != 2 || page.NextCursor == nil || *page.NextCursor != 2 {
		t.Fatalf("page = %#v, repository limit = %d", page, items.gotLimit)
	}
}

func TestServiceNormalizesEmptyCollections(t *testing.T) {
	service := NewService(fakeFeeds{}, &fakeItems{})
	feeds, err := service.ListFeeds(t.Context(), 1)
	if err != nil || feeds == nil {
		t.Fatalf("feeds = %#v, err = %v", feeds, err)
	}
	items, err := service.ListItems(t.Context(), 1)
	if err != nil || items == nil {
		t.Fatalf("items = %#v, err = %v", items, err)
	}
	page, err := service.ListItemsPage(t.Context(), 1, nil, 10)
	if err != nil || page.Items == nil {
		t.Fatalf("page = %#v, err = %v", page, err)
	}
}

func TestServicePropagatesRepositoryErrors(t *testing.T) {
	want := errors.New("storage failed")
	items := &fakeItems{getErr: want, deleteErr: ErrNotFound}
	service := NewService(fakeFeeds{err: want}, items)
	if _, err := service.ListFeeds(t.Context(), 1); !errors.Is(err, want) {
		t.Fatalf("ListFeeds error = %v", err)
	}
	if _, err := service.GetItem(t.Context(), 1, 2); !errors.Is(err, want) {
		t.Fatalf("GetItem error = %v", err)
	}
	if err := service.DeleteItem(t.Context(), 1, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteItem error = %v", err)
	}
}
