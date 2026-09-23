package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadUsesEnvironment(t *testing.T) {
	t.Setenv(addressEnvironmentVariable, "127.0.0.1:9000")
	t.Setenv(databaseEnvironmentVariable, "postgres://localhost/gkfeed")
	t.Setenv(allowedOriginsEnvironmentVariable, "https://one.example, https://two.example")
	t.Setenv(accessTTLEnvironmentVariable, "45m")

	configuration, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if configuration.Address != "127.0.0.1:9000" {
		t.Fatalf("Address = %q, want %q", configuration.Address, "127.0.0.1:9000")
	}
	if configuration.DatabaseURL != "postgres://localhost/gkfeed" {
		t.Fatalf("DatabaseURL = %q, want %q", configuration.DatabaseURL, "postgres://localhost/gkfeed")
	}
	wantOrigins := []string{"https://one.example", "https://two.example"}
	if !reflect.DeepEqual(configuration.AllowedOrigins, wantOrigins) {
		t.Fatalf("AllowedOrigins = %#v, want %#v", configuration.AllowedOrigins, wantOrigins)
	}
	if configuration.AccessTokenTTL.String() != "45m0s" {
		t.Fatalf("AccessTokenTTL = %s, want 45m0s", configuration.AccessTokenTTL)
	}
}

func TestEffectiveTokenTTLs(t *testing.T) {
	if got := (Config{}).EffectiveAccessTokenTTL(); got != 30*time.Minute {
		t.Fatalf("default access-token TTL = %s, want 30m", got)
	}
	if got := (Config{}).EffectiveRefreshTokenTTL(); got != 90*24*time.Hour {
		t.Fatalf("default refresh-token TTL = %s, want 2160h", got)
	}

	configuration := Config{
		AccessTokenTTL:  time.Minute,
		RefreshTokenTTL: time.Hour,
	}
	if got := configuration.EffectiveAccessTokenTTL(); got != time.Minute {
		t.Fatalf("configured access-token TTL = %s, want 1m", got)
	}
	if got := configuration.EffectiveRefreshTokenTTL(); got != time.Hour {
		t.Fatalf("configured refresh-token TTL = %s, want 1h", got)
	}
}

func TestLoadReturnsIndependentDefaultOrigins(t *testing.T) {
	t.Setenv(databaseEnvironmentVariable, "postgres://localhost/gkfeed")
	t.Setenv(allowedOriginsEnvironmentVariable, "")
	first, err := Load()
	if err != nil {
		t.Fatalf("first Load() returned error: %v", err)
	}
	first.AllowedOrigins[0] = "changed"

	second, err := Load()
	if err != nil {
		t.Fatalf("second Load() returned error: %v", err)
	}
	if second.AllowedOrigins[0] == "changed" {
		t.Fatal("Load() returned shared default origins")
	}
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv(databaseEnvironmentVariable, "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), databaseEnvironmentVariable) {
		t.Fatalf("error = %v", err)
	}
}
