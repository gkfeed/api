package auth

import (
	"context"

	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"
	"gkfeed/api/internal/passwordhash"
)

var getUser = db.GetUserFromDB

var passwordVerification = newPasswordVerifier(maxPasswordVerifications, passwordhash.ComparePassword)

func authenticateWithDB(ctx context.Context, username, password string) (models.User, bool, error) {
	return passwordVerification.authenticate(ctx, username, password, getUser)
}

func AuthenticatePassword(ctx context.Context, username, password string) (models.User, bool, error) {
	return authenticateWithDB(ctx, username, password)
}
