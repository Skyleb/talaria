package api

import "net/http"

// handleGifStatus tells the client whether GIF search is configured and hands
// it the GIPHY key. GIPHY keys are designed to be used from the browser (the
// API terms ask for client-side calls), so the server does not proxy search.
func (s *Server) handleGifStatus(w http.ResponseWriter, r *http.Request) {
	key := s.Config.Gifs.GiphyAPIKey
	writeJSON(w, 200, map[string]any{"enabled": key != "", "provider": "giphy", "key": key})
}
