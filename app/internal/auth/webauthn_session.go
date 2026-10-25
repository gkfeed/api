package auth

import (
	"errors"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Bound both retained sessions and the work done by lazy expiry cleanup.
const maxPendingCeremonies = 4096

// ErrTooManyPendingCeremonies means begin must be retried after capacity is freed.
var ErrTooManyPendingCeremonies = errors.New("too many pending WebAuthn ceremonies")

type pendingCeremony struct {
	session          *webauthn.SessionData
	registrationName string
}

type ceremonyStore struct {
	pendingCeremonies map[string]pendingCeremony
	mu                sync.Mutex
	now               func() time.Time
}

func newCeremonyStore() *ceremonyStore {
	return &ceremonyStore{
		pendingCeremonies: make(map[string]pendingCeremony),
		now:               time.Now,
	}
}

func (s *ceremonyStore) store(session *webauthn.SessionData, registrationName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.removeExpired(now)
	if session.Expires.IsZero() || !session.Expires.After(now) {
		return errors.New("WebAuthn ceremony expiry must be in the future")
	}
	if len(s.pendingCeremonies) >= maxPendingCeremonies {
		return ErrTooManyPendingCeremonies
	}
	s.pendingCeremonies[session.Challenge] = pendingCeremony{
		session:          session,
		registrationName: registrationName,
	}
	return nil
}

func (s *ceremonyStore) pop(challenge string) (pendingCeremony, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ceremony, ok := s.pendingCeremonies[challenge]
	if !ok {
		return pendingCeremony{}, false
	}
	delete(s.pendingCeremonies, challenge)
	if ceremony.session.Expires.IsZero() || !ceremony.session.Expires.After(s.now()) {
		return pendingCeremony{}, false
	}
	return ceremony, true
}

// removeExpired must be called while s.mu is held.
func (s *ceremonyStore) removeExpired(now time.Time) {
	for challenge, ceremony := range s.pendingCeremonies {
		if ceremony.session.Expires.IsZero() || !ceremony.session.Expires.After(now) {
			delete(s.pendingCeremonies, challenge)
		}
	}
}
