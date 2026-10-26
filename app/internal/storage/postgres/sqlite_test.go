package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/storage/repositorytest"
	"gkfeed/api/internal/testschema"
)

func TestLibraryRepositoryWithSQLiteFixtures(t *testing.T) {
	repositorytest.Run(t, func(t *testing.T) repositorytest.Fixture {
		database := newTestDB(t)
		return repositorytest.Fixture{
			Feeds: NewFeedRepository(database),
			Items: NewItemRepository(database),
			InsertItems: func(t *testing.T, feedID, count int) []int {
				return insertItems(t, database, feedID, count)
			},
		}
	})
}

func TestFeedDeleteRollsBackOnFailure(t *testing.T) {
	database := newTestDB(t)
	feeds := NewFeedRepository(database)
	items := NewItemRepository(database)
	feed, _ := feeds.Add(t.Context(), 1, library.CreateFeedInput{Title: "owner"})
	id := insertItems(t, database, feed.ID, 1)[0]
	if _, err := database.Exec(`CREATE TRIGGER reject_feed_delete BEFORE DELETE ON feed BEGIN SELECT RAISE(ABORT, 'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := feeds.Delete(t.Context(), 1, feed.ID); err == nil {
		t.Fatal("Delete() succeeded despite trigger")
	}
	if _, err := items.Get(t.Context(), 1, id); err != nil {
		t.Fatalf("item deletion was not rolled back: %v", err)
	}
}

func TestSummaryPageDoesNotReadText(t *testing.T) {
	database := newTestDB(t)
	database.SetMaxOpenConns(1)
	feed, err := NewFeedRepository(database).Add(t.Context(), 1, library.CreateFeedInput{Title: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	id := insertItems(t, database, feed.ID, 1)[0]
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Ask SQLite to reject any read of item.text. This verifies that the
	// repository excludes the column, rather than loading and discarding it.
	err = connection.Raw(func(driverConnection any) error {
		driverConnection.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(operation int, table, column, _ string) int {
			if operation == sqlite3.SQLITE_READ && table == "item" && column == "text" {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	})
	connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	items := NewItemRepository(database)
	summaries, err := items.ListSummaryPage(t.Context(), 1, nil, 1)
	if err != nil || len(summaries) != 1 || summaries[0].ID != id {
		t.Fatalf("summary with text reads denied = %#v, %v", summaries, err)
	}
	if _, err := items.ListPage(t.Context(), 1, nil, 1); err == nil {
		t.Fatal("full page succeeded despite denying reads of text")
	}
}

func TestFeedBatchRollsBackOnSecondInsertFailure(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%v", existing), func(t *testing.T) {
			database := newTestDB(t)
			feeds := NewFeedRepository(database)
			posts := library.CreateFeedInput{Title: "Original", Type: "inst", URL: "https://www.instagram.com/example"}
			if existing {
				if _, err := feeds.Add(t.Context(), 1, posts); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := database.Exec(`CREATE TRIGGER reject_stories BEFORE INSERT ON feed WHEN NEW.type = 'stories' BEGIN SELECT RAISE(ABORT, 'rejected'); END`); err != nil {
				t.Fatal(err)
			}
			posts.Title = "Replacement"
			stories := posts
			stories.Type = "stories"
			if results, err := feeds.AddBatch(t.Context(), 1, []library.CreateFeedInput{posts, stories}); err == nil || results != nil {
				t.Fatalf("AddBatch = %#v, %v", results, err)
			}
			listed, err := feeds.List(t.Context(), 1)
			want := 0
			if existing {
				want = 1
			}
			if err != nil || len(listed) != want {
				t.Fatalf("failed batch left records: %#v, %v", listed, err)
			}
			if existing && listed[0].Title != "Original" {
				t.Fatalf("existing feed modified: %#v", listed)
			}
		})
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", t.TempDir()+"/test.sqlite?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(8)
	t.Cleanup(func() { database.Close() })
	testschema.Init(t, database)
	return database
}

func insertItems(t *testing.T, database *sql.DB, feedID, count int) []int {
	t.Helper()
	ids := make([]int, 0, count)
	for i := range count {
		result, err := database.Exec(`INSERT INTO item (feed_id, title, text, date, link) VALUES (?, ?, '', ?, '')`,
			feedID, fmt.Sprintf("item-%d", i), time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		ids = append(ids, int(id))
	}
	return ids
}
