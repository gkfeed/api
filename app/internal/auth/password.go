package auth

import (
	"context"
	"database/sql"
	"errors"

	"gkfeed/api/internal/models"
	"gkfeed/api/internal/passwordhash"
)

type PasswordUserRepository interface {
	GetUserFromDB(context.Context, string) (models.User, error)
}

type PasswordAuthenticator struct{ users PasswordUserRepository }

func NewPasswordAuthenticator(users PasswordUserRepository) *PasswordAuthenticator {
	return &PasswordAuthenticator{users: users}
}

func (a *PasswordAuthenticator) AuthenticatePassword(ctx context.Context, username, password string) (models.User, bool, error) {
	user, err := a.users.GetUserFromDB(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, false, nil
	}
	if err != nil {
		return models.User{}, false, err
	}

	authenticated := user.HashedPassword.Valid && passwordhash.ComparePassword(user.HashedPassword.String, password)
	return user, authenticated, nil
}
