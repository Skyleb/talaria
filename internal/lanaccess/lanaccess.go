// Package lanaccess makes the server reachable at full speed from inside its
// own network without router or client changes. A second (lan) domain is kept
// pointed at the server's private LAN IP, carries its own Let's Encrypt
// certificate (via DNS-01), and devices whose requests arrive through the
// router's NAT loopback are redirected to it by the HTTP layer.
package lanaccess

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/skyleb/talaria/internal/ddns"
	"github.com/skyleb/talaria/internal/landns"
	"github.com/skyleb/talaria/internal/tlsx"
)

type Config struct {
	Domain   string // e.g. yourname-lan.duckdns.org or lan.talaria.gg
	Provider string // DNS provider: "duckdns" or "porkbun" (updates the record and solves DNS-01)
	Token    string // DuckDNS token / Porkbun API key
	Secret   string // Porkbun secret API key
	LanIP    string // empty = auto-detect
	DataDir  string // certificate cache location
}

// Runner owns the lan domain's record updater and certificate manager; its
// config can be swapped at runtime from the admin settings UI.
type Runner struct {
	log *slog.Logger

	mu      sync.Mutex
	domain  string
	lanIP   string
	mgr     *tlsx.DNS01Manager
	rec     *ddns.Runner
	lastErr string
}

func NewRunner(log *slog.Logger) *Runner {
	return &Runner{log: log, rec: ddns.NewRunner(log)}
}

// Set stops any current state and starts the record updater + cert manager
// for cfg. Errors (also kept for Status) are config-level; certificate
// issuance happens asynchronously and reports through Status.
func (r *Runner) Set(cfg Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()

	fail := func(msg string) error {
		r.lastErr = msg
		return errors.New(msg)
	}
	domain := strings.ToLower(strings.TrimSpace(cfg.Domain))
	if domain == "" {
		return fail("no home-access domain configured")
	}
	provider := cfg.Provider
	if provider == "" {
		provider = "duckdns"
	}
	txt, err := ddns.NewTXTSetter(provider, cfg.Token, cfg.Secret)
	if err != nil {
		return fail(err.Error())
	}
	lanIP := strings.TrimSpace(cfg.LanIP)
	if lanIP == "" {
		ip := landns.DetectLanIP()
		if ip == nil {
			return fail("could not detect this machine's LAN IP")
		}
		lanIP = ip.String()
	}

	r.domain = domain
	r.lanIP = lanIP
	r.rec.Set(ddns.Config{
		Provider: provider,
		Domain:   domain,
		Token:    cfg.Token,
		Secret:   cfg.Secret,
		IP:       lanIP,
		Interval: 5 * time.Minute,
	})
	r.mgr = tlsx.NewDNS01Manager(cfg.DataDir, domain, txt, "", r.log)
	r.log.Info("lan access enabled", "domain", domain, "provider", provider, "lan_ip", lanIP)
	return nil
}

func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

func (r *Runner) stopLocked() {
	if r.mgr != nil {
		r.mgr.Close()
		r.mgr = nil
	}
	r.rec.Stop()
	r.domain = ""
	r.lanIP = ""
	r.lastErr = ""
}

// Domain returns the lan domain, or "" when inactive.
func (r *Runner) Domain() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.domain
}

// LanIP returns the private address the lan domain points at.
func (r *Runner) LanIP() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lanIP
}

// Matches reports whether a TLS SNI belongs to the lan domain.
func (r *Runner) Matches(serverName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.domain != "" && strings.EqualFold(serverName, r.domain)
}

// Certificate returns the lan domain's certificate (used from GetCertificate
// when Matches is true).
func (r *Runner) Certificate() (*tls.Certificate, error) {
	r.mu.Lock()
	mgr := r.mgr
	r.mu.Unlock()
	if mgr == nil {
		return nil, errors.New("lan access not active")
	}
	return mgr.Certificate()
}

// Status is the JSON-friendly state shown in the admin settings UI.
func (r *Runner) Status() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := map[string]any{
		"active": r.domain != "", "domain": r.domain, "lan_ip": r.lanIP,
		"cert_ok": false, "cert_expires": "", "error": r.lastErr,
	}
	if r.mgr != nil {
		ok, notAfter, lastErr := r.mgr.Status()
		st["cert_ok"] = ok
		if ok {
			st["cert_expires"] = notAfter.UTC().Format(time.RFC3339)
		}
		if lastErr != "" && st["error"] == "" {
			st["error"] = lastErr
		}
	}
	return st
}
