package db

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gkfeed/api/internal/models"
)

var ErrInvalidRefreshToken = errors.New("invalid refresh token")
var ErrRefreshTokenReuse = errors.New("refresh token reuse detected")

// RefreshRotation identifies the user and token family, including when reuse
// or expiry requires the caller to revoke in-memory access sessions.
type RefreshRotation struct {
	User     models.User
	FamilyID []byte
}

type refreshTokenReuseError struct{ RefreshRotation }

func (e *refreshTokenReuseError) Error() string { return ErrRefreshTokenReuse.Error() }
func (e *refreshTokenReuseError) Unwrap() error { return ErrRefreshTokenReuse }

// The infra-owned refresh_tokens table grants the API INSERT and DELETE, but
// not UPDATE. Its id stores the token digest and family as hex. A consumed token
// stays as a tombstone with an expired timestamp so reuse can be detected.
func refreshID(hash, family []byte) string {
	return hex.EncodeToString(hash) + ":" + hex.EncodeToString(family)
}
func refreshPrefix(hash []byte) string   { return hex.EncodeToString(hash) + ":%" }
func familyPattern(family []byte) string { return "%:" + hex.EncodeToString(family) }

func CreateAuthRefreshToken(userID int, tokenHash, familyID []byte, expiresAt time.Time) error {
	tx, err := beginRefreshTransaction()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO refresh_tokens (id, user_id, expires_at) VALUES ($1, $2, $3)", refreshID(tokenHash, familyID), userID, expiresAt)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return tx.Commit()
}

func RotateAuthRefreshToken(oldHash, newHash []byte, now, expiresAt time.Time) (RefreshRotation, error) {
	tx, err := beginRefreshTransaction()
	if err != nil {
		return RefreshRotation{}, err
	}
	defer tx.Rollback()
	var id string
	var userID int
	var name string
	var expiry time.Time
	err = tx.QueryRow(`SELECT token.id, token.user_id, users.name, token.expires_at
 FROM refresh_tokens AS token JOIN users ON users.id = token.user_id
 WHERE token.id LIKE $1`, refreshPrefix(oldHash)).Scan(&id, &userID, &name, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return RefreshRotation{}, fmt.Errorf("%w: token not found", ErrInvalidRefreshToken)
	}
	if err != nil {
		return RefreshRotation{}, fmt.Errorf("query refresh token: %w", err)
	}
	parts := strings.Split(id, ":")
	if len(parts) != 2 {
		return RefreshRotation{}, errors.New("invalid stored refresh token id")
	}
	family, err := hex.DecodeString(parts[1])
	if err != nil {
		return RefreshRotation{}, fmt.Errorf("decode refresh-token family: %w", err)
	}
	rotation := RefreshRotation{User: models.User{ID: userID, Name: name}, FamilyID: family}
	if !expiry.After(now) {
		if err := revokeFamily(tx, family); err != nil {
			return RefreshRotation{}, err
		}
		if err := tx.Commit(); err != nil {
			return RefreshRotation{}, fmt.Errorf("commit refresh-token revocation: %w", err)
		}
		return rotation, &refreshTokenReuseError{rotation}
	}
	if _, err := tx.Exec("DELETE FROM refresh_tokens WHERE id = $1", id); err != nil {
		return RefreshRotation{}, fmt.Errorf("consume refresh token: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO refresh_tokens (id, user_id, expires_at) VALUES ($1, $2, $3)", id, userID, time.Unix(0, 0)); err != nil {
		return RefreshRotation{}, fmt.Errorf("record consumed refresh token: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO refresh_tokens (id, user_id, expires_at) VALUES ($1, $2, $3)", refreshID(newHash, family), userID, expiresAt); err != nil {
		return RefreshRotation{}, fmt.Errorf("insert rotated refresh token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RefreshRotation{}, fmt.Errorf("commit refresh-token rotation: %w", err)
	}
	return rotation, nil
}

func RevokeAuthRefreshToken(tokenHash []byte, _ time.Time) ([]byte, error) {
	tx, err := beginRefreshTransaction()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRow("SELECT id FROM refresh_tokens WHERE id LIKE $1", refreshPrefix(tokenHash)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: token not found", ErrInvalidRefreshToken)
	}
	if err != nil {
		return nil, fmt.Errorf("query refresh-token family: %w", err)
	}
	parts := strings.Split(id, ":")
	if len(parts) != 2 {
		return nil, errors.New("invalid stored refresh token id")
	}
	family, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode refresh-token family: %w", err)
	}
	if err := revokeFamily(tx, family); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refresh-token revocation: %w", err)
	}
	return family, nil
}

func RevokeUserAuthRefreshTokens(userID int, _ time.Time) error {
	tx, err := beginRefreshTransaction()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM refresh_tokens WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("revoke user refresh tokens: %w", err)
	}
	return tx.Commit()
}

// All refresh mutations take one transaction lock. This keeps family
// revocation, logout, and rotation ordered even though the application role
// has no UPDATE permission on the infra-owned refresh_tokens table.
func beginRefreshTransaction() (*sql.Tx, error) {
	database, err := getDB()
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	tx, err := database.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin refresh-token transaction: %w", err)
	}
	if !strings.Contains(fmt.Sprintf("%T", database.Driver()), "sqlite3") {
		if _, err := tx.Exec("SELECT pg_advisory_xact_lock(281474976710655)"); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("lock refresh-token transaction: %w", err)
		}
	}
	return tx, nil
}

func revokeFamily(tx *sql.Tx, family []byte) error {
	rows, err := tx.Query("SELECT id, user_id FROM refresh_tokens WHERE id LIKE $1", familyPattern(family))
	if err != nil {
		return fmt.Errorf("find refresh-token family: %w", err)
	}
	type token struct {
		id     string
		userID int
	}
	var tokens []token
	for rows.Next() {
		var value token
		if err := rows.Scan(&value.id, &value.userID); err != nil {
			rows.Close()
			return fmt.Errorf("scan refresh-token family: %w", err)
		}
		tokens = append(tokens, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read refresh-token family: %w", err)
	}
	rows.Close()
	if _, err := tx.Exec("DELETE FROM refresh_tokens WHERE id LIKE $1", familyPattern(family)); err != nil {
		return fmt.Errorf("revoke refresh-token family: %w", err)
	}
	for _, value := range tokens {
		if _, err := tx.Exec("INSERT INTO refresh_tokens (id, user_id, expires_at) VALUES ($1, $2, $3)", value.id, value.userID, time.Unix(0, 0)); err != nil {
			return fmt.Errorf("record revoked refresh token: %w", err)
		}
	}
	return nil
}
