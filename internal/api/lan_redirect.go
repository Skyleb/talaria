package api

import (
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/skyle/talaria/internal/landns"
)

// lanRedirect intercepts page loads that reach the main domain through the
// router's NAT loopback — identifiable because their source address is the
// server's own public IP — and answers with a plain HTTP 302 to the lan
// domain (direct, full speed, valid cert). An HTTP redirect is followed
// natively by every browser; a JavaScript redirect would trip mobile
// redirect-blockers. Routers with working loopback never need it; ones with
// broken loopback (bulk transfers stall) are exactly who this is for.
func (s *Server) lanRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lanDomain := ""
		if s.LanAccess != nil {
			lanDomain = s.LanAccess.Domain()
		}
		// Only page loads (GET for HTML, no websocket upgrades) on the main
		// domain are candidates.
		if lanDomain == "" || r.Method != http.MethodGet || r.Header.Get("Upgrade") != "" ||
			!strings.Contains(r.Header.Get("Accept"), "text/html") {
			next.ServeHTTP(w, r)
			return
		}
		pub := s.Config.TLS.Domain
		if pub == "" || !strings.EqualFold(stripPort(r.Host), pub) {
			next.ServeHTTP(w, r)
			return
		}
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !slices.Contains(s.publicIPs(pub), remote) {
			// A page load on the main domain from a non-hairpinned source:
			// a genuinely remote visitor, or something unexpected — log the
			// verdict so redirect problems are diagnosable from talaria.log.
			s.Log.Info("lan redirect: pass-through", "remote", remote, "path", r.URL.Path)
			next.ServeHTTP(w, r)
			return
		}
		// Redirecting a client to a domain its network refuses to resolve
		// (DNS rebind protection) would strand it; in that case keep serving
		// over the hairpin instead.
		if !s.lanResolvable(lanDomain, s.LanAccess.LanIP()) {
			s.Log.Warn("lan redirect: lan domain not resolvable here; serving over hairpin", "remote", remote)
			next.ServeHTTP(w, r)
			return
		}
		s.Log.Info("lan redirect: 302", "remote", remote, "path", r.URL.Path)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "https://"+lanDomain+r.URL.RequestURI(), http.StatusFound)
	})
}

// publicIPs resolves (and caches for 5 minutes) the main domain's public
// addresses — a request arriving *from* one of them is a hairpinned LAN
// client. Resolution goes to public resolvers on the wire (landns.ResolveA):
// the OS resolver would honor hosts-file entries or split-horizon DNS on the
// server machine, which are common on exactly the setups that need lan
// access and would hide the domain's true public IP.
func (s *Server) publicIPs(domain string) []string {
	s.pubIPMu.Lock()
	defer s.pubIPMu.Unlock()
	if s.pubIPDomain == domain && time.Since(s.pubIPFetched) < 5*time.Minute {
		return s.pubIPs
	}
	s.pubIPDomain = domain
	s.pubIPFetched = time.Now()
	s.pubIPs = nil
	if ips, err := landns.ResolveA(domain, []string{"1.1.1.1:53", "8.8.8.8:53"}); err == nil {
		for _, ip := range ips {
			s.pubIPs = append(s.pubIPs, ip.String())
		}
	}
	return s.pubIPs
}

type pubIPCache struct {
	pubIPMu     sync.Mutex
	pubIPDomain string
	pubIPs      []string
	pubIPFetched time.Time

	lanResMu      sync.Mutex
	lanResDomain  string
	lanResOK      bool
	lanResFetched time.Time
}

// lanResolvable reports whether the lan domain resolves to the server's LAN
// IP through the OS resolver — which on this machine means the router's DNS,
// the same resolver LAN clients use. Routers with DNS rebind protection
// filter private-IP answers network-wide, so this one check covers clients.
func (s *Server) lanResolvable(lanDomain, wantIP string) bool {
	s.lanResMu.Lock()
	defer s.lanResMu.Unlock()
	if s.lanResDomain == lanDomain && time.Since(s.lanResFetched) < 5*time.Minute {
		return s.lanResOK
	}
	s.lanResDomain = lanDomain
	s.lanResFetched = time.Now()
	s.lanResOK = false
	if ips, err := net.LookupIP(lanDomain); err == nil {
		for _, ip := range ips {
			if ip.String() == wantIP {
				s.lanResOK = true
				break
			}
		}
	}
	return s.lanResOK
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
