package auth

import (
	"context"
	"database/sql"
	"errors"

	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"
	"gkfeed/api/internal/passwordhash"
)

var getUser = db.GetUserFromDB

func authenticateWithDB(ctx context.Context, username, password string) (models.User, bool, error) {
	user, err := getUser(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, false, nil
	}
	if err != nil {
		return models.User{}, false, err
	}

	authenticated := user.HashedPassword.Valid && passwordhash.ComparePassword(user.HashedPassword.String, password)
	return user, authenticated, nil
}

func AuthenticatePassword(ctx context.Context, username, password string) (models.User, bool, error) {
	return authenticateWithDB(ctx, username, password)
}
