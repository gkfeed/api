package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestAuthPersistenceCancelsPoolWait(t *testing.T) {
	t.Parallel()
	database := useTestDatabase(t)
	users := NewUserRepository(database)
	credentialRepo := NewCredentialRepository(database)
	refreshTokens := NewRefreshTokenRepository(database)
	credential := webauthn.Credential{ID: []byte("passkey")}
	operations := map[string]func(context.Context) error{
		"user by name": func(ctx context.Context) error {
			_, err := users.GetUserFromDB(ctx, "reader")
			return err
		},
		"user by ID": func(ctx context.Context) error {
			_, err := users.GetUserFromDBByID(ctx, 1)
			return err
		},
		"add credential": func(ctx context.Context) error {
			return credentialRepo.AddWebAuthnCredential(ctx, 1, credential, "key")
		},
		"update credential": func(ctx context.Context) error {
			return credentialRepo.UpdateWebAuthnCredential(ctx, credential)
		},
		"user credentials": func(ctx context.Context) error {
			_, err := credentialRepo.GetWebAuthnCredentialsByUserID(ctx, 1)
			return err
		},
		"credential user": func(ctx context.Context) error {
			_, err := credentialRepo.GetWebAuthnUserIDByCredentialID(ctx, credential.ID)
			return err
		},
		"delete credential": func(ctx context.Context) error {
			_, err := credentialRepo.DeleteWebAuthnCredential(ctx, credential.ID, 1)
			return err
		},
		"list credentials": func(ctx context.Context) error {
			_, err := credentialRepo.ListUserWebAuthnCredentials(ctx, 1)
			return err
		},
		"create refresh": func(ctx context.Context) error {
			return refreshTokens.CreateAuthRefreshToken(ctx, 1, []byte("hash"), []byte("family"), time.Now().Add(time.Hour))
		},
		"rotate refresh": func(ctx context.Context) error {
			_, err := refreshTokens.RotateAuthRefreshToken(ctx, []byte("old"), []byte("new"), time.Now(), time.Now().Add(time.Hour))
			return err
		},
		"revoke family": func(ctx context.Context) error {
			_, err := refreshTokens.RevokeAuthRefreshToken(ctx, []byte("hash"), time.Now())
			return err
		},
		"revoke user": func(ctx context.Context) error {
			return refreshTokens.RevokeUserAuthRefreshTokens(ctx, 1, time.Now())
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			// The fixture pool has one connection. Keep it busy to verify that
			// every auth persistence entry point can cancel pool acquisition.
			conn, err := database.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- operation(ctx) }()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("pool wait = %v, want deadline exceeded", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("auth persistence still waits for the connection after its deadline")
			}
		})
	}
}
