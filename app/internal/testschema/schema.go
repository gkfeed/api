// Package testschema provisions SQLite fixtures for tests only.
package testschema

import (
	"database/sql"
	"testing"
)

func Init(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, hashed_password TEXT)",
		"CREATE TABLE feed (id INTEGER PRIMARY KEY, title TEXT, url TEXT NOT NULL, type TEXT NOT NULL, user_id INTEGER NOT NULL, UNIQUE(user_id, url, type))",
		"CREATE TABLE item (id INTEGER PRIMARY KEY, feed_id INTEGER, title TEXT, text TEXT, date DATETIME, link TEXT)",
		"CREATE TABLE webauthn_credentials (id BLOB PRIMARY KEY, user_id INTEGER NOT NULL, credential TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', created_at DATETIME DEFAULT CURRENT_TIMESTAMP, last_used_at DATETIME)",
		"CREATE TABLE refresh_tokens (id TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
