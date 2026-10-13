package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/skyleb/talaria/internal/auth"
	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

// --- setup (first run) ---

func (s *Server) setupComplete(r *http.Request) bool {
	v, _ := s.Store.GetSetting(r.Context(), "setup_complete")
	return v == "true"
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	name, _ := s.Store.GetSetting(r.Context(), "server_name")
	if name == "" {
		name = s.Config.Server.Name
	}
	writeJSON(w, 200, map[string]any{
		"needs_setup": !s.setupComplete(r),
		"server_name": name,
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.setupComplete(r) {
		forbidden(w, "setup already complete")
		return
	}
	var req struct {
		ServerName string `json:"server_name"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		Domain     string `json:"domain"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !auth.ValidUsername(req.Username) {
		badRequest(w, "username must be 2-32 chars of a-z 0-9 _ .")
		return
	}
	if !auth.ValidPassword(req.Password) {
		badRequest(w, "password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	u, err := s.Store.CreateUser(r.Context(), req.Username, req.Username, hash, true)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	name := strings.TrimSpace(req.ServerName)
	if name == "" {
		name = "Talaria"
	}
	s.Store.SetSetting(r.Context(), "server_name", name)
	s.Store.SetSetting(r.Context(), "setup_complete", "true")
	if d := strings.TrimSpace(req.Domain); d != "" {
		s.Config.TLS.Domain = d
		if err := s.Config.Save(); err != nil {
			s.Log.Error("save config", "err", err)
		}
		// TLS takes effect on next start; the wizard explains this.
	}
	if err := s.Auth.Issue(r.Context(), w, r, u.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 201, map[string]any{"user": u, "restart_needed_for_tls": req.Domain != ""})
}

// --- register / login / logout ---

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InviteCode  string `json:"invite_code"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !s.setupComplete(r) {
		forbidden(w, "server not set up yet")
		return
	}
	if !auth.ValidUsername(req.Username) {
		badRequest(w, "username must be 2-32 chars of a-z 0-9 _ .")
		return
	}
	if !auth.ValidPassword(req.Password) {
		badRequest(w, "password must be at least 8 characters")
		return
	}
	inv, err := s.Store.InviteByCode(r.Context(), strings.ToUpper(strings.TrimSpace(req.InviteCode)))
	if err != nil {
		badRequest(w, "invalid invite code")
		return
	}
	// Group invites double as registration invites: a new user lands in the group.
	if _, err := s.Store.UserByUsername(r.Context(), req.Username); err == nil {
		conflict(w, "username is taken")
		return
	}
	consumed, err := s.Store.ConsumeInvite(r.Context(), inv.Code)
	if err == store.ErrInviteInvalid {
		badRequest(w, "invite code is expired, revoked, or used up")
		return
	}
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	u, err := s.Store.CreateUser(r.Context(), req.Username, req.DisplayName, hash, false)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	if consumed.Kind == model.InviteGroup && consumed.GroupID != "" {
		if err := s.Store.AddGroupMember(r.Context(), consumed.GroupID, u.ID, model.RoleMember); err == nil {
			s.announceMemberAdd(r, consumed.GroupID, u.ID)
		}
	}
	// Whoever sent the invite (to the app or to a group) is the newcomer's
	// first friend.
	s.befriendInviter(r, consumed.CreatedBy, u.ID)
	if err := s.Auth.Issue(r.Context(), w, r, u.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 201, map[string]any{"user": u})
}

// loginLimiter is a tiny fixed-window limiter keyed by remote IP.
var loginLimiter = struct {
	sync.Mutex
	hits map[string][]int64
}{hits: map[string][]int64{}}

func loginAllowed(ip string) bool {
	const window, max = int64(60_000), 10
	nowMs := time.Now().UnixMilli()
	loginLimiter.Lock()
	defer loginLimiter.Unlock()
	keep := loginLimiter.hits[ip][:0]
	for _, t := range loginLimiter.hits[ip] {
		if nowMs-t < window {
			keep = append(keep, t)
		}
	}
	if len(keep) >= max {
		loginLimiter.hits[ip] = keep
		return false
	}
	loginLimiter.hits[ip] = append(keep, nowMs)
	return true
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if i := strings.LastIndex(ip, ":"); i > 0 {
		ip = ip[:i]
	}
	if !loginAllowed(ip) {
		writeErr(w, 429, "rate_limited", "too many attempts; wait a minute")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	u, err := s.Store.UserByUsername(r.Context(), req.Username)
	if err != nil || !auth.CheckPassword(req.Password, u.PasswordHash) {
		writeErr(w, 401, "bad_credentials", "wrong username or password")
		return
	}
	if u.DisabledAt != 0 {
		forbidden(w, "account disabled")
		return
	}
	if err := s.Auth.Issue(r.Context(), w, r, u.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]any{"user": u})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Clear(r.Context(), w, r)
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	cur := auth.SessionFrom(r.Context())
	sessions, err := s.Store.ListSessionsByUser(r.Context(), u.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	type sessOut struct {
		*model.Session
		Current bool `json:"current"`
	}
	out := make([]sessOut, len(sessions))
	for i, se := range sessions {
		out[i] = sessOut{Session: se, Current: se.ID == cur.ID}
	}
	writeJSON(w, 200, map[string]any{"sessions": out})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	id := r.PathValue("id")
	sess, err := s.Store.SessionByID(r.Context(), id)
	if err != nil || sess.UserID != u.ID {
		notFound(w)
		return
	}
	if err := s.Store.DeleteSession(r.Context(), id); err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// marshalable helper for events
func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
