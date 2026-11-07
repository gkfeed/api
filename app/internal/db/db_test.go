package db

import (
	"database/sql"
	"github.com/go-webauthn/webauthn/webauthn"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"gkfeed/api/internal/testschema"
)

func useTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", t.TempDir()+"/db.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() {
		database.Close()
	})
	testschema.Init(t, database)
	if _, err := database.Exec("INSERT INTO users (id, name, hashed_password) VALUES (1, 'reader', 'secret')"); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestNullPasswordLookups(t *testing.T) {
	t.Parallel()
	database := useTestDatabase(t)
	users := NewUserRepository(database)
	if _, err := database.Exec("INSERT INTO users (id, name, hashed_password) VALUES (2, 'passwordless', NULL)"); err != nil {
		t.Fatal(err)
	}
	byName, err := users.GetUserFromDB(t.Context(), "passwordless")
	if err != nil {
		t.Fatal(err)
	}
	byID, err := users.GetUserFromDBByID(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if byName != byID || byID.ID != 2 || byID.HashedPassword.Valid {
		t.Fatalf("users = %#v, %#v", byName, byID)
	}
}
func TestWebAuthnLookupForPasswordlessUser(t *testing.T) {
	t.Parallel()
	database := useTestDatabase(t)
	users := NewUserRepository(database)
	credentialRepo := NewCredentialRepository(database)
	if _, err := database.Exec("INSERT INTO users (id, name, hashed_password) VALUES (2, 'passwordless', NULL)"); err != nil {
		t.Fatal(err)
	}
	credential := webauthn.Credential{ID: []byte("passkey")}
	if err := credentialRepo.AddWebAuthnCredential(t.Context(), 2, credential, "key"); err != nil {
		t.Fatal(err)
	}
	id, err := credentialRepo.GetWebAuthnUserIDByCredentialID(t.Context(), credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	user, err := users.GetUserFromDBByID(t.Context(), id)
	if err != nil || user.ID != 2 || user.HashedPassword.Valid {
		t.Fatalf("user = %#v, %v", user, err)
	}
	credentials, err := credentialRepo.GetWebAuthnCredentialsByUserID(t.Context(), user.ID)
	if err != nil || len(credentials) != 1 || string(credentials[0].ID) != "passkey" {
		t.Fatalf("credentials = %#v, %v", credentials, err)
	}
}
