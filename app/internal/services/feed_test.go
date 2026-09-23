package services

import (
	"testing"

	"gkfeed/api/internal/library"
)

func TestCreateFeedFromURL(t *testing.T) {
	tests := []struct {
		name      string
		inputURL  string
		wantTitle string
		wantType  library.FeedType
		wantURL   string
	}{
		{
			name:      "YouTube channel",
			inputURL:  "https://www.youtube.com/@example/videos",
			wantTitle: "example",
			wantType:  feedTypeYouTube,
			wantURL:   "https://www.youtube.com/@example/videos",
		},
		{
			name:      "TikTok mirror",
			inputURL:  "https://tok.adminforge.de/@example",
			wantTitle: "example",
			wantType:  feedTypeTikTok,
			wantURL:   "https://www.tiktok.com/@example",
		},
		{
			name:      "Rezka film",
			inputURL:  "https://hdrezka.me/films/drama/example.html",
			wantTitle: "example",
			wantType:  feedTypeRezka,
			wantURL:   "https://hdrezka.me/films/drama/example.html",
		},
		{
			name:      "Instagram profile",
			inputURL:  "https://www.instagram.com/example/",
			wantTitle: "example",
			wantType:  feedTypeInstagram,
			wantURL:   "https://www.instagram.com/example/",
		},
		{
			name:      "Spotify artist",
			inputURL:  "https://open.spotify.com/artist/123",
			wantTitle: "123",
			wantType:  feedTypeSpotify,
			wantURL:   "https://open.spotify.com/artist/123",
		},
		{
			name:      "Rezka series",
			inputURL:  "https://hdrezka.me/series/drama/example.html",
			wantTitle: "example",
			wantType:  feedTypeRezka,
			wantURL:   "https://hdrezka.me/series/drama/example.html",
		},
		{
			name:      "Shikimori anime",
			inputURL:  "https://shikimori.one/animes/123-example",
			wantTitle: "123-example",
			wantType:  feedTypeShikimori,
			wantURL:   "https://shikimori.one/animes/123-example",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			feed, err := CreateFeedFromURL(test.inputURL)
			if err != nil {
				t.Fatalf("CreateFeedFromURL() returned an error: %v", err)
			}
			if feed.Title != test.wantTitle || feed.Type != test.wantType || feed.URL != test.wantURL {
				t.Fatalf("CreateFeedFromURL() = %#v, want title %q, type %q, URL %q", feed, test.wantTitle, test.wantType, test.wantURL)
			}
		})
	}
}

func TestCreateFeedFromURLRejectsIncompleteURL(t *testing.T) {
	_, err := CreateFeedFromURL("https://www.youtube.com/@")
	if err == nil {
		t.Fatal("CreateFeedFromURL() accepted an incomplete URL")
	}
}
