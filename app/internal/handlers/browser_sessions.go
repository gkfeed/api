package handlers

import (
	"fmt"
	"net/http"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"

	"github.com/go-webauthn/webauthn/protocol"
)

// @Summary Create browser session with a password
// @Description Returns the user and sets gkfeed.session.
// @Tags browser sessions
// @Param Origin header string true "Exact origin from GKFEED_ALLOWED_ORIGINS"
// @Accept json
// @Produce json,plain
// @Param body body loginRequest true "Credentials"
// @Success 201 {object} models.User
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Router /api/v2/sessions [post]
func (h *AuthHandler) CreateBrowserSession(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}
	user, ok, err := auth.AuthenticatePassword(req.Username, req.Password)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("authenticate password: %w", err))
		return
	}
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	h.issueBrowserSession(w, r, user)
}

func (h *AuthHandler) issueBrowserSession(w http.ResponseWriter, r *http.Request, user models.User) {
	token, expiresAt, err := h.browserSessions.Create(r.Context(), user)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("create browser session: %w", err))
		return
	}
	h.browserSessions.SetCookie(w, token, expiresAt)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, models.User{ID: user.ID, Name: user.Name})
}

// @Summary Current browser user
// @Description Uses only gkfeed.session and renews the cookie; Basic/Bearer credentials are ignored.
// @Tags browser sessions
// @Produce json
// @Security BrowserSession
// @Success 200 {object} models.User
// @Failure 401
// @Failure 500
// @Router /api/v2/me [get]
func (h *AuthHandler) BrowserMe(w http.ResponseWriter, r *http.Request) {
	HandleMe(w, r)
}

// @Summary Revoke current browser session
// @Description Revokes and clears gkfeed.session. Cookie is optional; repeated or anonymous logout returns 204.
// @Tags browser sessions
// @Param Origin header string true "Exact origin from GKFEED_ALLOWED_ORIGINS"
// @Success 204
// @Failure 403
// @Failure 500
// @Router /api/v2/sessions/current [delete]
func (h *AuthHandler) DeleteBrowserSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.BrowserSessionCookie)
	if err == nil && cookie.Value != "" {
		if err := db.DeleteBrowserSession(r.Context(), auth.DigestToken(cookie.Value)); err != nil {
			writeInternalServerError(w, fmt.Errorf("delete browser session: %w", err))
			return
		}
	}
	h.browserSessions.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Begin browser passkey login
// @Tags browser sessions
// @Param Origin header string true "Exact origin from GKFEED_ALLOWED_ORIGINS"
// @Produce json,plain
// @Success 200
// @Failure 403
// @Failure 503
// @Router /api/v2/sessions/passkey/begin [post]
func (h *AuthHandler) BeginBrowserLogin(w http.ResponseWriter, r *http.Request) {
	if h.webAuthnService == nil {
		http.Error(w, "Passkey login is not configured", http.StatusServiceUnavailable)
		return
	}
	h.BeginLogin(w, r)
}

// @Summary Finish browser passkey login
// @Description Returns the user and sets gkfeed.session.
// @Tags browser sessions
// @Param Origin header string true "Exact origin from GKFEED_ALLOWED_ORIGINS"
// @Accept json
// @Produce json,plain
// @Param body body object true "WebAuthn assertion response"
// @Success 201 {object} models.User
// @Failure 400
// @Failure 401
// @Failure 403
// @Failure 500
// @Failure 503
// @Router /api/v2/sessions/passkey/finish [post]
func (h *AuthHandler) FinishBrowserLogin(w http.ResponseWriter, r *http.Request) {
	if h.webAuthnService == nil {
		http.Error(w, "Passkey login is not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	parsed, err := protocol.ParseCredentialRequestResponseBody(r.Body)
	if err != nil {
		http.Error(w, "Invalid credential assertion response", http.StatusBadRequest)
		return
	}
	_, user, err := h.webAuthnService.FinishLogin(parsed)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	h.issueBrowserSession(w, r, user)
}
