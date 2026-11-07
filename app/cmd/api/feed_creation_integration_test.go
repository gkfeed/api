package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"gkfeed/api/internal/config"
	"gkfeed/api/internal/handlers"
	"gkfeed/api/internal/services"
	storage "gkfeed/api/internal/storage/postgres"
)

// Only the external type detector is substituted. Creation, auth and storage
// use the same implementations as the application.
type feedDetectorReply struct {
	suggestion services.FeedTypeSuggestion
	err        error
}

func (d feedDetectorReply) Detect(context.Context, string, string) (services.FeedTypeSuggestion, error) {
	return d.suggestion, d.err
}

type apiFeedCreation struct {
	Results []apiFeedMutation `json:"results"`
}

func TestFeedCreationPersistsCanonicalSubscriptions(t *testing.T) {
	database := resourceTestDatabase(t)
	creator := &services.FeedCreator{Detector: feedDetectorReply{suggestion: services.FeedTypeSuggestion{Type: "inst", Confidence: .9}}}
	libraryHandler := handlers.NewLibraryHandler(storage.NewFeedRepository(database), storage.NewItemRepository(database), services.FeedResolver{}, creator)
	handler := newHandlerWithLibrary(config.Config{}, database, libraryHandler)
	credentials := resourceBearer(t, handler)
	create := func(body string) apiFeedCreation {
		t.Helper()
		return decodeResponse[apiFeedCreation](t, resourceRequest(handler, "POST", "/api/v2/feeds", credentials, body), 200)
	}
	first := create(`{"url":"https://instagram.com/Example/?share=1"}`)
	if len(first.Results) != 2 {
		t.Fatalf("automatic Instagram selection = %#v", first)
	}
	for i, kind := range []string{"inst", "stories"} {
		result := first.Results[i]
		if !result.Created || result.Item.ID == 0 || result.Item.UserID != 1 || result.Item.Type != kind || result.Item.Title != "Example" || result.Item.URL != "https://www.instagram.com/example" {
			t.Fatalf("canonical subscription = %#v", result)
		}
		stored := decodeResponse[apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds/"+strconv.Itoa(result.Item.ID), credentials), 200)
		if stored != result.Item {
			t.Fatalf("created subscription = %#v, stored = %#v", result.Item, stored)
		}
	}
	if first.Results[0].Item.ID == first.Results[1].Item.ID {
		t.Fatal("different subscription types share an ID")
	}
	repeat := create(`{"url":"https://www.instagram.com/example","title":"Replacement","types":["stories","inst"]}`)
	if len(repeat.Results) != 2 || repeat.Results[0].Created || repeat.Results[1].Created || repeat.Results[0].Item != first.Results[1].Item || repeat.Results[1].Item != first.Results[0].Item {
		t.Fatalf("repeat must preserve titles and return results in selection order: %#v", repeat)
	}
	posts := create(`{"url":"https://instagram.com/other","type":"inst"}`)
	if len(posts.Results) != 1 || posts.Results[0].Item.Type != "inst" {
		t.Fatalf("explicit selection = %#v", posts)
	}
	other := decodeResponse[apiFeedCreation](t, resourceRequest(handler, "POST", "/api/v2/feeds", "other", `{"url":"https://instagram.com/example","types":["inst","stories"]}`), 200)
	if len(other.Results) != 2 {
		t.Fatalf("other user's selection = %#v", other)
	}
	for i, result := range other.Results {
		if !result.Created || result.Item.UserID != 2 || result.Item.ID == first.Results[i].Item.ID {
			t.Fatalf("subscriptions must belong to separate users: %#v", other)
		}
	}
	before := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds", credentials), 200)
	execFixture(t, database, `CREATE TRIGGER reject_stories BEFORE INSERT ON feed WHEN NEW.type = 'stories' BEGIN SELECT RAISE(ABORT,'rejected'); END`)
	response := resourceRequest(handler, "POST", "/api/v2/feeds", credentials, `{"url":"https://instagram.com/rollback","types":["inst","stories"]}`)
	if response.Code != 500 {
		t.Fatalf("failed batch = %d %s", response.Code, response.Body.String())
	}
	after := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds", credentials), 200)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed batch changed subscriptions: before %#v, after %#v", before, after)
	}
}

