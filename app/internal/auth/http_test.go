package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"gkfeed/api/internal/models"
	"gkfeed/api/internal/passwordhash"
)

func TestBasicAuthProvidesAuthenticatedUser(t *testing.T) {
	t.Parallel()
	hash := testPasswordHash(t, "secret")
	passwords := testPasswordAuthenticator(func(name string) (models.User, error) {
		return models.User{ID: 7, Name: name, HashedPassword: sql.NullString{String: hash, Valid: true}}, nil
	})

	var receivedUser models.User
	handler := passwords.BasicAuth(func(w http.ResponseWriter, r *http.Request) {
		receivedUser, _ = UserFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetBasicAuth("reader", "secret")
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if receivedUser.ID != 7 || receivedUser.Name != "reader" {
		t.Fatalf("authenticated user = %#v, want reader with ID 7", receivedUser)
	}
}

func TestBasicAuthRejectsInvalidPassword(t *testing.T) {
	t.Parallel()
	hash := testPasswordHash(t, "secret")
	passwords := testPasswordAuthenticator(func(name string) (models.User, error) {
		return models.User{Name: name, HashedPassword: sql.NullString{String: hash, Valid: true}}, nil
	})

	handler := passwords.BasicAuth(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetBasicAuth("reader", "wrong")
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestBasicAuthReturnsServerErrorWhenLookupFails(t *testing.T) {
	t.Parallel()
	passwords := testPasswordAuthenticator(func(string) (models.User, error) {
		return models.User{}, errors.New("database unavailable")
	})

	handler := passwords.BasicAuth(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetBasicAuth("reader", "secret")
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestAuthenticateFallsBackToBasicAuth(t *testing.T) {
	t.Parallel()
	hash := testPasswordHash(t, "secret")
	passwords := testPasswordAuthenticator(func(name string) (models.User, error) {
		return models.User{ID: 99, Name: name, HashedPassword: sql.NullString{String: hash, Valid: true}}, nil
	})

	var receivedUser models.User
	handler := passwords.Authenticate(nil)(func(w http.ResponseWriter, r *http.Request) {
		receivedUser, _ = UserFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetBasicAuth("basicuser", "secret")
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if receivedUser.ID != 99 || receivedUser.Name != "basicuser" {
		t.Fatalf("authenticated user = %#v, want basicuser with ID 99", receivedUser)
	}
}

func TestAuthenticateRejectsWithoutCredentials(t *testing.T) {
	t.Parallel()
	handler := NewPasswordAuthenticator(nil).Authenticate(nil)(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticateAdvertisesBearerAndBasicAuth(t *testing.T) {
	t.Parallel()
	handler := NewPasswordAuthenticator(nil).Authenticate(NewSessionStore(time.Minute))(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})

	response := httptest.NewRecorder()
	handler(response, httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"Bearer", `Basic realm="Restricted"`}
	if got := response.Header().Values("WWW-Authenticate"); !slices.Equal(got, want) {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestParseBearerToken(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		header string
		token  string
		ok     bool
	}{
		{header: "Bearer token", token: "token", ok: true},
		{header: "bearer token", token: "token", ok: true},
		{header: "  Bearer   token  ", token: "token", ok: true},
		{header: "Bearer ", ok: false},
		{header: "Basic token", ok: false},
	} {
		t.Run(test.header, func(t *testing.T) {
			token, ok := parseBearerToken(test.header)
			if token != test.token || ok != test.ok {
				t.Fatalf("parseBearerToken(%q) = %q, %v; want %q, %v", test.header, token, ok, test.token, test.ok)
			}
		})
	}
}

func TestAuthenticateDoesNotLogAuthorizationHeader(t *testing.T) {
	passwords := testPasswordAuthenticator(func(name string) (models.User, error) {
		return models.User{Name: name, HashedPassword: sql.NullString{String: "secret", Valid: true}}, nil
	})

	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
	})

	handler := passwords.Authenticate(nil)(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetBasicAuth("reader", "a-very-long-password-that-must-not-be-logged")
	authHeader := request.Header.Get("Authorization")
	response := httptest.NewRecorder()

	handler(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	output := logs.String()
	if strings.Contains(output, authHeader) {
		t.Fatalf("log output contains Authorization header: %q", output)
	}
	if strings.Contains(output, authHeader[:min(len(authHeader), 30)]) {
		t.Fatalf("log output contains part of Authorization header: %q", output)
	}
}

func testPasswordAuthenticator(lookup func(string) (models.User, error)) *PasswordAuthenticator {
	return NewPasswordAuthenticator(userLookupFunc(func(_ context.Context, name string) (models.User, error) { return lookup(name) }))
}

type userLookupFunc func(context.Context, string) (models.User, error)

func (f userLookupFunc) GetUserFromDB(ctx context.Context, name string) (models.User, error) {
	return f(ctx, name)
}

func testPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := passwordhash.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() returned error: %v", err)
	}
	return hash
}

func TestBasicAuthRejectsNullPassword(t *testing.T) {
	t.Parallel()
	passwords := testPasswordAuthenticator(func(name string) (models.User, error) {
		return models.User{ID: 7, Name: name}, nil
	})
	for _, password := range []string{"", "secret"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.SetBasicAuth("passwordless", password)
		response := httptest.NewRecorder()
		passwords.BasicAuth(func(http.ResponseWriter, *http.Request) { t.Error("authenticated passwordless user") })(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", response.Code)
		}
	}
}

func TestPasswordAuthenticatorPassesContextToInjectedRepository(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	passwords := NewPasswordAuthenticator(userLookupFunc(func(received context.Context, name string) (models.User, error) {
		if received != ctx || name != "reader" {
			t.Fatalf("lookup received context %v, user %q", received, name)
		}
		return models.User{}, received.Err()
	}))
	if _, ok, err := passwords.AuthenticatePassword(ctx, "reader", "secret"); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("authentication = %v, %v; want cancellation", ok, err)
	}
}
