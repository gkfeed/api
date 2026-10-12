package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gkfeed/api/internal/config"
)

var resourceOperations = []struct{ method, path string }{
	{http.MethodGet, "/api/v2/feeds"},
	{http.MethodGet, "/api/v2/feeds/11"},
	{http.MethodDelete, "/api/v2/feeds/11"},
	{http.MethodGet, "/api/v2/items/22"},
	{http.MethodDelete, "/api/v2/items/22"},
}

func resourceRequest(handler http.Handler, method, path, credentials string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	switch credentials {
	case "owner", "other":
		request.SetBasicAuth(credentials, "secret")
	case "wrong-password":
		request.SetBasicAuth("owner", "wrong")
	default:
		if credentials != "" {
			request.Header.Set("Authorization", credentials)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertResourceError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if response.Code != status || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %v %s, want JSON status %d", response.Code, response.Header(), response.Body.String(), status)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	want := map[string]string{"error": code, "message": message}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("error response = %#v, want %#v", body, want)
	}
}

func resourceBearer(t *testing.T, handler http.Handler) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"owner","password":"secret"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &tokens); response.Code != 200 || err != nil || tokens.AccessToken == "" {
		t.Fatalf("login = %d %s, %v", response.Code, response.Body.String(), err)
	}
	return "Bearer " + tokens.AccessToken
}

func TestResourceErrorsRequireAuthentication(t *testing.T) {
	resourceTestDatabase(t)
	handler := newHandler(config.Config{})
	for _, operation := range resourceOperations {
		for _, credentials := range []string{"", "wrong-password", "Bearer invalid-token"} {
			t.Run(operation.method+operation.path+credentials, func(t *testing.T) {
				response := resourceRequest(handler, operation.method, operation.path, credentials)
				assertResourceError(t, response, 401, "unauthorized", "Unauthorized")
				if got := response.Header().Values("WWW-Authenticate"); !slices.Equal(got, []string{"Bearer", `Basic realm="Restricted"`}) {
					t.Fatalf("authentication challenges = %q", got)
				}
			})
		}
	}
	response := resourceRequest(handler, http.MethodGet, "/api/v2/feeds/0", "")
	assertResourceError(t, response, 401, "unauthorized", "Unauthorized")
}

func TestResourceErrorsValidateIDAndOwnership(t *testing.T) {
	resourceTestDatabase(t)
	handler := newHandler(config.Config{})
	for _, resource := range []string{"feeds", "items"} {
		for _, method := range []string{http.MethodGet, http.MethodDelete} {
			for _, id := range []string{"nope", "0", "-1", strings.Repeat("9", 32)} {
				path := "/api/v2/" + resource + "/" + id
				t.Run(method+path, func(t *testing.T) {
					response := resourceRequest(handler, method, path, "owner")
					assertResourceError(t, response, 400, "invalid_id", "ID must be a positive integer")
				})
			}
			id := "11"
			if resource == "items" {
				id = "22"
			}
			foreign := resourceRequest(handler, method, "/api/v2/"+resource+"/"+id, "other")
			missing := resourceRequest(handler, method, "/api/v2/"+resource+"/999", "owner")
			assertResourceError(t, foreign, 404, "not_found", "Resource not found")
			assertResourceError(t, missing, 404, "not_found", "Resource not found")
			if foreign.Body.String() != missing.Body.String() {
				t.Fatal("foreign and missing resources disclose different responses")
			}
		}
	}
}

func TestResourceResponsesAcceptBearer(t *testing.T) {
	resourceTestDatabase(t)
	handler := newHandler(config.Config{})
	credentials := resourceBearer(t, handler)
	wantFeed := map[string]any{
		"id": float64(11), "title": "Owner feed", "type": "web",
		"url": "https://example.com/feed", "userid": float64(1),
	}
	response := resourceRequest(handler, http.MethodGet, "/api/v2/feeds", credentials)
	var feeds []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &feeds); response.Code != 200 || err != nil || !reflect.DeepEqual(feeds, []map[string]any{wantFeed}) {
		t.Fatalf("feed list = %d %s, %v", response.Code, response.Body.String(), err)
	}
	response = resourceRequest(handler, http.MethodGet, "/api/v2/feeds/11", credentials)
	var feed map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &feed); response.Code != 200 || err != nil || !reflect.DeepEqual(feed, feeds[0]) || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("feed read = %d %s, %v", response.Code, response.Body.String(), err)
	}
	response = resourceRequest(handler, http.MethodGet, "/api/v2/items/22", credentials)
	var item map[string]any
	wantItem := map[string]any{
		"feed": wantFeed,
		"item": map[string]any{"id": float64(22), "feed_id": float64(11), "title": "Item", "text": "Text", "date": "2026-01-01T00:00:00Z", "link": "https://example.com/item"},
	}
	if err := json.Unmarshal(response.Body.Bytes(), &item); response.Code != 200 || err != nil || !reflect.DeepEqual(item, wantItem) || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("item read = %d %s, %v", response.Code, response.Body.String(), err)
	}
	response = resourceRequest(handler, http.MethodGet, "/api/v2/feeds", "other")
	if response.Code != 200 || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("empty feeds = %d %s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/api/v2/items/23", "/api/v2/feeds/11"} {
		response = resourceRequest(handler, http.MethodDelete, path, credentials)
		if response.Code != 204 || response.Body.Len() != 0 {
			t.Fatalf("delete %s = %d %s", path, response.Code, response.Body.String())
		}
		assertResourceError(t, resourceRequest(handler, http.MethodDelete, path, credentials), 404, "not_found", "Resource not found")
		assertResourceError(t, resourceRequest(handler, http.MethodGet, path, credentials), 404, "not_found", "Resource not found")
	}
	assertResourceError(t, resourceRequest(handler, http.MethodGet, "/api/v2/items/22", credentials), 404, "not_found", "Resource not found")
}

