package auth

import (
	"bytes"
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
	hash := testPasswordHash(t, "secret")
	replaceUserLookup(t, func(name string) (models.User, error) {
		return models.User{ID: 7, Name: name, HashedPassword: sql.NullString{String: hash, Valid: true}}, nil
	})

	var receivedUser models.User
	handler := BasicAuth(func(w http.ResponseWriter, r *http.Request) {
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
	hash := testPasswordHash(t, "secret")
	replaceUserLookup(t, func(name string) (models.User, error) {
		return models.User{Name: name, HashedPassword: sql.NullString{String: hash, Valid: true}}, nil
	})

	handler := BasicAuth(func(http.ResponseWriter, *http.Request) {
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
	replaceUserLookup(t, func(string) (models.User, error) {
		return models.User{}, errors.New("database unavailable")
	})

	handler := BasicAuth(func(http.ResponseWriter, *http.Request) {
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
	hash := testPasswordHash(t, "secret")
	replaceUserLookup(t, func(name string) (models.User, error) {
		return models.User{ID: 99, Name: name, HashedPassword: sql.NullString{String: hash, Valid: true}}, nil
	})

	var receivedUser models.User
	handler := Authenticate(nil)(func(w http.ResponseWriter, r *http.Request) {
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
	handler := Authenticate(nil)(func(http.ResponseWriter, *http.Request) {
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
	handler := Authenticate(NewSessionStore(time.Minute))(func(http.ResponseWriter, *http.Request) {
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
	replaceUserLookup(t, func(name string) (models.User, error) {
		return models.User{Name: name, HashedPassword: sql.NullString{String: "secret", Valid: true}}, nil
	})

	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
	})

	handler := Authenticate(nil)(func(http.ResponseWriter, *http.Request) {
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

func replaceUserLookup(t *testing.T, lookup func(string) (models.User, error)) {
	t.Helper()
	original := getUser
	getUser = lookup
	t.Cleanup(func() { getUser = original })
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
	replaceUserLookup(t, func(name string) (models.User, error) {
		return models.User{ID: 7, Name: name}, nil
	})
	for _, password := range []string{"", "secret"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.SetBasicAuth("passwordless", password)
		response := httptest.NewRecorder()
		BasicAuth(func(http.ResponseWriter, *http.Request) { t.Error("authenticated passwordless user") })(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", response.Code)
		}
	}
}
