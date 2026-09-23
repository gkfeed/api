package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"
)

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

func (h *AuthHandler) issueTokenPair(user models.User) (tokenResponse, error) {
	accessToken, err := auth.GenerateOpaqueToken()
	if err != nil {
		return tokenResponse{}, err
	}
	refreshToken, err := auth.GenerateOpaqueToken()
	if err != nil {
		return tokenResponse{}, err
	}
	familyID, err := auth.GenerateTokenFamily()
	if err != nil {
		return tokenResponse{}, err
	}
	if err := db.CreateAuthRefreshToken(
		user.ID,
		auth.DigestToken(refreshToken),
		familyID,
		time.Now().Add(h.cfg.EffectiveRefreshTokenTTL()),
	); err != nil {
		return tokenResponse{}, fmt.Errorf("store refresh token: %w", err)
	}

	h.sessions.Add(accessToken, models.User{ID: user.ID, Name: user.Name}, familyID)
	return h.tokenResponse(accessToken, refreshToken), nil
}

func (h *AuthHandler) tokenResponse(accessToken, refreshToken string) tokenResponse {
	return tokenResponse{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		TokenType:        "Bearer",
		ExpiresIn:        int64(h.cfg.EffectiveAccessTokenTTL() / time.Second),
		RefreshExpiresIn: int64(h.cfg.EffectiveRefreshTokenTTL() / time.Second),
	}
}

// @Summary      Refresh access token
// @Description  Rotates a refresh token and returns a new access/refresh token pair.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      refreshRequest  true  "Refresh token"
// @Success      200   {object}  tokenResponse
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/refresh [post]
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		http.Error(w, "Refresh token is required", http.StatusBadRequest)
		return
	}

	newRefreshToken, err := auth.GenerateOpaqueToken()
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("generate refresh token: %w", err))
		return
	}
	accessToken, err := auth.GenerateOpaqueToken()
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("generate access token: %w", err))
		return
	}
	now := time.Now()
	rotation, err := db.RotateAuthRefreshToken(
		auth.DigestToken(req.RefreshToken),
		auth.DigestToken(newRefreshToken),
		now,
		now.Add(h.cfg.EffectiveRefreshTokenTTL()),
	)
	if err != nil {
		if errors.Is(err, db.ErrInvalidRefreshToken) || errors.Is(err, db.ErrRefreshTokenReuse) {
			if len(rotation.FamilyID) > 0 {
				h.sessions.RevokeFamily(rotation.FamilyID)
			}
			writeBearerUnauthorized(w)
			return
		}
		writeInternalServerError(w, fmt.Errorf("rotate refresh token: %w", err))
		return
	}

	h.sessions.Add(accessToken, rotation.User, rotation.FamilyID)
	writeJSON(w, h.tokenResponse(accessToken, newRefreshToken))
}

// @Summary      Logout
// @Description  Revokes the refresh-token family associated with the supplied token.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      refreshRequest  true  "Refresh token"
// @Success      204
// @Failure      400
// @Failure      500
// @Router       /api/v1/auth/logout [post]
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		http.Error(w, "Refresh token is required", http.StatusBadRequest)
		return
	}

	familyID, err := db.RevokeAuthRefreshToken(auth.DigestToken(req.RefreshToken), time.Now())
	if err != nil {
		if errors.Is(err, db.ErrInvalidRefreshToken) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeInternalServerError(w, fmt.Errorf("revoke refresh token: %w", err))
		return
	}
	h.sessions.RevokeFamily(familyID)
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Logout all sessions
// @Description  Revokes every refresh token and access session for the authenticated user.
// @Tags         auth
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      204
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/logout-all [post]
func (h *AuthHandler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	if err := db.RevokeUserAuthRefreshTokens(user.ID, time.Now()); err != nil {
		writeInternalServerError(w, fmt.Errorf("revoke user refresh tokens: %w", err))
		return
	}
	h.sessions.RevokeUser(user.ID)
	w.WriteHeader(http.StatusNoContent)
}

func writeBearerUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}
