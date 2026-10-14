package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/skyleb/talaria/internal/config"
	"github.com/skyleb/talaria/internal/lanaccess"
)

func TestLanRedirectRetiredDomainGoesToMainDomain(t *testing.T) {
	cfg := config.Default()
	cfg.TLS.Domain = "talaria.gg"
	lan := lanaccess.NewRunner(slog.Default())
	lan.Retire("lan.talaria.gg", t.TempDir())
	s := &Server{Log: slog.Default(), Config: cfg, LanAccess: lan}

	served := false
	h := s.lanRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name, method, host string
		wantCode           int
	}{
		{"page load", http.MethodGet, "lan.talaria.gg", http.StatusFound},
		{"with port", http.MethodGet, "LAN.talaria.gg:443", http.StatusFound},
		{"api call keeps its method", http.MethodPost, "lan.talaria.gg", http.StatusTemporaryRedirect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			served = false
			req := httptest.NewRequest(tc.method, "/app/settings?tab=1", nil)
			req.Host = tc.host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if loc := rec.Header().Get("Location"); loc != "https://talaria.gg/app/settings?tab=1" {
				t.Fatalf("Location = %q", loc)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", cc)
			}
			if served {
				t.Fatal("handler ran for a retired-domain request")
			}
		})
	}

	// The main domain itself is untouched.
	served = false
	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.Host = "talaria.gg"
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !served || rec.Code != http.StatusOK {
		t.Fatalf("main domain: served=%v status=%d", served, rec.Code)
	}
}

func TestLanRedirectNoRunnerPassesThrough(t *testing.T) {
	cfg := config.Default()
	cfg.TLS.Domain = "talaria.gg"
	s := &Server{Log: slog.Default(), Config: cfg}
	served := false
	h := s.lanRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served = true }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "lan.talaria.gg"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !served {
		t.Fatal("request dropped with no lan runner configured")
	}
}
