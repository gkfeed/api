package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"gkfeed/api/internal/library"
)

func TestPostgresSyncPaginationAndChanges(t *testing.T) {
	admin, app := postgresFixture(t)
	feeds, items := NewFeedRepository(app), NewItemRepository(app)
	owner := postgresSyncFeed(t, feeds, 1)
	foreign := postgresSyncFeed(t, feeds, 2)
	ids := make([]int, 4)
	for i := range ids {
		ids[i] = postgresSyncInsert(t, admin, owner, fmt.Sprintf("item-%d", i))
	}
	foreignID := postgresSyncInsert(t, admin, foreign, "foreign")
	first, err := items.SyncPage(t.Context(), 1, 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.MaxID != ids[3] || first.LastID != ids[2] || !slices.Equal(syncItemIDs(first.Items), []int{ids[3], ids[2]}) {
		t.Fatalf("first page = %#v", first)
	}
	var sequence int64
	if err := admin.QueryRowContext(t.Context(), "SELECT max(id) FROM item_changes WHERE user_id=1").Scan(&sequence); err != nil || first.Sequence != sequence {
		t.Fatalf("snapshot sequence = %d, want %d: %v", first.Sequence, sequence, err)
	}
	inserted := postgresSyncInsert(t, admin, owner, "new")
	if _, err := admin.ExecContext(t.Context(), "UPDATE item SET title='updated', text='body', link='https://item' WHERE id=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := items.Delete(t.Context(), 1, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err := items.Delete(t.Context(), 1, foreignID); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("foreign delete = %v", err)
	}
	second, err := items.SyncPage(t.Context(), 1, first.LastID, first.MaxID, 2)
	if err != nil || second.HasMore || second.MaxID != first.MaxID || !slices.Equal(syncItemIDs(second.Items), []int{ids[0]}) {
		t.Fatalf("second page = %#v, %v", second, err)
	}
	// The ceiling excludes later inserts; pages reflect current updates/deletes.
	if second.Items[0].Title != "updated" {
		t.Fatalf("updated page item = %#v", second.Items[0])
	}
	changes, err := items.Changes(t.Context(), 1, first.Sequence, 2)
	if err != nil || !changes.HasMore || len(changes.Changes) != 2 {
		t.Fatalf("first changes = %#v, %v", changes, err)
	}
	if changes.Changes[0].ItemID != inserted || changes.Changes[0].Item == nil || changes.Changes[0].Item.Title != "new" {
		t.Fatalf("insert change = %#v", changes.Changes[0])
	}
	updated := changes.Changes[1].Item
	if updated == nil || updated.ID != ids[0] || updated.FeedID != owner || updated.Title != "updated" || updated.Text != "body" || updated.Link != "https://item" || !updated.Date.Equal(postgresSyncDate) {
		t.Fatalf("update payload = %#v", updated)
	}
	if changes.Changes[0].Sequence <= first.Sequence || changes.Changes[1].Sequence <= changes.Changes[0].Sequence || changes.Sequence != changes.Changes[1].Sequence {
		t.Fatalf("change ordering = %#v", changes)
	}
	deleted, err := items.Changes(t.Context(), 1, changes.Sequence, 2)
	if err != nil || deleted.HasMore || len(deleted.Changes) != 1 || deleted.Changes[0].ItemID != ids[1] || deleted.Changes[0].Item != nil || deleted.Sequence <= changes.Sequence {
		t.Fatalf("delete changes = %#v, %v", deleted, err)
	}
	empty, err := items.Changes(t.Context(), 1, deleted.Sequence, 2)
	if err != nil || empty.HasMore || len(empty.Changes) != 0 || empty.Sequence != deleted.Sequence {
		t.Fatalf("empty changes = %#v, %v", empty, err)
	}
	foreignPage, err := items.SyncPage(t.Context(), 2, 0, 0, 2)
	if err != nil || foreignPage.HasMore || !slices.Equal(syncItemIDs(foreignPage.Items), []int{foreignID}) {
		t.Fatalf("foreign snapshot = %#v, %v", foreignPage, err)
	}
	foreignChanges, err := items.Changes(t.Context(), 2, 0, 10)
	if err != nil || len(foreignChanges.Changes) != 1 || foreignChanges.Changes[0].ItemID != foreignID {
		t.Fatalf("foreign changes = %#v, %v", foreignChanges, err)
	}
}

func TestPostgresSyncFeedDeletion(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprintf("direct_cascade=%v", direct), func(t *testing.T) {
			admin, app := postgresFixture(t)
			feeds, items := NewFeedRepository(app), NewItemRepository(app)
			owner, foreign := postgresSyncFeed(t, feeds, 1), postgresSyncFeed(t, feeds, 2)
			ids := []int{postgresSyncInsert(t, admin, owner, "one"), postgresSyncInsert(t, admin, owner, "two")}
			foreignID := postgresSyncInsert(t, admin, foreign, "foreign")
			before, err := items.SyncPage(t.Context(), 1, 0, 0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if direct {
				// Exercise infra's FK cascade, independently of repository pre-deletion.
				_, err = app.ExecContext(t.Context(), "DELETE FROM feed WHERE id=$1 AND user_id=1", owner)
			} else {
				err = feeds.Delete(t.Context(), 1, owner)
			}
			if err != nil {
				t.Fatal(err)
			}
			changes, err := items.Changes(t.Context(), 1, before.Sequence, 10)
			if err != nil || changes.HasMore || len(changes.Changes) != 2 {
				t.Fatalf("cascade changes = %#v, %v", changes, err)
			}
			deleted := make([]int, 0, len(changes.Changes))
			for _, change := range changes.Changes {
				if change.Item != nil || change.Sequence <= before.Sequence {
					t.Fatalf("cascade change = %#v", change)
				}
				deleted = append(deleted, change.ItemID)
			}
			slices.Sort(deleted)
			if !slices.Equal(deleted, ids) {
				t.Fatalf("cascade tombstones = %v, want %v", deleted, ids)
			}
			remaining, err := items.SyncPage(t.Context(), 1, 0, 0, 10)
			if err != nil || len(remaining.Items) != 0 || remaining.MaxID != 0 {
				t.Fatalf("after cascade = %#v, %v", remaining, err)
			}
			if _, err := items.Get(t.Context(), 2, foreignID); err != nil {
				t.Fatalf("foreign item after cascade: %v", err)
			}
		})
	}
}

