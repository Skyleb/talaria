// Package ddnshost turns a Talaria server into a dynamic-DNS provider for
// other Talaria installations. Each installation claims a label under the
// host's zone (skyle under talaria.gg → skyle.talaria.gg), receives a bearer
// token, and from then on keeps its A record (and the TXT record Let's
// Encrypt's DNS-01 challenge needs for home-network access) up to date by
// calling this server, which publishes the records through the host's own
// DNS provider (Porkbun).
package ddnshost

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/skyleb/talaria/internal/ddns"
	"github.com/skyleb/talaria/internal/store"
)

// Provider publishes records in the host's zone. *ddns.Porkbun satisfies it.
type Provider interface {
	SetRecord(ctx context.Context, host, typ, content string) error
	DeleteRecord(ctx context.Context, host, typ string) error
}

type Config struct {
	Zone     string // names are issued as <label>.<zone>
	MaxNames int    // total cap on claimed names (0 = 1000)
	// Reserved labels beyond the built-in list, e.g. this server's own
	// hostnames inside the zone.
	Reserved []string
	Provider Provider
}

// Error kinds the API maps to status codes.
var (
	ErrDisabled    = errors.New("hosted DDNS is not enabled on this server")
	ErrBadName     = errors.New("names are 3-32 characters: lowercase letters, digits and single hyphens, not at the ends")
	ErrReserved    = errors.New("that name is reserved")
	ErrTaken       = errors.New("that name is taken")
	ErrFull        = errors.New("this server is not accepting new names right now")
	ErrRateLimited = errors.New("too many claims from your address; try again in an hour")
	ErrBadToken    = errors.New("unknown or revoked token")
	ErrForeignHost = errors.New("that hostname does not belong to your name")
	ErrBadIP       = errors.New("ip must be an IPv4 address")
)

// ProviderError wraps a failure publishing through the DNS provider so the
// API can report it as 502 rather than a client mistake.
type ProviderError struct{ Err error }

func (e *ProviderError) Error() string { return "dns provider: " + e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

var (
	labelRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)
	subLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// reserved lists labels nobody may claim: infrastructure names, the
// host's own likely subdomains, and names that would confuse or impersonate.
var reserved = map[string]bool{
	"www": true, "mail": true, "smtp": true, "imap": true, "pop": true, "pop3": true, "mx": true,
	"ns": true, "ns1": true, "ns2": true, "ns3": true, "dns": true, "ddns": true,
	"api": true, "admin": true, "root": true, "sys": true, "system": true, "localhost": true,
	"lan": true, "home": true, "local": true, "app": true, "web": true, "static": true, "cdn": true,
	"ftp": true, "sftp": true, "ssh": true, "vpn": true, "git": true,
	"autodiscover": true, "autoconfig": true, "acme": true, "wpad": true,
	"talaria": true, "support": true, "help": true, "status": true, "blog": true, "docs": true,
	"dev": true, "test": true, "staging": true, "demo": true, "beta": true,
	"login": true, "auth": true, "account": true, "accounts": true, "secure": true, "security": true,
	"download": true, "downloads": true, "update": true, "updates": true,
}

// Service issues names and publishes their records. Reconfigurable at
// runtime (Set/Stop) like the other runners, so the admin UI can toggle it.
type Service struct {
	store *store.Store
	log   *slog.Logger

	mu  sync.RWMutex
	cfg *Config // nil = disabled

	claims claimLimiter
}

func New(st *store.Store, log *slog.Logger) *Service {
	return &Service{store: st, log: log, claims: claimLimiter{hits: map[string][]int64{}}}
}

// Set enables the service with cfg (zone and provider required).
func (s *Service) Set(cfg Config) error {
	cfg.Zone = strings.Trim(strings.ToLower(strings.TrimSpace(cfg.Zone)), ".")
	if cfg.Zone == "" {
		return errors.New("hosted DDNS needs a zone (the domain names are issued under)")
	}
	if _, _, err := ddns.SplitZone(cfg.Zone); err != nil {
		return err
	}
	if cfg.Provider == nil {
		return errors.New("hosted DDNS needs Porkbun API keys (set them under Dynamic DNS or here)")
	}
	if cfg.MaxNames <= 0 {
		cfg.MaxNames = 1000
	}
	s.mu.Lock()
	s.cfg = &cfg
	s.mu.Unlock()
	s.log.Info("hosted ddns enabled", "zone", cfg.Zone)
	return nil
}

// Stop disables the service; existing names stay in the database.
func (s *Service) Stop() {
	s.mu.Lock()
	s.cfg = nil
	s.mu.Unlock()
}

// Zone returns the active zone, or "" when disabled.
func (s *Service) Zone() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg == nil {
		return ""
	}
	return s.cfg.Zone
}

