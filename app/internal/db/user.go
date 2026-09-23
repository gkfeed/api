package db

import (
	"database/sql"
	"errors"
	"fmt"

	"gkfeed/api/internal/models"
)

func GetUserFromDB(name string) (models.User, error) {
	database, err := getDB()
	if err != nil {
		return models.User{}, fmt.Errorf("open database: %w", err)
	}
	var user models.User
	err = database.QueryRow(
		"SELECT id, name, hashed_password FROM users WHERE name = $1",
		name,
	).Scan(&user.ID, &user.Name, &user.HashedPassword)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, fmt.Errorf("user %q not found: %w", name, err)
	}
	if err != nil {
		return models.User{}, fmt.Errorf("get user %q: %w", name, err)
	}

	return user, nil
}

func GetUserFromDBByID(id int) (models.User, error) {
	database, err := getDB()
	if err != nil {
		return models.User{}, fmt.Errorf("open database: %w", err)
	}
	var user models.User
	err = database.QueryRow(
		"SELECT id, name, hashed_password FROM users WHERE id = $1",
		id,
	).Scan(&user.ID, &user.Name, &user.HashedPassword)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, fmt.Errorf("user %d not found: %w", id, err)
	}
	if err != nil {
		return models.User{}, fmt.Errorf("get user %d: %w", id, err)
	}

	return user, nil
}
