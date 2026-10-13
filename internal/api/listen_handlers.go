package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/listen"
)

// Listening-together endpoints: thin wrappers over the listen service. Every
// route requires channel membership; the service enforces session/host rules.

func (s *Server) listenErr(w http.ResponseWriter, err error) {
	var le *listen.Error
	if errors.As(err, &le) {
		writeErr(w, le.Status, "listen", le.Message)
		return
	}
	serverError(w, s.Log, err)
}

func (s *Server) listenRespond(w http.ResponseWriter, st *listen.State, err error) {
	if err != nil {
		s.listenErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"state": st})
}

func (s *Server) handleListenState(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	st, err := s.Listen.State(r.Context(), ch)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenStart(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		PlaylistID string `json:"playlist_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := s.Listen.Start(r.Context(), ch, me.ID, req.PlaylistID)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenJoin(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	st, err := s.Listen.Join(r.Context(), ch, me.ID)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenLeave(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	st, err := s.Listen.Leave(r.Context(), ch, me.ID)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenEnd(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	st, err := s.Listen.End(r.Context(), ch, me.ID)
	s.listenRespond(w, st, err)
}

type positionReq struct {
	PositionMS *int64 `json:"position_ms"`
}

func (s *Server) handleListenPlay(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req positionReq
	if !decode(w, r, &req) {
		return
	}
	st, err := s.Listen.Play(r.Context(), ch, me.ID, req.PositionMS)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenPause(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req positionReq
	if !decode(w, r, &req) {
		return
	}
	st, err := s.Listen.Pause(r.Context(), ch, me.ID, req.PositionMS)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenSeek(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		PositionMS int64 `json:"position_ms"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := s.Listen.Seek(r.Context(), ch, me.ID, req.PositionMS)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenSelect(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		ItemID string `json:"item_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := s.Listen.Select(r.Context(), ch, me.ID, req.ItemID)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenNext(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		FromItemID string `json:"from_item_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := s.Listen.Next(r.Context(), ch, me.ID, req.FromItemID)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenPrevious(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	st, err := s.Listen.Previous(r.Context(), ch, me.ID)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenAddItem(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		URL          string `json:"url"`
		AttachmentID string `json:"attachment_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := s.Listen.AddItem(r.Context(), ch, me.ID, req.URL, req.AttachmentID)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenRemoveItem(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	st, err := s.Listen.RemoveItem(r.Context(), ch, me.ID, r.PathValue("itemId"))
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenItemDuration(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		DurationMS int64 `json:"duration_ms"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.Listen.ReportDuration(r.Context(), ch, me.ID, r.PathValue("itemId"), req.DurationMS); err != nil {
		s.listenErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleListenHost(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	st, err := s.Listen.TransferHost(r.Context(), ch, me.ID, req.UserID)
	s.listenRespond(w, st, err)
}

func (s *Server) handleListenSave(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, err := s.Listen.Save(r.Context(), ch, me.ID, req.Name)
	if err != nil {
		s.listenErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"playlist": p})
}

// handleListenSearch finds YouTube videos for the add-to-queue dialog.
func (s *Server) handleListenSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 200, map[string]any{"results": []any{}})
		return
	}
	if len(q) > 200 {
		q = q[:200]
	}
	results, err := listen.SearchYouTube(r.Context(), q, s.Config.YouTube.APIKey)
	if err != nil {
		s.Log.Warn("youtube search", "err", err)
		writeErr(w, 502, "search", "YouTube search is unavailable right now. You can still paste a link.")
		return
	}
	writeJSON(w, 200, map[string]any{"results": results})
}

func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	lists, err := s.Store.SavedPlaylistsByChannel(r.Context(), ch.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]any{"playlists": lists})
}

func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	p, err := s.Store.PlaylistByID(r.Context(), r.PathValue("id"))
	if err != nil || p.Name == "" {
		notFound(w)
		return
	}
	ch, err := s.Store.ChannelByID(r.Context(), p.ChannelID)
	if err != nil {
		notFound(w)
		return
	}
	member, err := s.Store.IsChannelMember(r.Context(), ch, me.ID)
	if err != nil || !member {
		notFound(w)
		return
	}
	if p.OwnerID != me.ID && !me.IsAdmin {
		forbidden(w, "only whoever saved this playlist can delete it")
		return
	}
	if err := s.Store.DeletePlaylist(r.Context(), p.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
