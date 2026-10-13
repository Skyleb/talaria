// Command dns01check verifies the DNS-01 certificate flow end to end against
// a real ACME directory (defaults to Let's Encrypt staging so no production
// rate limits are consumed).
//
//	go run ./scripts/dns01check <domain> <duckdns-token> [acme-directory-url]
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/skyleb/talaria/internal/tlsx"
)

const stagingURL = "https://acme-staging-v02.api.letsencrypt.org/directory"

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: dns01check <domain> <duckdns-token> [acme-directory-url]")
		os.Exit(2)
	}
	domain, token := os.Args[1], os.Args[2]
	directory := stagingURL
	if len(os.Args) > 3 {
		directory = os.Args[3]
	}
	dir := filepath.Join(os.TempDir(), "talaria-dns01check")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	m := tlsx.NewDNS01Manager(dir, domain, token, directory, log)
	defer m.Close()
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		ok, notAfter, lastErr := m.Status()
		if ok {
			fmt.Printf("SUCCESS: certificate for %s, expires %s\n", domain, notAfter.Format(time.RFC3339))
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
