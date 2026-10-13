package api

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/skyle/talaria/internal/ddns"
	"github.com/skyle/talaria/internal/lanaccess"
	"github.com/skyle/talaria/internal/landns"
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
	p.DDNS.URL = c.DDNS.URL
	p.DDNS.IntervalMinutes = c.DDNS.IntervalMinutes
	p.LanAccess.Enabled = c.LanAccess.Enabled
	p.LanAccess.Domain = c.LanAccess.Domain
	p.YouTube.APIKey = c.YouTube.APIKey
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
	case !slices.Contains([]string{"duckdns", "custom"}, p.DDNS.Provider):
		return "DDNS provider must be duckdns or custom"
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
		switch {
		case domain == "":
			return "DDNS needs a domain (set one here or under HTTPS)"
		case p.DDNS.Provider == "duckdns" && p.DDNS.Token == "":
			return "DuckDNS needs the token shown at duckdns.org"
		case p.DDNS.Provider == "custom" && p.DDNS.URL == "":
			return "custom DDNS needs an update URL"
		}
	}
	if p.LanAccess.Enabled {
		domain := strings.TrimSpace(p.LanAccess.Domain)
		switch {
		case domain == "":
			return "home network access needs its own domain (create a second free DuckDNS subdomain)"
		case strings.EqualFold(domain, p.TLS.Domain):
			return "the home-access domain must be different from the main domain"
		case p.TLS.Domain == "" || (p.TLS.Mode != "auto" && p.TLS.Mode != "acme"):
			return "home network access requires HTTPS with a domain (Let's Encrypt)"
		case p.DDNS.Token == "":
			return "home network access needs the DuckDNS token (set under Dynamic DNS)"
		}
	}
	return ""
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
	c.DDNS.URL = p.DDNS.URL
	c.DDNS.IntervalMinutes = p.DDNS.IntervalMinutes
	c.LanAccess.Enabled = p.LanAccess.Enabled
	c.LanAccess.Domain = strings.TrimSpace(p.LanAccess.Domain)
	c.YouTube.APIKey = strings.TrimSpace(p.YouTube.APIKey)
	lanToken := c.LanAccess.Token
	if lanToken == "" {
		lanToken = c.DDNS.Token
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
			s.DDNS.Set(ddns.Config{
				Provider: p.DDNS.Provider,
				Domain:   domain,
				Token:    p.DDNS.Token,
				URL:      p.DDNS.URL,
				Interval: time.Duration(p.DDNS.IntervalMinutes) * time.Minute,
			})
		} else {
			s.DDNS.Stop()
		}
	}
	if s.LanAccess != nil {
		if p.LanAccess.Enabled {
			if err := s.LanAccess.Set(lanaccess.Config{
				Domain: p.LanAccess.Domain, Token: lanToken,
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
