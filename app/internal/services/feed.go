package services

import (
	"context"
	"errors"
	"strings"

	"gkfeed/api/internal/library"
)

const (
	feedTypeYouTube   = "yt"
	feedTypeInstagram = "inst"
	feedTypeTikTok    = "tiktok"
	feedTypeSpotify   = "spoti"
	feedTypeRezka     = "rezka"
	feedTypeShikimori = "shiki"
)

var (
	errInvalidFeedURL = errors.New("invalid feed URL")
	feedTypesByPrefix = []struct {
		prefix   string
		feedType string
	}{
		{"https://www.youtube.com/@", feedTypeYouTube},
		{"https://www.instagram.com/", feedTypeInstagram},
		{"https://tok.adminforge.de/@", feedTypeTikTok},
		{"https://open.spotify.com/artist/", feedTypeSpotify},
		{"https://hdrezka.me/series/", feedTypeRezka},
		{"https://hdrezka.me/films/", feedTypeRezka},
		{"https://shikimori.one/animes/", feedTypeShikimori},
	}
)

type FeedResolver struct{}

func (FeedResolver) Resolve(_ context.Context, rawURL string) (library.CreateFeedInput, error) {
	return CreateFeedFromURL(rawURL)
}

func CreateFeedFromURL(rawURL string) (library.CreateFeedInput, error) {
	feedType, err := recogniseFeedType(rawURL)
	if err != nil {
		return library.CreateFeedInput{}, err
	}

	return library.CreateFeedInput{
		Title: recogniseFeedTitle(rawURL, feedType),
		Type:  feedType,
		URL:   normaliseFeedURL(rawURL, feedType),
	}, nil
}

func recogniseFeedTitle(rawURL, feedType string) string {
	trimmedURL := strings.TrimSuffix(rawURL, "/")
	lastSegment := trimmedURL[strings.LastIndex(trimmedURL, "/")+1:]

	switch feedType {
	case feedTypeYouTube, feedTypeTikTok:
		_, handle, _ := strings.Cut(rawURL, "@")
		return strings.SplitN(handle, "/", 2)[0]
	case feedTypeRezka:
		return strings.TrimSuffix(lastSegment, ".html")
	default:
		return lastSegment
	}
}

func normaliseFeedURL(rawURL, feedType string) string {
	if feedType != feedTypeTikTok {
		return rawURL
	}

	_, handle, _ := strings.Cut(rawURL, "@")
	return "https://www.tiktok.com/@" + handle
}

func recogniseFeedType(rawURL string) (string, error) {
	for _, candidate := range feedTypesByPrefix {
		if strings.HasPrefix(rawURL, candidate.prefix) && len(rawURL) > len(candidate.prefix) {
			return candidate.feedType, nil
		}
	}

	return "", errInvalidFeedURL
}
