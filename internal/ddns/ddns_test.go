package ddns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestSplitZone(t *testing.T) {
	cases := []struct{ in, zone, sub string }{
		{"talaria.gg", "talaria.gg", ""},
		{"lan.talaria.gg", "talaria.gg", "lan"},
		{"_acme-challenge.lan.talaria.gg", "talaria.gg", "_acme-challenge.lan"},
		{"Example.CO.UK.", "example.co.uk", ""},
		{"www.example.co.uk", "example.co.uk", "www"},
	}
	for _, c := range cases {
		zone, sub, err := SplitZone(c.in)
		if err != nil || zone != c.zone || sub != c.sub {
			t.Errorf("SplitZone(%q) = %q, %q, %v; want %q, %q", c.in, zone, sub, err, c.zone, c.sub)
		}
	}
	for _, bad := range []string{"", "localhost", "co.uk"} {
		if _, _, err := SplitZone(bad); err == nil {
			t.Errorf("SplitZone(%q) should fail", bad)
		}
	}
}

// fakePorkbun is an in-memory stand-in for the Porkbun API: records keyed
// by "type name" (several values allowed), plus a log of the endpoints hit.
type fakePorkbun struct {
	t       *testing.T
	records map[string][]string
	calls   []string
}

func (f *fakePorkbun) handler(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	json.NewDecoder(r.Body).Decode(&body)
	if body["apikey"] != "pk" || body["secretapikey"] != "sk" {
		http.Error(w, `{"status":"ERROR","message":"Invalid API key."}`, 400)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	f.calls = append(f.calls, path)
	parts := strings.Split(path, "/")
	name := func(zone, sub string) string {
		if sub == "" {
			return zone
		}
		return sub + "." + zone
	}
	key := func(typ, host string) string { return typ + " " + host }
	subOf := func() string {
		if len(parts) > 4 {
			return parts[4]
		}
		return ""
	}
	out := map[string]any{"status": "SUCCESS"}
	switch {
	case path == "ping":
		out["yourIp"] = "203.0.113.7"
	case parts[1] == "retrieveByNameType":
		recs := []map[string]string{}
		host := name(parts[2], subOf())
		for i, c := range f.records[key(parts[3], host)] {
			recs = append(recs, map[string]string{"id": strconv.Itoa(i + 1), "name": host, "type": parts[3], "content": c})
		}
		out["records"] = recs
	case parts[1] == "editByNameType":
		k := key(parts[3], name(parts[2], subOf()))
		if len(f.records[k]) == 0 {
			http.Error(w, `{"status":"ERROR","message":"Edit error"}`, 400)
			return
		}
		for i := range f.records[k] {
			f.records[k][i] = body["content"]
		}
	case parts[1] == "create":
		k := key(body["type"], name(parts[2], body["name"]))
		f.records[k] = append(f.records[k], body["content"])
	case parts[1] == "deleteByNameType":
		delete(f.records, key(parts[3], name(parts[2], subOf())))
	default:
		f.t.Errorf("unexpected path %s", path)
	}
	json.NewEncoder(w).Encode(out)
}

func newFake(t *testing.T) (*fakePorkbun, *Porkbun) {
	f := &fakePorkbun{t: t, records: map[string][]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	return f, &Porkbun{APIKey: "pk", Secret: "sk", BaseURL: srv.URL, IPURL: srv.URL + "/ping"}
}

func TestPorkbunARecordLifecycle(t *testing.T) {
	f, p := newFake(t)
	ctx := context.Background()

	// First update: no record yet → detect IP, create at the apex.
	if err := updatePorkbun(ctx, p, Config{Domain: "talaria.gg"}); err != nil {
		t.Fatal(err)
	}
	if got := f.records["A talaria.gg"]; len(got) != 1 || got[0] != "203.0.113.7" {
		t.Fatalf("apex A = %q", got)
	}
	// Same IP again: nothing written.
	f.calls = nil
	if err := updatePorkbun(ctx, p, Config{Domain: "talaria.gg"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "edit") || strings.Contains(c, "create") {
			t.Fatalf("unchanged IP should not write, but hit %s", c)
		}
	}
	// Pinned LAN IP on a subdomain, then a change → edit, not create.
	if err := updatePorkbun(ctx, p, Config{Domain: "lan.talaria.gg", IP: "192.168.1.20"}); err != nil {
		t.Fatal(err)
	}
	if err := updatePorkbun(ctx, p, Config{Domain: "lan.talaria.gg", IP: "192.168.1.21"}); err != nil {
		t.Fatal(err)
	}
	if got := f.records["A lan.talaria.gg"]; len(got) != 1 || got[0] != "192.168.1.21" {
		t.Fatalf("lan A = %q", got)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "dns/editByNameType/talaria.gg/A/lan") {
		t.Fatalf("expected an edit call, got %v", f.calls)
	}
}

func TestPorkbunReplacesParkingRecords(t *testing.T) {
	f, p := newFake(t)
	// A freshly bought domain carries the registrar's two parking A records.
	f.records["A talaria.gg"] = []string{"207.207.210.107", "207.207.210.229"}
	if err := updatePorkbun(context.Background(), p, Config{Domain: "talaria.gg"}); err != nil {
		t.Fatal(err)
	}
	if got := f.records["A talaria.gg"]; len(got) != 1 || got[0] != "203.0.113.7" {
		t.Fatalf("apex A = %q, want exactly one record with the detected IP", got)
	}
}

func TestPorkbunTXT(t *testing.T) {
	f, p := newFake(t)
	ctx := context.Background()
	if err := p.SetTXT(ctx, "lan.talaria.gg", "challenge-1"); err != nil {
		t.Fatal(err)
	}
	if got := f.records["TXT _acme-challenge.lan.talaria.gg"]; len(got) != 1 || got[0] != "challenge-1" {
		t.Fatalf("TXT = %q", got)
	}
	if err := p.SetTXT(ctx, "lan.talaria.gg", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.records["TXT _acme-challenge.lan.talaria.gg"]; ok {
		t.Fatal("TXT should be cleared")
	}
	// Clearing when nothing exists is fine too.
	if err := p.SetTXT(ctx, "lan.talaria.gg", ""); err != nil {
		t.Fatal(err)
	}
}

func TestPorkbunBadCredentials(t *testing.T) {
	_, p := newFake(t)
	p.Secret = "wrong"
	err := p.SetRecord(context.Background(), "talaria.gg", "A", "1.2.3.4")
	if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("want credential error, got %v", err)
	}
}