func TestFeedCreationErrorsLeaveSubscriptionsUntouched(t *testing.T) {
	for _, test := range []struct {
		name, body string
		detector   feedDetectorReply
		status     int
		code       string
	}{
		{"low confidence", `{"url":"https://example.com"}`, feedDetectorReply{suggestion: services.FeedTypeSuggestion{Type: "web", Confidence: .34}}, 422, "type_required"},
		{"unconfigured", `{"url":"https://example.com"}`, feedDetectorReply{err: services.ErrDetectionUnavailable}, 503, "detection_unavailable"},
		{"upstream failure", `{"url":"https://example.com"}`, feedDetectorReply{err: services.ErrDetectionFailed}, 502, "detection_failed"},
		{"deadline", `{"url":"https://example.com"}`, feedDetectorReply{err: context.DeadlineExceeded}, 504, "resolution_timeout"},
		{"unknown type", `{"url":"https://example.com","type":"unknown"}`, feedDetectorReply{}, 400, "invalid_creation"},
		{"empty selection", `{"url":"https://example.com","types":[]}`, feedDetectorReply{}, 400, "invalid_creation"},
		{"duplicate selection", `{"url":"https://example.com","types":["web","web"]}`, feedDetectorReply{}, 400, "invalid_creation"},
		{"conflicting fields", `{"url":"https://example.com","type":"web","types":["inst"]}`, feedDetectorReply{}, 400, "invalid_creation"},
		{"credentials in URL", `{"url":"https://user:pass@example.com","type":"web"}`, feedDetectorReply{}, 400, "invalid_creation"},
		{"missing URL", `{}`, feedDetectorReply{}, 400, "invalid_creation"},
		{"unknown field", `{"url":"https://example.com","extra":true}`, feedDetectorReply{}, 400, ""},
		{"multiple JSON values", `{} {}`, feedDetectorReply{}, 400, ""},
		{"array", `[]`, feedDetectorReply{}, 400, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := resourceTestDatabase(t)
			creator := &services.FeedCreator{Detector: test.detector}
			libraryHandler := handlers.NewLibraryHandler(storage.NewFeedRepository(database), storage.NewItemRepository(database), services.FeedResolver{}, creator)
			handler := newHandlerWithLibrary(config.Config{}, database, libraryHandler)
			credentials := resourceBearer(t, handler)
			before := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds", credentials), 200)
			response := resourceRequest(handler, "POST", "/api/v2/feeds", credentials, test.body)
			if response.Code != test.status {
				t.Fatalf("creation error = %d %s, want %d", response.Code, response.Body.String(), test.status)
			}
			if test.code != "" {
				body := decodeResponse[struct {
					Error struct{ Code, Message string } `json:"error"`
				}](t, response, test.status)
				if body.Error.Code != test.code || body.Error.Message == "" {
					t.Fatalf("creation error = %#v", body)
				}
			}
			after := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds", credentials), 200)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected request changed subscriptions: before %#v, after %#v", before, after)
			}
		})
	}
}

