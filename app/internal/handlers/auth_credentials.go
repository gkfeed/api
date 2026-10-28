package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// @Summary      List passkeys
// @Description  Returns all registered passkeys for the authenticated user.
// @Tags         auth
// @Produce      json
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      200  {array}   object{id=string,name=string,created_at=string,last_used_at=string}
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/credentials [get]
func (h *AuthHandler) ListCredentials(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	infos, err := h.credentials.ListUserWebAuthnCredentials(r.Context(), user.ID)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("list credentials: %w", err))
		return
	}

	type credentialResponse struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		CreatedAt  string  `json:"created_at"`
		LastUsedAt *string `json:"last_used_at"`
	}

	result := make([]credentialResponse, len(infos))
	for i, info := range infos {
		result[i] = credentialResponse{
			ID:         base64.RawURLEncoding.EncodeToString(info.ID),
			Name:       info.Name,
			CreatedAt:  info.CreatedAt,
			LastUsedAt: info.LastUsedAt,
		}
	}

	writeJSON(w, result)
}

// @Summary      Delete passkey
// @Description  Removes a specific passkey credential for the authenticated user.
// @Tags         auth
// @Param        id   path      string  true  "Credential ID (base64url)"
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      204
// @Failure      400
// @Failure      401
// @Failure      404
// @Failure      500
// @Router       /api/v1/auth/credentials/{id} [delete]
func (h *AuthHandler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	credentialID, err := base64.RawURLEncoding.DecodeString(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid credential ID", http.StatusBadRequest)
		return
	}

	deleted, err := h.credentials.DeleteWebAuthnCredential(r.Context(), credentialID, user.ID)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("delete credential: %w", err))
		return
	}
	if !deleted {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
