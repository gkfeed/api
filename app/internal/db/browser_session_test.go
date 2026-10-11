package db

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestBrowserSessionExpiryAndActivity(t *testing.T) {
	database := useTestDatabase(t)
	now := time.Now().UTC().Truncate(time.Second)
	idle := 30 * 24 * time.Hour
	maximum := 90 * 24 * time.Hour
	for _, test := range []struct {
		name   string
		seen   time.Time
		expiry time.Time
		valid  bool
	}{
		{"active", now.Add(-idle + time.Second), now.Add(maximum), true},
		{"idle boundary", now.Add(-idle), now.Add(maximum), false},
		{"idle expired", now.Add(-idle - time.Second), now.Add(maximum), false},
		{"absolute boundary", now, now, false},
		{"absolute expired", now, now.Add(-time.Second), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			hash := []byte(test.name)
			if err := CreateBrowserSession(t.Context(), hash, 1, test.seen, test.expiry); err != nil {
				t.Fatal(err)
			}
			user, expiresAt, err := TouchBrowserSession(t.Context(), hash, now, idle)
			if !test.valid {
				if !errors.Is(err, ErrInvalidBrowserSession) {
					t.Fatalf("expired session = %v", err)
				}
				return
			}
			if err != nil || user.ID != 1 || user.Name != "reader" || !expiresAt.Equal(test.expiry) {
				t.Fatalf("touch = %#v, %v, %v", user, expiresAt, err)
			}
			var storedHash []byte
			var lastSeen time.Time
			if err := database.QueryRow("SELECT token_hash, last_seen_at FROM browser_sessions WHERE token_hash=$1", hash).Scan(&storedHash, &lastSeen); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(storedHash, hash) || !lastSeen.Equal(now) {
				t.Fatalf("stored session = %x, %v", storedHash, lastSeen)
			}
			// Continuous activity can renew the idle limit but never the absolute limit.
			for day := 29; day < 90; day += 29 {
				if _, expiry, err := TouchBrowserSession(t.Context(), hash, now.Add(time.Duration(day)*24*time.Hour), idle); err != nil || !expiry.Equal(test.expiry) {
					t.Fatalf("day %d = %v, %v", day, expiry, err)
				}
			}
			if _, _, err := TouchBrowserSession(t.Context(), hash, test.expiry, idle); !errors.Is(err, ErrInvalidBrowserSession) {
				t.Fatalf("maximum lifetime = %v", err)
			}
		})
	}
}

func TestBrowserSessionDeleteCannotBeRestored(t *testing.T) {
	useTestDatabase(t)
	hash := []byte("deleted")
	now := time.Now().UTC()
	if err := CreateBrowserSession(t.Context(), hash, 1, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := DeleteBrowserSession(t.Context(), hash); err != nil {
			t.Fatal(err)
		}
		if _, _, err := TouchBrowserSession(t.Context(), hash, now, time.Hour); !errors.Is(err, ErrInvalidBrowserSession) {
			t.Fatalf("deleted session accepted: %v", err)
		}
	}
}

func TestCheckBrowserSessionSchema(t *testing.T) {
	database := useTestDatabase(t)
	if err := CheckBrowserSessionSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DROP TABLE browser_sessions"); err != nil {
		t.Fatal(err)
	}
	if err := CheckBrowserSessionSchema(t.Context()); err == nil {
		t.Fatal("missing browser_sessions accepted")
	}
}
