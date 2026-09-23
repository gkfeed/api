package models

import (
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

type WebAuthnCredential struct {
	ID              []byte
	UserID          int
	PublicKey       []byte
	AttestationType string
	Transport       []string
	Flags           webauthn.CredentialFlags
	Authenticator   webauthn.Authenticator
	SignCount       uint32
	Name            string
	CreatedAt       time.Time
	LastUsedAt      *time.Time
}
