package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

type PageMetadata struct{ client *http.Client }

func (p *PageMetadata) Title(ctx context.Context, rawURL string) (string, error) {
	page, _, err := p.fetch(ctx, rawURL)
	if err != nil {
		return "", err
	}
	return pageTitle(page), nil
}

func (p *PageMetadata) YoutubeChannel(ctx context.Context, rawURL string) (string, string, error) {
	page, finalURL, err := p.fetch(ctx, rawURL)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrChannelUnresolved, err)
	}
	channel, ok := parseYoutubeChannel(finalURL)
	if !ok {
		return "", "", ErrChannelUnresolved
	}
	ids := map[string]bool{}
	if channel.id != "" {
		ids[channel.id] = true
	}
	walkHTML(page, false, func(node *html.Node, inHead bool) {
		if node.Type != html.ElementNode {
			return
		}
		attrs := htmlAttributes(node)
		if node.Data == "meta" && inHead && strings.EqualFold(attrs["itemprop"], "channelId") && channelIDPattern.MatchString(attrs["content"]) {
			ids[attrs["content"]] = true
		}
		value := ""
		rss := false
		if node.Data == "link" {
			rel := strings.ToLower(attrs["rel"])
			rss = rel == "alternate" && attrs["type"] == "application/rss+xml"
			if rel == "canonical" || rss {
				value = attrs["href"]
			}
		} else if node.Data == "meta" && strings.EqualFold(attrs["property"], "og:url") {
			value = attrs["content"]
		}
		if value == "" {
			return
		}
		u, err := url.Parse(value)
		if err != nil {
			return
		}
		u = finalURL.ResolveReference(u)
		if u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return
		}
		if target, ok := parseYoutubeChannel(u); ok && target.id != "" {
			ids[target.id] = true
		}
		if rss && u.Scheme == "https" && u.Host == "www.youtube.com" && u.Path == "/feeds/videos.xml" {
			if id := u.Query().Get("channel_id"); channelIDPattern.MatchString(id) {
				ids[id] = true
			}
		}
	})
	if len(ids) != 1 {
		return "", "", ErrChannelUnresolved
	}
	for id := range ids {
		return id, pageTitle(page), nil
	}
	return "", "", ErrChannelUnresolved
}

func (p *PageMetadata) fetch(ctx context.Context, rawURL string) (*html.Node, *url.URL, error) {
	if _, err := parseFeedURL(rawURL); err != nil {
		return nil, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("User-Agent", "GKFeed/2.0 (feed metadata)")
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/rss+xml,application/atom+xml")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, errors.New("metadata request failed")
	}
	const maxBytes = 3_000_000
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > maxBytes {
		return nil, nil, errors.New("metadata page is too large")
	}
	page, err := html.Parse(strings.NewReader(string(data)))
	return page, response.Request.URL, err
}

func htmlAttributes(node *html.Node) map[string]string {
	attrs := map[string]string{}
	for _, attr := range node.Attr {
		attrs[attr.Key] = attr.Val
	}
	return attrs
}

func walkHTML(node *html.Node, inHead bool, visit func(*html.Node, bool)) {
	inHead = inHead || (node.Type == html.ElementNode && node.Data == "head")
	visit(node, inHead)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, inHead, visit)
	}
}

func pageTitle(page *html.Node) string {
	ogTitle, title := "", ""
	walkHTML(page, false, func(node *html.Node, inHead bool) {
		if node.Type != html.ElementNode {
			return
		}
		if node.Data == "meta" {
			attrs := htmlAttributes(node)
			if strings.EqualFold(attrs["property"], "og:title") && ogTitle == "" {
				ogTitle = strings.TrimSpace(attrs["content"])
			}
		}
		if node.Data == "title" && title == "" {
			var content strings.Builder
			walkHTML(node, inHead, func(n *html.Node, _ bool) {
				if n.Type == html.TextNode {
					content.WriteString(n.Data)
				}
			})
			title = strings.TrimSpace(content.String())
		}
	})
	if ogTitle != "" {
		return ogTitle
	}
	return title
}
