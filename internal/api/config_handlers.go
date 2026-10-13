package api

import (
	"context"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/skyleb/talaria/internal/ddns"
	"github.com/skyleb/talaria/internal/lanaccess"
	"github.com/skyleb/talaria/internal/landns"
)

// configPayload is the admin-editable slice of talaria.toml, exposed over
// the API so the whole server can be configured from the web UI.
type configPayload struct {
	Server struct {
		Name        string `json:"name"`
		HTTPPort    int    `json:"http_port"`
		MaxUploadMB int64  `json:"max_upload_mb"`
		OpenBrowser bool   `json:"open_browser"`
	} `json:"server"`
	TLS struct {
		Mode      string `json:"mode"`
		Domain    string `json:"domain"`
		ACMEEmail string `json:"acme_email"`
		HTTPSPort int    `json:"https_port"`
	} `json:"tls"`
	DDNS struct {
		Enabled         bool   `json:"enabled"`
		Provider        string `json:"provider"`
		Domain          string `json:"domain"`
		Token           string `json:"token"`
		Secret          string `json:"secret"`
		URL             string `json:"url"`
		IntervalMinutes int    `json:"interval_minutes"`
	} `json:"ddns"`
	LanAccess struct {
		Enabled bool   `json:"enabled"`
		Domain  string `json:"domain"`
	} `json:"lan_access"`
	YouTube struct {
		APIKey string `json:"api_key"`
	} `json:"youtube"`
	Gifs struct {
		GiphyAPIKey string `json:"giphy_api_key"`
	} `json:"gifs"`
	WebRTC struct {
		UDPPort  int    `json:"udp_port"`
		PublicIP string `json:"public_ip"`
	} `json:"webrtc"`
}

func (s *Server) configSnapshot() configPayload {
	c := s.Config
	var p configPayload
	p.Server.Name = c.Server.Name
	p.Server.HTTPPort = c.Server.HTTPPort
	p.Server.MaxUploadMB = c.Server.MaxUploadMB
	p.Server.OpenBrowser = c.Server.OpenBrowser
	p.TLS.Mode = c.TLS.Mode
	p.TLS.Domain = c.TLS.Domain
	p.TLS.ACMEEmail = c.TLS.ACMEEmail
	p.TLS.HTTPSPort = c.TLS.HTTPSPort
	p.DDNS.Enabled = c.DDNS.Enabled
	p.DDNS.Provider = c.DDNS.Provider
	p.DDNS.Domain = c.DDNS.Domain
	p.DDNS.Token = c.DDNS.Token
	p.DDNS.Secret = c.DDNS.Secret
	p.DDNS.URL = c.DDNS.URL
	p.DDNS.IntervalMinutes = c.DDNS.IntervalMinutes
	p.LanAccess.Enabled = c.LanAccess.Enabled
	p.LanAccess.Domain = c.LanAccess.Domain
	p.YouTube.APIKey = c.YouTube.APIKey
	p.Gifs.GiphyAPIKey = c.Gifs.GiphyAPIKey
	p.WebRTC.UDPPort = c.WebRTC.UDPPort
	p.WebRTC.PublicIP = c.WebRTC.PublicIP
	return p
}

