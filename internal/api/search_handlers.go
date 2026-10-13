package api

import (
	"net/http"
	"strings"

	"github.com/skyle/talaria/internal/auth"
	"github.com/skyle/talaria/internal/model"
	"github.com/skyle/talaria/internal/store"
)

// handleSearch is full-text search over messages the caller can read.
// scope=channel&id=<channel> | scope=group&id=<group> | scope=all (default).
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	q := r.URL.Query()
	fts := store.BuildFTSQuery(q.Get("q"))
	if fts == "" {
		writeJSON(w, 200, map[string]any{"results": []any{}, "has_more": false})
		return
	}
	var channelIDs []string
	switch q.Get("scope") {
	case "channel":
		ch, ok := s.loadChannelAsMember(w, r, q.Get("id"))
		if !ok {
			return
		}
		channelIDs = []string{ch.ID}
	case "group":
		g, _, ok := s.requireGroupRole(w, r, q.Get("id"))
		if !ok {
			return
		}
		chans, err := s.Store.ListChannelsByGroup(r.Context(), g.ID)
		if err != nil {
			serverError(w, s.Log, err)
			return
		}
		for _, c := range chans {
			channelIDs = append(channelIDs, c.ID)
		}
	default:
		var err error
		if channelIDs, err = s.Store.VisibleChannelIDs(r.Context(), me.ID); err != nil {
			serverError(w, s.Log, err)
			return
		}
	}
	hits, more, err := s.Store.SearchMessages(r.Context(), fts, channelIDs, q.Get("before"), 30)
	if err != nil {
		// Most likely a query the FTS parser rejects; treat as no results.
		if strings.Contains(err.Error(), "fts5") {
			writeJSON(w, 200, map[string]any{"results": []any{}, "has_more": false})
			return
		}
		serverError(w, s.Log, err)
		return
	}
	msgs := make([]*model.Message, 0, len(hits))
	for _, h := range hits {
		msgs = append(msgs, h.Message)
	}
	if err := s.hydrateAuthors(r, msgs); err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]any{"results": hits, "has_more": more})
}

// hydrateAuthors attaches public author profiles to messages.
func (s *Server) hydrateAuthors(r *http.Request, msgs []*model.Message) error {
	idSet := map[string]struct{}{}
	for _, m := range msgs {
		idSet[m.AuthorID] = struct{}{}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	users, err := s.Store.PublicUsersByIDs(r.Context(), ids)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if u, ok := users[m.AuthorID]; ok {
			m.Author = &u
		}
	}
	return nil
}

// reverseMessages flips a slice in place (store helpers return oldest-first
// windows; the list API always answers newest-first).
func reverseMessages(msgs []*model.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}