func TestLegacyFeedCreationPersistsResolvedFeeds(t *testing.T) {
	for _, test := range []struct{ path, body, title, kind, canonical string }{
		{"/api/v1/add", `{"title":" \tManual title\u00a0","type":" web ","url":" https://example.com/manual-feed\n"}`, "Manual title", "web", "https://example.com/manual-feed"},
		{"/api/v1/add_lazy", `{"url":"https://tok.adminforge.de/@example"}`, "example", "tiktok", "https://www.tiktok.com/@example"},
	} {
		t.Run(test.path, func(t *testing.T) {
			database := resourceTestDatabase(t)
			handler := newHandler(config.Config{}, database)
			credentials := resourceBearer(t, handler)
			first := decodeResponse[apiFeedMutation](t, resourceRequest(handler, "POST", test.path, credentials, test.body), 200)
			if !first.Created || first.Item.ID == 0 || first.Item.UserID != 1 || first.Item.Title != test.title || first.Item.Type != test.kind || first.Item.URL != test.canonical {
				t.Fatalf("created feed = %#v", first)
			}
			repeat := decodeResponse[apiFeedMutation](t, resourceRequest(handler, "POST", test.path, credentials, test.body), 200)
			if repeat.Created || repeat.Item != first.Item {
				t.Fatalf("repeat = %#v, first = %#v", repeat, first)
			}
			stored := decodeResponse[apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds/"+strconv.Itoa(first.Item.ID), credentials), 200)
			if stored != first.Item {
				t.Fatalf("stored = %#v, created = %#v", stored, first.Item)
			}
			other := decodeResponse[apiFeedMutation](t, resourceRequest(handler, "POST", test.path, "other", test.body), 200)
			if !other.Created || other.Item.ID == first.Item.ID || other.Item.UserID != 2 {
				t.Fatalf("other user's feed = %#v", other)
			}
			for _, body := range []string{`{"unknown":true}`, `{} {}`, `[]`} {
				if response := resourceRequest(handler, "POST", test.path, credentials, body); response.Code != 400 {
					t.Fatalf("invalid body = %d %s", response.Code, response.Body.String())
				}
			}
			execFixture(t, database, `CREATE TRIGGER reject_feed_insert BEFORE INSERT ON feed BEGIN SELECT RAISE(ABORT,'private failure'); END`)
			if response := resourceRequest(handler, "POST", test.path, credentials, test.body); response.Code != 500 {
				t.Fatalf("storage failure = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestLegacyAddRejectsIncompletePayloads(t *testing.T) {
	database := resourceTestDatabase(t)
	handler := newHandler(config.Config{}, database)
	credentials := resourceBearer(t, handler)
	before := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds", credentials), 200)
	for _, test := range []struct{ name, body string }{
		{"null", `null`},
		{"empty object", `{}`},
		{"missing title", `{"type":"rss","url":"https://example.com"}`},
		{"missing type", `{"title":"Feed","url":"https://example.com"}`},
		{"missing url", `{"title":"Feed","type":"rss"}`},
		{"empty title", `{"title":"","type":"rss","url":"https://example.com"}`},
		{"empty type", `{"title":"Feed","type":"","url":"https://example.com"}`},
		{"empty url", `{"title":"Feed","type":"rss","url":""}`},
		{"blank title", `{"title":" \t\n\u00a0","type":"rss","url":"https://example.com"}`},
		{"blank type", `{"title":"Feed","type":" \t\n\u00a0","url":"https://example.com"}`},
		{"blank url", `{"title":"Feed","type":"rss","url":" \t\n\u00a0"}`},
		{"null title", `{"title":null,"type":"rss","url":"https://example.com"}`},
		{"null type", `{"title":"Feed","type":null,"url":"https://example.com"}`},
		{"null url", `{"title":"Feed","type":"rss","url":null}`},
		{"array", `[]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if response := resourceRequest(handler, "POST", "/api/v1/add", credentials, test.body); response.Code != 400 {
				t.Fatalf("invalid payload = %d %s, want 400", response.Code, response.Body.String())
			}
		})
	}
	after := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds", credentials), 200)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("invalid payloads changed feeds: before %#v, after %#v", before, after)
	}
}

func TestPublicFeedTypesCanBeUsedForCreation(t *testing.T) {
	database := resourceTestDatabase(t)
	handler := newHandler(config.Config{}, database)
	kinds := decodeResponse[[]string](t, resourceRequest(handler, "GET", "/api/v1/feed_types", ""), 200)
	if len(kinds) == 0 {
		t.Fatal("public feed type catalog is empty")
	}
	credentials := resourceBearer(t, handler)
	seen := make(map[string]bool)
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			if seen[kind] {
				t.Fatalf("duplicate catalog entry %q", kind)
			}
			seen[kind] = true
			body, err := json.Marshal(map[string]string{"title": "Selected feed", "type": kind, "url": "https://example.com/selected"})
			if err != nil {
				t.Fatal(err)
			}
			created := decodeResponse[apiFeedCreation](t, resourceRequest(handler, "POST", "/api/v2/feeds", credentials, string(body)), 200)
			if len(created.Results) != 1 || !created.Results[0].Created || created.Results[0].Item.Type != kind {
				t.Fatalf("public type %q cannot create its subscription: %#v", kind, created)
			}
		})
	}
	feeds := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds", credentials), 200)
	if len(feeds) != len(kinds)+1 {
		t.Fatalf("catalog selection persisted %d feeds, want %d", len(feeds), len(kinds)+1)
	}
}

func TestConcurrentFeedCreationReturnsOneSubscription(t *testing.T) {
	for _, test := range []struct{ path, body string }{
		{"/api/v1/add", `{"title":"Concurrent","type":"web","url":"https://example.com/concurrent"}`},
		{"/api/v1/add_lazy", `{"url":"https://www.instagram.com/concurrent"}`},
		{"/api/v2/feeds", `{"title":"Concurrent","type":"web","url":"https://example.com/concurrent"}`},
	} {
		t.Run(test.path, func(t *testing.T) {
			database := resourceTestDatabase(t)
			database.SetMaxOpenConns(8)
			handler := newHandler(config.Config{}, database)
			credentials := resourceBearer(t, handler)
			const count = 8
			responses := make([]*httptest.ResponseRecorder, count)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range count {
				wg.Go(func() {
					<-start
					responses[i] = resourceRequest(handler, "POST", test.path, credentials, test.body)
				})
			}
			close(start)
			wg.Wait()
			created, id := 0, 0
			for _, response := range responses {
				var result apiFeedMutation
				if test.path == "/api/v2/feeds" {
					batch := decodeResponse[apiFeedCreation](t, response, 200)
					if len(batch.Results) != 1 {
						t.Fatalf("batch = %#v", batch)
					}
					result = batch.Results[0]
				} else {
					result = decodeResponse[apiFeedMutation](t, response, 200)
				}
				if result.Created {
					created++
				}
				if id == 0 {
					id = result.Item.ID
				}
				if id == 0 || result.Item.ID != id {
					t.Fatalf("concurrent creation returned different IDs: %#v, want %d", result, id)
				}
			}
			feeds := decodeResponse[[]apiFeed](t, resourceRequest(handler, "GET", "/api/v2/feeds", credentials), 200)
			if created != 1 || len(feeds) != 2 { // One fixture feed and one newly created feed.
				t.Fatalf("concurrent requests created %d subscriptions, list = %#v", created, feeds)
			}
		})
	}
}
