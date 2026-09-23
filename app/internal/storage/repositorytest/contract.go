package repositorytest

import (
	"errors"
	"sync"
	"testing"

	"gkfeed/api/internal/library"
)

type Fixture struct {
	Feeds       library.FeedRepository
	Items       library.ItemRepository
	InsertItems func(t *testing.T, feedID, count int) []int
}

type Factory func(t *testing.T) Fixture

// Run exercises behavior shared by every library storage adapter.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	RunFeedCreation(t, factory)
	t.Run("ownership ordering pagination and mutations", func(t *testing.T) {
		fixture := factory(t)
		feeds, items := fixture.Feeds, fixture.Items
		ctx := t.Context()
		ownerFeed, err := feeds.Add(ctx, 1, library.CreateFeedInput{Title: "owner", Type: "rss", URL: "https://one"})
		if err != nil || ownerFeed.ID == 0 || ownerFeed.UserID != 1 {
			t.Fatalf("Add() = %#v, %v", ownerFeed, err)
		}
		otherFeed, err := feeds.Add(ctx, 2, library.CreateFeedInput{Title: "other"})
		if err != nil {
			t.Fatal(err)
		}
		ownerFeeds, err := feeds.List(ctx, 1)
		if err != nil || len(ownerFeeds) != 1 || ownerFeeds[0].ID != ownerFeed.ID {
			t.Fatalf("List() = %#v, %v", ownerFeeds, err)
		}
		empty, err := feeds.List(ctx, 99)
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatalf("empty List() = %#v, %v", empty, err)
		}

		ids := fixture.InsertItems(t, ownerFeed.ID, 3)
		otherIDs := fixture.InsertItems(t, otherFeed.ID, 1)
		details, err := items.Get(ctx, 1, ids[0])
		if err != nil || details.Item.ID != ids[0] || details.Feed.ID != ownerFeed.ID {
			t.Fatalf("Get() = %#v, %v", details, err)
		}
		for _, test := range []struct{ userID, itemID int }{{2, ids[0]}, {1, 999999}} {
			if _, err := items.Get(ctx, test.userID, test.itemID); !errors.Is(err, library.ErrNotFound) {
				t.Fatalf("Get(%d, %d) error = %v", test.userID, test.itemID, err)
			}
		}
		listed, err := items.List(ctx, 1)
		if err != nil || len(listed) != 3 || listed[0].ID != ids[2] || listed[2].ID != ids[0] {
			t.Fatalf("List items = %#v, %v", listed, err)
		}
		first, err := items.ListPage(ctx, 1, nil, 2)
		if err != nil || len(first) != 2 {
			t.Fatalf("first page = %#v, %v", first, err)
		}
		cursor := first[1].ID
		second, err := items.ListPage(ctx, 1, &cursor, 2)
		if err != nil || len(second) != 1 || second[0].ID != ids[0] {
			t.Fatalf("second page = %#v, %v", second, err)
		}

		if err := items.Delete(ctx, 2, ids[0]); !errors.Is(err, library.ErrNotFound) {
			t.Fatalf("foreign Delete() error = %v", err)
		}
		if err := items.Delete(ctx, 1, ids[0]); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if err := items.Delete(ctx, 1, ids[0]); !errors.Is(err, library.ErrNotFound) {
			t.Fatalf("second Delete() error = %v", err)
		}
		if _, err := items.Get(ctx, 1, ids[0]); !errors.Is(err, library.ErrNotFound) {
			t.Fatalf("deleted item Get() error = %v", err)
		}

		if err := feeds.Delete(ctx, 1, otherFeed.ID); !errors.Is(err, library.ErrNotFound) {
			t.Fatalf("foreign feed Delete() error = %v", err)
		}
		if _, err := items.Get(ctx, 2, otherIDs[0]); err != nil {
			t.Fatalf("foreign feed changed: %v", err)
		}
		if err := feeds.Delete(ctx, 1, ownerFeed.ID); err != nil {
			t.Fatalf("feed Delete() error = %v", err)
		}
		for _, id := range ids[1:] {
			if _, err := items.Get(ctx, 1, id); !errors.Is(err, library.ErrNotFound) {
				t.Fatalf("cascade item %d error = %v", id, err)
			}
		}
	})
}

func RunFeedCreation(t *testing.T, factory Factory) {
	t.Helper()
	t.Run("feed identity", func(t *testing.T) {
		feeds := factory(t).Feeds
		input := library.CreateFeedInput{Title: "original", Type: "rss", URL: "https://same"}
		first, err := feeds.Add(t.Context(), 1, input)
		if err != nil || !first.Created {
			t.Fatalf("first = %#v, %v", first, err)
		}
		for _, title := range []string{"original", "changed"} {
			input.Title = title
			repeat, err := feeds.Add(t.Context(), 1, input)
			if err != nil || repeat.Created || repeat.Feed != first.Feed {
				t.Fatalf("repeat = %#v, %v", repeat, err)
			}
		}
		otherUser, err := feeds.Add(t.Context(), 2, input)
		if err != nil || !otherUser.Created || otherUser.ID == first.ID {
			t.Fatalf("other user = %#v, %v", otherUser, err)
		}
		input.Type = "web"
		otherType, err := feeds.Add(t.Context(), 1, input)
		if err != nil || !otherType.Created || otherType.ID == first.ID {
			t.Fatalf("other type = %#v, %v", otherType, err)
		}
	})
	t.Run("concurrent repeats", func(t *testing.T) {
		feeds := factory(t).Feeds
		const count = 12
		results := make([]library.AddFeedResult, count)
		errs := make([]error, count)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range count {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results[i], errs[i] = feeds.Add(t.Context(), 1, library.CreateFeedInput{Title: "same", Type: "rss", URL: "https://same"})
			}()
		}
		close(start)
		wg.Wait()
		created := 0
		for i, result := range results {
			if errs[i] != nil {
				t.Fatal(errs[i])
			}
			if result.Created {
				created++
			}
			if result.ID == 0 || result.Feed != results[0].Feed {
				t.Fatalf("result = %#v", result)
			}
		}
		if created != 1 {
			t.Fatalf("created = %d", created)
		}
		listed, err := feeds.List(t.Context(), 1)
		if err != nil || len(listed) != 1 {
			t.Fatalf("feeds = %#v, %v", listed, err)
		}
	})
}
