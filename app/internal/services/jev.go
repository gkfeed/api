package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gkfeed/api/internal/library"
)

const jevURL = "https://openrouter.ai/api/alpha/decisions"

type JevDetector struct {
	apiKey string
	client *http.Client
}

func NewJevDetector(apiKey string) *JevDetector {
	return &JevDetector{apiKey: strings.TrimSpace(apiKey), client: &http.Client{
		Timeout: 5 * time.Second,
		// Never forward the credential to a redirect destination.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (d *JevDetector) Detect(ctx context.Context, rawURL, title string) (FeedTypeSuggestion, error) {
	if d.apiKey == "" {
		return FeedTypeSuggestion{}, ErrDetectionUnavailable
	}
	criteria := map[string]string{}
	for _, option := range library.FeedTypeOptions() {
		criteria[string(option.Type)] = option.Description
	}
	state := map[string]string{"url": rawURL}
	if title != "" {
		state["user_supplied_title"] = title
	}
	body, err := json.Marshal(map[string]any{
		"model": "typesafe/jev-1.13", "state": state,
		"questions": map[string]any{"feed_type": map[string]any{
			"type":         "choice",
			"instructions": "Which GKFEED source type best matches this URL and optional title? Choose web when no specialized source clearly matches.",
			"criteria":     criteria,
		}},
	})
	if err != nil {
		return FeedTypeSuggestion{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, jevURL, bytes.NewReader(body))
	if err != nil {
		return FeedTypeSuggestion{}, err
	}
	request.Header.Set("Authorization", "Bearer "+d.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-OpenRouter-Title", "GKFEED")
	response, err := d.client.Do(request)
	if err != nil {
		return FeedTypeSuggestion{}, fmt.Errorf("%w: request failed: %w", ErrDetectionFailed, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return FeedTypeSuggestion{}, ErrDetectionFailed
	}
	var result struct {
		Answers struct {
			FeedType struct {
				Type       string           `json:"type"`
				Choice     library.FeedType `json:"choice"`
				Confidence *float64         `json:"confidence"`
			} `json:"feed_type"`
		} `json:"answers"`
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, &result) != nil {
		return FeedTypeSuggestion{}, ErrDetectionFailed
	}
	answer := result.Answers.FeedType
	if answer.Type != "choice" || !library.IsFeedType(answer.Choice) || answer.Confidence == nil || *answer.Confidence < 0 || *answer.Confidence > 1 {
		return FeedTypeSuggestion{}, ErrDetectionFailed
	}
	return FeedTypeSuggestion{Type: answer.Choice, Confidence: *answer.Confidence}, nil
}
