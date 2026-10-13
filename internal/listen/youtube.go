package listen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ytIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// ParseYouTubeID extracts the 11-character video id from the link shapes
// people actually paste: watch?v=, youtu.be/, shorts/, live/, embed/, and
// music.youtube.com.
func ParseYouTubeID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if ytIDRe.MatchString(raw) {
		return raw, true
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	switch host {
	case "youtu.be":
		id := strings.Trim(u.Path, "/")
		return id, ytIDRe.MatchString(id)
	case "youtube.com", "music.youtube.com", "youtube-nocookie.com":
		if v := u.Query().Get("v"); v != "" {
			return v, ytIDRe.MatchString(v)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "live" || parts[0] == "embed" || parts[0] == "v") {
			return parts[1], ytIDRe.MatchString(parts[1])
		}
	}
	return "", false
}

type YouTubeMeta struct {
	Title     string
	Author    string
	Thumbnail string
}

// FetchYouTubeMeta asks YouTube's public oEmbed endpoint for title/author/
// thumbnail. No API key is needed. Falls back to a generic title on failure so
// a hiccup never blocks adding to the queue.
func FetchYouTubeMeta(ctx context.Context, id string) YouTubeMeta {
	meta := YouTubeMeta{Title: "YouTube video", Thumbnail: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"}
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	endpoint := "https://www.youtube.com/oembed?format=json&url=" +
		url.QueryEscape("https://www.youtube.com/watch?v="+id)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return meta
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return meta
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return meta
	}
	var body struct {
		Title        string `json:"title"`
		AuthorName   string `json:"author_name"`
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return meta
	}
	if body.Title != "" {
		meta.Title = body.Title
	}
	meta.Author = body.AuthorName
	if body.ThumbnailURL != "" {
		meta.Thumbnail = body.ThumbnailURL
	}
	return meta
}
