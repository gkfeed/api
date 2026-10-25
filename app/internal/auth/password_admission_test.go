package auth

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"gkfeed/api/internal/models"
)

func passwordUser(context.Context, string) (models.User, error) {
	return models.User{ID: 7, HashedPassword: sql.NullString{String: "fake-hash", Valid: true}}, nil
}

func passwordResult(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("password operation did not finish")
		return nil
	}
}

func TestPasswordAdmissionBoundsVerificationThroughCancellation(t *testing.T) {
	started := make(chan error, maxPasswordVerifications)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	verifier := passwordVerifier{
		slots: make(chan struct{}, maxPasswordVerifications),
		compare: func(_, password string) bool {
			started <- nil
			<-release
			return password == "secret"
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results := make(chan error, maxPasswordVerifications)
	for range maxPasswordVerifications {
		go func() {
			user, ok, err := verifier.authenticate(ctx, "reader", "secret", passwordUser)
			if ok || user.ID != 0 {
				t.Error("cancelled authentication published a user")
			}
			results <- err
		}()
		passwordResult(t, started)
	}
	cancel()

	// Cancellation must not free slots while the fake Argon2 calls still run.
	// Excess requests must finish without looking up the supplied account.
	overloaded := make(chan error, 32)
	for range cap(overloaded) {
		go func() {
			_, _, err := verifier.authenticate(t.Context(), "missing", "wrong", func(context.Context, string) (models.User, error) {
				t.Error("overloaded request reached lookup")
				return models.User{}, sql.ErrNoRows
			})
			overloaded <- err
		}()
	}
	for range cap(overloaded) {
		if err := passwordResult(t, overloaded); !errors.Is(err, ErrPasswordVerificationBusy) {
			t.Fatalf("overload = %v, want busy", err)
		}
	}
	if _, _, err := verifier.authenticate(ctx, "reader", "secret", passwordUser); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission = %v", err)
	}
	unblock()
	for range maxPasswordVerifications {
		if err := passwordResult(t, results); !errors.Is(err, context.Canceled) {
			t.Fatalf("running verification = %v", err)
		}
	}
	// Once computation ends, the same slots admit successful and failed checks.
	for _, password := range []string{"secret", "wrong"} {
		user, ok, err := verifier.authenticate(t.Context(), "reader", password, passwordUser)
		if err != nil || ok != (password == "secret") || user.ID != 7 {
			t.Fatalf("authentication after overload = %v, %v, %v", user, ok, err)
		}
	}
}

func TestPasswordAdmissionSkipsVerificationAfterCancelledLookup(t *testing.T) {
	verifier := passwordVerifier{
		slots: make(chan struct{}, maxPasswordVerifications),
		compare: func(string, string) bool {
			t.Error("cancelled lookup started verification")
			return true
		},
	}
	for range maxPasswordVerifications + 1 {
		ctx, cancel := context.WithCancel(t.Context())
		_, ok, err := verifier.authenticate(ctx, "reader", "secret", func(context.Context, string) (models.User, error) {
			cancel()
			return passwordUser(ctx, "reader")
		})
		cancel()
		if ok || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled lookup = %v, %v", ok, err)
		}
	}
}
