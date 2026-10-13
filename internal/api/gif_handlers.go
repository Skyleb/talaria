package api

import (
	"net/http"
	"strings"

	"github.com/skyleb/talaria/internal/gifs"
)

// handleGifStatus tells the client whether GIF search is configured, so the
// picker can explain what is missing instead of failing.
func (s *Server) handleGifStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"enabled": s.Config.Gifs.TenorAPIKey != ""})
}

// handleGifSearch proxies Tenor so the key never reaches browsers. An empty
// query returns trending GIFs.
func (s *Server) handleGifSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 120 {
		q = q[:120]
	}
	results, err := gifs.Search(r.Context(), q, s.Config.Gifs.TenorAPIKey)
	if err != nil {
		s.Log.Warn("gif search", "err", err)
		writeErr(w, 502, "gifs", "GIF search is unavailable right now.")
		return
	}
	writeJSON(w, 200, map[string]any{"results": results})
}
