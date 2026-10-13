package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/gateway"
	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

// validEmoji accepts a short run of non-space characters (an emoji, with
// modifiers / ZWJ sequences) and rejects anything that looks like text.
func validEmoji(s string) bool {
	n := utf8.RuneCountInString(s)
	if n == 0 || n > 16 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// reactionTarget loads the message and checks the caller may see its channel.
func (s *Server) reactionTarget(w http.ResponseWriter, r *http.Request) (*model.Message, *model.Channel, string, bool) {
	me := auth.UserFrom(r.Context())
	m, err := s.Store.MessageByID(r.Context(), r.PathValue("id"))
	if err != nil || m.DeletedAt != 0 {
		notFound(w)
		return nil, nil, "", false
	}
	ch, err := s.Store.ChannelByID(r.Context(), m.ChannelID)
	if err != nil {
		notFound(w)
		return nil, nil, "", false
	}
	if ok, err := s.Store.IsChannelMember(r.Context(), ch, me.ID); err != nil || !ok {
		notFound(w)
		return nil, nil, "", false
	}
	emoji := strings.TrimSpace(r.PathValue("emoji"))
	if !validEmoji(emoji) {
		badRequest(w, "that is not an emoji")
		return nil, nil, "", false
	}
	return m, ch, emoji, true
}

func (s *Server) finishReaction(w http.ResponseWriter, r *http.Request, m *model.Message, ch *model.Channel) {
	by, err := s.Store.ReactionsByMessageIDs(r.Context(), []string{m.ID})
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	reactions := by[m.ID]
	if reactions == nil {
		reactions = []model.Reaction{}
	}
	s.Hub.PublishToChannel(r.Context(), ch, gateway.EvtMessageReaction, map[string]any{
		"channel_id": ch.ID, "message_id": m.ID, "reactions": reactions,
	})
	writeJSON(w, 200, map[string]any{"reactions": reactions})
}

// PUT /api/v1/messages/{id}/reactions/{emoji}
func (s *Server) handleAddReaction(w http.ResponseWriter, r *http.Request) {
	m, ch, emoji, ok := s.reactionTarget(w, r)
	if !ok {
		return
	}
	me := auth.UserFrom(r.Context())
	if err := s.Store.AddReaction(r.Context(), m.ID, me.ID, emoji); err != nil {
		if errors.Is(err, store.ErrTooManyReactions) {
			conflict(w, err.Error())
			return
		}
		serverError(w, s.Log, err)
		return
	}
	s.finishReaction(w, r, m, ch)
}

// DELETE /api/v1/messages/{id}/reactions/{emoji}
func (s *Server) handleRemoveReaction(w http.ResponseWriter, r *http.Request) {
	m, ch, emoji, ok := s.reactionTarget(w, r)
	if !ok {
		return
	}
	me := auth.UserFrom(r.Context())
	if err := s.Store.RemoveReaction(r.Context(), m.ID, me.ID, emoji); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.finishReaction(w, r, m, ch)
}
