package main

import (
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"testing"

	"gkfeed/api/internal/config"
)

type apiSyncPage struct {
	Items      []apiItem `json:"items"`
	NextCursor string    `json:"next_cursor"`
	HasMore    bool      `json:"has_more"`
	SyncCursor string    `json:"sync_cursor"`
}

type apiChangesPage struct {
	Upserted   []apiItem `json:"upserted"`
	DeletedIDs []int     `json:"deleted_ids"`
	NextCursor string    `json:"next_cursor"`
	HasMore    bool      `json:"has_more"`
}

func readChanges(t *testing.T, handler http.Handler, credentials, cursor string) apiChangesPage {
	t.Helper()
	return decodeResponse[apiChangesPage](t, resourceRequest(handler, "GET", "/api/v2/items/changes?cursor="+url.QueryEscape(cursor), credentials), 200)
}

func TestItemSyncDuringLibraryChanges(t *testing.T) {
	database := resourceTestDatabase(t)
	execFixture(t, database, `INSERT INTO item (id,feed_id,title,text,date,link) VALUES (24,11,'Third','Body','2026-01-01','https://third')`)
	execFixture(t, database, `INSERT INTO feed (id,title,url,type,user_id) VALUES (12,'Foreign','https://foreign','web',2)`)
	handler := newHandler(config.Config{SyncCursorSecret: "integration cursor key"}, database)
	credentials := resourceBearer(t, handler)
	first := decodeResponse[apiSyncPage](t, resourceRequest(handler, "GET", "/api/v2/items/sync?limit=1", credentials), 200)
	if !slices.Equal(itemIDs(first.Items), []int{24}) || !first.HasMore || first.NextCursor == "" || first.SyncCursor == "" {
		t.Fatalf("first sync page = %#v", first)
	}

	// Parser writes can happen between HTTP pages. New items belong in changes,
	// while an item deleted before the next page must disappear from that page.
	execFixture(t, database, `INSERT INTO item (id,feed_id,title,text,date,link) VALUES (25,11,'New','New body','2026-02-01','https://new')`)
	execFixture(t, database, `UPDATE item SET title='Updated',text='Updated body' WHERE id=25`)
	execFixture(t, database, `INSERT INTO item (id,feed_id,title,text,date,link) VALUES (26,12,'Foreign','Private','2026-02-01','https://foreign/item')`)
	if response := resourceRequest(handler, "DELETE", "/api/v2/items/23", credentials); response.Code != 204 {
		t.Fatalf("delete during paging = %d %s", response.Code, response.Body.String())
	}
	second := decodeResponse[apiSyncPage](t, resourceRequest(handler, "GET", "/api/v2/items/sync?limit=1&cursor="+url.QueryEscape(first.NextCursor), credentials), 200)
	if !slices.Equal(itemIDs(second.Items), []int{22}) || second.HasMore || second.NextCursor != "" || second.SyncCursor == "" {
		t.Fatalf("continued sync included a new, foreign or deleted item: %#v", second)
	}
	changes := readChanges(t, handler, credentials, first.SyncCursor)
	if !slices.Equal(itemIDs(changes.Upserted), []int{25}) || !slices.Equal(changes.DeletedIDs, []int{23}) || changes.HasMore || changes.NextCursor == "" {
		t.Fatalf("changes = %#v", changes)
	}
	item := changes.Upserted[0]
	if item.FeedID != 11 || item.Title != "Updated" || item.Text != "Updated body" || item.Date != "2026-02-01T00:00:00Z" || item.Link != "https://new" {
		t.Fatalf("changes lost the latest item data: %#v", item)
	}
	// Verify the continuation's sync cursor behavior without decoding its contents.
	continued := readChanges(t, handler, credentials, second.SyncCursor)
	if !reflect.DeepEqual(changes.Upserted, continued.Upserted) || !slices.Equal(changes.DeletedIDs, continued.DeletedIDs) {
		t.Fatalf("continued sync cursor lost changes: %#v versus %#v", continued, changes)
	}

	// Apply the delta as a client would and compare with a fresh full list.
	local := make(map[int]apiItem)
	for _, item := range append(first.Items, second.Items...) {
		local[item.ID] = item
	}
	for _, item := range changes.Upserted {
		local[item.ID] = item
	}
	for _, id := range changes.DeletedIDs {
		delete(local, id)
	}
	current := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", "/api/v2/items", credentials), 200)
	if len(local) != len(current.Items) {
		t.Fatalf("synchronized items = %#v, current list = %#v", local, current)
	}
	for _, item := range current.Items {
		if local[item.ID] != item {
			t.Fatalf("synchronized item = %#v, current = %#v", local[item.ID], item)
		}
	}
	empty := readChanges(t, handler, credentials, changes.NextCursor)
	if empty.Upserted == nil || empty.DeletedIDs == nil || len(empty.Upserted) != 0 || len(empty.DeletedIDs) != 0 || empty.HasMore || empty.NextCursor == "" {
		t.Fatalf("caught-up changes = %#v", empty)
	}

	if response := resourceRequest(handler, "DELETE", "/api/v2/feeds/11", credentials); response.Code != 204 {
		t.Fatalf("feed deletion = %d %s", response.Code, response.Body.String())
	}
	deleted := readChanges(t, handler, credentials, empty.NextCursor)
	slices.Sort(deleted.DeletedIDs)
	if len(deleted.Upserted) != 0 || !slices.Equal(deleted.DeletedIDs, []int{22, 24, 25}) || deleted.HasMore {
		t.Fatalf("feed deletion changes = %#v", deleted)
	}
}

