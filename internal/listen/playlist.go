package listen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PlaylistEntry is one video pulled out of a YouTube / YouTube Music
// playlist or album.
type PlaylistEntry struct {
	ID         string
	Title      string
	Author     string
	Thumbnail  string
	DurationMS int64
}

const (
	playlistMaxItems = 500
	playlistMaxPages = 10
)

var ytListIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{10,80}$`)

// ParseYouTubePlaylistID recognises playlist and album links: anything with
// a list= parameter and no specific video (v=). A watch link that merely
// carries a list= alongside its video is treated as that one video, so
// pasting a song from a playlist view does not drag the whole list in.
// Mixes/radios (RD…) are generated per viewer and cannot be fetched.
func ParseYouTubePlaylistID(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	if host != "youtube.com" && host != "music.youtube.com" {
		return "", false
	}
	q := u.Query()
	list := q.Get("list")
	if list == "" || q.Get("v") != "" || !ytListIDRe.MatchString(list) {
		return "", false
	}
	if strings.HasPrefix(list, "RD") {
		return "", false
	}
	return list, true
}

// FetchYouTubePlaylist returns the playlist's title and its playable videos
// in order. With a Data API key it uses the official API; otherwise it reads
// the public playlist page (and its continuation pages) like a browser.
func FetchYouTubePlaylist(ctx context.Context, listID, apiKey string) (string, []PlaylistEntry, error) {
	if apiKey != "" {
		return playlistViaAPI(ctx, listID, apiKey)
	}
	return playlistViaPage(ctx, listID)
}

// --- keyless: the playlist page + youtubei continuations ---

var (
	innertubeKeyRe     = regexp.MustCompile(`"INNERTUBE_API_KEY":"([^"]+)"`)
	innertubeVersionRe = regexp.MustCompile(`"INNERTUBE_CLIENT_VERSION":"([^"]+)"`)
)

func playlistViaPage(ctx context.Context, listID string) (string, []PlaylistEntry, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://www.youtube.com/playlist?list="+url.QueryEscape(listID)+"&hl=en", nil)
	if err != nil {
		return "", nil, err
	}
	setBrowserHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("youtube returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", nil, err
	}
	m := ytInitialDataRe.FindSubmatch(body)
	if m == nil {
		return "", nil, errors.New("could not read that playlist (private, removed, or YouTube changed its page)")
	}
	var data any
	if err := json.Unmarshal(m[1], &data); err != nil {
		return "", nil, err
	}
	title := playlistTitle(data)
	entries, continuation := collectPlaylistVideos(data, nil, map[string]bool{})

	// Follow "load more" tokens the way the page does, up to a sane cap.
	key := string(firstGroup(innertubeKeyRe, body))
	version := string(firstGroup(innertubeVersionRe, body))
	for page := 0; continuation != "" && key != "" && len(entries) < playlistMaxItems && page < playlistMaxPages; page++ {
		next, err := browseContinuation(reqCtx, key, version, continuation)
		if err != nil {
			break // keep what we have
		}
		entries, continuation = collectPlaylistVideos(next, entries, nil)
	}
	if len(entries) == 0 {
		return title, nil, errors.New("that playlist has no playable videos (or it is private)")
	}
	if len(entries) > playlistMaxItems {
		entries = entries[:playlistMaxItems]
	}
	return title, entries, nil
}

func setBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cookie", "CONSENT=YES+cb.20210328-17-p0.en+FX+667; SOCS=CAI")
}

func firstGroup(re *regexp.Regexp, b []byte) []byte {
	m := re.FindSubmatch(b)
	if m == nil {
		return nil
	}
	return m[1]
}

