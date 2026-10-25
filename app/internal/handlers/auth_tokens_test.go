package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/config"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"
	"gkfeed/api/internal/testschema"
)

// Pause after the database commit, before the handler publishes its access
// token. This is the ordering that previously let refresh survive revocation.
type sqlite3CommitDriver struct {
	sqlite3.SQLiteDriver
	armed     atomic.Bool
	committed chan struct{}
	release   chan struct{}
}

type commitConn struct {
	driver.Conn
	gate *sqlite3CommitDriver
}

type commitTx struct {
	driver.Tx
	gate *sqlite3CommitDriver
}

func (d *sqlite3CommitDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.SQLiteDriver.Open(name)
	if err != nil {
		return nil, err
	}
	return &commitConn{Conn: conn, gate: d}, nil
}

func (c *commitConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return &commitTx{Tx: tx, gate: c.gate}, nil
}

func (tx *commitTx) Commit() error {
	err := tx.Tx.Commit()
	if err == nil && tx.gate.armed.CompareAndSwap(true, false) {
		close(tx.gate.committed)
		<-tx.gate.release
	}
	return err
}

var commitDriverSequence atomic.Int64

func TestConcurrentRefreshCannotSurviveRevocation(t *testing.T) {
	for _, flow := range []string{"logout", "logout-all", "refresh reuse", "logout after cancellation", "logout-all after cancellation", "refresh reuse after cancellation"} {
		t.Run(flow, func(t *testing.T) {
			operation := strings.TrimSuffix(flow, " after cancellation")
			refreshCtx, cancelRefresh := context.WithCancel(t.Context())
			defer cancelRefresh()
			gate := &sqlite3CommitDriver{committed: make(chan struct{}), release: make(chan struct{})}
			driverName := fmt.Sprintf("sqlite3-commit-gate-%d", commitDriverSequence.Add(1))
			sql.Register(driverName, gate)
			database, err := sql.Open(driverName, t.TempDir()+"/db.sqlite")
			if err != nil {
				t.Fatal(err)
			}
			database.SetMaxOpenConns(2)
			t.Cleanup(func() { database.Close() })
			testschema.Init(t, database)
			if _, err := database.Exec("INSERT INTO users (id,name) VALUES (1,'reader')"); err != nil {
				t.Fatal(err)
			}
			sessions := auth.NewSessionStore(time.Hour)
			refreshTokens := db.NewRefreshTokenRepository(database)
			browserSessions := auth.NewBrowserSessionStore(nil, db.NewBrowserSessionRepository(database))
			h := NewAuthHandler(config.Config{}, nil, sessions, nil, browserSessions, refreshTokens, nil)
			// Handlers sharing access sessions must also share the lifecycle lock.
			revoker := NewAuthHandler(config.Config{}, nil, sessions, nil, browserSessions, refreshTokens, nil)
			tokens, err := h.issueTokenPair(t.Context(), models.User{ID: 1, Name: "reader"})
			if err != nil {
				t.Fatal(err)
			}
			gate.armed.Store(true)
			var once sync.Once
			release := func() { once.Do(func() { close(gate.release) }) }
			defer release()
			refreshDone := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				h.Refresh(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"refresh_token":"`+tokens.RefreshToken+`"}`)).WithContext(refreshCtx))
				refreshDone <- w
			}()
			select {
			case <-gate.committed:
			case <-time.After(5 * time.Second):
				t.Fatal("refresh did not reach commit")
			}
			if operation != flow {
				cancelRefresh()
			}
			revoked := make(chan *httptest.ResponseRecorder, 1)
			started := make(chan struct{})
			go func() {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("POST", "/", strings.NewReader(`{"refresh_token":"`+tokens.RefreshToken+`"}`))
				close(started)
				switch operation {
				case "logout":
					revoker.Logout(w, r)
				case "logout-all":
					revoker.LogoutAll(w, r.WithContext(auth.WithUser(r.Context(), models.User{ID: 1, Name: "reader"})))
				case "refresh reuse":
					revoker.Refresh(w, r)
				}
				revoked <- w
			}()
			<-started
			// Revocation must wait for both the commit and in-memory publication.
			var premature bool
			var revokeResponse *httptest.ResponseRecorder
			select {
			case revokeResponse = <-revoked:
				premature = true
			case <-time.After(100 * time.Millisecond):
			}
			release()
			var refreshResponse *httptest.ResponseRecorder
			select {
			case refreshResponse = <-refreshDone:
			case <-time.After(5 * time.Second):
				t.Fatal("refresh remained blocked")
			}
			if revokeResponse == nil {
				select {
				case revokeResponse = <-revoked:
				case <-time.After(5 * time.Second):
					t.Fatal("revocation remained blocked")
				}
			}
			if premature {
				t.Error("revocation finished before refresh published its access token")
			}
			if refreshResponse.Code != http.StatusOK {
				t.Fatalf("refresh = %d, %s", refreshResponse.Code, refreshResponse.Body.String())
			}
			want := http.StatusNoContent
			if operation == "refresh reuse" {
				want = http.StatusUnauthorized
			}
			if revokeResponse.Code != want {
				t.Fatalf("revocation = %d, %s", revokeResponse.Code, revokeResponse.Body.String())
			}
			var rotated tokenResponse
			if err := json.Unmarshal(refreshResponse.Body.Bytes(), &rotated); err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{tokens.AccessToken, rotated.AccessToken} {
				if _, ok := sessions.Get(token); ok {
					t.Error("revoked access token survived concurrent refresh")
				}
			}
		})
	}
}

// Hold the lifecycle lock as another token request would. Each handler must
// honor its deadline without waiting for that request to finish.
func TestTokenHandlersCancelLifecycleWait(t *testing.T) {
	for _, flow := range []string{"issue", "refresh", "logout", "logout-all"} {
		t.Run(flow, func(t *testing.T) {
			sessions := auth.NewSessionStore(time.Hour)
			h := NewAuthHandler(config.Config{}, nil, sessions, nil, nil, nil, nil)
			unlock, err := sessions.LockLifecycle(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				user := models.User{ID: 1, Name: "reader"}
				if flow == "issue" {
					_, err := h.issueTokenPair(ctx, user)
					done <- err
					return
				}
				w := httptest.NewRecorder()
				r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"refresh_token":"token"}`)).WithContext(auth.WithUser(ctx, user))
				switch flow {
				case "refresh":
					h.Refresh(w, r)
				case "logout":
					h.Logout(w, r)
				case "logout-all":
					h.LogoutAll(w, r)
				}
				if w.Code != http.StatusInternalServerError {
					done <- fmt.Errorf("status = %d, body = %s", w.Code, w.Body.String())
					return
				}
				done <- ctx.Err()
			}()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("canceled lifecycle wait = %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("handler still waits for the lifecycle lock after its deadline")
			}
		})
	}
}
