package services

import (
	"errors"
	"net/http"
	"net/netip"
	"testing"
)

func TestYoutubePageIdentityEvidence(t *testing.T) {
	const id = "UC5TRrMsWLy7flttFTyS-bOA"
	for _, test := range []struct {
		name, body string
		valid      bool
	}{
		{"canonical", `<link rel="canonical" href="https://www.youtube.com/channel/` + id + `"><title>Channel</title>`, true},
		{"body metadata", `<body><meta property="og:url" content="https://www.youtube.com/channel/` + id + `">`, true},
		{"RSS", `<link rel="alternate" type="application/rss+xml" href="https://www.youtube.com/feeds/videos.xml?channel_id=` + id + `">`, true},
		{"head channelId", `<head><meta itemprop="channelId" content="` + id + `"></head>`, true},
		{"recommended channel", `<body><meta itemprop="channelId" content="` + id + `"><script>{"channelId":"` + id + `"}</script>`, false},
		{"conflicting metadata", `<link rel="canonical" href="https://www.youtube.com/channel/` + id + `"><meta property="og:url" content="https://www.youtube.com/channel/UC1234567890123456789012">`, false},
		{"foreign metadata", `<link rel="canonical" href="https://example.com/channel/` + id + `">`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			metadata := &PageMetadata{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return stubResponse(r, 200, test.body), nil })}}
			got, _, err := metadata.YoutubeChannel(t.Context(), "https://www.youtube.com/@example")
			if test.valid && (err != nil || got != id) {
				t.Fatalf("identity = %q, %v", got, err)
			}
			if !test.valid && !errors.Is(err, ErrChannelUnresolved) {
				t.Fatalf("accepted ambiguous identity: %q, %v", got, err)
			}
		})
	}
}

func TestMetadataUsesOpenGraphTitle(t *testing.T) {
	metadata := &PageMetadata{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return stubResponse(r, 200, `<title>Fallback</title><meta property="og:title" content=" Feed &amp; title ">`), nil
	})}}
	got, err := metadata.Title(t.Context(), "https://example.com")
	if err != nil || got != "Feed & title" {
		t.Fatalf("title = %q, %v", got, err)
	}
}

func TestMetadataPublicAddressesOnly(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "0.0.0.0", "2001:db8::1", "fe80::1", "fc00::1", "64:ff9b::7f00:1"} {
		if isPublicIP(netip.MustParseAddr(raw)) {
			t.Fatalf("allowed nonpublic IP %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublicIP(netip.MustParseAddr(raw)) {
			t.Fatalf("blocked public IP %s", raw)
		}
	}
	client := newPublicHTTPClient()
	for _, raw := range []string{"http://127.0.0.1/", "http://[::1]/", "http://169.254.169.254/"} {
		request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, raw, nil)
		if response, err := client.Do(request); err == nil {
			response.Body.Close()
			t.Fatalf("connected to private address %s", raw)
		}
	}
}
