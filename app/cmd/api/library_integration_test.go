package main

import (
	"encoding/xml"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"gkfeed/api/internal/config"
)

func TestItemPaginationAcrossRequests(t *testing.T) {
	database := resourceTestDatabase(t)
	execFixture(t, database, `INSERT INTO feed (id,title,url,type,user_id) VALUES (12,'Foreign','https://foreign','web',2)`)
	// Interleave a foreign item so filtering must happen before pagination.
	for _, item := range []struct{ id, feed int }{{24, 11}, {25, 12}, {26, 11}} {
		execFixture(t, database, `INSERT INTO item (id,feed_id,title,text,date,link) VALUES (?,?,'Paged','Body','2026-01-01','https://item')`, item.id, item.feed)
	}
	handler := newHandler(config.Config{}, database)
	credentials := resourceBearer(t, handler)
	for _, path := range []string{"/api/v1/get_items", "/api/v2/items"} {
		for _, view := range []string{"full", "summary"} {
			t.Run(path+"/"+view, func(t *testing.T) {
				query := "?limit=2&view=" + view
				var seen []int
				for range 3 { // A missing or repeated cursor must fail, never hang.
					page := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", path+query, credentials), 200)
					if len(page.Items) != 2 {
						t.Fatalf("page size = %d, want 2: %#v", len(page.Items), page)
					}
					seen = append(seen, itemIDs(page.Items)...)
					if page.NextCursor == nil {
						break
					}
					query = "?limit=2&view=" + view + "&cursor=" + strconv.Itoa(*page.NextCursor)
				}
				if !slices.Equal(seen, []int{26, 24, 23, 22}) {
					t.Fatalf("pagination returned %v, want all owned items once in descending order", seen)
				}
				for _, cursor := range []string{"22", "0"} {
					page := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", path+"?view="+view+"&cursor="+cursor, credentials), 200)
					if page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil {
						t.Fatalf("exhausted page = %#v", page)
					}
				}
			})
		}
	}
}

func TestItemListDefaultLimit(t *testing.T) {
	database := resourceTestDatabase(t)
	for id := 24; id <= 123; id++ {
		execFixture(t, database, `INSERT INTO item (id,feed_id,title,text,date,link) VALUES (?,11,'Item','Body','2026-01-01','https://item')`, id)
	}
	handler := newHandler(config.Config{}, database)
	credentials := resourceBearer(t, handler)
	for _, path := range []string{"/api/v1/get_items", "/api/v2/items"} {
		first := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", path, credentials), 200)
		if len(first.Items) != 100 || first.NextCursor == nil {
			t.Fatalf("default page = %d items, cursor %v", len(first.Items), first.NextCursor)
		}
		last := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", path+"?cursor="+strconv.Itoa(*first.NextCursor), credentials), 200)
		if !slices.Equal(itemIDs(last.Items), []int{23, 22}) || last.NextCursor != nil {
			t.Fatalf("remaining page = %#v", last)
		}
	}
}

func TestInvalidLibraryRequestsPreserveData(t *testing.T) {
	database := resourceTestDatabase(t)
	handler := newHandler(config.Config{}, database)
	credentials := resourceBearer(t, handler)
	for _, path := range []string{"/api/v1/get_items", "/api/v2/items"} {
		for _, query := range []string{"limit=0", "limit=-1", "limit=many", "cursor=-1", "cursor=nope"} {
			t.Run(path+"?"+query, func(t *testing.T) {
				response := resourceRequest(handler, "GET", path+"?"+query, credentials)
				if response.Code != http.StatusBadRequest {
					t.Fatalf("invalid query = %d %s", response.Code, response.Body.String())
				}
			})
		}
	}
	for _, path := range []string{"/api/v1/item", "/api/v1/delete"} {
		for _, query := range []string{"", "?id=nope", "?id=0", "?id=-1"} {
			method := http.MethodGet
			if path == "/api/v1/delete" {
				method = http.MethodDelete
			}
			response := resourceRequest(handler, method, path+query, credentials)
			if response.Code != 400 {
				t.Fatalf("%s %s = %d %s", method, path+query, response.Code, response.Body.String())
			}
		}
	}
	page := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", "/api/v2/items", credentials), 200)
	if !slices.Equal(itemIDs(page.Items), []int{23, 22}) {
		t.Fatalf("invalid requests changed data: %#v", page)
	}
}

