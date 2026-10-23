package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"gkfeed/api/internal/library"
)

var (
	ErrInvalidCreation      = errors.New("invalid feed creation request")
	ErrTypeRequired         = errors.New("feed type must be supplied explicitly")
	ErrDetectionUnavailable = errors.New("feed type detection is not configured")
	ErrDetectionFailed      = errors.New("feed type detection failed")
	ErrChannelUnresolved    = errors.New("YouTube channel could not be resolved")
)

type FeedTypeSuggestion struct {
	Type       library.FeedType
	Confidence float64
}

type FeedTypeDetector interface {
	Detect(context.Context, string, string) (FeedTypeSuggestion, error)
}

type FeedMetadata interface {
	Title(context.Context, string) (string, error)
	YoutubeChannel(context.Context, string) (string, string, error)
}

type FeedCreator struct {
	Detector FeedTypeDetector
	Metadata FeedMetadata
}

func NewFeedCreator(apiKey string) *FeedCreator {
	return &FeedCreator{Detector: NewJevDetector(apiKey), Metadata: &PageMetadata{client: newPublicHTTPClient()}}
}

// Prepare performs network work before storage starts its transaction.
func (c *FeedCreator) Prepare(ctx context.Context, input library.CreateFeedsInput) ([]library.CreateFeedInput, error) {
	u, err := parseFeedURL(input.URL)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(input.Title)
	kind := library.FeedType(strings.TrimSpace(string(input.Type)))
	if len(title) > 1000 {
		return nil, fmt.Errorf("%w: title is limited to 1000 bytes", ErrInvalidCreation)
	}
	if kind != "" && input.Types != nil {
		return nil, fmt.Errorf("%w: use type or types, not both", ErrInvalidCreation)
	}
	types := input.Types
	automatic := kind == "" && types == nil
	if kind != "" {
		types = []library.FeedType{kind}
	}
	if !automatic {
		if len(types) == 0 || len(types) > len(library.FeedTypeOptions()) {
			return nil, fmt.Errorf("%w: types must be a nonempty selection", ErrInvalidCreation)
		}
		seen := map[library.FeedType]bool{}
		for _, t := range types {
			if !library.IsFeedType(t) || seen[t] {
				return nil, fmt.Errorf("%w: unknown or repeated type", ErrInvalidCreation)
			}
			seen[t] = true
		}
	} else {
		if c.Detector == nil {
			return nil, ErrDetectionUnavailable
		}
		suggestion, err := c.Detector.Detect(ctx, u.String(), title)
		if err != nil {
			return nil, err
		}
		if !library.IsFeedType(suggestion.Type) || math.IsNaN(suggestion.Confidence) || suggestion.Confidence < 0 || suggestion.Confidence > 1 {
			return nil, ErrDetectionFailed
		}
		if suggestion.Confidence < 0.35 {
			return nil, ErrTypeRequired
		}
		types = []library.FeedType{suggestion.Type}
	}

	canonical := u.String()
	// Preserve the existing lazy resolver's supported mirror conversion.
	if source, err := recogniseFeedSource(canonical); err == nil && source.canonicalURL != nil {
		canonical = source.canonicalURL(canonical)
	}
	fallbackTitle := titleFromURL(u)
	if username := instagramUsername(u); username != "" {
		canonical = "https://www.instagram.com/" + strings.ToLower(username)
		fallbackTitle = username
		if automatic && (types[0] == "inst" || types[0] == "stories") {
			types = []library.FeedType{"inst", "stories"}
		}
		if title == "" {
			title = username
		}
	}
	if channel, ok := parseYoutubeChannel(u); ok {
		id := channel.id
		if id == "" {
			if c.Metadata == nil {
				return nil, ErrChannelUnresolved
			}
			var foundTitle string
			id, foundTitle, err = c.Metadata.YoutubeChannel(ctx, channel.pageURL)
			if err != nil {
				return nil, err
			}
			if !channelIDPattern.MatchString(id) {
				return nil, ErrChannelUnresolved
			}
			if title == "" {
				title = strings.TrimSpace(foundTitle)
			}
		}
		canonical = "https://www.youtube.com/channel/" + id + channel.tab
	}
	if title == "" && c.Metadata != nil {
		// A title is optional metadata. Request cancellation still prevents writes.
		foundTitle, _ := c.Metadata.Title(ctx, canonical)
		title = strings.TrimSpace(foundTitle)
	}
	if title == "" {
		title = fallbackTitle
	}
	if len(title) > 1000 {
		title = fallbackTitle
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]library.CreateFeedInput, len(types))
	for i, t := range types {
		results[i] = library.CreateFeedInput{Title: title, Type: t, URL: canonical}
	}
	return results, nil
}

func parseFeedURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || len(raw) > 8192 || u == nil || u.Opaque != "" || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%w: url must be an absolute HTTP(S) URL without credentials", ErrInvalidCreation)
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())
	}
	return u, nil
}

var instagramNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._]{1,30}$`)

func instagramUsername(u *url.URL) string {
	if u.Host != "instagram.com" && u.Host != "www.instagram.com" {
		return ""
	}
	name := strings.Trim(u.Path, "/")
	if !instagramNamePattern.MatchString(name) {
		return ""
	}
	switch strings.ToLower(name) {
	case "accounts", "direct", "explore", "p", "reel", "reels", "stories", "tv":
		return ""
	}
	return name
}

func titleFromURL(u *url.URL) string {
	var title string
	if channel, ok := parseYoutubeChannel(u); ok {
		title = strings.TrimPrefix(channel.locator, "@")
		if slash := strings.LastIndex(title, "/"); slash >= 0 {
			title = title[slash+1:]
		}
	} else {
		path := strings.TrimRight(u.Path, "/")
		title = path[strings.LastIndex(path, "/")+1:]
	}
	if title == "" {
		title = u.Hostname()
	}
	title = strings.TrimSpace(strings.TrimPrefix(title, "@"))
	title = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title))
	if title == "" || len(title) > 1000 {
		title = u.Hostname()
	}
	if len(title) > 1000 {
		title = "Feed"
	}
	return title
}
