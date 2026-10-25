package db

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRotateAuthRefreshTokenDetectsReuseAndRevokesFamily(t *testing.T) {
	t.Parallel()
	database := useTestDatabase(t)
	refreshTokens := NewRefreshTokenRepository(database)

	oldToken, err := testOpaqueToken()
	if err != nil {
		t.Fatalf("GenerateOpaqueToken() returned error: %v", err)
	}
	newToken, err := testOpaqueToken()
	if err != nil {
		t.Fatalf("GenerateOpaqueToken() returned error: %v", err)
	}
	familyID := []byte("test-family")
	now := time.Now()
	if err := refreshTokens.CreateAuthRefreshToken(t.Context(), 1, testDigestToken(oldToken), familyID, now.Add(time.Hour)); err != nil {
		t.Fatalf("refreshTokens.CreateAuthRefreshToken() returned error: %v", err)
	}

	rotation, err := refreshTokens.RotateAuthRefreshToken(t.Context(),
		testDigestToken(oldToken),
		testDigestToken(newToken),
		now, now.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("refreshTokens.RotateAuthRefreshToken() returned error: %v", err)
	}
	if rotation.User.ID != 1 || string(rotation.FamilyID) != string(familyID) {
		t.Fatalf("rotation = %#v, want user 1 and family %q", rotation, familyID)
	}

	_, err = refreshTokens.RotateAuthRefreshToken(t.Context(),
		testDigestToken(oldToken),
		testDigestToken(oldToken),
		now,
		now.Add(time.Hour),
	)
	if !errors.Is(err, ErrRefreshTokenReuse) {
		t.Fatalf("reusing old token returned %v, want ErrRefreshTokenReuse", err)
	}

	_, err = refreshTokens.RotateAuthRefreshToken(t.Context(),
		testDigestToken(newToken),
		testDigestToken(oldToken),
		now,
		now.Add(time.Hour),
	)
	if !errors.Is(err, ErrRefreshTokenReuse) {
		t.Fatalf("rotating revoked family returned %v, want ErrRefreshTokenReuse", err)
	}
}

func TestRotateAuthRefreshTokenIsAtomic(t *testing.T) {
	t.Parallel()
	database := useTestDatabase(t)
	refreshTokens := NewRefreshTokenRepository(database)

	oldToken, err := testOpaqueToken()
	if err != nil {
		t.Fatalf("GenerateOpaqueToken() returned error: %v", err)
	}
	if err := refreshTokens.CreateAuthRefreshToken(t.Context(),
		1,
		testDigestToken(oldToken),
		[]byte("concurrent-family"),
		time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("refreshTokens.CreateAuthRefreshToken() returned error: %v", err)
	}

	var wait sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			newToken, generateErr := testOpaqueToken()
			if generateErr != nil {
				results <- generateErr
				return
			}
			_, rotateErr := refreshTokens.RotateAuthRefreshToken(t.Context(),
				testDigestToken(oldToken),
				testDigestToken(newToken),
				time.Now(),
				time.Now().Add(time.Hour),
			)
			results <- rotateErr
		}()
	}
	wait.Wait()
	close(results)

	var successes, reuseErrors int
	for rotateErr := range results {
		switch {
		case rotateErr == nil:
			successes++
		case errors.Is(rotateErr, ErrRefreshTokenReuse):
			reuseErrors++
		default:
			t.Fatalf("concurrent rotation returned unexpected error: %v", rotateErr)
		}
	}
	if successes != 1 || reuseErrors != 1 {
		t.Fatalf("concurrent rotations = %d successes, %d reuse errors; want 1 and 1", successes, reuseErrors)
	}
}

func testOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return string(raw), nil
}

func testDigestToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}
