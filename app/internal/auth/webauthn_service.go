package auth

import (
	"context"
	"fmt"
	"strings"

	"gkfeed/api/internal/config"
	"gkfeed/api/internal/models"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
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

type WebAuthnService struct {
	webAuthn    *webauthn.WebAuthn
	ceremonies  *ceremonyStore
	users       WebAuthnUserRepository
	credentials WebAuthnCredentialRepository
}

func NewWebAuthnService(cfg config.Config, users WebAuthnUserRepository, credentials WebAuthnCredentialRepository) (*WebAuthnService, error) {
	if strings.TrimSpace(cfg.WebAuthnRPID) == "" || strings.TrimSpace(cfg.WebAuthnRPOrigin) == "" {
		return nil, fmt.Errorf("WebAuthn RP ID and origin must be configured")
	}
	webAuthn, err := webauthn.New(&webauthn.Config{
		RPDisplayName: cfg.WebAuthnRPDisplay,
		RPID:          cfg.WebAuthnRPID,
		RPOrigins:     []string{cfg.WebAuthnRPOrigin},
	})
	if err != nil {
		return nil, fmt.Errorf("create webauthn: %w", err)
	}

	service := &WebAuthnService{
		webAuthn:    webAuthn,
		ceremonies:  newCeremonyStore(),
		users:       users,
		credentials: credentials,
	}
	return service, nil
}

func (s *WebAuthnService) BeginRegistration(ctx context.Context, user models.User, credentialName string) (*protocol.CredentialCreation, error) {
	credentials, err := s.credentials.GetWebAuthnCredentialsByUserID(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("get credentials: %w", err)
	}

	webAuthnUser := webAuthnUserAdapter{User: user, credentials: credentials}
	creation, sessionData, err := s.webAuthn.BeginRegistration(
		webAuthnUser,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationPreferred,
		}),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		return nil, fmt.Errorf("begin registration: %w", err)
	}

	s.ceremonies.store(sessionData, credentialName)
	return creation, nil
}

func (s *WebAuthnService) FinishRegistration(ctx context.Context, user models.User, response *protocol.ParsedCredentialCreationData) (*webauthn.Credential, string, error) {
	credentials, err := s.credentials.GetWebAuthnCredentialsByUserID(ctx, user.ID)
	if err != nil {
		return nil, "", fmt.Errorf("get credentials: %w", err)
	}

	webAuthnUser := webAuthnUserAdapter{User: user, credentials: credentials}
	pendingCeremony, ok := s.ceremonies.pop(response.Response.CollectedClientData.Challenge)
	if !ok {
		return nil, "", fmt.Errorf("session not found or expired")
	}

	credential, err := s.webAuthn.CreateCredential(webAuthnUser, *pendingCeremony.session, response)
	if err != nil {
		return nil, "", fmt.Errorf("create credential: %w", err)
	}
	if err := s.credentials.AddWebAuthnCredential(ctx, user.ID, *credential, pendingCeremony.registrationName); err != nil {
		return nil, "", fmt.Errorf("store credential: %w", err)
	}
	return credential, pendingCeremony.registrationName, nil
}

func (s *WebAuthnService) BeginLogin() (*protocol.CredentialAssertion, error) {
	assertion, sessionData, err := s.webAuthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, fmt.Errorf("begin discoverable login: %w", err)
	}
	s.ceremonies.store(sessionData, "")
	return assertion, nil
}

func (s *WebAuthnService) FinishLogin(ctx context.Context, response *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, models.User, error) {
	pendingCeremony, ok := s.ceremonies.pop(response.Response.CollectedClientData.Challenge)
	if !ok {
		return nil, models.User{}, fmt.Errorf("session not found or expired")
	}

	userID, err := s.credentials.GetWebAuthnUserIDByCredentialID(ctx, response.RawID)
	if err != nil {
		return nil, models.User{}, fmt.Errorf("credential not found: %w", err)
	}

	user, err := s.users.GetUserFromDBByID(ctx, userID)
	if err != nil {
		return nil, models.User{}, fmt.Errorf("user not found: %w", err)
	}

	credentials, err := s.credentials.GetWebAuthnCredentialsByUserID(ctx, user.ID)
	if err != nil {
		return nil, models.User{}, fmt.Errorf("get credentials: %w", err)
	}
	webAuthnUser := webAuthnUserAdapter{User: user, credentials: credentials}

	credential, err := s.webAuthn.ValidateDiscoverableLogin(
		func(rawID, userHandle []byte) (webauthn.User, error) {
			if string(userHandle) != string(webAuthnUser.WebAuthnID()) {
				return nil, fmt.Errorf("user handle mismatch")
			}
			return webAuthnUser, nil
		},
		*pendingCeremony.session,
		response,
	)
	if err != nil {
		return nil, models.User{}, fmt.Errorf("validate login: %w", err)
	}

	if err := s.credentials.UpdateWebAuthnCredential(ctx, *credential); err != nil {
		return nil, models.User{}, fmt.Errorf("update credential: %w", err)
	}

	return credential, user, nil
}
