package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	addressEnvironmentVariable           = "GKFEED_ADDRESS"
	databaseEnvironmentVariable          = "GKFEED_DATABASE_URL"
	allowedOriginsEnvironmentVariable    = "GKFEED_ALLOWED_ORIGINS"
	accessTTLEnvironmentVariable         = "GKFEED_ACCESS_TTL"
	jwtRefreshTTLEnvironmentVariable     = "GKFEED_JWT_REFRESH_TTL"
	webauthnRPIDEnvironmentVariable      = "GKFEED_WEBAUTHN_RP_ID"
	webauthnRPOriginEnvironmentVariable  = "GKFEED_WEBAUTHN_RP_ORIGIN"
	webauthnRPDisplayEnvironmentVariable = "GKFEED_WEBAUTHN_RP_DISPLAY"
)

var defaultAllowedOrigins = []string{
	"http://localhost",
	"http://localhost:4200",
	"http://localhost:8086",
}

const (
	defaultAccessTokenTTL    = 30 * time.Minute
	defaultRefreshTokenTTL   = 90 * 24 * time.Hour
	defaultWebAuthnRPDisplay = "GKFeed"
)

type Config struct {
	Address           string
	DatabaseURL       string
	AllowedOrigins    []string
	ReadHeaderTimeout time.Duration

	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	WebAuthnRPID      string
	WebAuthnRPOrigin  string
	WebAuthnRPDisplay string
}

func (c Config) EffectiveAccessTokenTTL() time.Duration {
	if c.AccessTokenTTL > 0 {
		return c.AccessTokenTTL
	}
	return defaultAccessTokenTTL
}

func (c Config) EffectiveRefreshTokenTTL() time.Duration {
	if c.RefreshTokenTTL > 0 {
		return c.RefreshTokenTTL
	}
	return defaultRefreshTokenTTL
}

func Load() (Config, error) {
	if strings.TrimSpace(os.Getenv(databaseEnvironmentVariable)) == "" {
		return Config{}, fmt.Errorf("%s must be set to a PostgreSQL connection URL", databaseEnvironmentVariable)
	}
	return Config{
		Address:           valueOrDefault(addressEnvironmentVariable, ":8086"),
		DatabaseURL:       strings.TrimSpace(os.Getenv(databaseEnvironmentVariable)),
		AllowedOrigins:    allowedOrigins(),
		ReadHeaderTimeout: 5 * time.Second,

		AccessTokenTTL:  durationOrDefault(accessTTLEnvironmentVariable, defaultAccessTokenTTL),
		RefreshTokenTTL: durationOrDefault(jwtRefreshTTLEnvironmentVariable, defaultRefreshTokenTTL),

		WebAuthnRPID:      os.Getenv(webauthnRPIDEnvironmentVariable),
		WebAuthnRPOrigin:  os.Getenv(webauthnRPOriginEnvironmentVariable),
		WebAuthnRPDisplay: valueOrDefault(webauthnRPDisplayEnvironmentVariable, defaultWebAuthnRPDisplay),
	}, nil
}

func allowedOrigins() []string {
	value := os.Getenv(allowedOriginsEnvironmentVariable)
	if value == "" {
		return slices.Clone(defaultAllowedOrigins)
	}

	origins := make([]string, 0)
	for origin := range strings.SplitSeq(value, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return d
}
