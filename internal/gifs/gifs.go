// Package gifs searches GIFs through Tenor's v2 API (a free key from Google
// Cloud is configured by the admin). Results are cached briefly since people
// retype the same words while browsing.
package gifs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// GIF is one result: the full-size file to send and a small preview for the
// picker grid.
type GIF struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Preview string `json:"preview"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

const (
	cacheTTL = 10 * time.Minute
	cacheMax = 300
	limit    = 30
)

type entry struct {
	at      time.Time
	results []GIF
}

var (
	mu    sync.Mutex
	cache = map[string]entry{}
)

// Search returns GIFs for a query; an empty query returns what is trending.
func Search(ctx context.Context, query, apiKey string) ([]GIF, error) {
	if apiKey == "" {
		return nil, errors.New("GIF search is not set up on this server")
	}
	query = strings.TrimSpace(query)
	key := strings.ToLower(query)
	mu.Lock()
	if e, ok := cache[key]; ok && time.Since(e.at) < cacheTTL {
		mu.Unlock()
		return e.results, nil
	}
	mu.Unlock()

	q := url.Values{
		"key":           {apiKey},
		"client_key":    {"talaria"},
		"limit":         {fmt.Sprint(limit)},
		"media_filter":  {"gif,tinygif"},
		"contentfilter": {"medium"},
	}
	endpoint := "https://tenor.googleapis.com/v2/featured?"
	if query != "" {
		q.Set("q", query)
		endpoint = "https://tenor.googleapis.com/v2/search?"
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Results []struct {
			ID           string `json:"id"`
			Title        string `json:"title"`
			MediaFormats map[string]struct {
				URL  string `json:"url"`
				Dims []int  `json:"dims"`
			} `json:"media_formats"`
		} `json:"results"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("tenor: %w", err)
	}
	if body.Error != nil {
		return nil, errors.New("Tenor: " + body.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tenor returned %d", resp.StatusCode)
	}
	out := make([]GIF, 0, len(body.Results))
	for _, r := range body.Results {
		full, ok := r.MediaFormats["gif"]
		if !ok || full.URL == "" {
			continue
		}
		g := GIF{ID: r.ID, Title: r.Title, URL: full.URL, Preview: full.URL}
		if len(full.Dims) == 2 {
			g.Width, g.Height = full.Dims[0], full.Dims[1]
		}
		if tiny, ok := r.MediaFormats["tinygif"]; ok && tiny.URL != "" {
			g.Preview = tiny.URL
		}
		out = append(out, g)
	}
	mu.Lock()
	if len(cache) >= cacheMax {
		cache = map[string]entry{}
	}
	cache[key] = entry{at: time.Now(), results: out}
	mu.Unlock()
	return out, nil
}
