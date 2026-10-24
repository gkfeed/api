package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"gkfeed/api/internal/handlers"
)

// resourceErrors covers the selected resource paths, including errors produced
// before their handlers run. Other v2 operations and v1 retain their responses.
func resourceErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allow := resourceMethods(r.URL.Path)
		if allow == "" || r.URL.Path == "/api/v2/feeds" && r.Method == http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		if !slices.Contains(strings.Split(allow, ", "), r.Method) {
			writeResourceError(w, http.StatusMethodNotAllowed, allow)
			return
		}

		response := &resourceErrorWriter{ResponseWriter: w}
		next.ServeHTTP(response, r)
		if _, ok := resourceError(response.status); ok {
			writeResourceError(w, response.status, allow)
		}
	})
}

func writeResourceError(w http.ResponseWriter, status int, allow string) {
	body, _ := resourceError(status)
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Type", "application/json")
	if status == http.StatusMethodNotAllowed {
		w.Header().Set("Allow", allow)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func resourceMethods(path string) string {
	if path == "/api/v2/feeds" {
		return "GET, POST"
	}
	for _, prefix := range []string{"/api/v2/feeds/", "/api/v2/items/"} {
		id, ok := strings.CutPrefix(path, prefix)
		if !ok || id == "" || strings.Contains(id, "/") {
			continue
		}
		if prefix == "/api/v2/items/" && (id == "sync" || id == "changes") {
			return ""
		}
		return "GET, DELETE"
	}
	return ""
}

func resourceError(status int) (handlers.ResourceError, bool) {
	var code, message string
	switch status {
	case http.StatusBadRequest:
		code, message = "invalid_id", "ID must be a positive integer"
	case http.StatusUnauthorized:
		code, message = "unauthorized", "Unauthorized"
	case http.StatusNotFound:
		code, message = "not_found", "Resource not found"
	case http.StatusMethodNotAllowed:
		code, message = "method_not_allowed", "Method not allowed"
	case http.StatusInternalServerError:
		code, message = "internal_error", "Internal Server Error"
	default:
		return handlers.ResourceError{}, false
	}
	return handlers.ResourceError{Error: code, Message: message}, true
}

// Error bodies are discarded until the handler returns. Successful responses
// pass through directly, so this does not buffer lists or alter empty 204s.
type resourceErrorWriter struct {
	http.ResponseWriter
	status int
}

func (w *resourceErrorWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	if _, ok := resourceError(status); !ok {
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *resourceErrorWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if _, ok := resourceError(w.status); ok {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

func (w *resourceErrorWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
