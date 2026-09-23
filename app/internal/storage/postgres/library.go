package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gkfeed/api/internal/library"
)

func (r *ItemRepository) SyncPage(ctx context.Context, userID, afterID, maxID, limit int) (library.SyncPage, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return library.SyncPage{}, fmt.Errorf("begin items snapshot: %w", err)
	}
	defer tx.Rollback()
	var sequence int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM item_changes WHERE user_id = $1", userID).Scan(&sequence); err != nil {
		return library.SyncPage{}, fmt.Errorf("read change sequence: %w", err)
	}
	if maxID == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(item.id), 0) FROM item JOIN feed ON item.feed_id = feed.id WHERE feed.user_id = $1`, userID).Scan(&maxID); err != nil {
			return library.SyncPage{}, fmt.Errorf("read item ceiling: %w", err)
		}
	}
	query := `SELECT ` + itemColumns + ` FROM item JOIN feed ON item.feed_id = feed.id WHERE feed.user_id = $1 AND item.id <= $2`
	args := []any{userID, maxID}
	if afterID > 0 {
		query += " AND item.id < $3"
		args = append(args, afterID)
	}
	query += fmt.Sprintf(" ORDER BY item.id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return library.SyncPage{}, fmt.Errorf("query items snapshot: %w", err)
	}
	items := make([]library.Item, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanItem(rows)
		if scanErr != nil {
			rows.Close()
			return library.SyncPage{}, fmt.Errorf("scan snapshot item: %w", scanErr)
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return library.SyncPage{}, fmt.Errorf("iterate snapshot items: %w", err)
	}
	page := library.SyncPage{Items: items, MaxID: maxID, Sequence: sequence, HasMore: len(items) > limit}
	if page.HasMore {
		page.Items = items[:limit]
	}
	if len(page.Items) > 0 {
		page.LastID = page.Items[len(page.Items)-1].ID
	}
	if err := tx.Commit(); err != nil {
		return library.SyncPage{}, fmt.Errorf("commit items snapshot: %w", err)
	}
	return page, nil
}

func (r *ItemRepository) Changes(ctx context.Context, userID int, afterSequence int64, limit int) (library.ChangesPage, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, item_id, payload FROM item_changes WHERE user_id = $1 AND id > $2 ORDER BY id ASC LIMIT $3`, userID, afterSequence, limit+1)
	if err != nil {
		return library.ChangesPage{}, fmt.Errorf("query item changes: %w", err)
	}
	defer rows.Close()
	changes := make([]library.Change, 0, limit+1)
	for rows.Next() {
		var change library.Change
		var payload []byte
		if err := rows.Scan(&change.Sequence, &change.ItemID, &payload); err != nil {
			return library.ChangesPage{}, fmt.Errorf("scan item change: %w", err)
		}
		if len(payload) > 0 {
			var stored struct {
				ID     int
				FeedID int
				Title  string
				Text   string
				Date   string
				Link   string
			}
			if err := json.Unmarshal(payload, &stored); err != nil {
				return library.ChangesPage{}, fmt.Errorf("decode item change: %w", err)
			}
			item := library.Item{ID: stored.ID, FeedID: stored.FeedID, Title: stored.Title, Text: stored.Text, Link: stored.Link}
			if stored.Date != "" {
				for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999", "2006-01-02"} {
					item.Date, err = time.Parse(layout, stored.Date)
					if err == nil {
						break
					}
				}
				if err != nil {
					return library.ChangesPage{}, fmt.Errorf("parse item change date %q: %w", stored.Date, err)
				}
			}
			change.Item = &item
		}
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return library.ChangesPage{}, fmt.Errorf("iterate item changes: %w", err)
	}
	page := library.ChangesPage{Changes: changes, Sequence: afterSequence, HasMore: len(changes) > limit}
	if page.HasMore {
		page.Changes = changes[:limit]
	}
	if len(page.Changes) > 0 {
		page.Sequence = page.Changes[len(page.Changes)-1].Sequence
	}
	return page, nil
}

const (
	feedColumns = "feed.id, feed.title, feed.url, feed.type, feed.user_id"
	itemColumns = "item.id, item.feed_id, item.title, item.text, item.date, item.link"
)

type FeedRepository struct{ db *sql.DB }

func NewFeedRepository(db *sql.DB) *FeedRepository { return &FeedRepository{db: db} }

func (r *FeedRepository) List(ctx context.Context, userID int) ([]library.Feed, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+feedColumns+" FROM feed WHERE user_id = $1 ORDER BY id DESC", userID)
	if err != nil {
		return nil, fmt.Errorf("query feeds: %w", err)
	}
	defer rows.Close()

	feeds := make([]library.Feed, 0)
	for rows.Next() {
		feed, err := scanFeed(rows)
		if err != nil {
			return nil, fmt.Errorf("scan feed: %w", err)
		}
		feeds = append(feeds, feed)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feeds: %w", err)
	}
	return feeds, nil
}

