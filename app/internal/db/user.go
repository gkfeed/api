package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gkfeed/api/internal/models"
)

// UserRepository borrows the pool owned by the composition root.
type UserRepository struct{ database *sql.DB }

func NewUserRepository(database *sql.DB) *UserRepository {
	return &UserRepository{database: database}
}

func (r *UserRepository) GetUserFromDB(ctx context.Context, name string) (models.User, error) {
	var user models.User
	err := r.database.QueryRowContext(ctx,
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
	var user models.User
	err := r.database.QueryRowContext(ctx,
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