func TestPostgresSyncSnapshotConsistency(t *testing.T) {
	admin, app := postgresFixture(t)
	items := NewItemRepository(app)
	owner := postgresSyncFeed(t, NewFeedRepository(app), 1)
	initialID := postgresSyncInsert(t, admin, owner, "initial")
	initial, err := items.SyncPage(t.Context(), 1, 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	writer, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	inserted := postgresSyncInsert(t, writer, owner, "concurrent")
	locker, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback()
	var writerPID, lockerPID, readerPID int
	if err := writer.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&writerPID); err != nil {
		t.Fatal(err)
	}
	if err := locker.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&lockerPID); err != nil {
		t.Fatal(err)
	}
	app.SetMaxOpenConns(1)
	if err := app.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&readerPID); err != nil {
		t.Fatal(err)
	}
	locked := make(chan error, 1)
	go func() {
		_, err := locker.ExecContext(ctx, "LOCK TABLE feed IN ACCESS EXCLUSIVE MODE")
		locked <- err
	}()
	postgresSyncWaitBlocked(t, ctx, admin, lockerPID, writerPID)
	type pageResult struct {
		page library.SyncPage
		err  error
	}
	read := make(chan pageResult, 1)
	go func() {
		page, err := items.SyncPage(ctx, 1, 0, 0, 10)
		read <- pageResult{page, err}
	}()
	// SyncPage has read the change sequence and now waits on its item ceiling.
	// Publish the insert between those reads to verify REPEATABLE READ semantics.
	postgresSyncWaitBlocked(t, ctx, admin, readerPID, lockerPID)
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-locked; err != nil {
		t.Fatal(err)
	}
	if err := locker.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-read
	if result.err != nil || result.page.Sequence != initial.Sequence || result.page.MaxID != initialID || !slices.Equal(syncItemIDs(result.page.Items), []int{initialID}) {
		t.Fatalf("mixed snapshot = %#v, %v", result.page, result.err)
	}
	changes, err := items.Changes(ctx, 1, result.page.Sequence, 10)
	if err != nil || len(changes.Changes) != 1 || changes.Changes[0].ItemID != inserted || changes.Changes[0].Item == nil {
		t.Fatalf("concurrent insert missing from changes = %#v, %v", changes, err)
	}
}

