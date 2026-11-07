package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/config"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/handlers"
	"gkfeed/api/internal/models"
)

func TestPostgresRefreshCancellation(t *testing.T) {
	for _, alreadyCanceled := range []bool{false, true} {
		name := "blocked query"
		if alreadyCanceled {
			name = "already canceled"
		}
		t.Run(name, func(t *testing.T) {
			admin, app := postgresFixture(t)

			family := []byte("cancellation-family")
			if err := db.NewRefreshTokenRepository(app).CreateAuthRefreshToken(t.Context(), 1, auth.DigestToken("original-refresh"), family, time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			sessions := auth.NewSessionStore(time.Hour)
			sessions.Add("original-access", models.User{ID: 1, Name: "owner"}, family)
			h := handlers.NewAuthHandler(config.Config{}, nil, sessions, nil, nil, db.NewRefreshTokenRepository(app), nil)

			blocker, err := admin.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err := blocker.ExecContext(t.Context(), "SELECT pg_advisory_xact_lock(281474976710655)"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if alreadyCanceled {
				cancel()
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				h.Refresh(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"original-refresh"}`)).WithContext(ctx))
				done <- w
				close(done)
			}()
			defer func() {
				blocker.Rollback()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("refresh did not finish during cleanup")
				}
			}()
			if !alreadyCanceled {
				// Observe the actual PostgreSQL wait before canceling, rather than
				// assuming the handler reached the query after a fixed sleep.
				waitCtx, stopWaiting := context.WithTimeout(t.Context(), 5*time.Second)
				defer stopWaiting()
				for {
					var waiting bool
					err := admin.QueryRowContext(waitCtx, `SELECT EXISTS (
 SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted
 AND classid=65535 AND objid=4294967295 AND objsubid=1
 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`).Scan(&waiting)
					if err != nil {
						t.Fatalf("observe advisory-lock wait: %v", err)
					}
					if waiting {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				cancel()
			}
			select {
			case response := <-done:
				if response.Code != http.StatusInternalServerError {
					t.Fatalf("canceled refresh = %d, %s", response.Code, response.Body.String())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("canceled HTTP refresh still waits for the PostgreSQL lock")
			}
			lockCtx, stopLock := context.WithTimeout(t.Context(), 2*time.Second)
			defer stopLock()
			unlock, err := sessions.LockLifecycle(lockCtx)
			if err != nil {
				t.Fatalf("canceled refresh left the lifecycle lock held: %v", err)
			}
			unlock()
			if _, ok := sessions.Get("original-access"); !ok {
				t.Fatal("canceled refresh revoked the existing access session")
			}
			var count int
			if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM refresh_tokens").Scan(&count); err != nil || count != 1 {
				t.Fatalf("refresh rows after cancellation = %d, %v; want the original row only", count, err)
			}
			if err := blocker.Rollback(); err != nil {
				t.Fatal(err)
			}

			// The same token must still rotate, proving cancellation neither
			// consumed it nor left the transaction/lifecycle lock held.
			retryCtx, stopRetry := context.WithTimeout(t.Context(), 2*time.Second)
			defer stopRetry()
			response := httptest.NewRecorder()
			h.Refresh(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", strings.NewReader(`{"refresh_token":"original-refresh"}`)).WithContext(retryCtx))
			if response.Code != http.StatusOK {
				t.Fatalf("retry refresh = %d, %s", response.Code, response.Body.String())
			}
			var pair struct {
				AccessToken string `json:"access_token"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &pair); err != nil {
				t.Fatal(err)
			}
			if _, ok := sessions.Get(pair.AccessToken); !ok {
				t.Fatal("successful refresh did not publish its access session")
			}
		})
	}
}
