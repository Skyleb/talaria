package lanaccess

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/skyleb/talaria/internal/tlsx"
)

// writeLanCert drops a self-signed certificate for domain into the lan cert
// cache under dataDir, the way a DNS-01 issuance would have left it.
func writeLanCert(t *testing.T, dataDir, domain string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := tlsx.LanCertDir(dataDir, domain)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredDomainServesCachedCert(t *testing.T) {
	dataDir := t.TempDir()
	writeLanCert(t, dataDir, "lan.example.gg", time.Now().Add(24*time.Hour))

	r := NewRunner(slog.Default())
	r.Retire("LAN.example.gg", dataDir)

	if got := r.Retired(); got != "lan.example.gg" {
		t.Fatalf("Retired() = %q, want lan.example.gg", got)
	}
	if got := r.Domain(); got != "" {
		t.Fatalf("Domain() = %q, want inactive", got)
	}

	cert, handled, err := r.Certificate("lan.example.gg")
	if err != nil || !handled || cert == nil {
		t.Fatalf("Certificate(retired) = (%v, %v, %v), want cert, handled, nil", cert, handled, err)
	}
	if cert.Leaf == nil || cert.Leaf.DNSNames[0] != "lan.example.gg" {
		t.Fatalf("served certificate is not the cached one: %+v", cert.Leaf)
	}

	// Other names fall through to the main-domain certificate source.
	if _, handled, _ := r.Certificate("example.gg"); handled {
		t.Fatal("Certificate(main domain) handled by the lan runner")
	}

	st := r.Status()
	if st["active"] != false || st["retired_domain"] != "lan.example.gg" || st["retired_served"] != true {
		t.Fatalf("Status() = %v", st)
	}
}

func TestRetiredDomainWithoutCertIsRedirectOnly(t *testing.T) {
	r := NewRunner(slog.Default())
	r.Retire("lan.example.gg", t.TempDir())

	if got := r.Retired(); got != "lan.example.gg" {
		t.Fatalf("Retired() = %q; the HTTP redirect needs the name even without a cert", got)
	}
	if _, handled, _ := r.Certificate("lan.example.gg"); handled {
		t.Fatal("Certificate() claimed a name it has no certificate for")
	}
	if st := r.Status(); st["retired_served"] != false {
		t.Fatalf("Status() = %v", st)
	}
}

func TestRetiredDomainExpiredCertIsNotServed(t *testing.T) {
	dataDir := t.TempDir()
	writeLanCert(t, dataDir, "lan.example.gg", time.Now().Add(-time.Minute))

	r := NewRunner(slog.Default())
	r.Retire("lan.example.gg", dataDir)
	if _, handled, _ := r.Certificate("lan.example.gg"); handled {
		t.Fatal("expired cached certificate was served")
	}
	if got := r.Retired(); got != "lan.example.gg" {
		t.Fatalf("Retired() = %q", got)
	}
}

func TestRetireIgnoresEmptyDomain(t *testing.T) {
	r := NewRunner(slog.Default())
	r.Retire("  ", t.TempDir())
	if got := r.Retired(); got != "" {
		t.Fatalf("Retired() = %q, want empty", got)
	}
}
