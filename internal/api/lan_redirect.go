package api

import (
	"fmt"
	"html/template"
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
// server's own public IP — and serves a tiny probe page that bounces the
// browser to the lan domain (direct, full speed, valid cert). Routers with
// working loopback never trigger it for working transfers either way; ones
// with broken loopback (bulk transfers stall) are exactly who this is for.
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
		// The probe page sets this cookie when the lan domain is unreachable
		// (e.g. DNS rebind protection) so the user still gets the app.
		if c, _ := r.Cookie("talaria_lan"); c != nil && c.Value == "skip" {
			next.ServeHTTP(w, r)
			return
		}
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !slices.Contains(s.publicIPs(pub), remote) {
			next.ServeHTTP(w, r)
			return
		}
		serveLanProbe(w, lanDomain)
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
}

const lanProbeHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Talaria</title></head>
<body style="font-family:system-ui;background:#0b0d12;color:#a3a9b6;display:flex;height:100vh;align-items:center;justify-content:center;margin:0">
<p>Connecting&hellip;</p>
<script>
(function () {
  var lan = "https://%s";
  var dest = lan + location.pathname + location.search + location.hash;
  var done = false;
  function fallback() {
    if (done) return; done = true;
    document.cookie = "talaria_lan=skip; max-age=600; path=/";
    location.reload();
  }
  var t = setTimeout(fallback, 2500);
  fetch(lan + "/healthz", { mode: "cors" }).then(function (r) {
    if (!r.ok) throw 0;
    if (done) return; done = true; clearTimeout(t);
    location.replace(dest);
  }).catch(fallback);
})();
</script></body></html>`

func serveLanProbe(w http.ResponseWriter, lanDomain string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, lanProbeHTML, template.JSEscapeString(lanDomain))
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
