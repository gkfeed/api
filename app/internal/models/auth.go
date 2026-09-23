package models

import (
	"time"
)

type RefreshToken struct {
	ID        string
	UserID    int
	ExpiresAt time.Time
	CreatedAt time.Time
}