func TestPostgresSyncConcurrentCommitOrder(t *testing.T) {
	admin, app := postgresFixture(t)
	feeds, items := NewFeedRepository(app), NewItemRepository(app)
	owner, foreign := postgresSyncFeed(t, feeds, 1), postgresSyncFeed(t, feeds, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	first, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback()
	firstID := postgresSyncInsert(t, first, owner, "first")
	second, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback()
	var firstPID, secondPID int
	if err := first.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&firstPID); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	type insertResult struct {
		id  int
		err error
	}
	done := make(chan insertResult, 1)
	go func() {
		var result insertResult
		result.err = second.QueryRowContext(ctx, "INSERT INTO item (feed_id,title,text,date,link) VALUES ($1,'second','',$2,'') RETURNING id", owner, postgresSyncDate).Scan(&result.id)
		done <- result
	}()
	// Observe the actual lock dependency; scheduling delays cannot make this pass.
	postgresSyncWaitBlocked(t, ctx, admin, secondPID, firstPID)
	// Other users must remain writable while this user's trigger holds its lock.
	var foreignID int
	if err := admin.QueryRowContext(ctx, "INSERT INTO item (feed_id,title,text,date,link) VALUES ($1,'foreign','',$2,'') RETURNING id", foreign, postgresSyncDate).Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	before, err := items.SyncPage(ctx, 1, 0, 0, 10)
	if err != nil || len(before.Items) != 0 || before.MaxID != 0 || before.Sequence != 0 {
		t.Fatalf("uncommitted snapshot = %#v, %v", before, err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	committed, err := items.Changes(ctx, 1, before.Sequence, 10)
	if err != nil || committed.HasMore || len(committed.Changes) != 1 || committed.Changes[0].ItemID != firstID {
		t.Fatalf("first commit = %#v, %v", committed, err)
	}
	if err := second.Commit(); err != nil {
		t.Fatal(err)
	}
	later, err := items.Changes(ctx, 1, committed.Sequence, 10)
	if err != nil || later.HasMore || len(later.Changes) != 1 || later.Changes[0].ItemID != result.id || later.Sequence <= committed.Sequence {
		t.Fatalf("second commit was lost after cursor advance: %#v, %v", later, err)
	}
	foreignChanges, err := items.Changes(ctx, 2, 0, 10)
	if err != nil || len(foreignChanges.Changes) != 1 || foreignChanges.Changes[0].ItemID != foreignID {
		t.Fatalf("independent user commit = %#v, %v", foreignChanges, err)
	}
}

var postgresSyncDate = time.Date(2026, 10, 25, 12, 0, 0, 123456000, time.UTC)

func postgresSyncFeed(t *testing.T, feeds *FeedRepository, userID int) int {
	t.Helper()
	feed, err := feeds.Add(t.Context(), userID, library.CreateFeedInput{Title: "sync", Type: "rss", URL: "https://sync.example"})
	if err != nil {
		t.Fatal(err)
	}
	return feed.ID
}

func postgresSyncInsert(t *testing.T, db feedQuerier, feedID int, title string) int {
	t.Helper()
	var id int
	if err := db.QueryRowContext(t.Context(), "INSERT INTO item (feed_id,title,text,date,link) VALUES ($1,$2,'',$3,'') RETURNING id", feedID, title, postgresSyncDate).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func syncItemIDs(items []library.Item) []int {
	ids := make([]int, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func postgresSyncWaitBlocked(t *testing.T, ctx context.Context, admin *sql.DB, waiter, blocker int) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := admin.QueryRowContext(ctx, "SELECT $1 = ANY(pg_blocking_pids($2))", blocker, waiter).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("transaction %d never blocked on %d: %v", waiter, blocker, ctx.Err())
		case <-ticker.C:
		}
	}
}
