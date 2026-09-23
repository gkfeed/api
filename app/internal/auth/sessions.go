package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"gkfeed/api/internal/models"
)

const (
	opaqueTokenSize        = 32
	defaultTokenFamilySize = 32
)

// Session is an in-memory access-token session. The access token itself is
// never retained; only its SHA-256 digest is used as the map key.
type Session struct {
	User      models.User
	FamilyID  []byte
	ExpiresAt time.Time
}

// SessionStore keeps short-lived access tokens out of the database. A
// refresh-token family can revoke all access tokens issued for that family.
type SessionStore struct {
	mu       sync.Mutex
	ttl      time.Duration
	sessions map[string]Session
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		ttl:      ttl,
		sessions: make(map[string]Session),
	}
}

// Create creates an opaque access token and associates it with a refresh-token
// family. The raw access token is returned only to the caller.
func (s *SessionStore) Create(user models.User, familyID []byte) (string, error) {
	if s == nil {
		return "", errors.New("session store is nil")
	}

	token, err := GenerateOpaqueToken()
	if err != nil {
		return "", err
	}
	s.Add(token, user, familyID)
	return token, nil
}

// Add stores a previously generated access token. This lets callers generate
// every token before committing another state change, such as refresh rotation.
func (s *SessionStore) Add(token string, user models.User, familyID []byte) {
	if s == nil {
		return
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpired(now)
	s.sessions[string(DigestToken(token))] = Session{
		User:      user,
		FamilyID:  append([]byte(nil), familyID...),
		ExpiresAt: now.Add(s.ttl),
	}
}

func (s *SessionStore) Get(token string) (Session, bool) {
	if s == nil || token == "" {
		return Session{}, false
	}

	key := string(DigestToken(token))
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[key]
	if !ok {
		return Session{}, false
	}
	if !session.ExpiresAt.After(now) {
		delete(s.sessions, key)
		return Session{}, false
	}
	return session, true
}

func (s *SessionStore) RevokeFamily(familyID []byte) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if equalBytes(session.FamilyID, familyID) {
			delete(s.sessions, key)
		}
	}
}

func (s *SessionStore) RevokeUser(userID int) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for key, session := range s.sessions {
		if session.User.ID == userID {
			delete(s.sessions, key)
		}
	}
}

func (s *SessionStore) removeExpired(now time.Time) {
	for key, session := range s.sessions {
		if !session.ExpiresAt.After(now) {
			delete(s.sessions, key)
		}
	}
}

func GenerateOpaqueToken() (string, error) {
	raw := make([]byte, opaqueTokenSize)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func GenerateTokenFamily() ([]byte, error) {
	familyID := make([]byte, defaultTokenFamilySize)
	if _, err := rand.Read(familyID); err != nil {
		return nil, fmt.Errorf("generate token family: %w", err)
	}
	return familyID, nil
}

func DigestToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for i := range left {
		different |= left[i] ^ right[i]
	}
	return different == 0
}
