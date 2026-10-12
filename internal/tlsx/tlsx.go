// Package tlsx provides ACME (Let's Encrypt) and self-signed certificate modes.
package tlsx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// NewACMEManager returns an autocert manager caching certs under dataDir.
func NewACMEManager(dataDir, domain, email string) *autocert.Manager {
	return &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
		Cache:      autocert.DirCache(filepath.Join(dataDir, "certs", "acme")),
		Email:      email,
	}
}

// HTTPChallengeServer runs the :80 listener that answers HTTP-01 challenges and
// redirects everything else to HTTPS. Returns the server so callers can Shutdown.
func HTTPChallengeServer(m *autocert.Manager, domain string) *http.Server {
	redirect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := "https://" + domain + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
	return &http.Server{Addr: ":80", Handler: m.HTTPHandler(redirect)}
}

// SelfSigned loads or creates a persistent self-signed certificate whose SANs
// cover localhost, the machine hostname, current LAN IPs, and extraHosts.
func SelfSigned(dataDir string, extraHosts ...string) (tls.Certificate, error) {
	dir := filepath.Join(dataDir, "certs", "selfsigned")
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		// Reuse until within 30 days of expiry.
		if leaf, err := x509.ParseCertificate(cert.Certificate[0]); err == nil &&
			time.Until(leaf.NotAfter) > 30*24*time.Hour {
			return cert, nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return tls.Certificate{}, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	dnsNames := []string{"localhost"}
	if hn, err := os.Hostname(); err == nil {
		dnsNames = append(dnsNames, hn)
	}
	for _, h := range extraHosts {
		if h != "" {
			dnsNames = append(dnsNames, h)
		}
	}
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
				ips = append(ips, ipn.IP)
			}
		}
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Talaria self-signed", Organization: []string{"Talaria"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(2 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certOut, err := os.Create(certPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	certOut.Close()
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return tls.Certificate{}, err
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyOut.Close()
	return tls.LoadX509KeyPair(certPath, keyPath)
}
