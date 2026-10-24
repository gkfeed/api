package services

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"gkfeed/api/internal/library"
)

type detectorFunc func(context.Context, string, string) (FeedTypeSuggestion, error)

func (f detectorFunc) Detect(ctx context.Context, u, title string) (FeedTypeSuggestion, error) {
	return f(ctx, u, title)
}

type metadataStub struct {
	title, id  string
	err        error
	channelURL string
	titleCalls int
}

func (m *metadataStub) Title(context.Context, string) (string, error) {
	m.titleCalls++
	return m.title, m.err
}
func (m *metadataStub) YoutubeChannel(_ context.Context, u string) (string, string, error) {
	m.channelURL = u
	return m.id, m.title, m.err
}

func TestPrepareFeedSelection(t *testing.T) {
	for _, test := range []struct {
		name               string
		input              library.CreateFeedsInput
		guess              library.FeedType
		wantTypes          []library.FeedType
		wantURL, wantTitle string
	}{
		{"automatic Instagram", library.CreateFeedsInput{URL: "https://instagram.com/Example/?si=share#top"}, "inst", []library.FeedType{"inst", "stories"}, "https://www.instagram.com/example", "Example"},
		{"automatic stories also selects both", library.CreateFeedsInput{URL: "https://www.instagram.com/example"}, "stories", []library.FeedType{"inst", "stories"}, "https://www.instagram.com/example", "example"},
		{"explicit posts", library.CreateFeedsInput{URL: "https://instagram.com/example", Type: "inst"}, "", []library.FeedType{"inst"}, "https://www.instagram.com/example", "example"},
		{"explicit stories", library.CreateFeedsInput{URL: "https://instagram.com/example", Type: "stories"}, "", []library.FeedType{"stories"}, "https://www.instagram.com/example", "example"},
		{"explicit ordered selection", library.CreateFeedsInput{URL: "https://instagram.com/example", Types: []library.FeedType{"stories", "inst"}, Title: " Mine "}, "", []library.FeedType{"stories", "inst"}, "https://www.instagram.com/example", "Mine"},
		{"manual type overrides source", library.CreateFeedsInput{URL: "https://example.com/feed.xml", Type: "yt", Title: " My feed "}, "", []library.FeedType{"yt"}, "https://example.com/feed.xml", "My feed"},
		{"existing TikTok mirror conversion", library.CreateFeedsInput{URL: "https://tok.adminforge.de/@example", Type: "tiktok", Title: "My feed"}, "", []library.FeedType{"tiktok"}, "https://www.tiktok.com/@example", "My feed"},
		{"metadata title", library.CreateFeedsInput{URL: "https://EXAMPLE.com:443/feed.xml", Type: "web"}, "", []library.FeedType{"web"}, "https://example.com/feed.xml", "Page title"},
		{"title passed to detector", library.CreateFeedsInput{URL: "https://example.com/feed", Title: "My title"}, "web", []library.FeedType{"web"}, "https://example.com/feed", "My title"},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			metadata := &metadataStub{title: " Page title "}
			creator := FeedCreator{Metadata: metadata, Detector: detectorFunc(func(_ context.Context, _, title string) (FeedTypeSuggestion, error) {
				called = true
				if title != "" && title != "My title" {
					t.Fatalf("unexpected title %q", title)
				}
				return FeedTypeSuggestion{Type: test.guess, Confidence: .35}, nil
			})}
			got, err := creator.Prepare(t.Context(), test.input)
			if err != nil || len(got) != len(test.wantTypes) {
				t.Fatalf("Prepare = %#v, %v", got, err)
			}
			for i, result := range got {
				if result.Type != test.wantTypes[i] || result.URL != test.wantURL || result.Title != test.wantTitle {
					t.Fatalf("result = %#v", result)
				}
			}
			if called != (test.guess != "") {
				t.Fatalf("detector called = %v", called)
			}
			if test.input.Title != "" && metadata.titleCalls != 0 {
				t.Fatal("looked up a manually supplied title")
			}
		})
	}
}

