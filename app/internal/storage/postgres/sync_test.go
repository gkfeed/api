package postgres

import (
	"gkfeed/api/internal/library"
	"testing"
)

func TestSyncPaginationAndChanges(t *testing.T) {
	db := newTestDB(t)
	feeds := NewFeedRepository(db)
	items := NewItemRepository(db)
	owner, err := feeds.Add(t.Context(), 1, library.CreateFeedInput{Title: "owner", Type: "rss", URL: "https://owner"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := feeds.Add(t.Context(), 2, library.CreateFeedInput{Title: "foreign", Type: "rss", URL: "https://foreign"})
	if err != nil {
		t.Fatal(err)
	}
	ids := insertItems(t, db, owner.ID, 4)
	insertItems(t, db, foreign.ID, 1)
	first, err := items.SyncPage(t.Context(), 1, 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.MaxID != ids[3] || first.Items[0].ID != ids[3] || first.Items[1].ID != ids[2] {
		t.Fatalf("first = %#v", first)
	}
	inserted := insertItems(t, db, owner.ID, 1)[0]
	if err := items.Delete(t.Context(), 1, ids[1]); err != nil {
		t.Fatal(err)
	}
	second, err := items.SyncPage(t.Context(), 1, first.LastID, first.MaxID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasMore || len(second.Items) != 1 || second.Items[0].ID != ids[0] {
		t.Fatalf("second = %#v", second)
	}
	changes, err := items.Changes(t.Context(), 1, first.Sequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	if changes.HasMore || len(changes.Changes) != 2 || changes.Changes[0].ItemID != inserted || changes.Changes[0].Item == nil || changes.Changes[1].ItemID != ids[1] || changes.Changes[1].Item != nil {
		t.Fatalf("changes = %#v", changes)
	}
	if item := changes.Changes[0].Item; item.ID != inserted || item.FeedID != owner.ID || item.Title != "item-0" || item.Date.IsZero() {
		t.Fatalf("upsert fields were lost: %#v", item)
	}
	foreignChanges, err := items.Changes(t.Context(), 2, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(foreignChanges.Changes) != 1 {
		t.Fatalf("foreign changes = %#v", foreignChanges)
	}
	if err := feeds.Delete(t.Context(), 1, owner.ID); err != nil {
		t.Fatal(err)
	}
	feedDeletes, err := items.Changes(t.Context(), 1, changes.Sequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(feedDeletes.Changes) != 4 {
		t.Fatalf("feed deletes = %#v", feedDeletes)
	}
	for _, change := range feedDeletes.Changes {
		if change.Item != nil {
			t.Fatalf("feed delete was an upsert: %#v", change)
		}
	}
}
