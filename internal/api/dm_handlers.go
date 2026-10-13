package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/gateway"
	"github.com/skyleb/talaria/internal/model"
)

// Multi-person chats ("group DMs"): add friends to a conversation and name
// it, without the ceremony of creating a group. Adding someone to a 1:1 chat
// starts a NEW chat (the private pair history stays private); adding to an
// existing multi-person chat joins them in place.

const maxDMParticipants = 10

// attachDMParticipants fills ch.Participants from the database.
func (s *Server) attachDMParticipants(ctx context.Context, ch *model.Channel) {
	ids, _ := s.Store.DMParticipantIDs(ctx, ch.ID)
	users, _ := s.Store.PublicUsersByIDs(ctx, ids)
	ch.Participants = ch.Participants[:0]
	for _, id := range ids {
		if u, ok := users[id]; ok {
			ch.Participants = append(ch.Participants, u)
		}
	}
}

// postSystemMessage records a system line ("X added Y") in the channel and
// fans it out like any other message.
func (s *Server) postSystemMessage(ctx context.Context, ch *model.Channel, actorID, content string) {
	m := &model.Message{ChannelID: ch.ID, AuthorID: actorID, Kind: model.MessageSystem, Content: content}
	if err := s.Store.CreateMessage(ctx, m, nil); err != nil {
		s.Log.Error("system message", "err", err)
		return
	}
	full, err := s.Store.MessageByID(ctx, m.ID)
	if err != nil {
		full = m
	}
	s.Hub.PublishToChannel(ctx, ch, gateway.EvtMessageCreate, map[string]any{"message": full})
}

// requireDM loads the channel and verifies it is a DM the caller belongs to.
func (s *Server) requireDM(w http.ResponseWriter, r *http.Request) (*model.Channel, *model.User, bool) {
	me := auth.UserFrom(r.Context())
	ch, err := s.Store.ChannelByID(r.Context(), r.PathValue("id"))
	if err != nil || ch.Kind != model.ChannelDM {
		notFound(w)
		return nil, nil, false
	}
	member, err := s.Store.IsChannelMember(r.Context(), ch, me.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return nil, nil, false
	}
	if !member {
		notFound(w)
		return nil, nil, false
	}
	return ch, me, true
}

func (s *Server) handleAddDMParticipant(w http.ResponseWriter, r *http.Request) {
	ch, me, ok := s.requireDM(w, r)
	if !ok {
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	target, err := s.Store.UserByID(r.Context(), req.UserID)
	if err != nil {
		notFound(w)
		return
	}
	if target.ID == me.ID {
		badRequest(w, "you're already here")
		return
	}
	friends, err := s.Store.AreFriends(r.Context(), me.ID, target.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if !friends {
		forbidden(w, "you can only add your friends")
		return
	}
	ids, err := s.Store.DMParticipantIDs(r.Context(), ch.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if slices.Contains(ids, target.ID) {
		conflict(w, "they're already in this chat")
		return
	}
	if len(ids) >= maxDMParticipants {
		badRequest(w, fmt.Sprintf("chats are limited to %d people", maxDMParticipants))
		return
	}

	pair, err := s.Store.IsPairDM(r.Context(), ch.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if pair {
		// Start a fresh multi-person chat; the 1:1 keeps its history.
		newCh, err := s.Store.CreateGroupDM(r.Context(), append(ids, target.ID))
		if err != nil {
			serverError(w, s.Log, err)
			return
		}
		s.attachDMParticipants(r.Context(), newCh)
		s.Hub.PublishToUsers(append(ids, target.ID), gateway.EvtDMCreate, map[string]any{"channel": newCh})
		s.postSystemMessage(r.Context(), newCh, me.ID,
			fmt.Sprintf("%s started the chat and added %s", me.DisplayName, target.DisplayName))
		writeJSON(w, 201, map[string]any{"channel": newCh})
		return
	}

	if err := s.Store.AddDMParticipant(r.Context(), ch.ID, target.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.attachDMParticipants(r.Context(), ch)
	// dm.create is idempotent client-side: existing members refresh the
	// participant list, the newcomer gets the chat in their sidebar.
	s.Hub.PublishToUsers(append(ids, target.ID), gateway.EvtDMCreate, map[string]any{"channel": ch})
	s.postSystemMessage(r.Context(), ch, me.ID,
		fmt.Sprintf("%s added %s", me.DisplayName, target.DisplayName))
	writeJSON(w, 200, map[string]any{"channel": ch})
}

func (s *Server) handleLeaveDM(w http.ResponseWriter, r *http.Request) {
	ch, me, ok := s.requireDM(w, r)
	if !ok {
		return
	}
	pair, err := s.Store.IsPairDM(r.Context(), ch.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if pair {
		badRequest(w, "you can't leave a direct message")
		return
	}
	if err := s.Store.RemoveDMParticipant(r.Context(), ch.ID, me.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToUsers([]string{me.ID}, gateway.EvtChannelDelete, map[string]any{"channel_id": ch.ID})
	s.attachDMParticipants(r.Context(), ch)
	remaining, _ := s.Store.DMParticipantIDs(r.Context(), ch.ID)
	s.Hub.PublishToUsers(remaining, gateway.EvtDMCreate, map[string]any{"channel": ch})
	s.postSystemMessage(r.Context(), ch, me.ID, fmt.Sprintf("%s left the chat", me.DisplayName))
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// updateDMChannel handles renames of multi-person chats (any participant may
// rename; 1:1 chats always display the friend's name instead).
func (s *Server) updateDMChannel(w http.ResponseWriter, r *http.Request, ch *model.Channel) {
	me := auth.UserFrom(r.Context())
	member, err := s.Store.IsChannelMember(r.Context(), ch, me.ID)
	if err != nil || !member {
		notFound(w)
		return
	}
	pair, err := s.Store.IsPairDM(r.Context(), ch.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if pair {
		badRequest(w, "direct messages can't be renamed")
		return
	}
	var req struct {
		Name *string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name == nil || len(*req.Name) > 64 {
		badRequest(w, "chat name must be at most 64 characters")
		return
	}
	name := strings.TrimSpace(*req.Name)
	if err := s.Store.UpdateChannel(r.Context(), ch.ID, &name, nil); err != nil {
		serverError(w, s.Log, err)
		return
	}
	updated, err := s.Store.ChannelByID(r.Context(), ch.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.attachDMParticipants(r.Context(), updated)
	s.Hub.PublishToChannel(r.Context(), updated, gateway.EvtChannelUpdate, map[string]any{"channel": updated})
	if name != "" {
		s.postSystemMessage(r.Context(), updated, me.ID,
			fmt.Sprintf("%s renamed the chat to %q", me.DisplayName, name))
	}
	writeJSON(w, 200, map[string]any{"channel": updated})
}