func TestPrepareRejectsBeforeMetadata(t *testing.T) {
	for _, input := range []library.CreateFeedsInput{
		{URL: ""}, {URL: "ftp://example.com", Type: "web"}, {URL: "https://user:password@example.com", Type: "web"},
		{URL: "https://example.com", Type: "unknown"}, {URL: "https://example.com", Types: []library.FeedType{}},
		{URL: "https://example.com", Type: "web", Types: []library.FeedType{"inst"}},
		{URL: "https://example.com", Types: []library.FeedType{"web", "web"}},
	} {
		creator := FeedCreator{Detector: detectorFunc(func(context.Context, string, string) (FeedTypeSuggestion, error) {
			t.Fatal("unexpected detection")
			return FeedTypeSuggestion{}, nil
		})}
		if _, err := creator.Prepare(t.Context(), input); !errors.Is(err, ErrInvalidCreation) {
			t.Fatalf("input %#v: %v", input, err)
		}
	}
	for _, confidence := range []float64{.3499, -1, 1.1, math.NaN()} {
		creator := FeedCreator{Detector: detectorFunc(func(context.Context, string, string) (FeedTypeSuggestion, error) {
			return FeedTypeSuggestion{Type: "web", Confidence: confidence}, nil
		})}
		if _, err := creator.Prepare(t.Context(), library.CreateFeedsInput{URL: "https://example.com"}); err == nil {
			t.Fatalf("accepted confidence %v", confidence)
		}
	}
}

func TestPrepareTitleFallbackAndCancellation(t *testing.T) {
	creator := FeedCreator{Metadata: &metadataStub{err: errors.New("offline")}}
	got, err := creator.Prepare(t.Context(), library.CreateFeedsInput{URL: "https://example.com/@example", Type: "web"})
	if err != nil || got[0].Title != "example" {
		t.Fatalf("fallback = %#v, %v", got, err)
	}
	got, err = creator.Prepare(t.Context(), library.CreateFeedsInput{URL: "https://example.com/%00", Type: "web"})
	if err != nil || got[0].Title != "example.com" {
		t.Fatalf("empty fallback = %#v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := creator.Prepare(ctx, library.CreateFeedsInput{URL: "https://example.com", Type: "web"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled = %v", err)
	}
}

func TestPrepareYoutubeCanonicalIdentity(t *testing.T) {
	const id = "UC5TRrMsWLy7flttFTyS-bOA"
	for _, tab := range []string{"", "/videos", "/shorts", "/streams"} {
		for _, locator := range []string{"@example", "c/example", "user/example", "channel/" + id} {
			metadata := &metadataStub{id: id, title: "Channel title"}
			creator := FeedCreator{Metadata: metadata}
			input := library.CreateFeedsInput{URL: "https://m.youtube.com/" + locator + tab + "/?si=share#top", Type: "yt", Title: "Manual"}
			got, err := creator.Prepare(t.Context(), input)
			want := []library.CreateFeedInput{{Type: "yt", Title: "Manual", URL: "https://www.youtube.com/channel/" + id + tab}}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("%s%s: %#v, %v", locator, tab, got, err)
			}
			if locator == "channel/"+id && metadata.channelURL != "" {
				t.Fatal("permanent URL required resolution")
			}
			if locator != "channel/"+id && metadata.channelURL != "https://www.youtube.com/"+locator {
				t.Fatalf("resolved tab instead of root: %q", metadata.channelURL)
			}
			again, err := creator.Prepare(t.Context(), library.CreateFeedsInput{URL: got[0].URL, Title: "Manual", Type: "yt"})
			if err != nil || !reflect.DeepEqual(got, again) {
				t.Fatalf("normalization is not idempotent: %#v, %v", again, err)
			}
		}
	}
	creator := FeedCreator{Metadata: &metadataStub{err: ErrChannelUnresolved}}
	if _, err := creator.Prepare(t.Context(), library.CreateFeedsInput{URL: "https://youtube.com/@example/streams", Title: "Manual", Type: "yt"}); !errors.Is(err, ErrChannelUnresolved) {
		t.Fatalf("unresolved = %v", err)
	}
	for _, raw := range []string{"https://www.youtube.com/watch?v=123", "https://www.youtube.com/playlist?list=123"} {
		got, err := creator.Prepare(t.Context(), library.CreateFeedsInput{URL: raw, Type: "yt", Title: "Manual"})
		if err != nil || got[0].URL != raw {
			t.Fatalf("video/playlist changed: %#v, %v", got, err)
		}
	}
}
