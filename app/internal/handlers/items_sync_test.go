package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/models"
)

func TestItemsSyncCursorsAndDelta(t *testing.T) {
	call := 0
	service := &fakeLibraryState{}
	service.syncPage = func(_ context.Context, userID, afterID, maxID, limit int) (library.SyncPage, error) {
		if userID != 7 || limit != 1 {
			t.Fatalf("user=%d limit=%d", userID, limit)
		}
		call++
		if call == 1 {
			if afterID != 0 || maxID != 0 {
				t.Fatalf("initial bounds=%d,%d", afterID, maxID)
			}
			return library.SyncPage{Items: []library.Item{{ID: 3}}, LastID: 3, MaxID: 3, Sequence: 4, HasMore: true}, nil
		}
		if afterID != 3 || maxID != 3 {
			t.Fatalf("continued bounds=%d,%d", afterID, maxID)
		}
		return library.SyncPage{Items: []library.Item{{ID: 2}}, LastID: 2, MaxID: 3, Sequence: 9}, nil
	}
	service.changes = func(_ context.Context, userID int, sequence int64, limit int) (library.ChangesPage, error) {
		if userID != 7 || sequence != 4 {
			t.Fatalf("changes user=%d sequence=%d", userID, sequence)
		}
		item := library.Item{ID: 5}
		return library.ChangesPage{Changes: []library.Change{{Sequence: 5, ItemID: 5, Item: &item}, {Sequence: 6, ItemID: 5}, {Sequence: 7, ItemID: 2}}, Sequence: 7}, nil
	}
	handler := NewItemsSyncHandler(library.NewService(fakeFeedRepository{service}, fakeItemRepository{service}), "test secret")
	request := func(path string, userID int) *http.Request {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		return r.WithContext(auth.WithUser(r.Context(), models.User{ID: userID}))
	}
	first := httptest.NewRecorder()
	handler.HandleItems(first, request("/api/v2/items/sync?limit=1", 7))
	if first.Code != 200 {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	var a syncItemsResponse
	if err := json.Unmarshal(first.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if !a.HasMore || a.NextCursor == "" || a.SyncCursor == "" || len(a.Items) != 1 {
		t.Fatalf("first=%#v", a)
	}
	second := httptest.NewRecorder()
	handler.HandleItems(second, request("/api/v2/items/sync?limit=1&cursor="+url.QueryEscape(a.NextCursor), 7))
	var b syncItemsResponse
	if err := json.Unmarshal(second.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	continuedToken, err := handler.decode(b.SyncCursor, "sync", 7)
	if b.HasMore || b.NextCursor != "" || continuedToken.Sequence != 4 || len(b.Items) != 1 {
		t.Fatalf("second=%#v", b)
	}
	denied := httptest.NewRecorder()
	handler.HandleItems(denied, request("/api/v2/items/sync?cursor="+url.QueryEscape(a.NextCursor), 8))
	if denied.Code != 400 {
		t.Fatalf("cross-user cursor status=%d", denied.Code)
	}
	tampered := httptest.NewRecorder()
	handler.HandleItems(tampered, request("/api/v2/items/sync?cursor="+url.QueryEscape(a.NextCursor+"x"), 7))
	if tampered.Code != 400 {
		t.Fatalf("tampered cursor status=%d", tampered.Code)
	}
	delta := httptest.NewRecorder()
	handler.HandleChanges(delta, request("/api/v2/items/changes?cursor="+url.QueryEscape(a.SyncCursor), 7))
	var c itemChangesResponse
	if err := json.Unmarshal(delta.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if delta.Code != 200 || len(c.Upserted) != 0 || len(c.DeletedIDs) != 2 || c.NextCursor == "" {
		t.Fatalf("delta=%#v status=%d", c, delta.Code)
	}
	token, err := handler.decode(c.NextCursor, "sync", 7)
	if err != nil || token.Sequence != 7 {
		t.Fatalf("next token=%#v err=%v", token, err)
	}
}