// seen is shared across pages; pass nil to reuse the one threaded through
// the entries already collected (ids are unique per playlist anyway).
func collectPlaylistVideos(data any, entries []PlaylistEntry, seen map[string]bool) ([]PlaylistEntry, string) {
	if seen == nil {
		seen = map[string]bool{}
		for _, e := range entries {
			seen[e.ID] = true
		}
	}
	continuation := findListContinuation(data)
	walk(data, func(node map[string]any) {
		// Newer pages list videos as lockupViewModel entries.
		if lv, ok := node["lockupViewModel"].(map[string]any); ok {
			if e, ok := lockupEntry(lv); ok && !seen[e.ID] {
				seen[e.ID] = true
				entries = append(entries, e)
			}
			return
		}
		vr, ok := node["playlistVideoRenderer"].(map[string]any)
		if !ok {
			return
		}
		id, _ := vr["videoId"].(string)
		if id == "" || seen[id] {
			return
		}
		if playable, ok := vr["isPlayable"].(bool); ok && !playable {
			return
		}
		seen[id] = true
		var dur int64
		if s, ok := vr["lengthSeconds"].(string); ok {
			n, _ := strconv.Atoi(s)
			dur = int64(n) * 1000
		}
		entries = append(entries, PlaylistEntry{
			ID:         id,
			Title:      runsText(vr["title"]),
			Author:     firstNonEmpty(runsText(vr["shortBylineText"]), runsText(vr["longBylineText"])),
			Thumbnail:  "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg",
			DurationMS: dur,
		})
	})
	return entries, continuation
}

