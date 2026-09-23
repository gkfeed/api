package handlers

import (
	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/config"
)

type AuthHandler struct {
	cfg             config.Config
	webAuthnService *auth.WebAuthnService
	sessions        *auth.SessionStore
}

func NewAuthHandler(cfg config.Config, webAuthnService *auth.WebAuthnService) *AuthHandler {
	return NewAuthHandlerWithSessions(cfg, webAuthnService, nil)
}

func NewAuthHandlerWithSessions(
	cfg config.Config,
	webAuthnService *auth.WebAuthnService,
	sessions *auth.SessionStore,
) *AuthHandler {
	if sessions == nil {
		sessions = auth.NewSessionStore(cfg.EffectiveAccessTokenTTL())
	}
	return &AuthHandler{cfg: cfg, webAuthnService: webAuthnService, sessions: sessions}
}
