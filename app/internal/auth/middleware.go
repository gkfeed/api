package auth

import (
	"log"
	"net/http"
	"strings"
)

func Authenticate(sessions *SessionStore) func(http.HandlerFunc) http.HandlerFunc {
	return func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if sessions != nil {
				if session, ok := sessions.Get(bearerToken(r)); ok {
					log.Printf("auth: authenticated via access token user=%s id=%d", session.User.Name, session.User.ID)
					handler(w, r.WithContext(WithUser(r.Context(), session.User)))
					return
				}
			}

			username, password, ok := r.BasicAuth()
			if ok {
				user, authenticated, err := authenticateWithDB(username, password)
				if err != nil {
					log.Printf("auth: basic auth error for user=%q: %v", username, err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				if authenticated {
					log.Printf("auth: authenticated via BasicAuth user=%s id=%d", user.Name, user.ID)
					handler(w, r.WithContext(WithUser(r.Context(), user)))
					return
				}
				log.Printf("auth: basic auth failed (wrong password) for user=%q", username)
			}

			if r.Header.Get("Authorization") != "" {
				log.Printf("auth: rejecting request, Authorization header provided, parsed basic=%v", ok)
			} else {
				log.Printf("auth: rejecting request, no Authorization header")
			}
			if sessions != nil {
				w.Header().Add("WWW-Authenticate", "Bearer")
			}
			w.Header().Add("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		}
	}
}

func bearerToken(r *http.Request) string {
	token, ok := parseBearerToken(r.Header.Get("Authorization"))
	if !ok {
		return ""
	}
	return token
}

func parseBearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
