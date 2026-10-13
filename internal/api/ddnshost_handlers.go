package api

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/skyleb/talaria/internal/ddnshost"
	"github.com/skyleb/talaria/internal/store"
)

// Hosted dynamic DNS: the public endpoints other Talaria installations call
// when this server is their DDNS provider (see internal/ddnshost), plus the
// admin view of claimed names.

func (s *Server) ddnsHostEnabled(w http.ResponseWriter) bool {
	if s.DDNSHost == nil || s.DDNSHost.Zone() == "" {
		writeErr(w, 404, "disabled", ddnshost.ErrDisabled.Error())
		return false
	}
	return true
}

// handleDDNSHostZone lets a prospective client show what a name here looks like.
func (s *Server) handleDDNSHostZone(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if !s.ddnsHostEnabled(w) {
		return
	}
	writeJSON(w, 200, map[string]any{
		"zone":   s.DDNSHost.Zone(),
		"server": s.Config.Server.Name,
		"rules":  ddnshost.ErrBadName.Error(),
	})
}

func (s *Server) handleDDNSHostClaim(w http.ResponseWriter, r *http.Request) {
	if !s.ddnsHostEnabled(w) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	claim, err := s.DDNSHost.Claim(r.Context(), req.Name, remoteIP(r))
	if err != nil {
		writeDDNSHostError(w, s, err)
		return
	}
	writeJSON(w, 201, claim)
}

// ddnsHostAuth resolves the bearer token; replies and returns nil on failure.
func (s *Server) ddnsHostAuth(w http.ResponseWriter, r *http.Request) (name *store.DDNSName, ok bool) {
	if !s.ddnsHostEnabled(w) {
		return nil, false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	n, err := s.DDNSHost.Authenticate(r.Context(), tok)
	if err != nil {
		writeErr(w, 401, "bad_token", err.Error())
		return nil, false
	}
	return n, true
}

func (s *Server) handleDDNSHostUpdate(w http.ResponseWriter, r *http.Request) {
	n, ok := s.ddnsHostAuth(w, r)
	if !ok {
		return
	}
	var req struct {
		Host string `json:"host"`
		IP   string `json:"ip"`
	}
	if !decode(w, r, &req) {
		return
	}
	ip := strings.TrimSpace(req.IP)
	if ip == "" {
		ip = remoteIP(r)
	}
	fqdn, err := s.DDNSHost.Update(r.Context(), n, req.Host, ip)
	if err != nil {
		if errors.Is(err, ddnshost.ErrBadIP) && req.IP == "" {
			badRequest(w, "your address "+ip+" is not IPv4; connect over IPv4 or send an explicit ip")
			return
		}
		writeDDNSHostError(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]string{"host": fqdn, "ip": ip})
}

func (s *Server) handleDDNSHostTXT(w http.ResponseWriter, r *http.Request) {
	n, ok := s.ddnsHostAuth(w, r)
	if !ok {
		return
	}
	var req struct {
		Host string `json:"host"`
		TXT  string `json:"txt"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.TXT) > 255 {
		badRequest(w, "txt is too long")
		return
	}
	if err := s.DDNSHost.SetTXT(r.Context(), n, req.Host, req.TXT); err != nil {
		writeDDNSHostError(w, s, err)
		return
	}
	w.WriteHeader(204)
}

// handleDDNSHostRelease lets a client give its name back.
func (s *Server) handleDDNSHostRelease(w http.ResponseWriter, r *http.Request) {
	n, ok := s.ddnsHostAuth(w, r)
	if !ok {
		return
	}
	if err := s.DDNSHost.Release(r.Context(), n.Name); err != nil {
		writeDDNSHostError(w, s, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) handleAdminDDNSHostList(w http.ResponseWriter, r *http.Request) {
	if s.DDNSHost == nil {
		writeJSON(w, 200, map[string]any{"names": []any{}, "zone": ""})
		return
	}
	names, err := s.DDNSHost.List(r.Context())
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if names == nil {
		names = []*store.DDNSName{}
	}
	writeJSON(w, 200, map[string]any{"names": names, "zone": s.DDNSHost.Zone()})
}

func (s *Server) handleAdminDDNSHostDelete(w http.ResponseWriter, r *http.Request) {
	if !s.ddnsHostEnabled(w) {
		return
	}
	if err := s.DDNSHost.Release(r.Context(), r.PathValue("name")); err != nil {
		writeDDNSHostError(w, s, err)
		return
	}
	w.WriteHeader(204)
}

func writeDDNSHostError(w http.ResponseWriter, s *Server, err error) {
	var pe *ddnshost.ProviderError
	switch {
	case errors.As(err, &pe):
		s.Log.Error("hosted ddns provider call failed", "err", pe.Err)
		writeErr(w, 502, "provider", ddnshost.FormatClaimError(err))
	case errors.Is(err, ddnshost.ErrTaken):
		conflict(w, err.Error())
	case errors.Is(err, ddnshost.ErrRateLimited):
		writeErr(w, 429, "rate_limited", err.Error())
	case errors.Is(err, ddnshost.ErrFull):
		writeErr(w, 503, "full", err.Error())
	case errors.Is(err, ddnshost.ErrDisabled):
		writeErr(w, 404, "disabled", err.Error())
	case errors.Is(err, ddnshost.ErrBadName), errors.Is(err, ddnshost.ErrReserved),
		errors.Is(err, ddnshost.ErrForeignHost), errors.Is(err, ddnshost.ErrBadIP):
		badRequest(w, err.Error())
	default:
		serverError(w, s.Log, err)
	}
}

// remoteIP is the caller's address without the port. Talaria terminates
// its own TLS, so RemoteAddr is the real peer.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
