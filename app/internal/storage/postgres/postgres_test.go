package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/passwordhash"
	"gkfeed/api/internal/storage/repositorytest"
)

// This opt-in suite truncates application data. Use only a disposable database
// provisioned by infra's pinned dbmate migrations. Calls must be serialized;
// each fixture resets application data, including independent change history.
func postgresFixture(t *testing.T) (*sql.DB, *sql.DB) {
	t.Helper()
	url := os.Getenv("GKFEED_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set GKFEED_TEST_DATABASE_URL to a disposable infra-provisioned PostgreSQL database")
	}
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	if err := CheckSchema(t.Context(), admin); err != nil {
		t.Fatal(err)
	}
	// item_changes has no foreign key to users, so CASCADE cannot clear it.
	if _, err := admin.Exec("TRUNCATE public.users, public.item_changes RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("INSERT INTO public.users (id, name) VALUES (1, 'owner'), (2, 'other')"); err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["role"] = "gkfeed_api"
	app := stdlib.OpenDB(*config)
	app.SetMaxOpenConns(12)
	t.Cleanup(func() { app.Close() })
	if err := CheckSchema(t.Context(), app); err != nil {
		t.Fatal(err)
	}
	return admin, app
}

func TestPostgresFixtureIsolation(t *testing.T) {
	admin, app := postgresFixture(t)
	feed, err := NewFeedRepository(app).Add(t.Context(), 1, library.CreateFeedInput{
		Title: "fixture", Type: "rss", URL: "https://fixture.example/feed",
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID int
	if err := admin.QueryRow("INSERT INTO public.item (feed_id,title,text,date,link) VALUES ($1,'previous','','2026-01-01','') RETURNING id", feed.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	items := NewItemRepository(app)
	if err := items.Delete(t.Context(), 1, itemID); err != nil {
		t.Fatal(err)
	}
	changes, err := items.Changes(t.Context(), 1, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 2 {
		t.Fatalf("expected an upsert and tombstone before reset, got %#v", changes)
	}

	_, app = postgresFixture(t)
	changes, err = NewItemRepository(app).Changes(t.Context(), 1, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 0 {
		t.Fatalf("fixture retained item changes: %#v", changes)
	}
}

func TestPostgresRepositoryContract(t *testing.T) {
	repositorytest.Run(t, func(t *testing.T) repositorytest.Fixture {
		admin, app := postgresFixture(t)
		return repositorytest.Fixture{
			Feeds: NewFeedRepository(app), Items: NewItemRepository(app),
			InsertItems: func(t *testing.T, feedID, count int) []int {
				t.Helper()
				ids := make([]int, count)
				for i := range count {
					err := admin.QueryRow("INSERT INTO item (feed_id,title,text,date,link) VALUES ($1,$2,'',$3,'') RETURNING id", feedID, fmt.Sprintf("item-%d", i), time.Now()).Scan(&ids[i])
					if err != nil {
						t.Fatal(err)
					}
				}
				return ids
			},
		}
	})
}

func TestPostgresAuthContract(t *testing.T) {
	admin, app := postgresFixture(t)
	db.Configure(app)
	t.Cleanup(func() { db.Configure(nil) })
	byName, err := db.GetUserFromDB("owner")
	if err != nil {
		t.Fatal(err)
	}
	byID, err := db.GetUserFromDBByID(1)
	if err != nil || byName != byID || byID.HashedPassword.Valid {
		t.Fatalf("user = %#v, %v", byID, err)
	}
	for _, password := range []string{"", "secret"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.SetBasicAuth("owner", password)
		response := httptest.NewRecorder()
		auth.BasicAuth(func(http.ResponseWriter, *http.Request) { t.Error("accepted null password") })(response, request)
		if response.Code != 401 {
			t.Fatalf("status = %d", response.Code)
		}
	}
	hash, err := passwordhash.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("UPDATE users SET hashed_password=$1 WHERE id=2", hash); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := auth.AuthenticatePassword("other", "secret"); err != nil || !ok {
		t.Fatalf("login = %v, %v", ok, err)
	}
	credential := webauthn.Credential{ID: []byte("passkey")}
	if err := db.AddWebAuthnCredential(1, credential, "key"); err != nil {
		t.Fatal(err)
	}
	id, err := db.GetWebAuthnUserIDByCredentialID(credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	user, err := db.GetUserFromDBByID(id)
	if err != nil || user.ID != 1 || user.HashedPassword.Valid {
		t.Fatalf("passkey user = %#v, %v", user, err)
	}
	if err := db.UpdateWebAuthnCredential(credential); err != nil {
		t.Fatal(err)
	}
	credentials, err := db.GetWebAuthnCredentialsByUserID(1)
	if err != nil || len(credentials) != 1 {
		t.Fatalf("credentials = %#v, %v", credentials, err)
	}
	infos, err := db.ListUserWebAuthnCredentials(1)
	if err != nil || len(infos) != 1 {
		t.Fatalf("credential infos = %#v, %v", infos, err)
	}
	tokenHash := []byte("test-token-digest")
	family := []byte("test-family")
	if err := db.CreateAuthRefreshToken(1, tokenHash, family, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rotation, err := db.RotateAuthRefreshToken(tokenHash, []byte("next-token-digest"), time.Now(), time.Now().Add(time.Hour))
	if err != nil || rotation.User.ID != 1 {
		t.Fatalf("rotation = %#v, %v", rotation, err)
	}
	if _, err := db.RevokeAuthRefreshToken([]byte("next-token-digest"), time.Now()); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.DeleteWebAuthnCredential(credential.ID, 1); err != nil || !ok {
		t.Fatalf("delete credential = %v, %v", ok, err)
	}
	// The application role must not acquire schema ownership.
	if _, err := app.Exec("CREATE TABLE forbidden (id integer)"); err == nil {
		t.Fatal("application role can create tables")
	}
}

func TestPostgresBrowserSessionContract(t *testing.T) {
	_, app := postgresFixture(t)
	db.Configure(app)
	t.Cleanup(func() { db.Configure(nil) })
	if err := db.CheckBrowserSessionSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	hash := auth.DigestToken("browser-contract-session")
	expiry := now.Add(auth.BrowserSessionMaxTTL)
	if err := db.CreateBrowserSession(t.Context(), hash, 1, now, expiry); err != nil {
		t.Fatal(err)
	}
	user, actualExpiry, err := db.TouchBrowserSession(t.Context(), hash, now.Add(time.Hour), auth.BrowserSessionIdleTTL)
	if err != nil || user.ID != 1 || !actualExpiry.Truncate(time.Microsecond).Equal(expiry.Truncate(time.Microsecond)) {
		t.Fatalf("browser session = %#v, %v, %v", user, actualExpiry, err)
	}
	if err := db.DeleteBrowserSession(t.Context(), hash); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.TouchBrowserSession(t.Context(), hash, now.Add(2*time.Hour), auth.BrowserSessionIdleTTL); !errors.Is(err, db.ErrInvalidBrowserSession) {
		t.Fatalf("deleted browser session = %v", err)
	}
	if err := db.CreateBrowserSession(t.Context(), hash, 1, now, expiry); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUserBrowserSessions(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.TouchBrowserSession(t.Context(), hash, now, auth.BrowserSessionIdleTTL); !errors.Is(err, db.ErrInvalidBrowserSession) {
		t.Fatalf("logout-all browser session = %v", err)
	}
}
