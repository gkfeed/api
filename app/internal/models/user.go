package models

import "database/sql"

type User struct {
	ID             int            `json:"id"`
	Name           string         `json:"name"`
	HashedPassword sql.NullString `json:"-"`
}
