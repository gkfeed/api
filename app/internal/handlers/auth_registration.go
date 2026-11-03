package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"gkfeed/api/internal/auth"

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
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/register/begin [post]
func (h *AuthHandler) BeginRegistration(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	var req registerBeginRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		// The registration name is optional, including an empty request body.
		if !errors.Is(err, io.EOF) {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	} else if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "Request body must contain one JSON object", http.StatusBadRequest)
		return
	}

	creation, err := h.webAuthnService.BeginRegistration(user, req.Name)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	parsedResponse, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		http.Error(w, "Invalid credential creation response", http.StatusBadRequest)
		return
	}

	credential, name, err := h.webAuthnService.FinishRegistration(user, parsedResponse)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidWebAuthnCeremony) {
			http.Error(w, "Invalid credential creation response", http.StatusBadRequest)
			return
		}
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
