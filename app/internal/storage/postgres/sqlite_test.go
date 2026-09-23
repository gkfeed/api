package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
