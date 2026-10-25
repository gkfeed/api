package auth

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gkfeed/api/internal/config"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"
	"gkfeed/api/internal/testschema"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	_ "github.com/mattn/go-sqlite3"
)

func newTestWebAuthnService(t *testing.T) *WebAuthnService {
	t.Helper()
	service, err := NewWebAuthnService(config.Config{
		WebAuthnRPID:      "localhost",
		WebAuthnRPOrigin:  "http://localhost",
		WebAuthnRPDisplay: "GKFeed",
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func useWebAuthnTestDatabase(t *testing.T) {
	t.Helper()
	database, err := sql.Open("sqlite3", t.TempDir()+"/webauthn.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	db.Configure(database)
	t.Cleanup(func() {
		db.Configure(nil)
		database.Close()
	})
	testschema.Init(t, database)
	if _, err := database.Exec("INSERT INTO users (id, name) VALUES (1, 'reader'), (2, 'other')"); err != nil {
		t.Fatal(err)
	}
}

func beginTestWebAuthnCeremony(t *testing.T, service *WebAuthnService, registration bool) (string, time.Duration) {
	t.Helper()
	if registration {
		creation, err := service.BeginRegistration(models.User{ID: 1, Name: "reader"}, "work laptop")
		if err != nil {
			t.Fatal(err)
		}
		return creation.Response.Challenge.String(), time.Duration(creation.Response.Timeout) * time.Millisecond
	}
	assertion, err := service.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	return assertion.Response.Challenge.String(), time.Duration(assertion.Response.Timeout) * time.Millisecond
}

func TestWebAuthnServiceCeremonyDeadlineAndSingleUse(t *testing.T) {
	for _, name := range []string{"login", "registration"} {
		t.Run(name, func(t *testing.T) {
			registration := name == "registration"
			if registration {
				useWebAuthnTestDatabase(t)
			}
			service := newTestWebAuthnService(t)
			before := time.Now()
			challenge, timeout := beginTestWebAuthnCeremony(t, service, registration)
			after := time.Now()
			pending := service.ceremonies.pendingCeremonies[challenge]
			if pending.session == nil || pending.session.Expires.IsZero() {
				t.Fatal("begin returned a challenge without a server deadline")
			}
			if timeout <= 0 {
				t.Fatalf("browser timeout must be positive, got %v", timeout)
			}
			if pending.session.Expires.Before(before.Add(timeout)) || pending.session.Expires.After(after.Add(timeout)) {
				t.Fatalf("server deadline %v does not match browser timeout %v", pending.session.Expires, timeout)
			}
			if registration && pending.registrationName != "work laptop" {
				t.Fatalf("registration name = %q, want work laptop", pending.registrationName)
			}

			var successes atomic.Int32
			var wg sync.WaitGroup
			for range 32 {
				wg.Go(func() {
					if _, ok := service.ceremonies.pop(challenge); ok {
						successes.Add(1)
					}
				})
			}
			wg.Wait()
			if got := successes.Load(); got != 1 {
				t.Fatalf("concurrent pop succeeded %d times, want 1", got)
			}
		})
	}
}

func TestWebAuthnServiceRejectsExpiredCeremonies(t *testing.T) {
	for _, name := range []string{"login", "registration"} {
		t.Run(name, func(t *testing.T) {
			registration := name == "registration"
			if registration {
				useWebAuthnTestDatabase(t)
			}
			service := newTestWebAuthnService(t)
			service.webAuthn.Config.Timeouts.Login.Timeout = 100 * time.Millisecond
			service.webAuthn.Config.Timeouts.Registration.Timeout = 100 * time.Millisecond
			challenge, _ := beginTestWebAuthnCeremony(t, service, registration)
			deadline := service.ceremonies.pendingCeremonies[challenge].session.Expires
			time.Sleep(time.Until(deadline))

			var err error
			if registration {
				response := &protocol.ParsedCredentialCreationData{}
				response.Response.CollectedClientData.Challenge = challenge
				_, _, err = service.FinishRegistration(models.User{ID: 1, Name: "reader"}, response)
			} else {
				response := &protocol.ParsedCredentialAssertionData{}
				response.Response.CollectedClientData.Challenge = challenge
				_, _, err = service.FinishLogin(response)
			}
			if err == nil || err.Error() != "session not found or expired" {
				t.Fatalf("finish after the deadline = %v, want expired ceremony", err)
			}
			if len(service.ceremonies.pendingCeremonies) != 0 {
				t.Fatal("finish retained an expired ceremony")
			}
		})
	}
}

func TestWebAuthnRegistrationRejectsAnotherUserAndReplay(t *testing.T) {
	useWebAuthnTestDatabase(t)
	service := newTestWebAuthnService(t)
	challenge, _ := beginTestWebAuthnCeremony(t, service, true)
	response := &protocol.ParsedCredentialCreationData{}
	response.Response.CollectedClientData.Challenge = challenge
	if _, _, err := service.FinishRegistration(models.User{ID: 2, Name: "other"}, response); err == nil || !strings.Contains(err.Error(), "ID mismatch for User and Session") {
		t.Fatalf("finish for another user = %v, want user/session mismatch", err)
	}
	if _, _, err := service.FinishRegistration(models.User{ID: 1, Name: "reader"}, response); err == nil || err.Error() != "session not found or expired" {
		t.Fatalf("replay after failed finish = %v, want consumed ceremony", err)
	}
}

func TestWebAuthnServiceBoundsPendingCeremonies(t *testing.T) {
	useWebAuthnTestDatabase(t)
	service := newTestWebAuthnService(t)
	registration, _ := beginTestWebAuthnCeremony(t, service, true)
	for range maxPendingCeremonies - 1 {
		beginTestWebAuthnCeremony(t, service, false)
	}
	if assertion, err := service.BeginLogin(); assertion != nil || !errors.Is(err, ErrTooManyPendingCeremonies) {
		t.Fatalf("login at capacity = %v, %v", assertion, err)
	}
	if creation, err := service.BeginRegistration(models.User{ID: 1, Name: "reader"}, "new key"); creation != nil || !errors.Is(err, ErrTooManyPendingCeremonies) {
		t.Fatalf("registration at capacity = %v, %v", creation, err)
	}
	if got := len(service.ceremonies.pendingCeremonies); got != maxPendingCeremonies {
		t.Fatalf("pending ceremonies = %d, want %d", got, maxPendingCeremonies)
	}
	if pending, ok := service.ceremonies.pop(registration); !ok || pending.registrationName != "work laptop" {
		t.Fatal("capacity rejection evicted an existing registration")
	}
	beginTestWebAuthnCeremony(t, service, false)

	// Expire this full batch without waiting for the normal five-minute timeout.
	for _, pending := range service.ceremonies.pendingCeremonies {
		pending.session.Expires = time.Now().Add(-time.Second)
	}
	beginTestWebAuthnCeremony(t, service, true)
	if got := len(service.ceremonies.pendingCeremonies); got != 1 {
		t.Fatalf("pending ceremonies after expiry cleanup = %d, want 1", got)
	}
}

func TestCeremonyStoreRejectsInvalidDeadline(t *testing.T) {
	for _, deadline := range []time.Time{{}, time.Now().Add(-time.Second)} {
		store := newCeremonyStore()
		if err := store.store(&webauthn.SessionData{Challenge: "invalid", Expires: deadline}, ""); err == nil {
			t.Fatalf("store() accepted invalid deadline %v", deadline)
		}
		if len(store.pendingCeremonies) != 0 {
			t.Fatal("store() retained a ceremony with an invalid deadline")
		}
	}
}
