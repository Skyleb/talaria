package api

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/skyle/talaria/internal/auth"
	"github.com/skyle/talaria/internal/gateway"
	"github.com/skyle/talaria/internal/model"
)

func nowMs() int64 { return time.Now().UnixMilli() }

const maxMessageLen = 4000

// loadChannelAsMember 404s unless the caller is a member of the channel.
func (s *Server) loadChannelAsMember(w http.ResponseWriter, r *http.Request, channelID string) (*model.Channel, bool) {
	me := auth.UserFrom(r.Context())
	ch, err := s.Store.ChannelByID(r.Context(), channelID)
	if err != nil {
		notFound(w)
		return nil, false
	}
	ok, err := s.Store.IsChannelMember(r.Context(), ch, me.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return nil, false
	}
	if !ok {
		notFound(w)
		return nil, false
	}
	return ch, true
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	before := r.URL.Query().Get("before")
	limit := 50
	msgs, err := s.Store.ListMessages(r.Context(), ch.ID, before, limit)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	// Hydrate authors (and reply targets) in one query.
	idSet := map[string]struct{}{}
	var replyIDs []string
	for _, m := range msgs {
		idSet[m.AuthorID] = struct{}{}
		if m.ReplyToID != "" {
			replyIDs = append(replyIDs, m.ReplyToID)
		}
	}
	replies := map[string]*model.Message{}
	for _, rid := range replyIDs {
		if _, done := replies[rid]; done {
			continue
		}
		if rm, err := s.Store.MessageByID(r.Context(), rid); err == nil {
			replies[rid] = rm
			idSet[rm.AuthorID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	users, err := s.Store.PublicUsersByIDs(r.Context(), ids)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	for _, m := range msgs {
		if u, ok := users[m.AuthorID]; ok {
			uc := u
			m.Author = &uc
		}
		if rm := replies[m.ReplyToID]; rm != nil {
			cp := *rm
			if u, ok := users[rm.AuthorID]; ok {
				uc := u
				cp.Author = &uc
			}
			m.ReplyTo = &cp
		}
	}
	writeJSON(w, 200, map[string]any{"messages": msgs, "has_more": len(msgs) == limit})
}

func (s *Server) handleCreateMessage(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		Content       string   `json:"content"`
		ReplyToID     string   `json:"reply_to_id"`
		AttachmentIDs []string `json:"attachment_ids"`
		Nonce         string   `json:"nonce"`
	}
	if !decode(w, r, &req) {
		return
	}
	content := strings.TrimRight(req.Content, " \t\n")
	if utf8.RuneCountInString(content) > maxMessageLen {
		badRequest(w, "message too long (max 4000 characters)")
		return
	}
	if content == "" && len(req.AttachmentIDs) == 0 {
		badRequest(w, "message needs text or an attachment")
		return
	}
	if len(req.AttachmentIDs) > 10 {
		badRequest(w, "at most 10 attachments per message")
		return
	}
	if req.ReplyToID != "" {
		rm, err := s.Store.MessageByID(r.Context(), req.ReplyToID)
		if err != nil || rm.ChannelID != ch.ID {
			badRequest(w, "reply target not found in this channel")
			return
		}
	}
	m := &model.Message{
		ChannelID: ch.ID, AuthorID: me.ID, Kind: model.MessageDefault,
		Content: content, ReplyToID: req.ReplyToID,
	}
	if err := s.Store.CreateMessage(r.Context(), m, req.AttachmentIDs); err != nil {
		badRequest(w, "one or more attachments are invalid")
		return
	}
	full, err := s.Store.MessageByID(r.Context(), m.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	pub := me.Public()
	full.Author = &pub
	full.Nonce = req.Nonce
	if full.ReplyToID != "" {
		if rm, err := s.Store.MessageByID(r.Context(), full.ReplyToID); err == nil {
			if users, err := s.Store.PublicUsersByIDs(r.Context(), []string{rm.AuthorID}); err == nil {
				if u, ok := users[rm.AuthorID]; ok {
					rm.Author = &u
				}
			}
			full.ReplyTo = rm
		}
	}
	s.Hub.PublishToChannel(r.Context(), ch, gateway.EvtMessageCreate, map[string]any{"message": full})
	writeJSON(w, 201, map[string]any{"message": full})
}

func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	m, err := s.Store.MessageByID(r.Context(), r.PathValue("id"))
	if err != nil || m.DeletedAt != 0 {
		notFound(w)
		return
	}
	if m.AuthorID != me.ID {
		forbidden(w, "only the author can edit")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if !decode(w, r, &req) {
		return
	}
	content := strings.TrimRight(req.Content, " \t\n")
	if content == "" || utf8.RuneCountInString(content) > maxMessageLen {
		badRequest(w, "content must be 1-4000 characters")
		return
	}
	if err := s.Store.UpdateMessageContent(r.Context(), m.ID, content); err != nil {
		serverError(w, s.Log, err)
		return
	}
	ch, err := s.Store.ChannelByID(r.Context(), m.ChannelID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	updated, _ := s.Store.MessageByID(r.Context(), m.ID)
	pub := me.Public()
	updated.Author = &pub
	s.Hub.PublishToChannel(r.Context(), ch, gateway.EvtMessageUpdate, map[string]any{"message": updated})
	writeJSON(w, 200, map[string]any{"message": updated})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	m, err := s.Store.MessageByID(r.Context(), r.PathValue("id"))
	if err != nil || m.DeletedAt != 0 {
		notFound(w)
		return
	}
	ch, err := s.Store.ChannelByID(r.Context(), m.ChannelID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	allowed := m.AuthorID == me.ID
	if !allowed && ch.Kind != model.ChannelDM {
		if mem, err := s.Store.GroupMember(r.Context(), ch.GroupID, me.ID); err == nil {
			allowed = mem.Role == model.RoleOwner || mem.Role == model.RoleAdmin
		}
	}
	if !allowed {
		forbidden(w, "you cannot delete this message")
		return
	}
	if err := s.Store.SoftDeleteMessage(r.Context(), m.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToChannel(r.Context(), ch, gateway.EvtMessageDelete,
		map[string]any{"message_id": m.ID, "channel_id": ch.ID})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleReadUpdate(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	ch, ok := s.loadChannelAsMember(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		LastReadMessageID string `json:"last_read_message_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.Store.UpsertReadState(r.Context(), me.ID, ch.ID, req.LastReadMessageID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToUsers([]string{me.ID}, gateway.EvtReadUpdate, map[string]any{
		"channel_id": ch.ID, "last_read_message_id": req.LastReadMessageID,
	})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
