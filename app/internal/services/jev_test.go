package services

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"gkfeed/api/internal/library"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func stubResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}
}

func TestJevRequestContract(t *testing.T) {
	detector := NewJevDetector("unit-key")
	detector.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != jevURL || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer unit-key" {
			t.Fatal("unexpected request destination or authorization")
		}
		var body struct {
			Model     string
			State     map[string]string
			Questions map[string]struct {
				Type     string
				Criteria map[string]string
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "typesafe/jev-1.13" || body.State["url"] != "https://example.com" || body.State["user_supplied_title"] != "My title" {
			t.Fatalf("payload = %#v", body)
		}
		criteria := body.Questions["feed_type"].Criteria
		if len(criteria) != len(library.FeedTypeOptions()) {
			t.Fatal("catalog diverged")
		}
		for _, option := range library.FeedTypeOptions() {
			if criteria[string(option.Type)] != option.Description {
				t.Fatalf("missing criterion %s", option.Type)
			}
		}
		return stubResponse(r, 200, `{"answers":{"feed_type":{"type":"choice","choice":"web","confidence":0.8}}}`), nil
	})
	got, err := detector.Detect(t.Context(), "https://example.com", "My title")
	if err != nil || got.Type != "web" || got.Confidence != .8 {
		t.Fatalf("Detect = %#v, %v", got, err)
	}
}

func TestJevRejectsInvalidAnswers(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{}`, `{"answers":{"feed_type":{"type":"choice","choice":"web"}}}`, `{"answers":{"feed_type":{"type":"choice","choice":"unknown","confidence":1}}}`, `{"answers":{"feed_type":{"type":"choice","choice":"web","confidence":2}}}`} {
		d := NewJevDetector("unit-key")
		d.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return stubResponse(r, 200, body), nil })
		if _, err := d.Detect(t.Context(), "https://example.com", ""); !errors.Is(err, ErrDetectionFailed) {
			t.Fatalf("accepted %s: %v", body, err)
		}
	}
	d := NewJevDetector("")
	d.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("request without key"); return nil, nil })
	if _, err := d.Detect(t.Context(), "https://example.com", ""); !errors.Is(err, ErrDetectionUnavailable) {
		t.Fatal(err)
	}
	d = NewJevDetector("unit-key")
	d.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return stubResponse(r, 503, `{"error":"unavailable"}`), nil
	})
	if _, err := d.Detect(t.Context(), "https://example.com", ""); !errors.Is(err, ErrDetectionFailed) {
		t.Fatal(err)
	}
}

func TestJevDoesNotFollowRedirects(t *testing.T) {
	detector := NewJevDetector("unit-key")
	calls := 0
	detector.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != jevURL {
			t.Fatal("followed credential-bearing redirect")
		}
		response := stubResponse(r, http.StatusTemporaryRedirect, "")
		response.Header.Set("Location", "https://example.com/collect")
		return response, nil
	})
	if _, err := detector.Detect(t.Context(), "https://example.com", ""); !errors.Is(err, ErrDetectionFailed) || calls != 1 {
		t.Fatalf("redirect calls=%d, error=%v", calls, err)
	}
}