func validateConfig(p *configPayload) string {
	validPort := func(n int) bool { return n >= 1 && n <= 65535 }
	switch {
	case p.Server.Name == "" || len(p.Server.Name) > 64:
		return "server name must be 1-64 characters"
	case !validPort(p.Server.HTTPPort):
		return "HTTP port must be 1-65535"
	case p.Server.MaxUploadMB < 1 || p.Server.MaxUploadMB > 10240:
		return "max upload must be 1-10240 MB"
	case !slices.Contains([]string{"auto", "acme", "selfsigned", "off"}, p.TLS.Mode):
		return "TLS mode must be auto, acme, selfsigned, or off"
	case !validPort(p.TLS.HTTPSPort):
		return "HTTPS port must be 1-65535"
	case p.TLS.Mode == "acme" && p.TLS.Domain == "":
		return "TLS mode \"acme\" requires a domain"
	case !slices.Contains([]string{"duckdns", "porkbun", "custom"}, p.DDNS.Provider):
		return "DDNS provider must be duckdns, porkbun or custom"
	case !validPort(p.WebRTC.UDPPort):
		return "call media UDP port must be 1-65535"
	}
	if p.DDNS.IntervalMinutes < 1 {
		p.DDNS.IntervalMinutes = 5
	}
	if p.DDNS.Enabled {
		domain := p.DDNS.Domain
		if domain == "" {
			domain = p.TLS.Domain
		}
		if domain == "" {
			return "DDNS needs a domain (set one here or under HTTPS)"
		}
		if err := ddns.Validate(ddnsConfig(p, domain)); err != nil {
			return err.Error()
		}
		if p.DDNS.Provider == "duckdns" && !isDuckDNS(domain) {
			return "DuckDNS can only update *.duckdns.org names; " + domain + " needs the Porkbun provider (or set the DDNS domain to your DuckDNS name)"
		}
	}
	if p.LanAccess.Enabled {
		domain := strings.TrimSpace(p.LanAccess.Domain)
		switch {
		case domain == "":
			return "home network access needs its own domain (a second DuckDNS subdomain, or a subdomain of your own domain)"
		case strings.EqualFold(domain, p.TLS.Domain):
			return "the home-access domain must be different from the main domain"
		case p.TLS.Domain == "" || (p.TLS.Mode != "auto" && p.TLS.Mode != "acme"):
			return "home network access requires HTTPS with a domain (Let's Encrypt)"
		case !ddns.SupportsDNS01(p.DDNS.Provider):
			return "home network access needs a DNS provider that can publish TXT records: choose DuckDNS or Porkbun under Dynamic DNS"
		case p.DDNS.Provider == "duckdns" && !isDuckDNS(domain):
			return "the home-access domain " + domain + " is not a DuckDNS name; switch the Dynamic DNS provider to Porkbun first"
		}
		if _, err := ddns.NewTXTSetter(p.DDNS.Provider, p.DDNS.Token, p.DDNS.Secret); err != nil {
			return "home network access " + err.Error() + " (set under Dynamic DNS)"
		}
	}
	return ""
}

func isDuckDNS(domain string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(domain)), ".duckdns.org")
}

// checkDomainSwitch guards against locking yourself out: once the HTTPS
// domain changes, the old name stops being served and the new one only works
// if Let's Encrypt can reach this server through it. So the new domain must
// already resolve to the same address the old (working) one does. Returns a
// user-facing message, or "" when the switch is fine or can't be judged.
func checkDomainSwitch(oldDomain, newDomain string) string {
	oldDomain = strings.ToLower(strings.TrimSpace(oldDomain))
	newDomain = strings.ToLower(strings.TrimSpace(newDomain))
	if oldDomain == "" || newDomain == "" || oldDomain == newDomain {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	oldIPs, err := net.DefaultResolver.LookupHost(ctx, oldDomain)
	if err != nil || len(oldIPs) == 0 {
		return "" // nothing to compare against
	}
	newIPs, err := net.DefaultResolver.LookupHost(ctx, newDomain)
	if err != nil || len(newIPs) == 0 {
		return newDomain + " does not resolve yet. Keep the current HTTPS domain, enable Dynamic DNS for " +
			newDomain + " (set it as the DDNS domain), wait for the record to appear, then switch."
	}
	for _, ip := range newIPs {
		if slices.Contains(oldIPs, ip) {
			return ""
		}
	}
	return newDomain + " currently points at " + strings.Join(newIPs, ", ") + ", not at this server (" +
		strings.Join(oldIPs, ", ") + "). Keep the current HTTPS domain, set " + newDomain +
		" as the DDNS domain so Talaria updates its record, wait a minute, then switch."
}

// ddnsConfig builds the updater config for the payload's DDNS section.
func ddnsConfig(p *configPayload, domain string) ddns.Config {
	return ddns.Config{
		Provider: p.DDNS.Provider,
		Domain:   domain,
		Token:    p.DDNS.Token,
		Secret:   p.DDNS.Secret,
		URL:      p.DDNS.URL,
		Interval: time.Duration(p.DDNS.IntervalMinutes) * time.Minute,
	}
}

// restartRequired lists the human-readable settings that changed but only
// take effect on the next launch (listeners and the SFU bind at startup).
func restartRequired(old, new configPayload) []string {
	var out []string
	add := func(changed bool, label string) {
		if changed {
			out = append(out, label)
		}
	}
	add(old.Server.HTTPPort != new.Server.HTTPPort, "HTTP port")
	add(old.TLS.Mode != new.TLS.Mode, "TLS mode")
	add(old.TLS.Domain != new.TLS.Domain, "HTTPS domain")
	add(old.TLS.ACMEEmail != new.TLS.ACMEEmail, "ACME email")
	add(old.TLS.HTTPSPort != new.TLS.HTTPSPort, "HTTPS port")
	add(old.WebRTC.UDPPort != new.WebRTC.UDPPort, "call media UDP port")
	add(old.WebRTC.PublicIP != new.WebRTC.PublicIP, "public IP override")
	return out
}

func (s *Server) handleAdminGetConfig(w http.ResponseWriter, r *http.Request) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	writeJSON(w, 200, map[string]any{
		"config":           s.configSnapshot(),
		"config_path":      s.Config.Path(),
		"lan_ip":           lanIPString(),
		"lanaccess_status": s.lanAccessStatus(),
	})
}

