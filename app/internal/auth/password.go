package auth

import (
	"database/sql"
	"errors"

	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"
	"gkfeed/api/internal/passwordhash"
)

var getUser = db.GetUserFromDB

func authenticateWithDB(username, password string) (models.User, bool, error) {
	user, err := getUser(username)
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, false, nil
	}
	if err != nil {
		return models.User{}, false, err
	}

	authenticated := user.HashedPassword.Valid && passwordhash.ComparePassword(user.HashedPassword.String, password)
	return user, authenticated, nil
}

func AuthenticatePassword(username, password string) (models.User, bool, error) {
	return authenticateWithDB(username, password)
}
