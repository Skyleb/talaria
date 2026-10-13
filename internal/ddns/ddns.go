// Package ddns keeps a dynamic-DNS hostname pointed at this connection's
// public IP, so users don't need a separate updater client or router support.
package ddns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	// Provider: "duckdns", "porkbun" and "talaria" (a name issued by another
	// Talaria server, see Talaria) are built in; "custom" calls URL verbatim
	// (templates: {DOMAIN}, {TOKEN}).
	Provider string
	Domain   string // full hostname, e.g. skyle.duckdns.org or talaria.gg
	Token    string // DuckDNS token / Porkbun API key / Talaria name token / custom {TOKEN}
	Secret   string // Porkbun secret API key
	URL      string // custom: update URL; talaria: the host server, e.g. https://talaria.gg
	// IP pins the record to an explicit address (e.g. a LAN IP for the
	// lan-access domain). Empty = the provider detects the caller's IP.
	IP       string
	Interval time.Duration
}

// TXTSetter publishes (or, with empty txt, clears) the TXT record Let's
// Encrypt's DNS-01 challenge reads for domain. Where the record lives is
// the provider's business: DuckDNS serves one TXT for the whole subdomain,
// Porkbun gets a real _acme-challenge.<domain> record.
type TXTSetter interface {
	SetTXT(ctx context.Context, domain, txt string) error
}

// SupportsDNS01 reports whether provider can solve DNS-01 challenges, i.e.
// whether NewTXTSetter will succeed for it.
func SupportsDNS01(provider string) bool {
	return provider == "duckdns" || provider == "porkbun" || provider == "talaria"
}

// Validate reports why cfg can't run: an unknown provider, or missing
// credentials / update URL. Messages are shown verbatim in the admin UI.
func Validate(cfg Config) error {
	switch cfg.Provider {
	case "duckdns":
		if cfg.Token == "" {
			return errors.New("DuckDNS needs the token shown at duckdns.org")
		}
	case "porkbun":
		if cfg.Token == "" || cfg.Secret == "" {
			return errors.New("Porkbun needs the API key and secret API key (porkbun.com → Account → API Access)")
		}
	case "talaria":
		if cfg.Token == "" {
			return errors.New("a Talaria name needs its token: claim one with the button, or paste the token you were given")
		}
	case "custom":
		if cfg.URL == "" {
			return errors.New("custom DDNS needs an update URL")
		}
	default:
		return errors.New("DDNS provider must be duckdns, porkbun, talaria or custom")
	}
	return nil
}

// NewTXTSetter returns the TXT publisher for a provider, or an error for
// providers that can only update an A record (custom update URLs).
func NewTXTSetter(provider, token, secret, url string) (TXTSetter, error) {
	switch provider {
	case "duckdns":
		if token == "" {
			return nil, errors.New("needs the DuckDNS token")
		}
		return duckDNSSetter{token: token}, nil
	case "porkbun":
		if token == "" || secret == "" {
			return nil, errors.New("needs the Porkbun API key and secret key")
		}
		return &Porkbun{APIKey: token, Secret: secret}, nil
	case "talaria":
		if token == "" {
			return nil, errors.New("needs the Talaria name token")
		}
		return &Talaria{URL: url, Token: token}, nil
	default:
		return nil, fmt.Errorf("provider %q cannot publish TXT records (needed for the certificate)", provider)
	}
}

type duckDNSSetter struct{ token string }

func (d duckDNSSetter) SetTXT(ctx context.Context, domain, txt string) error {
	return SetDuckDNSTXT(ctx, domain, d.token, txt)
}

// Runner owns a single background update loop whose config can be swapped
// at runtime (the admin settings UI reconfigures DDNS without a restart).
type Runner struct {
	log    *slog.Logger
	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewRunner(log *slog.Logger) *Runner { return &Runner{log: log} }

// Set stops any current loop and, when cfg.Domain is non-empty, starts a
// new one with cfg.
func (r *Runner) Set(cfg Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if cfg.Domain == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go Run(ctx, cfg, r.log)
	r.log.Info("ddns updater running", "provider", cfg.Provider, "domain", cfg.Domain)
}

// Stop halts the loop until the next Set.
func (r *Runner) Stop() { r.Set(Config{}) }

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

// SetDuckDNSTXT sets (or, with empty txt, clears) the TXT record DuckDNS
// serves for the domain — used for Let's Encrypt DNS-01 challenges.
func SetDuckDNSTXT(ctx context.Context, domain, token, txt string) error {
	sub := strings.TrimSuffix(domain, ".duckdns.org")
	endpoint := fmt.Sprintf("https://www.duckdns.org/update?domains=%s&token=%s&txt=%s",
		url.QueryEscape(sub), url.QueryEscape(token), url.QueryEscape(txt))
	if txt == "" {
		endpoint += "&clear=true"
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
	if trimmed := strings.TrimSpace(string(body)); trimmed != "OK" {
		return fmt.Errorf("duckdns rejected the TXT update: %q", trimmed)
	}
	return nil
}

// UpdateOnce performs a single update for cfg (exported for tests and tools).
func UpdateOnce(ctx context.Context, cfg Config) error { return updateOnce(ctx, cfg) }

func updateOnce(ctx context.Context, cfg Config) error {
	if cfg.Provider == "porkbun" {
		return updatePorkbun(ctx, &Porkbun{APIKey: cfg.Token, Secret: cfg.Secret}, cfg)
	}
	if cfg.Provider == "talaria" {
		return (&Talaria{URL: cfg.URL, Token: cfg.Token}).Update(ctx, cfg.Domain, cfg.IP)
	}
	var endpoint string
	switch cfg.Provider {
	case "duckdns":
		// DuckDNS wants the subdomain label only; strip the suffix if present.
		sub := strings.TrimSuffix(cfg.Domain, ".duckdns.org")
		endpoint = fmt.Sprintf("https://www.duckdns.org/update?domains=%s&token=%s&ip=%s",
			url.QueryEscape(sub), url.QueryEscape(cfg.Token), url.QueryEscape(cfg.IP))
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

// updatePorkbun points cfg.Domain's A record at cfg.IP, or at the public
// IPv4 Porkbun sees this connection coming from when cfg.IP is empty.
func updatePorkbun(ctx context.Context, p *Porkbun, cfg Config) error {
	ip := cfg.IP
	if ip == "" {
		var err error
		if ip, err = p.PublicIPv4(ctx); err != nil {
			return err
		}
	}
	return p.SetRecord(ctx, cfg.Domain, "A", ip)
}