func (r *FeedRepository) Add(ctx context.Context, userID int, input library.CreateFeedInput) (library.AddFeedResult, error) {
	feed, err := scanFeed(r.db.QueryRowContext(ctx,
		"INSERT INTO feed (title, type, url, user_id) VALUES ($1, $2, $3, $4) ON CONFLICT (user_id, url, type) DO NOTHING RETURNING "+feedColumns,
		input.Title, input.Type, input.URL, userID))
	if err == nil {
		return library.AddFeedResult{Feed: feed, Created: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return library.AddFeedResult{}, fmt.Errorf("insert feed: %w", err)
	}
	// Use a separate statement so a concurrent insert that won the conflict
	// is visible after it commits. A no-op UPDATE would require feed UPDATE
	// privileges, which the infra application role intentionally does not have.
	feed, err = scanFeed(r.db.QueryRowContext(ctx, "SELECT "+feedColumns+" FROM feed WHERE user_id = $1 AND url = $2 AND type = $3", userID, input.URL, input.Type))
	if err != nil {
		return library.AddFeedResult{}, fmt.Errorf("get existing feed: %w", err)
	}
	return library.AddFeedResult{Feed: feed}, nil
}

func (r *FeedRepository) Delete(ctx context.Context, userID, feedID int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin feed delete: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM feed WHERE id = $1 AND user_id = $2", feedID, userID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return library.ErrNotFound
		}
		return fmt.Errorf("find feed: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM item WHERE feed_id = $1", feedID); err != nil {
		return fmt.Errorf("delete feed items: %w", err)
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM feed WHERE id = $1 AND user_id = $2", feedID, userID)
	if err != nil {
		return fmt.Errorf("delete feed: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted feeds: %w", err)
	}
	if affected != 1 {
		return library.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit feed delete: %w", err)
	}
	return nil
}

type ItemRepository struct{ db *sql.DB }

func NewItemRepository(db *sql.DB) *ItemRepository { return &ItemRepository{db: db} }

func (r *ItemRepository) Get(ctx context.Context, userID, itemID int) (library.ItemDetails, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+itemColumns+`, `+feedColumns+`
		FROM item JOIN feed ON item.feed_id = feed.id
		WHERE item.id = $1 AND feed.user_id = $2`, itemID, userID)
	item, feed, err := scanItemWithFeed(row)
	if errors.Is(err, sql.ErrNoRows) {
		return library.ItemDetails{}, library.ErrNotFound
	}
	if err != nil {
		return library.ItemDetails{}, fmt.Errorf("get item: %w", err)
	}
	return library.ItemDetails{Item: item, Feed: feed}, nil
}

func (r *ItemRepository) List(ctx context.Context, userID int) ([]library.Item, error) {
	return r.list(ctx, `SELECT `+itemColumns+`
		FROM item JOIN feed ON item.feed_id = feed.id
		WHERE feed.user_id = $1 ORDER BY item.id DESC`, userID)
}

func (r *ItemRepository) ListPage(ctx context.Context, userID int, cursor *int, limit int) ([]library.Item, error) {
	query := `SELECT ` + itemColumns + `
		FROM item JOIN feed ON item.feed_id = feed.id
		WHERE feed.user_id = $1`
	args := []any{userID}
	if cursor != nil {
		query += " AND item.id < $2"
		args = append(args, *cursor)
	}
	query += fmt.Sprintf(" ORDER BY item.id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)
	return r.list(ctx, query, args...)
}

func (r *ItemRepository) Delete(ctx context.Context, userID, itemID int) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM item WHERE id = $1 AND EXISTS (
		SELECT 1 FROM feed WHERE feed.id = item.feed_id AND feed.user_id = $2
	)`, itemID, userID)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted items: %w", err)
	}
	if affected == 0 {
		return library.ErrNotFound
	}
	return nil
}

func (r *ItemRepository) list(ctx context.Context, query string, args ...any) ([]library.Item, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()
	items := make([]library.Item, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}
	return items, nil
}

type rowScanner interface{ Scan(...any) error }

func scanFeed(row rowScanner) (library.Feed, error) {
	var feed library.Feed
	err := row.Scan(&feed.ID, &feed.Title, &feed.URL, &feed.Type, &feed.UserID)
	return feed, err
}

func scanItem(row rowScanner) (library.Item, error) {
	var item library.Item
	err := row.Scan(&item.ID, &item.FeedID, &item.Title, &item.Text, &item.Date, &item.Link)
	return item, err
}

func scanItemWithFeed(row rowScanner) (library.Item, library.Feed, error) {
	var item library.Item
	var feed library.Feed
	err := row.Scan(&item.ID, &item.FeedID, &item.Title, &item.Text, &item.Date, &item.Link,
		&feed.ID, &feed.Title, &feed.URL, &feed.Type, &feed.UserID)
	return item, feed, err
}