func TestResourceErrorsHideStorageFailures(t *testing.T) {
	database := resourceTestDatabase(t)
	handler := newHandler(config.Config{})
	credentials := resourceBearer(t, handler)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	for _, operation := range resourceOperations {
		t.Run(operation.method+operation.path, func(t *testing.T) {
			response := resourceRequest(handler, operation.method, operation.path, credentials)
			assertResourceError(t, response, 500, "internal_error", "Internal Server Error")
		})
	}
	// A Basic lookup failure is produced by auth before the library handler.
	assertResourceError(t, resourceRequest(handler, http.MethodGet, "/api/v2/feeds", "owner"), 500, "internal_error", "Internal Server Error")
	response := resourceRequest(handler, http.MethodGet, "/api/v1/list", credentials)
	if response.Code != 500 || response.Body.String() != "Internal Server Error\n" || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("v1 storage error = %d %v %s", response.Code, response.Header(), response.Body.String())
	}
}

func TestResourceMethodErrors(t *testing.T) {
	handler := newHandler(config.Config{})
	for _, test := range []struct{ method, path, allow string }{
		{http.MethodDelete, "/api/v2/feeds", "GET, POST"},
		{http.MethodPatch, "/api/v2/feeds/11", "GET, DELETE"},
		{http.MethodPost, "/api/v2/items/22", "GET, DELETE"},
		{http.MethodPatch, "/api/v2/items/nope", "GET, DELETE"},
		{http.MethodHead, "/api/v2/feeds/11", "GET, DELETE"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response := resourceRequest(handler, test.method, test.path, "")
			assertResourceError(t, response, 405, "method_not_allowed", "Method not allowed")
			if response.Header().Get("Allow") != test.allow || response.Header().Get("WWW-Authenticate") != "" {
				t.Fatalf("method error headers = %v", response.Header())
			}
		})
	}
}

func TestResourceErrorsPreserveOtherRoutes(t *testing.T) {
	resourceTestDatabase(t)
	handler := newHandler(config.Config{})
	for _, test := range []struct {
		method, path, credentials, body string
		status                          int
	}{
		{http.MethodGet, "/api/v1/list", "", "Unauthorized\n", 401},
		{http.MethodDelete, "/api/v1/delete?id=0", "owner", "Invalid id\n", 400},
		{http.MethodGet, "/api/v1/item?id=999", "owner", "Not Found\n", 404},
		{http.MethodDelete, "/api/v1/items/999", "owner", "Not Found\n", 404},
		{http.MethodGet, "/api/v1/delete?id=11", "", "404 page not found\n", 404},
		{http.MethodPatch, "/api/v1/items/22", "", "404 page not found\n", 404},
		{http.MethodPost, "/api/v2/feeds", "", "Unauthorized\n", 401},
		{http.MethodPost, "/api/v2/feeds", "owner", "Invalid request body\n", 400},
		{http.MethodGet, "/api/v2/items?limit=nope", "owner", "Invalid limit\n", 400},
		{http.MethodGet, "/api/v2/items/sync?limit=nope", "owner", "Invalid limit\n", 400},
		{http.MethodGet, "/api/v2/items/changes?cursor=nope", "owner", "Invalid cursor\n", 400},
		{http.MethodPatch, "/api/v2/items/sync", "", "Method Not Allowed\n", 405},
		{http.MethodDelete, "/api/v2/items/changes", "owner", "Invalid id\n", 400},
		{http.MethodGet, "/api/v2/unknown", "", "404 page not found\n", 404},
		{http.MethodGet, "/api/v2/feeds/11/extra", "", "404 page not found\n", 404},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response := resourceRequest(handler, test.method, test.path, test.credentials)
			if response.Code != test.status || response.Body.String() != test.body || response.Header().Get("Content-Type") == "application/json" || response.Header().Get("Allow") != "" {
				t.Fatalf("legacy response = %d %v %q, want %d %q", response.Code, response.Header(), response.Body.String(), test.status, test.body)
			}
		})
	}
}

func TestResourceErrorsPreserveBrowserSessionPolicyAndCookies(t *testing.T) {
	_, cfg := browserFixture(t)
	handler := newHandler(cfg)
	cookie := browserLogin(t, handler)
	response := browserRequest(handler, http.MethodDelete, "/api/v2/feeds/11", "", "", cookie)
	if response.Code != 403 || response.Body.String() != "Forbidden origin\n" || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("browser Origin error = %d %v %s", response.Code, response.Header(), response.Body.String())
	}
	response = browserRequest(handler, http.MethodDelete, "/api/v2/sessions/current", "", browserOrigin, cookie)
	if response.Code != 204 {
		t.Fatalf("browser logout = %d %s", response.Code, response.Body.String())
	}
	response = browserRequest(handler, http.MethodGet, "/api/v2/feeds", "", "", cookie)
	assertResourceError(t, response, 401, "unauthorized", "Unauthorized")
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookie.Name || cookies[0].MaxAge != -1 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("revoked browser session headers = %v", response.Header())
	}
}

func TestResourceErrorsPreserveCORSPreflight(t *testing.T) {
	handler := newHandler(config.Config{AllowedOrigins: []string{"https://reader.example"}})
	request := httptest.NewRequest(http.MethodOptions, "/api/v2/feeds/11", nil)
	request.Header.Set("Origin", "https://reader.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 204 || response.Body.Len() != 0 || response.Header().Get("Access-Control-Allow-Origin") != "https://reader.example" {
		t.Fatalf("preflight = %d %v %s", response.Code, response.Header(), response.Body.String())
	}
}
