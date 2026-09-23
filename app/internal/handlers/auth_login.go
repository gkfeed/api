package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"gkfeed/api/internal/auth"

	"github.com/go-webauthn/webauthn/protocol"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// @Summary      Login with a password
// @Description  Exchanges username and password for a short-lived access token and a rotating refresh token.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      loginRequest  true  "Credentials"
// @Success      200   {object}  tokenResponse
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/login [post]
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	user, authenticated, err := auth.AuthenticatePassword(req.Username, req.Password)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("authenticate password: %w", err))
		return
	}
	if !authenticated {
		writeBearerUnauthorized(w)
		return
	}

	tokens, err := h.issueTokenPair(user)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("create authentication session: %w", err))
		return
	}
	writeJSON(w, tokens)
}

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
// @Description  Completes WebAuthn authentication, returns an access/refresh token pair.
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

	tokens, err := h.issueTokenPair(user)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("create authentication session: %w", err))
		return
	}

	writeJSON(w, struct {
		tokenResponse
		CredentialID string `json:"credential_id"`
	}{
		tokenResponse: tokens,
		CredentialID:  base64.RawURLEncoding.EncodeToString(credential.ID),
	})
}
