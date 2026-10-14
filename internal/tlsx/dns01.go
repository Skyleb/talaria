package tlsx

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/skyleb/talaria/internal/ddns"
)

// DNS01Manager obtains and renews a Let's Encrypt certificate for one domain
// using the DNS-01 challenge, publishing the TXT record through a DNS
// provider (DuckDNS, Porkbun). This is how the lan-access domain gets a
// trusted certificate: it resolves to a private IP, which HTTP-01
// validation could never reach.
type DNS01Manager struct {
	domain    string
	txt       ddns.TXTSetter
	dir       string
	directory string // ACME directory URL; "" = Let's Encrypt production
	log       *slog.Logger

	cert    atomic.Pointer[tls.Certificate]
	errMu   sync.Mutex
	lastErr string
	cancel  context.CancelFunc
}

// renewBefore is how far ahead of expiry renewal starts.
const renewBefore = 30 * 24 * time.Hour

// NewDNS01Manager loads any cached certificate and starts the obtain/renew
// loop in the background. directoryURL "" selects Let's Encrypt production.
func NewDNS01Manager(dataDir, domain string, txt ddns.TXTSetter, directoryURL string, log *slog.Logger) *DNS01Manager {
	if directoryURL == "" {
		directoryURL = acme.LetsEncryptURL
	}
	m := &DNS01Manager{
		domain:    strings.ToLower(domain),
		txt:       txt,
		dir:       LanCertDir(dataDir, domain),
		directory: directoryURL,
		log:       log,
	}
	m.loadCached()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go m.maintain(ctx)
	return m
}

// Certificate returns the current certificate, or an error (including the
// last issuance failure) while none is available yet.
func (m *DNS01Manager) Certificate() (*tls.Certificate, error) {
	if c := m.cert.Load(); c != nil {
		return c, nil
	}
	m.errMu.Lock()
	defer m.errMu.Unlock()
	if m.lastErr != "" {
		return nil, fmt.Errorf("no certificate for %s yet: %s", m.domain, m.lastErr)
	}
	return nil, fmt.Errorf("certificate for %s is still being issued", m.domain)
}

// Status reports whether a certificate is loaded, its expiry, and the last
// issuance error (if any).
func (m *DNS01Manager) Status() (ok bool, notAfter time.Time, lastErr string) {
	m.errMu.Lock()
	lastErr = m.lastErr
	m.errMu.Unlock()
	if c := m.cert.Load(); c != nil && c.Leaf != nil {
		return true, c.Leaf.NotAfter, lastErr
	}
	return false, time.Time{}, lastErr
}

func (m *DNS01Manager) Close() { m.cancel() }

func (m *DNS01Manager) setErr(err error) {
	m.errMu.Lock()
	defer m.errMu.Unlock()
	if err == nil {
		m.lastErr = ""
	} else {
		m.lastErr = err.Error()
	}
}

// retryAfterFailure is how soon a failed issuance is retried (a brand-new
// domain's delegation or a DNS hiccup should not cost half a day).
const retryAfterFailure = 5 * time.Minute

