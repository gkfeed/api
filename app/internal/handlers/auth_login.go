package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
)

// @Summary      Begin passkey login
// @Description  Starts discoverable WebAuthn login, returns assertion options for the browser.
// @Tags         auth
// @Produce      json
// @Success      200
// @Failure      500
// @Router       /api/v1/auth/login/begin [post]
func (h *AuthHandler) BeginLogin(w http.ResponseWriter, _ *http.Request) {
	assertion, err := h.webAuthnService.BeginLogin()
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("begin login: %w", err))
		return
	}

	writeJSON(w, assertion)
}

// @Summary      Finish passkey login
// @Description  Completes WebAuthn authentication, returns a JWT access/refresh token pair.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      object  true  "WebAuthn assertion response"
// @Success      200   {object}  object{access_token=string,refresh_token=string,credential_id=string}
// @Failure      400
// @Failure      500
// @Router       /api/v1/auth/login/finish [post]
func (h *AuthHandler) FinishLogin(w http.ResponseWriter, r *http.Request) {
	parsedResponse, err := protocol.ParseCredentialRequestResponseBody(r.Body)
	if err != nil {
		http.Error(w, "Invalid credential assertion response", http.StatusBadRequest)
		return
	}

	credential, user, err := h.webAuthnService.FinishLogin(parsedResponse)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("finish login: %w", err))
		return
	}

	tokens, err := h.issueTokens(user)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}

	writeJSON(w, struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		CredentialID string `json:"credential_id"`
	}{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		CredentialID: base64.RawURLEncoding.EncodeToString(credential.ID),
	})
}
