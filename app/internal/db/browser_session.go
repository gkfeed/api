package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gkfeed/api/internal/models"
)

var ErrInvalidBrowserSession = errors.New("invalid browser session")

func CreateBrowserSession(ctx context.Context, hash []byte, userID int, now, expiresAt time.Time) error {
	database, err := getDB()
	if err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, `INSERT INTO browser_sessions
 (token_hash, user_id, created_at, last_seen_at, expires_at) VALUES ($1, $2, $3, $3, $4)`, hash, userID, now, expiresAt)
	return err
}

// TouchBrowserSession checks expiry and advances activity in the same statement.
// A concurrent delete cannot be undone by a late authentication request.
func TouchBrowserSession(ctx context.Context, hash []byte, now time.Time, idleTTL time.Duration) (models.User, time.Time, error) {
	database, err := getDB()
	if err != nil {
		return models.User{}, time.Time{}, err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return models.User{}, time.Time{}, err
	}
	defer tx.Rollback()
	var user models.User
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `UPDATE browser_sessions
 SET last_seen_at = CASE WHEN last_seen_at < $1 THEN $1 ELSE last_seen_at END
 WHERE token_hash = $2 AND last_seen_at > $3 AND expires_at > $1
 RETURNING user_id, expires_at`, now, hash, now.Add(-idleTTL)).Scan(&user.ID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, time.Time{}, ErrInvalidBrowserSession
	}
	if err != nil {
		return models.User{}, time.Time{}, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT name FROM users WHERE id = $1", user.ID).Scan(&user.Name); err != nil {
		return models.User{}, time.Time{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.User{}, time.Time{}, err
	}
	return user, expiresAt, nil
}

func DeleteBrowserSession(ctx context.Context, hash []byte) error {
	database, err := getDB()
	if err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, "DELETE FROM browser_sessions WHERE token_hash = $1", hash)
	return err
}

// Infra owns DDL. Fail before serving requests if its browser-session contract
// has not been installed or the API role cannot read the required columns.
func CheckBrowserSessionSchema(ctx context.Context) error {
	database, err := getDB()
	if err != nil {
		return err
	}
	rows, err := database.QueryContext(ctx, `SELECT token_hash, user_id, created_at, last_seen_at, expires_at
 FROM browser_sessions LIMIT 0`)
	if err != nil {
		return fmt.Errorf("browser_sessions schema is required; apply the infra browser-session migration: %w", err)
	}
	return rows.Close()
}

func DeleteUserBrowserSessions(ctx context.Context, userID int) error {
	database, err := getDB()
	if err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, "DELETE FROM browser_sessions WHERE user_id = $1", userID)
	return err
}
