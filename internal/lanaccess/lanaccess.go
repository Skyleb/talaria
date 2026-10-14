// Package lanaccess makes the server reachable at full speed from inside its
// own network without router or client changes. A second (lan) domain is kept
// pointed at the server's private LAN IP, carries its own Let's Encrypt
// certificate (via DNS-01), and devices whose requests arrive through the
// router's NAT loopback are redirected to it by the HTTP layer.
package lanaccess

import (
	"crypto/tls"
	"crypto/x509"
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
	Provider string // DNS provider: "duckdns", "porkbun" or "talaria" (updates the record and solves DNS-01)
	Token    string // DuckDNS token / Porkbun API key / Talaria name token
	Secret   string // Porkbun secret API key
	URL      string // talaria: the host server, e.g. https://talaria.gg
	LanIP    string // empty = auto-detect
	DataDir  string // certificate cache location
}

// Runner owns the lan domain's record updater and certificate manager; its
// config can be swapped at runtime from the admin settings UI.
//
// A domain that stops being the active lan domain (feature switched off, or
// the name changed) is kept as "retired": its DNS record still points at this
// machine and devices may still hold its URL (an open tab, a home-screen
// shortcut), so the runner keeps answering TLS for it with the cached
// certificate and the HTTP layer redirects it to the main domain. Without
// this, switching the feature off turns every such device into a handshake
// error that a refresh cannot fix.
type Runner struct {
	log *slog.Logger

	mu      sync.Mutex
	domain  string
	lanIP   string
	dataDir string
	mgr     *tlsx.DNS01Manager
	rec     *ddns.Runner
	lastErr string

	retired     string
	retiredCert *tls.Certificate // nil when no usable cached cert
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
	r.dataDir = cfg.DataDir
	if r.retired == domain {
		// Re-enabled under the same name: it is the live domain again.
		r.retired, r.retiredCert = "", nil
	}
	provider := cfg.Provider
	if provider == "" {
		provider = "duckdns"
	}
	txt, err := ddns.NewTXTSetter(provider, cfg.Token, cfg.Secret, cfg.URL)
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
		URL:      cfg.URL,
		IP:       lanIP,
		Interval: 5 * time.Minute,
	})
	r.mgr = tlsx.NewDNS01Manager(cfg.DataDir, domain, txt, "", r.log)
	r.log.Info("lan access enabled", "domain", domain, "provider", provider, "lan_ip", lanIP)
	return nil
}

// Stop switches lan access off. The domain that was active stays retired so
// devices still pointed at it are redirected rather than refused.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

func (r *Runner) stopLocked() {
	if r.domain != "" {
		var cert *tls.Certificate
		if r.mgr != nil {
			cert, _ = r.mgr.Certificate()
		}
		r.retireLocked(r.domain, cert)
	}
	if r.mgr != nil {
		r.mgr.Close()
		r.mgr = nil
	}
	r.rec.Stop()
	r.domain = ""
	r.lanIP = ""
	r.lastErr = ""
}

// Retire registers a lan domain that is configured but not enabled (the
// startup case) so it is served and redirected like one switched off at
// runtime. The certificate comes from the on-disk cache under dataDir.
func (r *Runner) Retire(domain, dataDir string) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.domain == domain {
		return // live, not retired
	}
	r.dataDir = dataDir
	r.retireLocked(domain, nil)
}

func (r *Runner) retireLocked(domain string, cert *tls.Certificate) {
	if cert != nil && cert.Leaf == nil {
		if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil {
			cert.Leaf = leaf
		} else {
			cert = nil
		}
	}
	if cert == nil && r.dataDir != "" {
		c, err := tlsx.LoadCachedLanCert(r.dataDir, domain)
		if err != nil {
			r.log.Info("lan access off; no usable certificate for the old domain, HTTPS on it will fail until re-enabled",
				"domain", domain, "err", err)
		}
		cert = c
	}
	r.retired = domain
	r.retiredCert = cert
	if cert != nil {
		r.log.Info("lan access off; redirecting the old domain to the main domain",
			"domain", domain, "cert_expires", cert.Leaf.NotAfter.UTC().Format(time.RFC3339))
	}
}

// Retired returns the switched-off lan domain still being redirected, or "".
func (r *Runner) Retired() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.retired
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

// Certificate answers a TLS ClientHello's server name. handled is true when
// the name is the live lan domain or a retired one with a usable cached
// certificate; callers fall through to their main-domain source otherwise.
func (r *Runner) Certificate(serverName string) (cert *tls.Certificate, handled bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.domain != "" && strings.EqualFold(serverName, r.domain):
		if r.mgr == nil {
			return nil, true, errors.New("lan access not active")
		}
		cert, err = r.mgr.Certificate()
		return cert, true, err
	case r.retired != "" && r.retiredCert != nil && strings.EqualFold(serverName, r.retired):
		if time.Now().After(r.retiredCert.Leaf.NotAfter) {
			// Nobody renews a retired domain's certificate; once it lapses
			// the name is refused like any other unknown host.
			r.log.Info("retired lan domain certificate expired; no longer served", "domain", r.retired)
			r.retiredCert = nil
			return nil, false, nil
		}
		return r.retiredCert, true, nil
	}
	return nil, false, nil
}

// Status is the JSON-friendly state shown in the admin settings UI.
func (r *Runner) Status() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := map[string]any{
		"active": r.domain != "", "domain": r.domain, "lan_ip": r.lanIP,
		"cert_ok": false, "cert_expires": "", "error": r.lastErr,
		"retired_domain": r.retired, "retired_served": r.retiredCert != nil,
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
