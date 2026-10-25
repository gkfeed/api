package auth

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestPendingCeremonyIsPoppedWithRegistrationName(t *testing.T) {
	store := newCeremonyStore()
	session := &webauthn.SessionData{
		Challenge: "challenge",
		Expires:   time.Now().Add(time.Minute),
	}

	if err := store.store(session, "work laptop"); err != nil {
		t.Fatal(err)
	}
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

	if err := store.store(session, "unused name"); err == nil {
		t.Fatal("store() accepted an expired ceremony")
	}
	if _, ok := store.pop(session.Challenge); ok {
		t.Fatal("pop() returned an expired ceremony")
	}
	if len(store.pendingCeremonies) != 0 {
		t.Fatalf("pending ceremonies = %d, want 0", len(store.pendingCeremonies))
	}
}

func TestStoringCeremonyRemovesExpiredEntriesOnly(t *testing.T) {
	store := newCeremonyStore()
	store.pendingCeremonies = map[string]pendingCeremony{
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
		"no-deadline": {session: &webauthn.SessionData{Challenge: "no-deadline"}},
	}

	if err := store.store(&webauthn.SessionData{
		Challenge: "new",
		Expires:   time.Now().Add(time.Minute),
	}, ""); err != nil {
		t.Fatal(err)
	}

	if _, ok := store.pendingCeremonies["expired"]; ok {
		t.Fatal("expired ceremony was not removed")
	}
	if _, ok := store.pendingCeremonies["no-deadline"]; ok {
		t.Fatal("ceremony without a deadline was not removed")
	}
	if _, ok := store.pendingCeremonies["active"]; !ok {
		t.Fatal("active ceremony was removed")
	}
	if _, ok := store.pendingCeremonies["new"]; !ok {
		t.Fatal("new ceremony was not stored")
	}
}

func TestCeremonyStoreRejectsMissingDeadline(t *testing.T) {
	store := newCeremonyStore()
	if err := store.store(&webauthn.SessionData{Challenge: "no-deadline"}, ""); err == nil {
		t.Fatal("store() accepted a ceremony without a deadline")
	}
	if len(store.pendingCeremonies) != 0 {
		t.Fatal("store() retained a ceremony without a deadline")
	}
}
