package api

import (
	"context"
	"net/http"
	"time"

	"github.com/skyle/talaria/internal/auth"
	"github.com/skyle/talaria/internal/model"
	"github.com/skyle/talaria/internal/store"
)

func timeoutCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]any{"users": users})
}

func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	id := r.PathValue("id")
	target, err := s.Store.UserByID(r.Context(), id)
	if err != nil {
		notFound(w)
		return
	}
	var req struct {
		Disabled    *bool   `json:"disabled"`
		IsAdmin     *bool   `json:"is_admin"`
		NewPassword *string `json:"new_password"` // admin reset
	}
	if !decode(w, r, &req) {
		return
	}
	if id == me.ID && ((req.Disabled != nil && *req.Disabled) || (req.IsAdmin != nil && !*req.IsAdmin)) {
		badRequest(w, "you cannot disable or demote yourself")
		return
	}
	patch := store.UserPatch{Disabled: req.Disabled, IsAdmin: req.IsAdmin}
	if req.NewPassword != nil {
		if !auth.ValidPassword(*req.NewPassword) {
			badRequest(w, "password must be at least 8 characters")
			return
		}
		h, err := auth.HashPassword(*req.NewPassword)
		if err != nil {
			serverError(w, s.Log, err)
			return
		}
		patch.PasswordHash = &h
	}
	if err := s.Store.UpdateUser(r.Context(), target.ID, patch); err != nil {
		serverError(w, s.Log, err)
		return
	}
	updated, _ := s.Store.UserByID(r.Context(), target.ID)
	writeJSON(w, 200, map[string]any{"user": updated})
}

func (s *Server) handleAdminListInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.Store.ListInvites(r.Context(), model.InviteRegistration, "")
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]any{"invites": invites})
}

func (s *Server) handleAdminCreateInvite(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
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
	inv, err := s.Store.CreateInvite(r.Context(), model.InviteRegistration, "", me.ID, req.MaxUses, expiresAt)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 201, map[string]any{"invite": inv, "url": "/register?invite=" + inv.Code})
}

func (s *Server) handleAdminRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RevokeInvite(r.Context(), r.PathValue("code")); err != nil {
		notFound(w)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	var userCount, msgCount, fileCount int
	s.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&userCount)
	s.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM messages`).Scan(&msgCount)
	s.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM files`).Scan(&fileCount)
	blobBytes, _ := s.Blobs.Usage()
	writeJSON(w, 200, map[string]any{
		"users": userCount, "messages": msgCount, "files": fileCount,
		"blob_bytes": blobBytes, "version": s.Version,
	})
}
