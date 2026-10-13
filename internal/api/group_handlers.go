package api

import (
	"net/http"
	"strings"

	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/gateway"
	"github.com/skyleb/talaria/internal/model"
)

// requireGroupRole loads the group and verifies the caller holds one of roles
// (empty roles = any member).
func (s *Server) requireGroupRole(w http.ResponseWriter, r *http.Request, groupID string, roles ...string) (*model.Group, *model.GroupMember, bool) {
	me := auth.UserFrom(r.Context())
	g, err := s.Store.GroupByID(r.Context(), groupID)
	if err != nil {
		notFound(w)
		return nil, nil, false
	}
	m, err := s.Store.GroupMember(r.Context(), groupID, me.ID)
	if err != nil {
		notFound(w) // non-members can't tell the group exists
		return nil, nil, false
	}
	if len(roles) > 0 {
		ok := false
		for _, role := range roles {
			if m.Role == role {
				ok = true
				break
			}
		}
		if !ok {
			forbidden(w, "insufficient group role")
			return nil, nil, false
		}
	}
	return g, m, true
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 64 {
		badRequest(w, "group name must be 1-64 characters")
		return
	}
	g, ch, err := s.Store.CreateGroup(r.Context(), name, me.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToUsers([]string{me.ID}, gateway.EvtGroupCreate, map[string]any{
		"group": g, "channels": []*model.Channel{ch},
		"members": []*model.GroupMember{{GroupID: g.ID, UserID: me.ID, Role: model.RoleOwner}},
	})
	writeJSON(w, 201, map[string]any{"group": g, "channels": []*model.Channel{ch}})
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	g, _, ok := s.requireGroupRole(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	channels, err := s.Store.ListChannelsByGroup(r.Context(), g.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	members, err := s.Store.ListGroupMembers(r.Context(), g.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.UserID
	}
	users, err := s.Store.PublicUsersByIDs(r.Context(), ids)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"group": g, "channels": channels, "members": members, "users": users,
		"online_user_ids": s.Hub.OnlineAmong(ids),
	})
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	g, _, ok := s.requireGroupRole(w, r, r.PathValue("id"), model.RoleOwner, model.RoleAdmin)
	if !ok {
		return
	}
	var req struct {
		Name       *string `json:"name"`
		IconFileID *string `json:"icon_file_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil && (strings.TrimSpace(*req.Name) == "" || len(*req.Name) > 64) {
		badRequest(w, "group name must be 1-64 characters")
		return
	}
	if req.IconFileID != nil && *req.IconFileID != "" {
		f, err := s.Store.FileByID(r.Context(), *req.IconFileID)
		if err != nil || !strings.HasPrefix(f.MIME, "image/") {
			badRequest(w, "icon_file_id must reference an uploaded image")
			return
		}
	}
	if err := s.Store.UpdateGroup(r.Context(), g.ID, req.Name, req.IconFileID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	updated, err := s.Store.GroupByID(r.Context(), g.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	ids, _ := s.Store.GroupMemberIDs(r.Context(), g.ID)
	s.Hub.PublishToUsers(ids, gateway.EvtGroupUpdate, map[string]any{"group": updated})
	writeJSON(w, 200, map[string]any{"group": updated})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	g, _, ok := s.requireGroupRole(w, r, r.PathValue("id"), model.RoleOwner)
	if !ok {
		return
	}
	ids, _ := s.Store.GroupMemberIDs(r.Context(), g.ID)
	if err := s.Store.DeleteGroup(r.Context(), g.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToUsers(ids, gateway.EvtGroupDelete, map[string]any{"group_id": g.ID})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	g, _, ok := s.requireGroupRole(w, r, r.PathValue("id"), model.RoleOwner, model.RoleAdmin)
	if !ok {
		return
	}
	var req struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 64 {
		badRequest(w, "channel name must be 1-64 characters")
		return
	}
	existing, err := s.Store.ListChannelsByGroup(r.Context(), g.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	ch, err := s.Store.CreateGroupChannel(r.Context(), g.ID, name, req.Topic, len(existing))
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	ids, _ := s.Store.GroupMemberIDs(r.Context(), g.ID)
	s.Hub.PublishToUsers(ids, gateway.EvtChannelCreate, map[string]any{"channel": ch})
	writeJSON(w, 201, map[string]any{"channel": ch})
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	ch, err := s.Store.ChannelByID(r.Context(), r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	if ch.Kind == model.ChannelDM {
		s.updateDMChannel(w, r, ch)
		return
	}
	if _, _, ok := s.requireGroupRole(w, r, ch.GroupID, model.RoleOwner, model.RoleAdmin); !ok {
		return
	}
	var req struct {
		Name  *string `json:"name"`
		Topic *string `json:"topic"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name != nil && (strings.TrimSpace(*req.Name) == "" || len(*req.Name) > 64) {
		badRequest(w, "channel name must be 1-64 characters")
		return
	}
	if err := s.Store.UpdateChannel(r.Context(), ch.ID, req.Name, req.Topic); err != nil {
		serverError(w, s.Log, err)
		return
	}
	updated, _ := s.Store.ChannelByID(r.Context(), ch.ID)
	s.Hub.PublishToChannel(r.Context(), ch, gateway.EvtChannelUpdate, map[string]any{"channel": updated})
	writeJSON(w, 200, map[string]any{"channel": updated})
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	ch, err := s.Store.ChannelByID(r.Context(), r.PathValue("id"))
	if err != nil || ch.Kind == model.ChannelDM {
		notFound(w)
		return
	}
	g, _, ok := s.requireGroupRole(w, r, ch.GroupID, model.RoleOwner, model.RoleAdmin)
	if !ok {
		return
	}
	chans, err := s.Store.ListChannelsByGroup(r.Context(), g.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if len(chans) <= 1 {
		badRequest(w, "a group must keep at least one channel")
		return
	}
	ids, _ := s.Store.GroupMemberIDs(r.Context(), g.ID)
	if err := s.Store.DeleteChannel(r.Context(), ch.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToUsers(ids, gateway.EvtChannelDelete,
		map[string]any{"channel_id": ch.ID, "group_id": g.ID})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	targetID := r.PathValue("userId")
	groupID := r.PathValue("id")
	g, myMem, ok := s.requireGroupRole(w, r, groupID)
	if !ok {
		return
	}
	isSelf := targetID == me.ID
	if !isSelf && myMem.Role != model.RoleOwner && myMem.Role != model.RoleAdmin {
		forbidden(w, "only admins can remove members")
		return
	}
	if targetID == g.OwnerID {
		badRequest(w, "the owner cannot be removed; delete the group instead")
		return
	}
	if _, err := s.Store.GroupMember(r.Context(), groupID, targetID); err != nil {
		notFound(w)
		return
	}
	ids, _ := s.Store.GroupMemberIDs(r.Context(), groupID)
	if err := s.Store.RemoveGroupMember(r.Context(), groupID, targetID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToUsers(ids, gateway.EvtMemberRemove,
		map[string]any{"group_id": groupID, "user_id": targetID})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// --- invites ---

func (s *Server) handleCreateGroupInvite(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	g, _, ok := s.requireGroupRole(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct {
		MaxUses   int64 `json:"max_uses"`
		ExpiresIn int64 `json:"expires_in_s"`
	}
	if !decode(w, r, &req) {
		return
	}
	var expiresAt int64
	if req.ExpiresIn > 0 {
		expiresAt = nowMs() + req.ExpiresIn*1000
	}
	inv, err := s.Store.CreateInvite(r.Context(), model.InviteGroup, g.ID, me.ID, req.MaxUses, expiresAt)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 201, map[string]any{"invite": inv, "url": "/invite/" + inv.Code})
}

func (s *Server) handleInvitePreview(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	inv, err := s.Store.InviteByCode(r.Context(), code)
	if err != nil {
		notFound(w)
		return
	}
	nowM := nowMs()
	valid := inv.RevokedAt == 0 &&
		(inv.ExpiresAt == 0 || inv.ExpiresAt > nowM) &&
		(inv.MaxUses == 0 || inv.UseCount < inv.MaxUses)
	out := map[string]any{"code": inv.Code, "kind": inv.Kind, "valid": valid}
	if inv.Kind == model.InviteGroup && inv.GroupID != "" {
		if g, err := s.Store.GroupByID(r.Context(), inv.GroupID); err == nil {
			n, _ := s.Store.CountGroupMembers(r.Context(), g.ID)
			out["group"] = map[string]any{
				"name": g.Name, "icon_file_id": g.IconFileID, "member_count": n,
			}
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleJoinInvite(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
	inv, err := s.Store.InviteByCode(r.Context(), code)
	if err != nil || inv.Kind != model.InviteGroup || inv.GroupID == "" {
		notFound(w)
		return
	}
	if _, err := s.Store.GroupMember(r.Context(), inv.GroupID, me.ID); err == nil {
		conflict(w, "already a member")
		return
	}
	if _, err := s.Store.ConsumeInvite(r.Context(), code); err != nil {
		badRequest(w, "invite code is expired, revoked, or used up")
		return
	}
	if err := s.Store.AddGroupMember(r.Context(), inv.GroupID, me.ID, model.RoleMember); err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.announceMemberAdd(r, inv.GroupID, me.ID)

	g, _ := s.Store.GroupByID(r.Context(), inv.GroupID)
	channels, _ := s.Store.ListChannelsByGroup(r.Context(), inv.GroupID)
	writeJSON(w, 200, map[string]any{"group": g, "channels": channels})
}

// announceMemberAdd notifies existing members and sends the joiner a group.create.
func (s *Server) announceMemberAdd(r *http.Request, groupID, userID string) {
	ctx := r.Context()
	ids, err := s.Store.GroupMemberIDs(ctx, groupID)
	if err != nil {
		return
	}
	users, _ := s.Store.PublicUsersByIDs(ctx, []string{userID})
	member, _ := s.Store.GroupMember(ctx, groupID, userID)
	var others []string
	for _, id := range ids {
		if id != userID {
			others = append(others, id)
		}
	}
	s.Hub.PublishToUsers(others, gateway.EvtMemberAdd, map[string]any{
		"group_id": groupID, "member": member, "user": users[userID],
	})
	// The joiner gets the whole group so their client can render it immediately.
	g, _ := s.Store.GroupByID(ctx, groupID)
	channels, _ := s.Store.ListChannelsByGroup(ctx, groupID)
	members, _ := s.Store.ListGroupMembers(ctx, groupID)
	s.Hub.PublishToUsers([]string{userID}, gateway.EvtGroupCreate, map[string]any{
		"group": g, "channels": channels, "members": members,
	})
}

// --- DMs ---

func (s *Server) handleOpenDM(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.UserID == me.ID {
		badRequest(w, "cannot DM yourself")
		return
	}
	friends, err := s.Store.AreFriends(r.Context(), me.ID, req.UserID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if !friends {
		forbidden(w, "DMs require friendship")
		return
	}
	ch, created, err := s.Store.GetOrCreateDM(r.Context(), me.ID, req.UserID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	users, _ := s.Store.PublicUsersByIDs(r.Context(), []string{me.ID, req.UserID})
	for _, u := range users {
		ch.Participants = append(ch.Participants, u)
	}
	if created {
		s.Hub.PublishToUsers([]string{me.ID, req.UserID}, gateway.EvtDMCreate,
			map[string]any{"channel": ch})
	}
	writeJSON(w, 200, map[string]any{"channel": ch})
}
