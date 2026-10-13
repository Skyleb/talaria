// Package api implements the REST surface and static/file serving.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/skyle/talaria/internal/auth"
	"github.com/skyle/talaria/internal/blob"
	"github.com/skyle/talaria/internal/config"
	"github.com/skyle/talaria/internal/ddns"
	"github.com/skyle/talaria/internal/gateway"
	"github.com/skyle/talaria/internal/lanaccess"
	"github.com/skyle/talaria/internal/landns"
	"github.com/skyle/talaria/internal/store"
)

type Server struct {
	Store   *store.Store
	Hub     *gateway.Hub
	Auth    *auth.Manager
	Blobs   *blob.Store
	Config  *config.Config
	Log     *slog.Logger
	Version string
	// DDNS lets the admin config endpoint hot-apply dynamic-DNS changes.
	DDNS *ddns.Runner
	// LanDNS lets the admin config endpoint hot-apply LAN DNS changes.
	LanDNS *landns.Runner
	// LanAccess serves the lan domain (cert + redirect target) for devices
	// stuck behind broken router NAT loopback.
	LanAccess *lanaccess.Runner

	// cfgMu serializes admin edits to Config.
	cfgMu sync.Mutex
	// pubIPCache backs hairpin detection in lanRedirect.
	pubIPCache
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: msg}})
}

func badRequest(w http.ResponseWriter, msg string)  { writeErr(w, 400, "bad_request", msg) }
func unauthorized(w http.ResponseWriter)            { writeErr(w, 401, "unauthorized", "not signed in") }
func forbidden(w http.ResponseWriter, msg string)   { writeErr(w, 403, "forbidden", msg) }
func notFound(w http.ResponseWriter)                { writeErr(w, 404, "not_found", "not found") }
func conflict(w http.ResponseWriter, msg string)    { writeErr(w, 409, "conflict", msg) }
func serverError(w http.ResponseWriter, log *slog.Logger, err error) {
	log.Error("internal error", "err", err)
	writeErr(w, 500, "internal", "internal server error")
}

// decode parses a JSON body with a size cap.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}
