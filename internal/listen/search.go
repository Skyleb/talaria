package listen

import (
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
	"sync"
	"time"
)

// SearchResult is one YouTube video hit.
type SearchResult struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Channel    string `json:"channel"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Thumbnail  string `json:"thumbnail"`
}

// Search results are cached briefly: people retype the same query while
// browsing, and the keyless path is a full results-page fetch each time.
type searchCache struct {
	mu      sync.Mutex
	entries map[string]searchEntry
}

type searchEntry struct {
	at      time.Time
	results []SearchResult
}

const (
	searchCacheTTL = 10 * time.Minute
	searchCacheMax = 300
	searchLimit    = 15
)

var cache = &searchCache{entries: map[string]searchEntry{}}

func (c *searchCache) get(key string) ([]SearchResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Since(e.at) > searchCacheTTL {
		return nil, false
	}
	return e.results, true
}

func (c *searchCache) put(key string, r []SearchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= searchCacheMax {
		// Drop the oldest few; exactness doesn't matter here.
		var oldest []string
		for k, e := range c.entries {
			if time.Since(e.at) > searchCacheTTL/2 {
				oldest = append(oldest, k)
			}
		}
		if len(oldest) == 0 {
			c.entries = map[string]searchEntry{}
		}
		for _, k := range oldest {
			delete(c.entries, k)
		}
	}
	c.entries[key] = searchEntry{at: time.Now(), results: r}
}

// SearchYouTube finds videos for a free-text query. With an API key it uses
// the YouTube Data API; otherwise it reads YouTube's own results page, which
// needs no credentials.
func SearchYouTube(ctx context.Context, query, apiKey string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, nil
	}
	key := strings.ToLower(query)
	if apiKey != "" {
		key = "api:" + key
	}
	if r, ok := cache.get(key); ok {
		return r, nil
	}
	var (
		results []SearchResult
		err     error
	)
	if apiKey != "" {
		results, err = searchViaAPI(ctx, query, apiKey)
	} else {
		results, err = searchViaPage(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	if results == nil {
		results = []SearchResult{}
	}
	cache.put(key, results)
	return results, nil
}

// --- keyless: parse the results page ---

var ytInitialDataRe = regexp.MustCompile(`(?s)var ytInitialData\s*=\s*(\{.*?\});\s*</script>`)

func searchViaPage(ctx context.Context, query string) ([]SearchResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// sp=EgIQAQ%3D%3D filters to videos only (no channels/playlists).
	endpoint := "https://www.youtube.com/results?search_query=" + url.QueryEscape(query) + "&sp=EgIQAQ%253D%253D&hl=en"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// Pre-accepted consent cookies so EU/UK users don't get the consent page.
	req.Header.Set("Cookie", "CONSENT=YES+cb.20210328-17-p0.en+FX+667; SOCS=CAI")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return nil, err
	}
	m := ytInitialDataRe.FindSubmatch(body)
	if m == nil {
		return nil, errors.New("could not read YouTube results (page layout changed?)")
	}
	var data any
	if err := json.Unmarshal(m[1], &data); err != nil {
		return nil, err
	}
	var out []SearchResult
	seen := map[string]bool{}
	walk(data, func(node map[string]any) {
		if len(out) >= searchLimit {
			return
		}
		vr, ok := node["videoRenderer"].(map[string]any)
		if !ok {
			return
		}
		id, _ := vr["videoId"].(string)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, SearchResult{
			ID:         id,
			Title:      runsText(vr["title"]),
			Channel:    firstNonEmpty(runsText(vr["ownerText"]), runsText(vr["longBylineText"]), runsText(vr["shortBylineText"])),
			DurationMS: parseClock(simpleText(vr["lengthText"])),
			Thumbnail:  "https://i.ytimg.com/vi/" + id + "/mqdefault.jpg",
		})
	})
	return out, nil
}

func walk(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, c := range t {
			walk(c, fn)
		}
	case []any:
		for _, c := range t {
			walk(c, fn)
		}
	}
}

// runsText joins {"runs":[{"text":...}]} into a string.
func runsText(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	if s, ok := m["simpleText"].(string); ok {
		return s
	}
	runs, _ := m["runs"].([]any)
	var b strings.Builder
	for _, r := range runs {
		if rm, ok := r.(map[string]any); ok {
			if s, ok := rm["text"].(string); ok {
				b.WriteString(s)
			}
		}
	}
	return b.String()
}

func simpleText(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m["simpleText"].(string)
	return s
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// parseClock turns "3:33" / "1:02:03" into milliseconds (0 when unknown).
func parseClock(s string) int64 {
	if s == "" {
		return 0
	}
	parts := strings.Split(strings.TrimSpace(s), ":")
	var total int64
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0
		}
		total = total*60 + int64(n)
	}
	return total * 1000
}

// --- official: YouTube Data API v3 ---

func searchViaAPI(ctx context.Context, query, apiKey string) ([]SearchResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	q := url.Values{
		"part": {"snippet"}, "type": {"video"}, "maxResults": {strconv.Itoa(searchLimit)},
		"q": {query}, "key": {apiKey}, "videoEmbeddable": {"true"},
	}
	var search struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title        string `json:"title"`
				ChannelTitle string `json:"channelTitle"`
			} `json:"snippet"`
		} `json:"items"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := getJSON(reqCtx, "https://www.googleapis.com/youtube/v3/search?"+q.Encode(), &search); err != nil {
		return nil, err
	}
	if search.Error != nil {
		return nil, errors.New("YouTube API: " + search.Error.Message)
	}
	out := make([]SearchResult, 0, len(search.Items))
	ids := make([]string, 0, len(search.Items))
	for _, it := range search.Items {
		if it.ID.VideoID == "" {
			continue
		}
		ids = append(ids, it.ID.VideoID)
		out = append(out, SearchResult{
			ID: it.ID.VideoID, Title: it.Snippet.Title, Channel: it.Snippet.ChannelTitle,
			Thumbnail: "https://i.ytimg.com/vi/" + it.ID.VideoID + "/mqdefault.jpg",
		})
	}
	if len(ids) == 0 {
		return out, nil
	}
	// Durations live on a second endpoint (1 quota unit).
	vq := url.Values{"part": {"contentDetails"}, "id": {strings.Join(ids, ",")}, "key": {apiKey}}
	var videos struct {
		Items []struct {
			ID             string `json:"id"`
			ContentDetails struct {
				Duration string `json:"duration"`
			} `json:"contentDetails"`
		} `json:"items"`
	}
	if err := getJSON(reqCtx, "https://www.googleapis.com/youtube/v3/videos?"+vq.Encode(), &videos); err == nil {
		dur := map[string]int64{}
		for _, v := range videos.Items {
			dur[v.ID] = parseISODuration(v.ContentDetails.Duration)
		}
		for i := range out {
			out[i].DurationMS = dur[out[i].ID]
		}
	}
	return out, nil
}

func getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

var isoDurRe = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

// parseISODuration handles the PT#H#M#S form the Data API returns.
func parseISODuration(s string) int64 {
	m := isoDurRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	var total int64
	for i, mult := range []int64{3600, 60, 1} {
		if m[i+1] != "" {
			n, _ := strconv.Atoi(m[i+1])
			total += int64(n) * mult
		}
	}
	return total * 1000
}
