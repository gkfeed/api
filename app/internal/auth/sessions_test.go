package auth

import (
	"testing"
	"time"

	"gkfeed/api/internal/models"
)

func TestSessionStoreRevokesFamiliesAndUsers(t *testing.T) {
	store := NewSessionStore(time.Minute)
	user := models.User{ID: 1, Name: "reader"}
	other := models.User{ID: 2, Name: "other"}

	familyToken, err := store.Create(user, []byte("family-a"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	otherFamilyToken, err := store.Create(user, []byte("family-b"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	otherUserToken, err := store.Create(other, []byte("family-c"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	store.RevokeFamily([]byte("family-a"))
	if _, ok := store.Get(familyToken); ok {
		t.Fatal("RevokeFamily() left the revoked access token usable")
	}
	if _, ok := store.Get(otherFamilyToken); !ok {
		t.Fatal("RevokeFamily() revoked a different token family")
	}

	store.RevokeUser(user.ID)
	if _, ok := store.Get(otherFamilyToken); ok {
		t.Fatal("RevokeUser() left the user's access token usable")
	}
	if _, ok := store.Get(otherUserToken); !ok {
		t.Fatal("RevokeUser() revoked another user's access token")
	}
}

func TestSessionStoreExpiresAccessTokens(t *testing.T) {
	store := NewSessionStore(time.Nanosecond)
	token, err := store.Create(models.User{ID: 1}, []byte("family"))
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	time.Sleep(time.Millisecond)

	if _, ok := store.Get(token); ok {
		t.Fatal("Get() returned an expired access token")
	}
}
