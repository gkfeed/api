package db

import (
	"database/sql"
	"errors"
	"sync"
)

var (
	databaseMu sync.RWMutex
	database   *sql.DB
)

// Configure temporarily wires legacy auth persistence to the shared pool.
// TODO: move auth repositories to constructor injection.
func Configure(db *sql.DB) {
	databaseMu.Lock()
	database = db
	databaseMu.Unlock()
}

func getDB() (*sql.DB, error) {
	databaseMu.RLock()
	defer databaseMu.RUnlock()
	if database == nil {
		return nil, errors.New("database is not configured")
	}
	return database, nil
}

// ConfiguredDB exists only while legacy auth storage still uses package-level wiring.
func ConfiguredDB() (*sql.DB, error) { return getDB() }
