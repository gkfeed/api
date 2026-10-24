package services

import (
	"net/url"
	"regexp"
	"strings"
)

var channelIDPattern = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)

type youtubeChannel struct {
	id      string
	locator string
	pageURL string
	tab     string
}

func parseYoutubeChannel(u *url.URL) (youtubeChannel, bool) {
	if u.Host != "youtube.com" && u.Host != "www.youtube.com" && u.Host != "m.youtube.com" {
		return youtubeChannel{}, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/"), "/")
	count := 2
	if len(parts) > 0 && strings.HasPrefix(parts[0], "@") {
		count = 1
	}
	if len(parts) < count || len(parts) > count+1 {
		return youtubeChannel{}, false
	}
	name, err := url.PathUnescape(parts[count-1])
	if err != nil || name == "" || name == "@" || strings.ContainsAny(name, "/\\?# \t\r\n") {
		return youtubeChannel{}, false
	}
	channel := youtubeChannel{locator: strings.Join(parts[:count], "/")}
	switch {
	case count == 1:
	case parts[0] == "channel":
		if !channelIDPattern.MatchString(name) {
			return youtubeChannel{}, false
		}
		channel.id = name
	case parts[0] == "c" || parts[0] == "user":
	default:
		return youtubeChannel{}, false
	}
	if len(parts) == count+1 {
		switch parts[count] {
		case "videos", "shorts", "streams", "featured", "playlists", "community", "about", "releases", "podcasts", "courses", "posts":
			channel.tab = "/" + parts[count]
		default:
			return youtubeChannel{}, false
		}
	}
	channel.pageURL = "https://www.youtube.com/" + channel.locator
	return channel, true
}