func (s *Service) config() (*Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg == nil {
		return nil, ErrDisabled
	}
	return s.cfg, nil
}

// Claim is the result of a successful name claim.
type Claim struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Token  string `json:"token"`
}

// Claim reserves label for the caller at ip, returning the token that
// authorizes updates for <label>.<zone> and every name beneath it. The A
// record is pointed at ip right away so the name resolves immediately.
func (s *Service) Claim(ctx context.Context, label, ip string) (*Claim, error) {
	cfg, err := s.config()
	if err != nil {
		return nil, err
	}
	label = strings.ToLower(strings.TrimSpace(label))
	if !labelRe.MatchString(label) || strings.Contains(label, "--") {
		return nil, ErrBadName
	}
	if reserved[label] || contains(cfg.Reserved, label) || strings.HasPrefix(label, "xn-") {
		return nil, ErrReserved
	}
	if !s.claims.allow(ip) {
		return nil, ErrRateLimited
	}
	n, err := s.store.CountDDNSNames(ctx)
	if err != nil {
		return nil, err
	}
	if n >= cfg.MaxNames {
		return nil, ErrFull
	}
	token, hash, err := newToken()
	if err != nil {
		return nil, err
	}
	if err := s.store.CreateDDNSName(ctx, label, hash, ip); err != nil {
		if errors.Is(err, store.ErrDDNSNameTaken) {
			return nil, ErrTaken
		}
		return nil, err
	}
	domain := label + "." + cfg.Zone
	s.log.Info("ddns name claimed", "domain", domain, "ip", ip)
	if v4 := net.ParseIP(ip); v4 != nil && v4.To4() != nil {
		// Best effort: the client's updater repeats this within minutes.
		if err := s.publish(ctx, cfg, label, domain, "A", ip); err != nil {
			s.log.Warn("initial record for claimed name failed", "domain", domain, "err", err)
		}
	}
	return &Claim{Name: label, Domain: domain, Token: token}, nil
}

// Authenticate resolves a bearer token to its name.
func (s *Service) Authenticate(ctx context.Context, token string) (*store.DDNSName, error) {
	if _, err := s.config(); err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrBadToken
	}
	n, err := s.store.DDNSNameByToken(ctx, hashToken(token))
	if err != nil {
		return nil, ErrBadToken
	}
	return n, nil
}

