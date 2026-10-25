package auth

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gkfeed/api/internal/models"
)

func passwordUser(context.Context, string) (models.User, error) {
	return models.User{ID: 7, HashedPassword: sql.NullString{String: "fake-hash", Valid: true}}, nil
}

func awaitPasswordResult[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("password operation did not finish")
		var zero T
		return zero
	}
}

func TestPasswordAdmissionBoundsConcurrentVerification(t *testing.T) {
	started := make(chan struct{}, maxPasswordVerifications)
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	verifier := newPasswordVerifier(maxPasswordVerifications, func(hash, password string) bool {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return hash == "fake-hash" && password == "secret"
	})

	results := make(chan error, maxPasswordVerifications)
	for range maxPasswordVerifications {
		go func() {
			_, ok, err := verifier.authenticate(context.Background(), "reader", "secret", passwordUser)
			if err == nil && !ok {
				err = errors.New("fake verification rejected password")
			}
			results <- err
		}()
		awaitPasswordResult(t, started)
	}

	// Simultaneous excess requests must finish while both fake checks remain
	// blocked. Neither lookup nor verification may run for a rejected request.
	var lookups atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			_, ok, err := verifier.authenticate(context.Background(), "unknown", "secret", func(context.Context, string) (models.User, error) {
				lookups.Add(1)
				return models.User{}, sql.ErrNoRows
			})
			if ok || !errors.Is(err, ErrPasswordVerificationBusy) {
				t.Errorf("overload = %v, %v; want false, busy", ok, err)
			}
		})
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	awaitPasswordResult(t, finished)
	if got := calls.Load(); got != maxPasswordVerifications {
		t.Errorf("verification calls = %d, want %d", got, maxPasswordVerifications)
	}
	if got := lookups.Load(); got != 0 {
		t.Errorf("overloaded lookup calls = %d, want 0", got)
	}
	for range maxPasswordVerifications {
		release <- struct{}{}
		if err := awaitPasswordResult(t, results); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := verifier.authenticate(context.Background(), "passwordless", "secret", func(context.Context, string) (models.User, error) {
		return models.User{}, nil
	}); ok || err != nil {
		t.Fatalf("capacity after completion = %v, %v", ok, err)
	}
}

func TestPasswordAdmissionCancellationKeepsRunningSlot(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	verifier := newPasswordVerifier(1, func(string, string) bool {
		started <- struct{}{}
		<-release
		return true
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		user, ok, err := verifier.authenticate(ctx, "reader", "secret", passwordUser)
		if user.ID != 0 || ok {
			t.Error("cancelled authentication published a user")
		}
		result <- err
	}()
	awaitPasswordResult(t, started)
	cancel()

	if _, _, err := verifier.authenticate(context.Background(), "reader", "secret", passwordUser); !errors.Is(err, ErrPasswordVerificationBusy) {
		t.Fatalf("active cancelled check released its slot: %v", err)
	}
	if _, _, err := verifier.authenticate(ctx, "reader", "secret", passwordUser); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission = %v, want context.Canceled", err)
	}
	release <- struct{}{}
	if err := awaitPasswordResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("running check = %v, want context.Canceled", err)
	}
	// A subsequent request can use the released slot.
	_, _, err := verifier.authenticate(context.Background(), "missing", "secret", func(context.Context, string) (models.User, error) {
		return models.User{}, sql.ErrNoRows
	})
	if err != nil {
		t.Fatalf("slot was not released: %v", err)
	}
}

func TestPasswordAdmissionReleasesOnEveryOutcome(t *testing.T) {
	lookupError := errors.New("lookup failed")
	for _, name := range []string{"mismatch", "missing", "passwordless", "lookup error", "cancel before lookup", "cancel during lookup"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var lookups, comparisons int
			verifier := newPasswordVerifier(1, func(string, string) bool { comparisons++; return false })
			if name == "cancel before lookup" {
				cancel()
			}
			_, ok, err := verifier.authenticate(ctx, "reader", "wrong", func(context.Context, string) (models.User, error) {
				lookups++
				switch name {
				case "missing":
					return models.User{}, sql.ErrNoRows
				case "passwordless":
					return models.User{}, nil
				case "lookup error":
					return models.User{}, lookupError
				case "cancel during lookup":
					cancel()
				}
				return passwordUser(ctx, "reader")
			})
			var wantErr error
			if name == "lookup error" {
				wantErr = lookupError
			} else if name == "cancel before lookup" || name == "cancel during lookup" {
				wantErr = context.Canceled
			}
			if ok || !errors.Is(err, wantErr) {
				t.Fatalf("authentication = %v, %v; want false, %v", ok, err, wantErr)
			}
			if name == "cancel before lookup" && lookups != 0 {
				t.Fatal("cancelled request reached lookup")
			}
			wantComparisons := 0
			if name == "mismatch" {
				wantComparisons = 1
			}
			if comparisons != wantComparisons {
				t.Fatalf("comparisons = %d, want %d", comparisons, wantComparisons)
			}
			_, _, err = verifier.authenticate(context.Background(), "missing", "secret", func(context.Context, string) (models.User, error) {
				return models.User{}, sql.ErrNoRows
			})
			if err != nil {
				t.Fatalf("slot was not released: %v", err)
			}
		})
	}
}
