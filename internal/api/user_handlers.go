package api

import (
	"net/http"
	"strings"

	"github.com/skyle/talaria/internal/auth"
	"github.com/skyle/talaria/internal/gateway"
	"github.com/skyle/talaria/internal/store"
)

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"user": auth.UserFrom(r.Context())})
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	var req struct {
		DisplayName      *string `json:"display_name"`
		StatusText       *string `json:"status_text"`
		AvatarFileID     *string `json:"avatar_file_id"`
		CurrentPassword  *string `json:"current_password"`
		NewPassword      *string `json:"new_password"`
	}
	if !decode(w, r, &req) {
		return
	}
	patch := store.UserPatch{}
	if req.DisplayName != nil {
		d := strings.TrimSpace(*req.DisplayName)
		if d == "" || len(d) > 64 {
			badRequest(w, "display name must be 1-64 characters")
			return
		}
		patch.DisplayName = &d
	}
	if req.StatusText != nil {
		st := strings.TrimSpace(*req.StatusText)
		if len(st) > 128 {
			badRequest(w, "status must be at most 128 characters")
			return
		}
		patch.StatusText = &st
	}
	if req.AvatarFileID != nil {
		// Must reference a file the user uploaded (via an attachment they own).
		if *req.AvatarFileID != "" {
			f, err := s.Store.FileByID(r.Context(), *req.AvatarFileID)
			if err != nil || !strings.HasPrefix(f.MIME, "image/") {
				badRequest(w, "avatar_file_id must reference an uploaded image")
				return
			}
		}
		patch.AvatarFileID = req.AvatarFileID
	}
	if req.NewPassword != nil {
		if req.CurrentPassword == nil || !auth.CheckPassword(*req.CurrentPassword, u.PasswordHash) {
			forbidden(w, "current password is wrong")
			return
		}
		if !auth.ValidPassword(*req.NewPassword) {
			badRequest(w, "new password must be at least 8 characters")
			return
		}
		h, err := auth.HashPassword(*req.NewPassword)
		if err != nil {
			serverError(w, s.Log, err)
			return
		}
		patch.PasswordHash = &h
	}
	if err := s.Store.UpdateUser(r.Context(), u.ID, patch); err != nil {
		serverError(w, s.Log, err)
		return
	}
	updated, err := s.Store.UserByID(r.Context(), u.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	// Tell everyone who can see this user about profile changes.
	if req.DisplayName != nil || req.StatusText != nil || req.AvatarFileID != nil {
		ids, err := s.Store.VisibleUserIDs(r.Context(), u.ID)
		if err == nil {
			s.Hub.PublishToUsers(append(ids, u.ID), gateway.EvtUserUpdate,
				map[string]any{"user": updated.Public()})
		}
	}
	writeJSON(w, 200, map[string]any{"user": updated})
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	id := r.PathValue("id")
	target, err := s.Store.UserByID(r.Context(), id)
	if err != nil {
		notFound(w)
		return
	}
	if id != me.ID {
		visible, err := s.Store.VisibleUserIDs(r.Context(), me.ID)
		if err != nil {
			serverError(w, s.Log, err)
			return
		}
		ok := false
		for _, v := range visible {
			if v == id {
				ok = true
				break
			}
		}
		// Also allow lookups of pending requesters (they appear via friendships).
		if !ok {
			if _, ferr := s.Store.FriendshipBetween(r.Context(), me.ID, id); ferr != nil {
				notFound(w)
				return
			}
		}
	}
	writeJSON(w, 200, map[string]any{"user": target.Public()})
}
