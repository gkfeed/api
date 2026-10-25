package handlers

import (
	"context"
	"time"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/config"
	"gkfeed/api/internal/db"
)

type RefreshTokenRepository interface {
	CreateAuthRefreshToken(context.Context, int, []byte, []byte, time.Time) error
	RotateAuthRefreshToken(context.Context, []byte, []byte, time.Time, time.Time) (db.RefreshRotation, error)
	RevokeAuthRefreshToken(context.Context, []byte, time.Time) ([]byte, error)
	RevokeUserAuthRefreshTokens(context.Context, int, time.Time) error
}

type CredentialRepository interface {
	ListUserWebAuthnCredentials(context.Context, int) ([]db.WebAuthnCredentialInfo, error)
	DeleteWebAuthnCredential(context.Context, []byte, int) (bool, error)
}

type AuthRepositories struct {
	Users           auth.PasswordUserRepository
	Credentials     CredentialRepository
	RefreshTokens   RefreshTokenRepository
	BrowserSessions auth.BrowserSessionRepository
}

type AuthHandler struct {
	cfg             config.Config
	webAuthnService *auth.WebAuthnService
	sessions        *auth.SessionStore
	browserSessions *auth.BrowserSessionStore
	passwords       *auth.PasswordAuthenticator
	refreshTokens   RefreshTokenRepository
	credentials     CredentialRepository
}

func NewAuthHandler(cfg config.Config, webAuthnService *auth.WebAuthnService, sessions *auth.SessionStore, repositories AuthRepositories) *AuthHandler {
	if sessions == nil {
		sessions = auth.NewSessionStore(cfg.EffectiveAccessTokenTTL())
	}
	return &AuthHandler{
		cfg: cfg, webAuthnService: webAuthnService, sessions: sessions,
		browserSessions: auth.NewBrowserSessionStore(cfg.AllowedOrigins, repositories.BrowserSessions),
		passwords:       auth.NewPasswordAuthenticator(repositories.Users),
		refreshTokens:   repositories.RefreshTokens, credentials: repositories.Credentials,
	}
}
