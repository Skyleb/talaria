package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/skyleb/talaria/internal/config"
	"github.com/skyleb/talaria/internal/ddns"
	"github.com/skyleb/talaria/internal/ddnshost"
	"github.com/skyleb/talaria/internal/store"
)

// fakeProvider records what the host publishes.
type fakeProvider struct {
	mu      sync.Mutex
	records map[string]string // "type host" -> content
	calls   int
	fail    bool
}

func (f *fakeProvider) SetRecord(_ context.Context, host, typ, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return context.DeadlineExceeded
	}
	f.records[typ+" "+host] = content
	return nil
}

func (f *fakeProvider) DeleteRecord(_ context.Context, host, typ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	delete(f.records, typ+" "+host)
	return nil
}

func (f *fakeProvider) get(typ, host string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records[typ+" "+host]
}

// newDDNSHost spins up a host server with only the hosted-DDNS routes.
func newDDNSHost(t *testing.T) (*httptest.Server, *fakeProvider, *ddnshost.Service) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	prov := &fakeProvider{records: map[string]string{}}
	svc := ddnshost.New(st, slog.Default())
	if err := svc.Set(ddnshost.Config{Zone: "talaria.gg", Provider: prov, Reserved: []string{"skyle"}}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Log: slog.Default(), Config: config.Default(), DDNSHost: svc}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/ddns/zone", s.handleDDNSHostZone)
	mux.HandleFunc("POST /api/v1/ddns/claim", s.handleDDNSHostClaim)
	mux.HandleFunc("POST /api/v1/ddns/update", s.handleDDNSHostUpdate)
	mux.HandleFunc("POST /api/v1/ddns/txt", s.handleDDNSHostTXT)
	mux.HandleFunc("DELETE /api/v1/ddns/name", s.handleDDNSHostRelease)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, prov, svc
}

func TestDDNSHostEndToEnd(t *testing.T) {
	host, prov, svc := newDDNSHost(t)
	ctx := context.Background()
	client := &ddns.Talaria{URL: host.URL}

	info, err := client.Zone(ctx)
	if err != nil || info.Zone != "talaria.gg" {
		t.Fatalf("zone: %v %+v", err, info)
	}

	// Claiming points the A record at the caller immediately.
	claim, err := client.Claim(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if claim.Domain != "alice.talaria.gg" || !strings.HasPrefix(claim.Token, "tdns_") {
		t.Fatalf("claim = %+v", claim)
	}
	if got := prov.get("A", "alice.talaria.gg"); got != "127.0.0.1" {
		t.Fatalf("initial A record = %q", got)
	}

	// Reserved, taken and malformed names are refused.
	for _, bad := range []string{"skyle", "alice", "www", "a", "-abc", "ab--cd", "Hello World"} {
		if _, err := client.Claim(ctx, bad); err == nil {
			t.Errorf("claim %q should fail", bad)
		}
	}

	// Updates through the normal DDNS loop: the main name with the caller's
	// IP, the lan name pinned to a private address, both on one token.
	client.Token = claim.Token
	calls := prov.calls
	if err := ddnsUpdateOnce(ctx, ddns.Config{Provider: "talaria", URL: host.URL, Token: claim.Token, Domain: "alice.talaria.gg"}); err != nil {
		t.Fatal(err)
	}
	if prov.calls != calls {
		t.Fatalf("unchanged IP should not reach the provider (calls %d → %d)", calls, prov.calls)
	}
	if err := ddnsUpdateOnce(ctx, ddns.Config{Provider: "talaria", URL: host.URL, Token: claim.Token, Domain: "lan.alice.talaria.gg", IP: "192.168.1.20"}); err != nil {
		t.Fatal(err)
	}
	if got := prov.get("A", "lan.alice.talaria.gg"); got != "192.168.1.20" {
		t.Fatalf("lan A record = %q", got)
	}

	// Someone else's name, or a bogus token, is refused.
	if err := client.Update(ctx, "bob.talaria.gg", ""); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("foreign host: %v", err)
	}
	if err := (&ddns.Talaria{URL: host.URL, Token: "tdns_nope"}).Update(ctx, "", ""); err == nil {
		t.Fatal("bad token accepted")
	}

	// DNS-01 TXT through the TXTSetter interface used by lan-access.
	setter, err := ddns.NewTXTSetter("talaria", claim.Token, "", host.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := setter.SetTXT(ctx, "lan.alice.talaria.gg", "challenge-1"); err != nil {
		t.Fatal(err)
	}
	if got := prov.get("TXT", "_acme-challenge.lan.alice.talaria.gg"); got != "challenge-1" {
		t.Fatalf("TXT = %q", got)
	}
	if err := setter.SetTXT(ctx, "lan.alice.talaria.gg", ""); err != nil {
		t.Fatal(err)
	}
	if got := prov.get("TXT", "_acme-challenge.lan.alice.talaria.gg"); got != "" {
		t.Fatalf("TXT after clear = %q", got)
	}

	// Admin view sees the name with its two records; release removes all.
	names, err := svc.List(ctx)
	if err != nil || len(names) != 1 || len(names[0].Records) != 2 {
		t.Fatalf("list: %v %+v", err, names)
	}
	req, _ := http.NewRequest(http.MethodDelete, host.URL+"/api/v1/ddns/name", nil)
	req.Header.Set("Authorization", "Bearer "+claim.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("release: %v %v", err, resp.Status)
	}
	if prov.get("A", "alice.talaria.gg") != "" || prov.get("A", "lan.alice.talaria.gg") != "" {
		t.Fatal("records survived release")
	}
	if _, err := client.Claim(ctx, "alice"); err != nil {
		t.Fatalf("re-claim after release: %v", err)
	}
}

func TestDDNSHostDisabled(t *testing.T) {
	host, _, svc := newDDNSHost(t)
	svc.Stop()
	if _, err := (&ddns.Talaria{URL: host.URL}).Claim(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("disabled host: %v", err)
	}
}

func TestDDNSHostProviderFailure(t *testing.T) {
	host, prov, _ := newDDNSHost(t)
	ctx := context.Background()
	client := &ddns.Talaria{URL: host.URL}
	claim, err := client.Claim(ctx, "carol")
	if err != nil {
		t.Fatal(err)
	}
	prov.fail = true
	client.Token = claim.Token
	err = client.Update(ctx, "", "203.0.113.9")
	if err == nil || !strings.Contains(err.Error(), "DNS provider rejected") {
		t.Fatalf("provider failure surfaced as: %v", err)
	}
	// Nothing was recorded as published, so the next attempt retries.
	prov.fail = false
	if err := client.Update(ctx, "", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	if prov.get("A", "carol.talaria.gg") != "203.0.113.9" {
		t.Fatal("retry did not publish")
	}
}

// ddnsUpdateOnce runs one tick of the client updater.
func ddnsUpdateOnce(ctx context.Context, cfg ddns.Config) error { return ddns.UpdateOnce(ctx, cfg) }
