package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
)

type registerBeginRequest struct {
	Name string `json:"name"`
}

// @Summary      Begin passkey registration
// @Description  Starts WebAuthn registration, returns creation options for the browser.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body  registerBeginRequest  false  "Optional name for the passkey"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/register/begin [post]
func (h *AuthHandler) BeginRegistration(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	var req registerBeginRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	creation, err := h.webAuthnService.BeginRegistration(r.Context(), user, req.Name)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("begin registration: %w", err))
		return
	}

	writeJSON(w, creation)
}

// @Summary      Finish passkey registration
// @Description  Completes WebAuthn registration with the attestation response from the browser.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "WebAuthn attestation response"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200   {object}  object{id=string,name=string}
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/register/finish [post]
func (h *AuthHandler) FinishRegistration(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		http.Error(w, "Invalid credential creation response", http.StatusBadRequest)
		return
	}

	credential, name, err := h.webAuthnService.FinishRegistration(r.Context(), user, parsedResponse)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("finish registration: %w", err))
		return
	}

	writeJSON(w, struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{
		ID:   base64.RawURLEncoding.EncodeToString(credential.ID),
		Name: name,
	})
}
