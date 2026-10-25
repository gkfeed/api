package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPasswordAdmissionIsSharedAcrossHTTPEntryPoints(t *testing.T) {
	database, cfg := browserFixture(t)
	if _, err := database.Exec("INSERT INTO users (id,name,hashed_password) VALUES (2,'passwordless',NULL)"); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(cfg)
	otherHandler := newHandler(cfg)
	login := browserRequest(handler, "POST", "/api/v1/auth/login", `{"username":"reader","password":"secret"}`, "", nil)
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &tokens); login.Code != 200 || err != nil || tokens.AccessToken == "" {
		t.Fatalf("setup login = %d %s, %v", login.Code, login.Body.String(), err)
	}

	// Occupy the fixture's only SQL connection. Two admitted password requests
	// now wait in lookup without allocating any Argon2 memory.
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	released := false
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{}, 2)
	defer func() {
		cancel()
		if released {
			return
		}
		for range 2 {
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Error("cancelled password lookup did not finish")
			}
		}
	}()
	basic := httptest.NewRequest("GET", "/api/v1/auth/me", nil).WithContext(ctx)
	basic.SetBasicAuth("reader", "secret")
	passwordLogin := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"reader","password":"secret"}`)).WithContext(ctx)
	for i, request := range []*http.Request{basic, passwordLogin} {
		go func() {
			handler.ServeHTTP(httptest.NewRecorder(), request)
			finished <- struct{}{}
		}()
		deadline := time.After(5 * time.Second)
		ticker := time.NewTicker(time.Millisecond)
		for database.Stats().WaitCount < int64(i+1) {
			select {
			case <-deadline:
				ticker.Stop()
				t.Fatal("admitted request did not reach lookup")
			case <-ticker.C:
			}
		}
		ticker.Stop()
	}

	for _, username := range []string{"reader", "missing", "passwordless"} {
		for _, endpoint := range []struct{ method, path string }{
			{"GET", "/api/v1/auth/me"},
			{"GET", "/api/v2/feeds"},
			{"POST", "/api/v1/auth/login"},
			{"POST", "/api/v2/sessions"},
		} {
			t.Run(username+endpoint.path, func(t *testing.T) {
				requestCtx, stop := context.WithTimeout(t.Context(), 2*time.Second)
				defer stop()
				request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{"username":"`+username+`","password":"wrong"}`)).WithContext(requestCtx)
				if endpoint.method == "GET" {
					request.SetBasicAuth(username, "wrong")
				}
				request.Header.Set("Origin", browserOrigin)
				response := httptest.NewRecorder()
				otherHandler.ServeHTTP(response, request)
				if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" || response.Header().Get("WWW-Authenticate") != "" || len(response.Result().Cookies()) != 0 {
					t.Fatalf("overload = %d %v %s", response.Code, response.Header(), response.Body.String())
				}
				if endpoint.path == "/api/v2/feeds" {
					assertResourceError(t, response, 503, "service_unavailable", "Service Unavailable")
				} else if response.Body.String() != "Service Unavailable\n" {
					t.Fatalf("overload disclosed authentication details: %s", response.Body.String())
				}
			})
		}
	}
	// Authentication without password verification remains available.
	request := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Bearer authentication during overload = %d %s", response.Code, response.Body.String())
	}
	if response := browserRequest(otherHandler, "GET", "/api/v1/feed_types", "", "", nil); response.Code != http.StatusOK {
		t.Fatalf("public route during overload = %d", response.Code)
	}

	cancel()
	for range 2 {
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled password lookup did not release admission")
		}
	}
	released = true
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"reader", "missing", "passwordless"} {
		response := browserRequest(otherHandler, "POST", "/api/v1/auth/login", `{"username":"`+username+`","password":"wrong"}`, "", nil)
		if response.Code != http.StatusUnauthorized || response.Header().Get("Retry-After") != "" {
			t.Fatalf("invalid password after overload = %d %v %s", response.Code, response.Header(), response.Body.String())
		}
	}
}
