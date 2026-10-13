package ddns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Talaria is the client half of hosted dynamic DNS: another Talaria server
// (the host, e.g. https://talaria.gg) issued us a name and a token, and we
// keep our A record and DNS-01 TXT record current by calling its API. The
// token covers the claimed name and every name beneath it, which is how the
// home-network-access domain (lan.<name>) shares the same credentials.
type Talaria struct {
	// URL is the host's base address, e.g. https://talaria.gg.
	URL   string
	Token string
	// Client overrides the HTTP client (tests).
	Client *http.Client
}

// DefaultHost is the public Talaria instance offering free names.
const DefaultHost = "https://talaria.gg"

// NormalizeHostURL turns user input ("talaria.gg", "https://talaria.gg/")
// into a base URL with scheme and no trailing slash.
func NormalizeHostURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultHost
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	return strings.TrimRight(s, "/")
}

// ClaimResult is what the host returns for a successful claim.
type ClaimResult struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Token  string `json:"token"`
}

// ZoneInfo describes the names a host offers.
type ZoneInfo struct {
	Zone   string `json:"zone"`
	Server string `json:"server"`
	Rules  string `json:"rules"`
}

// Zone asks the host which zone it issues names under.
func (t *Talaria) Zone(ctx context.Context) (*ZoneInfo, error) {
	var out ZoneInfo
	if err := t.call(ctx, http.MethodGet, "/api/v1/ddns/zone", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Claim asks the host for name; the result's Token is shown once.
func (t *Talaria) Claim(ctx context.Context, name string) (*ClaimResult, error) {
	var out ClaimResult
	if err := t.call(ctx, http.MethodPost, "/api/v1/ddns/claim", map[string]string{"name": name}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Update points host (empty = the claimed name) at ip, or at the address
// the host sees us coming from when ip is empty.
func (t *Talaria) Update(ctx context.Context, host, ip string) error {
	return t.call(ctx, http.MethodPost, "/api/v1/ddns/update", map[string]string{"host": host, "ip": ip}, nil)
}

// SetTXT implements TXTSetter through the host.
func (t *Talaria) SetTXT(ctx context.Context, domain, txt string) error {
	return t.call(ctx, http.MethodPost, "/api/v1/ddns/txt", map[string]string{"host": domain, "txt": txt}, nil)
}

func (t *Talaria) call(ctx context.Context, method, path string, body any, out any) error {
	base := NormalizeHostURL(t.URL)
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(buf)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t.Token != "" {
		req.Header.Set("Authorization", "Bearer "+t.Token)
	}
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reach %s: %w", base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			return errors.New(e.Error.Message)
		}
		return fmt.Errorf("%s answered %d: %s", base, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}
