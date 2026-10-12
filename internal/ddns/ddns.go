// Package ddns keeps a dynamic-DNS hostname pointed at this connection's
// public IP, so users don't need a separate updater client or router support.
package ddns

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	// Provider: "duckdns" is built in; "custom" calls URL verbatim
	// (templates: {DOMAIN}, {TOKEN}).
	Provider string
	Domain   string // full hostname, e.g. skyle.duckdns.org
	Token    string
	URL      string // custom provider only
	Interval time.Duration
}

// Run updates immediately, then on every interval tick until ctx ends.
func Run(ctx context.Context, cfg Config, log *slog.Logger) {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	update := func() {
		if err := updateOnce(ctx, cfg); err != nil {
			log.Error("ddns update failed", "provider", cfg.Provider, "domain", cfg.Domain, "err", err)
		} else {
			log.Debug("ddns updated", "domain", cfg.Domain)
		}
	}
	update()
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			update()
		}
	}
}

func updateOnce(ctx context.Context, cfg Config) error {
	var endpoint string
	switch cfg.Provider {
	case "duckdns":
		// DuckDNS wants the subdomain label only; strip the suffix if present.
		sub := strings.TrimSuffix(cfg.Domain, ".duckdns.org")
		endpoint = fmt.Sprintf("https://www.duckdns.org/update?domains=%s&token=%s&ip=",
			url.QueryEscape(sub), url.QueryEscape(cfg.Token))
	case "custom":
		endpoint = strings.NewReplacer(
			"{DOMAIN}", url.QueryEscape(cfg.Domain),
			"{TOKEN}", url.QueryEscape(cfg.Token),
		).Replace(cfg.URL)
	default:
		return fmt.Errorf("unknown ddns provider %q", cfg.Provider)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	trimmed := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, trimmed)
	}
	// DuckDNS answers the literal body "OK" or "KO".
	if cfg.Provider == "duckdns" && trimmed != "OK" {
		return fmt.Errorf("duckdns rejected the update (check token and domain): %q", trimmed)
	}
	return nil
}
