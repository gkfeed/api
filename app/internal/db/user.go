package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gkfeed/api/internal/models"
)

func (r *UserRepository) GetUserFromDB(ctx context.Context, name string) (models.User, error) {
	database, err := requireDatabase(r.database)
	if err != nil {
		return models.User{}, fmt.Errorf("open database: %w", err)
	}
	var user models.User
	err = database.QueryRowContext(ctx,
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

func (r *UserRepository) GetUserFromDBByID(ctx context.Context, id int) (models.User, error) {
	database, err := requireDatabase(r.database)
	if err != nil {
		return models.User{}, fmt.Errorf("open database: %w", err)
	}
	var user models.User
	err = database.QueryRowContext(ctx,
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