func (m *DNS01Manager) maintain(ctx context.Context) {
	for {
		delay := 12 * time.Hour
		if !m.ensure(ctx) {
			delay = retryAfterFailure
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// ensure obtains or renews the certificate when needed and reports whether
// a usable certificate is in place afterwards.
func (m *DNS01Manager) ensure(ctx context.Context) bool {
	if c := m.cert.Load(); c != nil && c.Leaf != nil && time.Until(c.Leaf.NotAfter) > renewBefore {
		return true
	}
	obtainCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	if err := m.obtain(obtainCtx); err != nil {
		m.setErr(err)
		m.log.Error("lan cert issuance failed", "domain", m.domain, "err", err)
		return m.cert.Load() != nil
	}
	m.setErr(nil)
	if c := m.cert.Load(); c != nil && c.Leaf != nil {
		m.log.Info("lan cert issued", "domain", m.domain, "expires", c.Leaf.NotAfter.Format(time.RFC3339))
	}
	return true
}

// LanCertDir is where a lan domain's certificate, key and ACME account live.
func LanCertDir(dataDir, domain string) string {
	return filepath.Join(dataDir, "certs", "lan", strings.ToLower(domain))
}

// LoadCachedLanCert returns the lan domain's on-disk certificate (Leaf set),
// or an error when there is none or it has expired. Besides warm-starting the
// manager, it is what lets a switched-off lan domain keep answering TLS: the
// DNS record and the certificate both outlive the feature being enabled.
func LoadCachedLanCert(dataDir, domain string) (*tls.Certificate, error) {
	dir := LanCertDir(dataDir, domain)
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	if time.Now().After(leaf.NotAfter) {
		return nil, fmt.Errorf("cached certificate for %s expired %s", domain, leaf.NotAfter.Format(time.RFC3339))
	}
	cert.Leaf = leaf
	return &cert, nil
}

func (m *DNS01Manager) loadCached() {
	// m.dir is <dataDir>/certs/lan/<domain>; walk back up to dataDir.
	dataDir := filepath.Dir(filepath.Dir(filepath.Dir(m.dir)))
	if cert, err := LoadCachedLanCert(dataDir, m.domain); err == nil {
		m.cert.Store(cert)
	}
}

func (m *DNS01Manager) obtain(ctx context.Context) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	acctKey, err := loadOrCreateKey(filepath.Join(m.dir, "account.key"))
	if err != nil {
		return fmt.Errorf("account key: %w", err)
	}
	client := &acme.Client{Key: acctKey, DirectoryURL: m.directory}
	if _, err := client.Register(ctx, &acme.Account{}, acme.AcceptTOS); err != nil &&
		!errors.Is(err, acme.ErrAccountAlreadyExists) {
		return fmt.Errorf("acme register: %w", err)
	}

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(m.domain))
	if err != nil {
		return fmt.Errorf("authorize order: %w", err)
	}
	// Always clear the TXT record afterwards; it's only challenge plumbing.
	defer func() {
		clearCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		m.txt.SetTXT(clearCtx, m.domain, "")
	}()
	for _, zurl := range order.AuthzURLs {
		z, err := client.GetAuthorization(ctx, zurl)
		if err != nil {
			return err
		}
		if z.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range z.Challenges {
			if c.Type == "dns-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return errors.New("ACME offered no dns-01 challenge")
		}
		txt, err := client.DNS01ChallengeRecord(chal.Token)
		if err != nil {
			return err
		}
		if err := m.txt.SetTXT(ctx, m.domain, txt); err != nil {
			return fmt.Errorf("publish TXT record: %w", err)
		}
		// Don't race the validator: wait until the zone's own nameservers
		// serve the record (DuckDNS is near-instant, Porkbun can take a
		// little while).
		if err := waitForTXT(ctx, m.domain, txt, m.log); err != nil {
			return err
		}
		if _, err := client.Accept(ctx, chal); err != nil {
			return fmt.Errorf("accept challenge: %w", err)
		}
		if _, err := client.WaitAuthorization(ctx, zurl); err != nil {
			return fmt.Errorf("dns-01 validation: %w", err)
		}
	}
	if _, err := client.WaitOrder(ctx, order.URI); err != nil {
		return fmt.Errorf("wait order: %w", err)
	}

	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{DNSNames: []string{m.domain}}, certKey)
	if err != nil {
		return err
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return err
	}

	// Persist, then publish.
	keyDER, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return err
	}
	keyOut, err := os.OpenFile(filepath.Join(m.dir, "key.pem"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyOut.Close()
	certOut, err := os.Create(filepath.Join(m.dir, "cert.pem"))
	if err != nil {
		return err
	}
	for _, der := range chain {
		pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	certOut.Close()

	m.cert.Store(&tls.Certificate{Certificate: chain, PrivateKey: certKey, Leaf: leaf})
	return nil
}

// waitForTXT polls the authoritative nameservers of domain's zone until all
// of them return want for _acme-challenge.<domain>, giving up (but not
// failing) after two minutes. It does fail when the zone has no reachable
// nameservers at all: validation could only produce NXDOMAIN, so better to
// report the real problem (usually a freshly registered domain whose
// delegation hasn't reached the registry yet).
func waitForTXT(ctx context.Context, domain, want string, log *slog.Logger) error {
	name := "_acme-challenge." + domain
	pause := func(d time.Duration) error {
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	zone, _, err := ddns.SplitZone(domain)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	var servers []string
	for len(servers) == 0 {
		lookupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		nss, lookupErr := net.DefaultResolver.LookupNS(lookupCtx, zone)
		cancel()
		for _, ns := range nss {
			servers = append(servers, strings.TrimSuffix(ns.Host, "."))
		}
		if len(servers) > 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no nameservers found for %s (%v); if the domain was just registered, "+
				"the registry can take up to an hour to publish it — will retry", zone, lookupErr)
		}
		if err := pause(15 * time.Second); err != nil {
			return err
		}
	}
	for {
		if txtServedBy(ctx, servers, name, want) {
			return nil
		}
		if time.Now().After(deadline) {
			log.Warn("TXT record not visible on all nameservers yet; trying validation anyway",
				"name", name, "nameservers", servers)
			return nil
		}
		if err := pause(5 * time.Second); err != nil {
			return err
		}
	}
}

// txtServedBy reports whether every server answers a TXT query for name
// with want among the values.
func txtServedBy(ctx context.Context, servers []string, name, want string) bool {
	for _, server := range servers {
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, net.JoinHostPort(server, "53"))
			},
		}
		qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		txts, err := r.LookupTXT(qCtx, name)
		cancel()
		if err != nil || !slices.Contains(txts, want) {
			return false
		}
	}
	return true
}

func loadOrCreateKey(path string) (*ecdsa.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		if block, _ := pem.Decode(data); block != nil {
			return x509.ParseECPrivateKey(block.Bytes)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	defer out.Close()
	if err := pem.Encode(out, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}); err != nil {
		return nil, err
	}
	return key, nil
}
