package handlers

import (
	"fmt"
	"net/http"
	"time"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"

	"github.com/google/uuid"
)

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) issueTokens(user models.User) (tokenPair, error) {
	accessToken, err := auth.GenerateAccessToken(user.ID, user.Name, h.cfg)
	if err != nil {
		return tokenPair{}, fmt.Errorf("generate access token: %w", err)
	}
	refreshToken := models.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(h.cfg.RefreshTokenTTL),
	}
	if err := db.StoreRefreshToken(refreshToken); err != nil {
		return tokenPair{}, fmt.Errorf("store refresh token: %w", err)
	}
	return tokenPair{AccessToken: accessToken, RefreshToken: refreshToken.ID}, nil
}

// @Summary      Refresh access token
// @Description  Exchanges a valid refresh token for a new access/refresh token pair. Old refresh token is invalidated.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      refreshRequest  true  "Refresh token"
// @Success      200   {object}  object{access_token=string,refresh_token=string}
// @Failure      400
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/refresh [post]
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	stored, err := db.GetRefreshToken(req.RefreshToken)
	if err != nil {
		http.Error(w, "Invalid refresh token", http.StatusUnauthorized)
		return
	}

	if time.Now().After(stored.ExpiresAt) {
		db.DeleteRefreshToken(stored.ID)
		http.Error(w, "Refresh token expired", http.StatusUnauthorized)
		return
	}

	if err := db.DeleteRefreshToken(stored.ID); err != nil {
		writeInternalServerError(w, fmt.Errorf("delete refresh token: %w", err))
		return
	}

	user, err := db.GetUserFromDBByID(stored.UserID)
	if err != nil {
		writeInternalServerError(w, fmt.Errorf("get user: %w", err))
		return
	}

	tokens, err := h.issueTokens(user)
	if err != nil {
		writeInternalServerError(w, err)
		return
	}
	writeJSON(w, tokens)
}

// @Summary      Logout
// @Description  Invalidates all refresh tokens for the authenticated user.
// @Tags         auth
// @Security     BasicAuth
// @Security     BearerAuth
// @Success      204
// @Failure      401
// @Failure      500
// @Router       /api/v1/auth/logout [post]
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	user, ok := authenticatedUser(w, r)
	if !ok {
		return
	}

	if err := db.DeleteUserRefreshTokens(user.ID); err != nil {
		writeInternalServerError(w, fmt.Errorf("delete refresh tokens: %w", err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