func TestFeedDeletionRollsBackAndCanBeRetried(t *testing.T) {
	database := resourceTestDatabase(t)
	handler := newHandler(config.Config{}, database)
	credentials := resourceBearer(t, handler)
	execFixture(t, database, `CREATE TRIGGER reject_feed_delete BEFORE DELETE ON feed BEGIN SELECT RAISE(ABORT,'private storage failure'); END`)
	assertResourceError(t, resourceRequest(handler, "DELETE", "/api/v2/feeds/11", credentials), 500, "internal_error", "Internal Server Error")
	page := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", "/api/v2/items", credentials), 200)
	if !slices.Equal(itemIDs(page.Items), []int{23, 22}) {
		t.Fatalf("failed feed deletion lost items: %#v", page)
	}
	decodeResponse[apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds/11", credentials), 200)
	execFixture(t, database, "DROP TRIGGER reject_feed_delete")
	response := resourceRequest(handler, "DELETE", "/api/v2/feeds/11", credentials)
	if response.Code != 204 || response.Body.Len() != 0 {
		t.Fatalf("retry = %d %s", response.Code, response.Body.String())
	}
	page = decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", "/api/v2/items", credentials), 200)
	if page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatalf("deleted feed still has items: %#v", page)
	}
}

func TestDeprecatedDeletionLeavesLibraryUntouched(t *testing.T) {
	database := resourceTestDatabase(t)
	handler := newHandler(config.Config{}, database)
	credentials := resourceBearer(t, handler)
	if response := resourceRequest(handler, "POST", "/api/v1/add_deleted_items", "", "not JSON"); response.Code != 401 {
		t.Fatalf("unauthenticated deprecated request = %d", response.Code)
	}
	for _, body := range []string{"not JSON", `[22,23]`, `{"items":[22,23]}`} {
		result := decodeResponse[map[string]string](t, resourceRequest(handler, "POST", "/api/v1/add_deleted_items", credentials, body), 410)
		if result["error"] != "endpoint_gone" || result["replacement"] != "DELETE /api/v1/items/{id}" {
			t.Fatalf("deprecated response = %#v", result)
		}
	}
	page := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", "/api/v2/items", credentials), 200)
	if !slices.Equal(itemIDs(page.Items), []int{23, 22}) {
		t.Fatalf("deprecated request changed library: %#v", page)
	}
	for _, path := range []string{"/api/v1/list", "/api/v2/feeds"} {
		feeds := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", path, "other"), 200)
		if feeds == nil || len(feeds) != 0 {
			t.Fatalf("empty feeds = %#v", feeds)
		}
	}
	for _, path := range []string{"/api/v1/get_items", "/api/v2/items"} {
		page := decodeResponse[apiItemPage](t, resourceRequest(handler, "GET", path, "other"), 200)
		if page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil {
			t.Fatalf("empty items = %#v", page)
		}
	}
}

func TestRSSRepresentsOwnedItems(t *testing.T) {
	database := resourceTestDatabase(t)
	const title = `Title <one> & "two"`
	const text = `<p>Body & details</p>`
	execFixture(t, database, "UPDATE item SET title=?, text=? WHERE id=22", title, text)
	execFixture(t, database, `INSERT INTO feed (id,title,url,type,user_id) VALUES (12,'Foreign','https://foreign','web',2)`)
	execFixture(t, database, `INSERT INTO item (id,feed_id,title,text,date,link) VALUES (24,12,'Private','Private','2026-01-01','https://private')`)
	handler := newHandler(config.Config{}, database)
	if response := resourceRequest(handler, "GET", "/api/v1/feed", ""); response.Code != 401 {
		t.Fatalf("unauthenticated RSS = %d", response.Code)
	}
	response := resourceRequest(handler, "GET", "/api/v1/feed", resourceBearer(t, handler))
	if response.Code != 200 || response.Header().Get("Content-Type") != "application/rss+xml" {
		t.Fatalf("RSS = %d %v %s", response.Code, response.Header(), response.Body.String())
	}
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
			Items []struct {
				ID          int    `xml:"id"`
				Title       string `xml:"title"`
				Description string `xml:"description"`
				Link        string `xml:"link"`
				PubDate     string `xml:"pubDate"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &feed); err != nil {
		t.Fatal(err)
	}
	if feed.Version != "2.0" || feed.Channel.Title != "GKFeed" || feed.Channel.Link != "/api/v1/feed" || len(feed.Channel.Items) != 2 {
		t.Fatalf("RSS channel = %#v", feed)
	}
	item := feed.Channel.Items[1]
	if feed.Channel.Items[0].ID != 23 || item.ID != 22 || item.Title != title || item.Description != text || item.Link != "https://example.com/item" {
		t.Fatalf("RSS lost item data or exposed a foreign item: %#v", feed.Channel.Items)
	}
	date, err := time.Parse(time.RFC1123, item.PubDate)
	if err != nil || !date.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("RSS publication date = %q, %v", item.PubDate, err)
	}
}
