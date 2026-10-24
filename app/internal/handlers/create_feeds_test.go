package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/models"
	"gkfeed/api/internal/services"
	storage "gkfeed/api/internal/storage/postgres"
	"gkfeed/api/internal/testschema"
)

type creationDetector struct {
	kind       library.FeedType
	confidence float64
	err        error
}

func (d creationDetector) Detect(context.Context, string, string) (services.FeedTypeSuggestion, error) {
	return services.FeedTypeSuggestion{Type: d.kind, Confidence: d.confidence}, d.err
}

func creationRequest(h *LibraryHandler, userID int, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v2/feeds", strings.NewReader(body))
	if userID != 0 {
		r = r.WithContext(auth.WithUser(r.Context(), models.User{ID: userID}))
	}
	w := httptest.NewRecorder()
	h.HandleCreateFeed(w, r)
	return w
}

func TestUnifiedCreationHTTPContract(t *testing.T) {
	database, err := sql.Open("sqlite3", t.TempDir()+"/creation.sqlite?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	testschema.Init(t, database)
	feeds := storage.NewFeedRepository(database)
	creator := &services.FeedCreator{Detector: creationDetector{kind: "inst", confidence: .9}}
	h := NewLibraryHandler(feeds, storage.NewItemRepository(database), nil, creator)
	call := func(user int, body string) feedCreationResponse {
		t.Helper()
		w := creationRequest(h, user, body)
		var result feedCreationResponse
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatalf("response = %d %s", w.Code, w.Body.String())
		}
		return result
	}
	first := call(1, `{"url":"https://instagram.com/Example/?share=1"}`)
	if len(first.Results) != 2 || !first.Results[0].Created || !first.Results[1].Created {
		t.Fatalf("first = %#v", first)
	}
	repeat := call(1, `{"url":"https://www.instagram.com/example","title":"Replacement","types":["stories","inst"]}`)
	if len(repeat.Results) != 2 || repeat.Results[0].Created || repeat.Results[1].Created || repeat.Results[0].Item.ID != first.Results[1].Item.ID || repeat.Results[1].Item.Title != "Example" {
		t.Fatalf("repeat = %#v", repeat)
	}
	posts := call(1, `{"url":"https://instagram.com/other","type":"inst"}`)
	if len(posts.Results) != 1 || posts.Results[0].Item.Type != "inst" {
		t.Fatalf("explicit posts = %#v", posts)
	}
	other := call(2, `{"url":"https://instagram.com/example","types":["inst","stories"]}`)
	if other.Results[0].Item.ID == first.Results[0].Item.ID || other.Results[0].Item.UserID != 2 {
		t.Fatalf("other user = %#v", other)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_stories BEFORE INSERT ON feed WHEN NEW.type = 'stories' BEGIN SELECT RAISE(ABORT,'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	w := creationRequest(h, 1, `{"url":"https://instagram.com/rollback","types":["inst","stories"]}`)
	if w.Code != 500 {
		t.Fatalf("failure = %d %s", w.Code, w.Body.String())
	}
	listed, _ := feeds.List(t.Context(), 1)
	for _, feed := range listed {
		if strings.Contains(feed.URL, "rollback") {
			t.Fatalf("partial write: %#v", feed)
		}
	}
}

func TestCreationErrorsNeverWrite(t *testing.T) {
	for _, test := range []struct {
		name, body string
		detector   creationDetector
		status     int
		code       string
	}{
		{"low confidence", `{"url":"https://example.com"}`, creationDetector{kind: "web", confidence: .34}, 422, "type_required"},
		{"unconfigured", `{"url":"https://example.com"}`, creationDetector{err: services.ErrDetectionUnavailable}, 503, "detection_unavailable"},
		{"upstream", `{"url":"https://example.com"}`, creationDetector{err: services.ErrDetectionFailed}, 502, "detection_failed"},
		{"deadline", `{"url":"https://example.com"}`, creationDetector{err: context.DeadlineExceeded}, 504, "resolution_timeout"},
		{"unknown explicit type", `{"url":"https://example.com","type":"unknown"}`, creationDetector{}, 400, "invalid_creation"},
		{"empty selection", `{"url":"https://example.com","types":[]}`, creationDetector{}, 400, "invalid_creation"},
		{"conflicting fields", `{"url":"https://example.com","type":"web","types":["inst"]}`, creationDetector{}, 400, "invalid_creation"},
		{"credentials", `{"url":"https://user:pass@example.com","type":"web"}`, creationDetector{}, 400, "invalid_creation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &fakeLibraryState{addBatch: func(context.Context, int, []library.CreateFeedInput) ([]library.AddFeedResult, error) {
				t.Fatal("write on resolution/validation error")
				return nil, errors.New("unexpected")
			}}
			h := NewLibraryHandler(fakeFeedRepository{state}, fakeItemRepository{state}, nil, &services.FeedCreator{Detector: test.detector})
			w := creationRequest(h, 1, test.body)
			var result feedCreationError
			if w.Code != test.status || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Error.Code != test.code {
				t.Fatalf("response = %d %s", w.Code, w.Body.String())
			}
		})
	}
	h := newTestLibraryHandler(&fakeLibraryState{}, nil)
	for _, test := range []struct {
		user   int
		body   string
		status int
	}{{0, `{}`, 401}, {1, `{"unknown":1}`, 400}, {1, `{} {}`, 400}, {1, `[]`, 400}} {
		if w := creationRequest(h, test.user, test.body); w.Code != test.status {
			t.Fatalf("response = %d %s", w.Code, w.Body.String())
		}
	}
}