// lockupEntry reads a video out of the lockupViewModel shape YouTube's
// playlist pages switched to: id in contentId, title and channel under
// metadata, and the length as a thumbnail badge like "5:21".
func lockupEntry(lv map[string]any) (PlaylistEntry, bool) {
	id, _ := lv["contentId"].(string)
	ct, _ := lv["contentType"].(string)
	if id == "" || !ytIDRe.MatchString(id) || (ct != "" && ct != "LOCKUP_CONTENT_TYPE_VIDEO") {
		return PlaylistEntry{}, false
	}
	e := PlaylistEntry{ID: id, Thumbnail: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"}
	md, _ := lv["metadata"].(map[string]any)
	lm, _ := md["lockupMetadataViewModel"].(map[string]any)
	if t, ok := lm["title"].(map[string]any); ok {
		e.Title, _ = t["content"].(string)
	}
	// First metadata part is the channel / artist.
	walk(lm["metadata"], func(n map[string]any) {
		if e.Author != "" {
			return
		}
		if parts, ok := n["metadataParts"].([]any); ok && len(parts) > 0 {
			if p, ok := parts[0].(map[string]any); ok {
				if t, ok := p["text"].(map[string]any); ok {
					e.Author, _ = t["content"].(string)
				}
			}
		}
	})
	walk(lv["contentImage"], func(n map[string]any) {
		if e.DurationMS != 0 {
			return
		}
		if b, ok := n["thumbnailBadgeViewModel"].(map[string]any); ok {
			if t, ok := b["text"].(string); ok {
				e.DurationMS = parseClock(t)
			}
		}
	})
	if e.Title == "" {
		e.Title = "YouTube video"
	}
	return e, true
}

// findListContinuation returns the "load more" token that belongs to the
// video list itself: the continuation item sitting in the same array as the
// video entries (pages also carry unrelated continuations for other
// sections). Go map iteration is unordered, so this looks at arrays only.
func findListContinuation(v any) string {
	var found string
	var visit func(v any)
	visit = func(v any) {
		if found != "" {
			return
		}
		switch t := v.(type) {
		case []any:
			hasVideos, token := false, ""
			for _, c := range t {
				m, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if _, ok := m["lockupViewModel"]; ok {
					hasVideos = true
				} else if _, ok := m["playlistVideoRenderer"]; ok {
					hasVideos = true
				} else if cr, ok := m["continuationItemRenderer"]; ok {
					token = continuationToken(cr)
				} else if cr, ok := m["continuationItemViewModel"]; ok {
					token = continuationToken(cr)
				}
			}
			if hasVideos && token != "" {
				found = token
				return
			}
			for _, c := range t {
				visit(c)
			}
		case map[string]any:
			for _, c := range t {
				visit(c)
			}
		}
	}
	visit(v)
	return found
}

// continuationToken digs the token out of either continuation item shape.
func continuationToken(v any) string {
	var tok string
	walk(v, func(n map[string]any) {
		if tok != "" {
			return
		}
		if cc, ok := n["continuationCommand"].(map[string]any); ok {
			if t, ok := cc["token"].(string); ok && t != "" {
				tok = t
			}
		}
	})
	return tok
}

func playlistTitle(data any) string {
	var title string
	walk(data, func(node map[string]any) {
		if title != "" {
			return
		}
		for _, k := range []string{"playlistMetadataRenderer", "playlistHeaderRenderer"} {
			if r, ok := node[k].(map[string]any); ok {
				if t, ok := r["title"].(string); ok && t != "" {
					title = t
				} else if t := runsText(r["title"]); t != "" {
					title = t
				}
			}
		}
	})
	return title
}

func browseContinuation(ctx context.Context, key, version, token string) (any, error) {
	if version == "" {
		version = "2.20240101.00.00"
	}
	payload, _ := json.Marshal(map[string]any{
		"context":      map[string]any{"client": map[string]any{"clientName": "WEB", "clientVersion": version, "hl": "en"}},
		"continuation": token,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.youtube.com/youtubei/v1/browse?key="+url.QueryEscape(key)+"&prettyPrint=false", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	setBrowserHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube continuation returned %d", resp.StatusCode)
	}
	var data any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

// --- official: YouTube Data API v3 ---

func playlistViaAPI(ctx context.Context, listID, apiKey string) (string, []PlaylistEntry, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	var title string
	var meta struct {
		Items []struct {
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	mq := url.Values{"part": {"snippet"}, "id": {listID}, "key": {apiKey}}
	if getJSON(reqCtx, "https://www.googleapis.com/youtube/v3/playlists?"+mq.Encode(), &meta) == nil && len(meta.Items) > 0 {
		title = meta.Items[0].Snippet.Title
	}

	var entries []PlaylistEntry
	pageToken := ""
	for page := 0; page < playlistMaxPages && len(entries) < playlistMaxItems; page++ {
		q := url.Values{
			"part": {"snippet,status"}, "playlistId": {listID}, "maxResults": {"50"}, "key": {apiKey},
		}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		var res struct {
			NextPageToken string `json:"nextPageToken"`
			Items         []struct {
				Snippet struct {
					Title                  string `json:"title"`
					VideoOwnerChannelTitle string `json:"videoOwnerChannelTitle"`
					ResourceID             struct {
						VideoID string `json:"videoId"`
					} `json:"resourceId"`
				} `json:"snippet"`
				Status struct {
					PrivacyStatus string `json:"privacyStatus"`
				} `json:"status"`
			} `json:"items"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := getJSON(reqCtx, "https://www.googleapis.com/youtube/v3/playlistItems?"+q.Encode(), &res); err != nil {
			return title, nil, err
		}
		if res.Error != nil {
			return title, nil, errors.New("YouTube API: " + res.Error.Message)
		}
		for _, it := range res.Items {
			id := it.Snippet.ResourceID.VideoID
			if id == "" || it.Status.PrivacyStatus == "private" || it.Snippet.Title == "Deleted video" {
				continue
			}
			entries = append(entries, PlaylistEntry{
				ID: id, Title: it.Snippet.Title, Author: it.Snippet.VideoOwnerChannelTitle,
				Thumbnail: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg",
			})
		}
		pageToken = res.NextPageToken
		if pageToken == "" {
			break
		}
	}
	if len(entries) == 0 {
		return title, nil, errors.New("that playlist has no playable videos (or it is private)")
	}
	// Durations come from the videos endpoint, 50 ids at a time.
	for i := 0; i < len(entries); i += 50 {
		end := min(i+50, len(entries))
		ids := make([]string, 0, end-i)
		for _, e := range entries[i:end] {
			ids = append(ids, e.ID)
		}
		vq := url.Values{"part": {"contentDetails"}, "id": {strings.Join(ids, ",")}, "key": {apiKey}}
		var videos struct {
			Items []struct {
				ID             string `json:"id"`
				ContentDetails struct {
					Duration string `json:"duration"`
				} `json:"contentDetails"`
			} `json:"items"`
		}
		if getJSON(reqCtx, "https://www.googleapis.com/youtube/v3/videos?"+vq.Encode(), &videos) != nil {
			continue
		}
		dur := map[string]int64{}
		for _, v := range videos.Items {
			dur[v.ID] = parseISODuration(v.ContentDetails.Duration)
		}
		for j := i; j < end; j++ {
			entries[j].DurationMS = dur[entries[j].ID]
		}
	}
	return title, entries, nil
}