// Update points host's A record at ip. host defaults to the name's own
// domain and may be any name beneath it (lan.skyle.talaria.gg). Private
// IPs are allowed: that is how home-network access works.
func (s *Service) Update(ctx context.Context, n *store.DDNSName, host, ip string) (fqdn string, err error) {
	cfg, err := s.config()
	if err != nil {
		return "", err
	}
	fqdn, err = s.resolveHost(cfg, n, host)
	if err != nil {
		return "", err
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || parsed.To4() == nil {
		return "", ErrBadIP
	}
	return fqdn, s.publish(ctx, cfg, n.Name, fqdn, "A", parsed.To4().String())
}

// SetTXT publishes (or with empty txt clears) the DNS-01 challenge record
// _acme-challenge.<host>.
func (s *Service) SetTXT(ctx context.Context, n *store.DDNSName, host, txt string) error {
	cfg, err := s.config()
	if err != nil {
		return err
	}
	fqdn, err := s.resolveHost(cfg, n, host)
	if err != nil {
		return err
	}
	fqdn = "_acme-challenge." + fqdn
	if txt == "" {
		return s.unpublish(ctx, cfg, fqdn, "TXT")
	}
	return s.publish(ctx, cfg, n.Name, fqdn, "TXT", txt)
}

// Release deletes a name and every record published for it.
func (s *Service) Release(ctx context.Context, name string) error {
	cfg, err := s.config()
	if err != nil {
		return err
	}
	recs, err := s.store.DDNSRecordsFor(ctx, name)
	if err != nil {
		return err
	}
	var firstErr error
	for _, r := range recs {
		if err := cfg.Provider.DeleteRecord(ctx, r.Host, r.Type); err != nil && firstErr == nil {
			firstErr = &ProviderError{err}
		}
	}
	if firstErr != nil {
		return firstErr
	}
	s.log.Info("ddns name released", "name", name)
	return s.store.DeleteDDNSName(ctx, name)
}

// List returns every claimed name for the admin UI.
func (s *Service) List(ctx context.Context) ([]*store.DDNSName, error) {
	return s.store.ListDDNSNames(ctx)
}

func (s *Service) resolveHost(cfg *Config, n *store.DDNSName, host string) (string, error) {
	own := n.Name + "." + cfg.Zone
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || host == own {
		return own, nil
	}
	if !strings.HasSuffix(host, "."+own) {
		return "", ErrForeignHost
	}
	for _, l := range strings.Split(strings.TrimSuffix(host, "."+own), ".") {
		if !subLabelRe.MatchString(l) {
			return "", ErrForeignHost
		}
	}
	return host, nil
}

// publish sets a record through the provider unless the store already shows
// it with that content, so steady-state updates cost one SQLite read.
func (s *Service) publish(ctx context.Context, cfg *Config, name, host, typ, content string) error {
	cur, err := s.store.DDNSRecord(ctx, host, typ)
	if err != nil {
		return err
	}
	if cur == content {
		return nil
	}
	if err := cfg.Provider.SetRecord(ctx, host, typ, content); err != nil {
		return &ProviderError{err}
	}
	s.log.Info("ddns record published", "host", host, "type", typ, "content", content)
	return s.store.PutDDNSRecord(ctx, name, host, typ, content)
}

func (s *Service) unpublish(ctx context.Context, cfg *Config, host, typ string) error {
	cur, err := s.store.DDNSRecord(ctx, host, typ)
	if err != nil || cur == "" {
		return err
	}
	if err := cfg.Provider.DeleteRecord(ctx, host, typ); err != nil {
		return &ProviderError{err}
	}
	return s.store.DeleteDDNSRecord(ctx, host, typ)
}

// newToken returns a random bearer token and its storage hash.
func newToken() (token, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = "tdns_" + base64.RawURLEncoding.EncodeToString(buf)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), v) {
			return true
		}
	}
	return false
}

// ReservedFromHostnames extracts the labels of the host's own names that
// sit directly inside zone, so clients cannot claim them.
func ReservedFromHostnames(zone string, hosts ...string) []string {
	zone = strings.Trim(strings.ToLower(zone), ".")
	var out []string
	for _, h := range hosts {
		h = strings.Trim(strings.ToLower(strings.TrimSpace(h)), ".")
		if rest, ok := strings.CutSuffix(h, "."+zone); ok && rest != "" {
			// lan.skyle.talaria.gg under talaria.gg reserves "skyle".
			labels := strings.Split(rest, ".")
			out = append(out, labels[len(labels)-1])
		}
	}
	return out
}

// claimLimiter allows a few claims per address per hour.
type claimLimiter struct {
	sync.Mutex
	hits map[string][]int64
}

func (l *claimLimiter) allow(ip string) bool {
	const window, max = int64(time.Hour / time.Millisecond), 5
	now := time.Now().UnixMilli()
	l.Lock()
	defer l.Unlock()
	keep := l.hits[ip][:0]
	for _, t := range l.hits[ip] {
		if now-t < window {
			keep = append(keep, t)
		}
	}
	if len(keep) >= max {
		l.hits[ip] = keep
		return false
	}
	l.hits[ip] = append(keep, now)
	return true
}

// FormatClaimError renders a service error for the API.
func FormatClaimError(err error) string {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return fmt.Sprintf("the DNS provider rejected the change: %v", pe.Err)
	}
	return err.Error()
}
