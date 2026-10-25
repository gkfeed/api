package auth

import (
	"context"
	"database/sql"
	"errors"

	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"
	"gkfeed/api/internal/passwordhash"
)

// Two standard Argon2id checks use about 128 MiB of working memory. Reject
// excess requests immediately instead of retaining passwords in a work queue.
const maxPasswordVerifications = 2

// ErrPasswordVerificationBusy means all process-wide password slots are in use.
// It is independent of whether the supplied username exists.
var ErrPasswordVerificationBusy = errors.New("password verification capacity exhausted")

type passwordVerifier struct {
	slots   chan struct{}
	compare func(encodedHash, password string) bool
}

var getUser = db.GetUserFromDB

var passwordVerification = passwordVerifier{
	slots:   make(chan struct{}, maxPasswordVerifications),
	compare: passwordhash.ComparePassword,
}

func authenticateWithDB(ctx context.Context, username, password string) (models.User, bool, error) {
	return passwordVerification.authenticate(ctx, username, password, getUser)
}

func AuthenticatePassword(ctx context.Context, username, password string) (models.User, bool, error) {
	return authenticateWithDB(ctx, username, password)
}

func (v *passwordVerifier) authenticate(ctx context.Context, username, password string, lookup func(context.Context, string) (models.User, error)) (models.User, bool, error) {
	if err := ctx.Err(); err != nil {
		return models.User{}, false, err
	}
	// Admission precedes lookup so overload cannot reveal account existence or
	// whether an account has a password.
	select {
	case v.slots <- struct{}{}:
		defer func() { <-v.slots }()
	default:
		return models.User{}, false, ErrPasswordVerificationBusy
	}
	if err := ctx.Err(); err != nil {
		return models.User{}, false, err
	}

	user, err := lookup(ctx, username)
	if err := ctx.Err(); err != nil {
		return models.User{}, false, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return models.User{}, false, nil
	}
	if err != nil {
		return models.User{}, false, err
	}
	if !user.HashedPassword.Valid {
		return user, false, nil
	}

	matched := v.compare(user.HashedPassword.String, password)
	// Argon2 cannot be interrupted. Keep the slot until it finishes even if
	// the request is cancelled, and do not publish a cancelled authentication.
	if err := ctx.Err(); err != nil {
		return models.User{}, false, err
	}
	return user, matched, nil
}
