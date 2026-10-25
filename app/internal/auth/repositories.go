package auth

import (
	"context"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"gkfeed/api/internal/models"
)

type WebAuthnUserRepository interface {
	GetUserFromDBByID(context.Context, int) (models.User, error)
}

type WebAuthnCredentialRepository interface {
	GetWebAuthnCredentialsByUserID(context.Context, int) ([]webauthn.Credential, error)
	GetWebAuthnUserIDByCredentialID(context.Context, []byte) (int, error)
	AddWebAuthnCredential(context.Context, int, webauthn.Credential, string) error
	UpdateWebAuthnCredential(context.Context, webauthn.Credential) error
}

type BrowserSessionRepository interface {
	CreateBrowserSession(context.Context, []byte, int, time.Time, time.Time) error
	TouchBrowserSession(context.Context, []byte, time.Time, time.Duration) (models.User, time.Time, error)
	DeleteBrowserSession(context.Context, []byte) error
	DeleteUserBrowserSessions(context.Context, int) error
}
