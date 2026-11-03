package auth

import (
	"database/sql"
	"strings"
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

func TestPendingCeremonyIsPoppedWithRegistrationName(t *testing.T) {
	store := newCeremonyStore()
	session := &webauthn.SessionData{
		Challenge: "challenge",
		Expires:   time.Now().Add(time.Minute),
	}

	store.store(session, "work laptop")
	pendingCeremony, ok := store.pop(session.Challenge)

	if !ok {
		t.Fatal("pop() did not find an active ceremony")
	}
	if pendingCeremony.session != session {
		t.Fatal("pop() returned a different session")
	}
	if pendingCeremony.registrationName != "work laptop" {
		t.Fatalf("registration name = %q, want %q", pendingCeremony.registrationName, "work laptop")
	}
	if _, ok := store.pop(session.Challenge); ok {
		t.Fatal("pop() returned the same ceremony twice")
	}
}

func TestPendingCeremonyExpiresAtSessionDeadline(t *testing.T) {
	store := newCeremonyStore()
	session := &webauthn.SessionData{
		Challenge: "expired-challenge",
		Expires:   time.Now().Add(-time.Second),
	}

	store.store(session, "unused name")
	if _, ok := store.pop(session.Challenge); ok {
		t.Fatal("pop() returned an expired ceremony")
	}
	if len(store.pendingCeremonies) != 0 {
		t.Fatalf("pending ceremonies = %d, want 0", len(store.pendingCeremonies))
	}
}

func TestStoringCeremonyRemovesExpiredEntriesOnly(t *testing.T) {
	store := &ceremonyStore{pendingCeremonies: map[string]pendingCeremony{
		"expired": {
			session: &webauthn.SessionData{
				Challenge: "expired",
				Expires:   time.Now().Add(-time.Second),
			},
		},
		"active": {
			session: &webauthn.SessionData{
				Challenge: "active",
				Expires:   time.Now().Add(time.Minute),
			},
		},
	}}

	store.store(&webauthn.SessionData{
		Challenge: "new",
		Expires:   time.Now().Add(time.Minute),
	}, "")

	if _, ok := store.pendingCeremonies["expired"]; ok {
		t.Fatal("expired ceremony was not removed")
	}
	if _, ok := store.pendingCeremonies["active"]; !ok {
		t.Fatal("active ceremony was removed")
	}
	if _, ok := store.pendingCeremonies["new"]; !ok {
		t.Fatal("new ceremony was not stored")
	}
}

func TestWebAuthnServiceCeremoniesExpireAndAreSingleUse(t *testing.T) {
	database, err := sql.Open("sqlite3", t.TempDir()+"/webauthn.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	testschema.Init(t, database)
	db.Configure(database)
	t.Cleanup(func() {
		db.Configure(nil)
		database.Close()
	})

	for _, name := range []string{"login", "registration"} {
		t.Run(name, func(t *testing.T) {
			service, err := NewWebAuthnService(config.Config{
				WebAuthnRPID: "localhost", WebAuthnRPOrigin: "http://localhost", WebAuthnRPDisplay: "GKFeed",
			})
			if err != nil {
				t.Fatal(err)
			}
			user := models.User{ID: 1, Name: "reader"}
			begin := func() (string, time.Duration) {
				t.Helper()
				if name == "registration" {
					creation, err := service.BeginRegistration(user, "work laptop")
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

			before := time.Now()
			challenge, timeout := begin()
			after := time.Now()
			pending := service.ceremonies.pendingCeremonies[challenge]
			if pending.session == nil || pending.session.Expires.IsZero() || timeout <= 0 {
				t.Fatal("begin returned a challenge without a server deadline")
			}
			if pending.session.Expires.Before(before.Add(timeout)) || pending.session.Expires.After(after.Add(timeout)) {
				t.Fatalf("server deadline %v does not match browser timeout %v", pending.session.Expires, timeout)
			}

			if name == "registration" {
				if pending.registrationName != "work laptop" {
					t.Fatalf("registration name = %q", pending.registrationName)
				}
				response := &protocol.ParsedCredentialCreationData{}
				response.Response.CollectedClientData.Challenge = challenge
				_, _, err := service.FinishRegistration(models.User{ID: 2, Name: "other"}, response)
				if err == nil || !strings.Contains(err.Error(), "ID mismatch for User and Session") {
					t.Fatalf("finish for another user = %v, want user/session mismatch", err)
				}
			} else if _, ok := service.ceremonies.pop(challenge); !ok {
				t.Fatal("active challenge was not found")
			}
			if _, ok := service.ceremonies.pop(challenge); ok {
				t.Fatal("challenge was returned twice")
			}

			service.webAuthn.Config.Timeouts.Login.Timeout = 100 * time.Millisecond
			service.webAuthn.Config.Timeouts.Registration.Timeout = 100 * time.Millisecond
			challenge, _ = begin()
			deadline := service.ceremonies.pendingCeremonies[challenge].session.Expires
			time.Sleep(time.Until(deadline))
			if _, ok := service.ceremonies.pop(challenge); ok {
				t.Fatal("expired challenge was returned")
			}
		})
	}
}
