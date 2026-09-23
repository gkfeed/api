package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/models"
	storage "gkfeed/api/internal/storage/postgres"
	"gkfeed/api/internal/testschema"
)

func TestFeedCreationHandlers(t *testing.T) {
	for _, lazy := range []bool{false, true} {
		name := "add"
		if lazy {
			name = "add_lazy"
		}
		t.Run(name, func(t *testing.T) {
			database, err := sql.Open("sqlite3", t.TempDir()+"/test.sqlite?_busy_timeout=5000")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			database.SetMaxOpenConns(8)
			testschema.Init(t, database)
			service := library.NewService(storage.NewFeedRepository(database), storage.NewItemRepository(database))
			call := func(userID int, kind, url string) (feedMutationResponse, int, error) {
				input := library.CreateFeedInput{Title: "feed", Type: kind, URL: url}
				handler := NewLibraryHandler(service, fakeResolver{input})
				payload := map[string]string{"title": input.Title, "type": kind, "url": url}
				if lazy {
					payload = map[string]string{"url": url}
				}
				body, err := json.Marshal(payload)
				if err != nil {
					return feedMutationResponse{}, 0, err
				}
				request := httptest.NewRequest(http.MethodPost, "/"+name, bytes.NewReader(body))
				request = request.WithContext(auth.WithUser(request.Context(), models.User{ID: userID}))
				response := httptest.NewRecorder()
				if lazy {
					handler.HandleAddFeedLazy(response, request)
				} else {
					handler.HandleAddFeed(response, request)
				}
				var result feedMutationResponse
				err = json.Unmarshal(response.Body.Bytes(), &result)
				return result, response.Code, err
			}
			first, status, err := call(1, "rss", "https://same")
			if err != nil || status != 200 || !first.Created {
				t.Fatalf("first = %#v, %d, %v", first, status, err)
			}
			for _, tc := range []struct {
				user    int
				kind    string
				created bool
			}{
				{1, "rss", false}, {1, "web", true}, {2, "rss", true},
			} {
				result, status, err := call(tc.user, tc.kind, "https://same")
				if err != nil || status != 200 || result.Created != tc.created {
					t.Fatalf("result = %#v, %d, %v", result, status, err)
				}
				if (result.Item.ID == first.Item.ID) == tc.created {
					t.Fatalf("unexpected feed ID: %#v", result)
				}
			}
			const count = 8
			results := make([]feedMutationResponse, count)
			statuses := make([]int, count)
			errs := make([]error, count)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range count {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					results[i], statuses[i], errs[i] = call(1, "rss", "https://concurrent")
				}()
			}
			close(start)
			wg.Wait()
			created := 0
			for i, result := range results {
				if errs[i] != nil || statuses[i] != 200 {
					t.Fatalf("request %d: status %d, %v", i, statuses[i], errs[i])
				}
				if result.Created {
					created++
				}
				if result.Item.ID == 0 || result.Item.ID != results[0].Item.ID {
					t.Fatalf("result = %#v", result)
				}
			}
			if created != 1 {
				t.Fatalf("created = %d", created)
			}
		})
	}
}
