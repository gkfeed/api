package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"net/url"
	"slices"
	"time"

	"gkfeed/api/internal/db"
	"gkfeed/api/internal/models"
)

const (
	BrowserSessionCookie  = "gkfeed.session"
	BrowserSessionIdleTTL = 30 * 24 * time.Hour
	BrowserSessionMaxTTL  = 90 * 24 * time.Hour
)

type BrowserSessionStore struct {
	origins    []string
	repository BrowserSessionRepository
}

func NewBrowserSessionStore(origins []string, repository BrowserSessionRepository) *BrowserSessionStore {
	return &BrowserSessionStore{origins: slices.Clone(origins), repository: repository}
}

func (s *BrowserSessionStore) Create(ctx context.Context, user models.User) (string, time.Time, error) {
	token, err := GenerateOpaqueToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(BrowserSessionMaxTTL)
	if err := s.repository.CreateBrowserSession(ctx, DigestToken(token), user.ID, now, expiresAt); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *BrowserSessionStore) SetCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	now := time.Now().UTC()
	expiry := minTime(now.Add(BrowserSessionIdleTTL), expiresAt)
	maxAge := int(expiry.Sub(now) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name: BrowserSessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteLaxMode, Expires: expiry, MaxAge: maxAge,
	})
}

func (s *BrowserSessionStore) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: BrowserSessionCookie, Path: "/", HttpOnly: true,
		Secure: true, SameSite: http.SameSiteLaxMode, Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
	})
}

func (s *BrowserSessionStore) RequireOrigin(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		values := r.Header.Values("Origin")
		origin := r.Header.Get("Origin")
		parsed, err := url.Parse(origin)
		// Exact origins only. CORS wildcard rules do not authorize cookie mutations.
		if len(values) != 1 || err != nil || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
			!slices.Contains(s.origins, origin) {
			http.Error(w, "Forbidden origin", http.StatusForbidden)
			return
		}
		handler(w, r)
	}
}

func (s *BrowserSessionStore) Authenticate(handler http.HandlerFunc) http.HandlerFunc {
	authenticated := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookie, err := r.Cookie(BrowserSessionCookie)
		if err != nil || !validBrowserToken(cookie.Value) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		user, expiresAt, err := s.repository.TouchBrowserSession(r.Context(), DigestToken(cookie.Value), time.Now().UTC(), BrowserSessionIdleTTL)
		if errors.Is(err, db.ErrInvalidBrowserSession) {
			s.ClearCookie(w)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if err != nil {
			log.Printf("browser authentication failed: %v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		s.SetCookie(w, cookie.Value, expiresAt)
		handler(w, r.WithContext(WithUser(r.Context(), user)))
	}
	guarded := s.RequireOrigin(authenticated)
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			authenticated(w, r)
		default:
			guarded(w, r)
		}
	}
}

// Explicit Authorization retains the v1 Basic/Bearer contract. Cookie requests
// use the same protected handlers with Origin checks on every unsafe method.
func (s *BrowserSessionStore) WithFallback(handler, fallback http.HandlerFunc) http.HandlerFunc {
	cookieHandler := s.Authenticate(handler)
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(BrowserSessionCookie); err == nil && r.Header.Get("Authorization") == "" {
			cookieHandler(w, r)
			return
		}
		fallback(w, r)
	}
}

func validBrowserToken(token string) bool {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(raw) == opaqueTokenSize
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *BrowserSessionStore) Delete(ctx context.Context, hash []byte) error {
	return s.repository.DeleteBrowserSession(ctx, hash)
}

func (s *BrowserSessionStore) DeleteUser(ctx context.Context, userID int) error {
	return s.repository.DeleteUserBrowserSessions(ctx, userID)
}
