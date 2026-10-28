package auth

import (
	"log"
	"net/http"
)

func BasicAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if ok {
			user, authenticated, err := authenticateWithDB(r.Context(), username, password)
			if err != nil {
				log.Printf("authentication failed: %v", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if authenticated {
				handler(w, r.WithContext(WithUser(r.Context(), user)))
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	}
}
