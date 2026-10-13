package api

import (
	"net/http"
	"strings"

	"github.com/skyleb/talaria/internal/config"
	"github.com/skyleb/talaria/internal/ddns"
	"github.com/skyleb/talaria/internal/ddnshost"
)

// validateDDNSHost checks the hosted-DDNS section of an admin config save.
func validateDDNSHost(p *configPayload) string {
	zone := strings.TrimSpace(p.DDNSHost.Zone)
	if zone == "" {
		return "hosted DDNS needs a zone: the domain names are issued under (e.g. talaria.gg gives alice.talaria.gg)"
	}
	if _, _, err := ddns.SplitZone(zone); err != nil {
		return "hosted DDNS zone: " + err.Error()
	}
	key, secret := strings.TrimSpace(p.DDNSHost.APIKey), strings.TrimSpace(p.DDNSHost.Secret)
	if key == "" && secret == "" && p.DDNS.Provider == "porkbun" {
		key, secret = p.DDNS.Token, p.DDNS.Secret
	}
	if key == "" || secret == "" {
		return "hosted DDNS publishes records through Porkbun: set the API key pair here, or choose Porkbun under Dynamic DNS"
	}
	if p.DDNSHost.MaxNames < 0 {
		return "hosted DDNS name limit cannot be negative"
	}
	return ""
}

// DDNSHostConfig builds the service config from the app config: Porkbun
// keys fall back to the [ddns] ones, and this server's own hostnames inside
// the zone are reserved so nobody can claim them.
func DDNSHostConfig(c *config.Config) ddnshost.Config {
	key, secret := c.DDNSHost.APIKey, c.DDNSHost.Secret
	if key == "" && secret == "" && c.DDNS.Provider == "porkbun" {
		key, secret = c.DDNS.Token, c.DDNS.Secret
	}
	cfg := ddnshost.Config{
		Zone:     c.DDNSHost.Zone,
		MaxNames: c.DDNSHost.MaxNames,
		Reserved: ddnshost.ReservedFromHostnames(c.DDNSHost.Zone, c.TLS.Domain, c.DDNS.Domain, c.LanAccess.Domain),
	}
	if key != "" && secret != "" {
		cfg.Provider = &ddns.Porkbun{APIKey: key, Secret: secret}
	}
	return cfg
}

func ddnsHostConfig(c *config.Config) ddnshost.Config { return DDNSHostConfig(c) }

// handleAdminClaimName claims a name at a hosting Talaria server on the
// admin's behalf (server-side, so the browser needs no cross-origin access)
// and returns the token for the settings form to store.
func (s *Server) handleAdminClaimName(w http.ResponseWriter, r *http.Request) {
	s.claimName(w, r)
}

// handleSetupClaimName is the same for the first-run wizard, before any
// admin account exists.
func (s *Server) handleSetupClaimName(w http.ResponseWriter, r *http.Request) {
	if s.setupComplete(r) {
		forbidden(w, "setup already complete")
		return
	}
	s.claimName(w, r)
}

func (s *Server) claimName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	host := &ddns.Talaria{URL: ddns.NormalizeHostURL(req.URL)}
	if strings.TrimSpace(req.Name) == "" {
		// Just asking what the host offers.
		info, err := host.Zone(r.Context())
		if err != nil {
			writeErr(w, 502, "host", err.Error())
			return
		}
		writeJSON(w, 200, info)
		return
	}
	res, err := host.Claim(r.Context(), req.Name)
	if err != nil {
		writeErr(w, 502, "host", err.Error())
		return
	}
	writeJSON(w, 201, res)
}
