package services

import (
	"context"
	"errors"
	"strings"

	"gkfeed/api/internal/library"
)

const (
	feedTypeYouTube   library.FeedType = "yt"
	feedTypeInstagram library.FeedType = "inst"
	feedTypeTikTok    library.FeedType = "tiktok"
	feedTypeSpotify   library.FeedType = "spoti"
	feedTypeRezka     library.FeedType = "rezka"
	feedTypeShikimori library.FeedType = "shiki"
)

var errInvalidFeedURL = errors.New("invalid feed URL")

type feedSource struct {
	prefix       string
	feedType     library.FeedType
	titleFromURL func(string) string
	canonicalURL func(string) string
}

var feedSources = []feedSource{
	{"https://www.youtube.com/@", feedTypeYouTube, handleFromURL, nil},
	{"https://www.instagram.com/", feedTypeInstagram, nil, nil},
	{"https://tok.adminforge.de/@", feedTypeTikTok, handleFromURL, tiktokURL},
	{"https://open.spotify.com/artist/", feedTypeSpotify, nil, nil},
	{"https://hdrezka.me/series/", feedTypeRezka, rezkaTitle, nil},
	{"https://hdrezka.me/films/", feedTypeRezka, rezkaTitle, nil},
	{"https://shikimori.one/animes/", feedTypeShikimori, nil, nil},
}

type FeedResolver struct{}

func (FeedResolver) Resolve(_ context.Context, rawURL string) (library.CreateFeedInput, error) {
	return CreateFeedFromURL(rawURL)
}

func CreateFeedFromURL(rawURL string) (library.CreateFeedInput, error) {
	source, err := recogniseFeedSource(rawURL)
	if err != nil {
		return library.CreateFeedInput{}, err
	}

	title := lastURLSegment(rawURL)
	if source.titleFromURL != nil {
		title = source.titleFromURL(rawURL)
	}
	canonicalURL := rawURL
	if source.canonicalURL != nil {
		canonicalURL = source.canonicalURL(rawURL)
	}
	return library.CreateFeedInput{Title: title, Type: source.feedType, URL: canonicalURL}, nil
}

func lastURLSegment(rawURL string) string {
	trimmedURL := strings.TrimSuffix(rawURL, "/")
	return trimmedURL[strings.LastIndex(trimmedURL, "/")+1:]
}

func handleFromURL(rawURL string) string {
	_, handle, _ := strings.Cut(rawURL, "@")
	return strings.SplitN(handle, "/", 2)[0]
}

func rezkaTitle(rawURL string) string {
	return strings.TrimSuffix(lastURLSegment(rawURL), ".html")
}

func tiktokURL(rawURL string) string {
	_, handle, _ := strings.Cut(rawURL, "@")
	return "https://www.tiktok.com/@" + handle
}

func recogniseFeedSource(rawURL string) (feedSource, error) {
	for _, candidate := range feedSources {
		if strings.HasPrefix(rawURL, candidate.prefix) && len(rawURL) > len(candidate.prefix) {
			return candidate, nil
		}
	}

	return feedSource{}, errInvalidFeedURL
}