// lanAccessStatus reports the lan-access runner state for the admin UI.
func (s *Server) lanAccessStatus() map[string]any {
	if s.LanAccess == nil {
		return map[string]any{"active": false}
	}
	return s.LanAccess.Status()
}

func lanIPString() string {
	if ip := landns.DetectLanIP(); ip != nil {
		return ip.String()
	}
	return ""
}

func (s *Server) handleAdminPutConfig(w http.ResponseWriter, r *http.Request) {
	var p configPayload
	if !decode(w, r, &p) {
		return
	}
	if msg := validateConfig(&p); msg != "" {
		badRequest(w, msg)
		return
	}
	s.cfgMu.Lock()
	currentDomain := s.Config.TLS.Domain
	s.cfgMu.Unlock()
	if p.TLS.Mode == "auto" || p.TLS.Mode == "acme" {
		if msg := checkDomainSwitch(currentDomain, p.TLS.Domain); msg != "" {
			badRequest(w, msg)
			return
		}
	}

	s.cfgMu.Lock()
	old := s.configSnapshot()
	c := s.Config
	c.Server.Name = p.Server.Name
	c.Server.HTTPPort = p.Server.HTTPPort
	c.Server.MaxUploadMB = p.Server.MaxUploadMB
	c.Server.OpenBrowser = p.Server.OpenBrowser
	c.TLS.Mode = p.TLS.Mode
	c.TLS.Domain = p.TLS.Domain
	c.TLS.ACMEEmail = p.TLS.ACMEEmail
	c.TLS.HTTPSPort = p.TLS.HTTPSPort
	c.DDNS.Enabled = p.DDNS.Enabled
	c.DDNS.Provider = p.DDNS.Provider
	c.DDNS.Domain = p.DDNS.Domain
	c.DDNS.Token = p.DDNS.Token
	c.DDNS.Secret = p.DDNS.Secret
	c.DDNS.URL = p.DDNS.URL
	c.DDNS.IntervalMinutes = p.DDNS.IntervalMinutes
	c.LanAccess.Enabled = p.LanAccess.Enabled
	c.LanAccess.Domain = strings.TrimSpace(p.LanAccess.Domain)
	c.YouTube.APIKey = strings.TrimSpace(p.YouTube.APIKey)
	c.Gifs.GiphyAPIKey = strings.TrimSpace(p.Gifs.GiphyAPIKey)
	lanToken := c.LanAccess.Token
	if lanToken == "" {
		lanToken = c.DDNS.Token
	}
	lanSecret := c.LanAccess.Secret
	if lanSecret == "" {
		lanSecret = c.DDNS.Secret
	}
	lanIP := c.LanAccess.LanIP
	dataDir := c.ResolvedDataDir
	c.WebRTC.UDPPort = p.WebRTC.UDPPort
	c.WebRTC.PublicIP = p.WebRTC.PublicIP
	err := c.Save()
	s.cfgMu.Unlock()
	if err != nil {
		serverError(w, s.Log, err)
		return
	}

	// DDNS and LAN DNS hot-apply; everything else that changed is reported
	// back as restart-required.
	warnings := []string{}
	if s.DDNS != nil {
		if p.DDNS.Enabled {
			domain := p.DDNS.Domain
			if domain == "" {
				domain = p.TLS.Domain
			}
			s.DDNS.Set(ddnsConfig(&p, domain))
		} else {
			s.DDNS.Stop()
		}
	}
	if s.LanAccess != nil {
		if p.LanAccess.Enabled {
			if err := s.LanAccess.Set(lanaccess.Config{
				Domain: p.LanAccess.Domain, Provider: p.DDNS.Provider,
				Token: lanToken, Secret: lanSecret,
				LanIP: lanIP, DataDir: dataDir,
			}); err != nil {
				warnings = append(warnings, "Home network access could not start: "+err.Error())
			}
		} else {
			s.LanAccess.Stop()
		}
	}

	writeJSON(w, 200, map[string]any{
		"config":           p,
		"restart_required": restartRequired(old, p),
		"warnings":         warnings,
		"lanaccess_status": s.lanAccessStatus(),
	})
}
