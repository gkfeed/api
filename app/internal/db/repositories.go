package db

import (
	"database/sql"
	"errors"
)

// Repositories borrow the pool owned by the composition root. They never close it.
type UserRepository struct{ database *sql.DB }

func NewUserRepository(database *sql.DB) *UserRepository {
	return &UserRepository{database: database}
}

type CredentialRepository struct{ database *sql.DB }

func NewCredentialRepository(database *sql.DB) *CredentialRepository {
	return &CredentialRepository{database: database}
}

type RefreshTokenRepository struct{ database *sql.DB }

func NewRefreshTokenRepository(database *sql.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{database: database}
}

type BrowserSessionRepository struct{ database *sql.DB }

func NewBrowserSessionRepository(database *sql.DB) *BrowserSessionRepository {
	return &BrowserSessionRepository{database: database}
}

func requireDatabase(database *sql.DB) (*sql.DB, error) {
	if database == nil {
		return nil, errors.New("database is not configured")
	}
	return database, nil
}
