package api

import (
	"fmt"
	"net"
	"net/http"
)

// BaseURL is one address the server believes it is reachable at.
type BaseURL struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// lanIP finds the outbound LAN address (no packets are actually sent).
func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

// handleServerInfo reports shareable base URLs, most widely reachable first:
// DDNS domain (if configured), then LAN address. The client offers these as
// choices when generating invite links, instead of whatever origin the admin
// happens to be browsing from.
func (s *Server) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	name, _ := s.Store.GetSetting(r.Context(), "server_name")
	if name == "" {
		name = s.Config.Server.Name
	}

	var urls []BaseURL
	if domain := s.Config.TLS.Domain; domain != "" && s.Config.TLS.Mode != "off" {
		u := "https://" + domain
		if p := s.Config.TLS.HTTPSPort; p != 443 {
			u = fmt.Sprintf("%s:%d", u, p)
		}
		urls = append(urls, BaseURL{Label: "Internet (DDNS)", URL: u})
	}
	if ip := lanIP(); ip != "" {
		u := "http://" + ip
		if p := s.Config.Server.HTTPPort; p != 80 {
			u = fmt.Sprintf("%s:%d", u, p)
		}
		urls = append(urls, BaseURL{Label: "Local network", URL: u})
	}

	writeJSON(w, 200, map[string]any{
		"server_name": name,
		"version":     s.Version,
		"urls":        urls,
	})
}
