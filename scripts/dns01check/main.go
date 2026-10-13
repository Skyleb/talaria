// Command dns01check verifies the DNS-01 certificate flow end to end against
// a real ACME directory (defaults to Let's Encrypt staging so no production
// rate limits are consumed).
//
//	go run ./scripts/dns01check -domain lan.example.duckdns.org -token <duckdns-token>
//	go run ./scripts/dns01check -provider porkbun -domain lan.talaria.gg -token <api-key> -secret <secret-key>
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/skyleb/talaria/internal/ddns"
	"github.com/skyleb/talaria/internal/tlsx"
)

const stagingURL = "https://acme-staging-v02.api.letsencrypt.org/directory"

func main() {
	provider := flag.String("provider", "duckdns", "DNS provider: duckdns or porkbun")
	domain := flag.String("domain", "", "domain to get a certificate for")
	token := flag.String("token", "", "DuckDNS token or Porkbun API key")
	secret := flag.String("secret", "", "Porkbun secret API key")
	directory := flag.String("acme", stagingURL, "ACME directory URL")
	flag.Parse()
	if *domain == "" || *token == "" {
		flag.Usage()
		os.Exit(2)
	}
	setter, err := ddns.NewTXTSetter(*provider, *token, *secret, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	dir := filepath.Join(os.TempDir(), "talaria-dns01check")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	m := tlsx.NewDNS01Manager(dir, *domain, setter, *directory, log)
	defer m.Close()
	deadline := time.Now().Add(6 * time.Minute)
	for time.Now().Before(deadline) {
		ok, notAfter, lastErr := m.Status()
		if ok {
			fmt.Printf("SUCCESS: certificate for %s, expires %s\n", *domain, notAfter.Format(time.RFC3339))
			return
		}
		if lastErr != "" {
			fmt.Fprintf(os.Stderr, "FAILED: %s\n", lastErr)
			os.Exit(1)
		}
		time.Sleep(3 * time.Second)
	}
	fmt.Fprintln(os.Stderr, "TIMEOUT waiting for certificate")
	os.Exit(1)
}