func TestItemChangesPaginationAndCollapsedMutations(t *testing.T) {
	database := resourceTestDatabase(t)
	handler := newHandler(config.Config{SyncCursorSecret: "integration cursor key"}, database)
	credentials := resourceBearer(t, handler)
	initial := decodeResponse[apiSyncPage](t, resourceRequest(handler, "GET", "/api/v2/items/sync", credentials), 200)
	execFixture(t, database, `UPDATE item SET title='First update' WHERE id=22`)
	execFixture(t, database, `UPDATE item SET title='Last update' WHERE id=22`)
	if response := resourceRequest(handler, "DELETE", "/api/v2/items/23", credentials); response.Code != 204 {
		t.Fatalf("delete = %d %s", response.Code, response.Body.String())
	}
	page := decodeResponse[apiChangesPage](t, resourceRequest(handler, "GET", "/api/v2/items/changes?limit=2&cursor="+url.QueryEscape(initial.SyncCursor), credentials), 200)
	if !page.HasMore || page.NextCursor == "" || !slices.Equal(itemIDs(page.Upserted), []int{22}) || page.Upserted[0].Title != "Last update" || len(page.DeletedIDs) != 0 {
		t.Fatalf("collapsed first page = %#v", page)
	}
	last := readChanges(t, handler, credentials, page.NextCursor)
	if last.HasMore || len(last.Upserted) != 0 || !slices.Equal(last.DeletedIDs, []int{23}) {
		t.Fatalf("last changes page = %#v", last)
	}
	// An item created and deleted inside one response leaves only a tombstone.
	execFixture(t, database, `INSERT INTO item (id,feed_id,title,text,date,link) VALUES (24,11,'Transient','','2026-01-01','https://transient')`)
	if response := resourceRequest(handler, "DELETE", "/api/v2/items/24", credentials); response.Code != 204 {
		t.Fatalf("transient delete = %d %s", response.Code, response.Body.String())
	}
	transient := readChanges(t, handler, credentials, last.NextCursor)
	if transient.HasMore || len(transient.Upserted) != 0 || !slices.Equal(transient.DeletedIDs, []int{24}) {
		t.Fatalf("transient item changes = %#v", transient)
	}
}

func TestSyncRejectsUnusableCursors(t *testing.T) {
	database := resourceTestDatabase(t)
	handler := newHandler(config.Config{SyncCursorSecret: "integration cursor key"}, database)
	credentials := resourceBearer(t, handler)
	first := decodeResponse[apiSyncPage](t, resourceRequest(handler, "GET", "/api/v2/items/sync?limit=1", credentials), 200)
	rotated := newHandler(config.Config{SyncCursorSecret: "rotated cursor key"}, database)
	for _, test := range []struct {
		name, path, credentials string
		handler                 http.Handler
	}{
		{"cross-user page", "/api/v2/items/sync?cursor=" + first.NextCursor, "other", handler},
		{"cross-user changes", "/api/v2/items/changes?cursor=" + first.SyncCursor, "other", handler},
		{"tampered page", "/api/v2/items/sync?cursor=" + first.NextCursor + "x", credentials, handler},
		{"tampered changes", "/api/v2/items/changes?cursor=" + first.SyncCursor + "x", credentials, handler},
		{"wrong cursor kind for page", "/api/v2/items/sync?cursor=" + first.SyncCursor, credentials, handler},
		{"wrong cursor kind for changes", "/api/v2/items/changes?cursor=" + first.NextCursor, credentials, handler},
		{"rotated page key", "/api/v2/items/sync?cursor=" + first.NextCursor, "owner", rotated},
		{"rotated changes key", "/api/v2/items/changes?cursor=" + first.SyncCursor, "owner", rotated},
		{"empty page cursor", "/api/v2/items/sync?cursor=", credentials, handler},
		{"repeated page cursor", "/api/v2/items/sync?cursor=" + first.NextCursor + "&cursor=" + first.NextCursor, credentials, handler},
		{"missing changes cursor", "/api/v2/items/changes", credentials, handler},
		{"repeated changes cursor", "/api/v2/items/changes?cursor=" + first.SyncCursor + "&cursor=" + first.SyncCursor, credentials, handler},
		{"page limit too large", "/api/v2/items/sync?limit=501", credentials, handler},
		{"changes limit too large", "/api/v2/items/changes?limit=501&cursor=" + first.SyncCursor, credentials, handler},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := resourceRequest(test.handler, "GET", test.path, test.credentials)
			if response.Code != 400 {
				t.Fatalf("invalid cursor request = %d %s", response.Code, response.Body.String())
			}
		})
	}
}
