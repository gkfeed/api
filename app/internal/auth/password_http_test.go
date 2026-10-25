package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuthOverloadIsNotUnauthorized(t *testing.T) {
	for range maxPasswordVerifications {
		passwordVerification.slots <- struct{}{}
	}
	defer func() {
		for range maxPasswordVerifications {
			<-passwordVerification.slots
		}
	}()
	for _, wrap := range []func(http.HandlerFunc) http.HandlerFunc{BasicAuth, Authenticate(nil)} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", "/", nil)
		request.SetBasicAuth("missing", "wrong")
		wrap(func(http.ResponseWriter, *http.Request) {
			t.Error("overloaded authentication reached protected handler")
		})(response, request)
		if response.Code != http.StatusServiceUnavailable || response.Body.String() != "Service Unavailable\n" || response.Header().Get("Retry-After") != "1" || response.Header().Get("WWW-Authenticate") != "" {
			t.Fatalf("overload response = %d %v %s", response.Code, response.Header(), response.Body.String())
		}
	}
}
