package api

import (
	"net/http"

	"github.com/skyle/talaria/internal/auth"
	"github.com/skyle/talaria/internal/gateway"
	"github.com/skyle/talaria/internal/model"
	"github.com/skyle/talaria/internal/store"
)

func (s *Server) handleListFriends(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	fs, err := s.Store.ListFriendships(r.Context(), me.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	var ids []string
	entries := make([]gateway.FriendEntry, 0, len(fs))
	for _, f := range fs {
		other := f.RequesterID
		if other == me.ID {
			other = f.AddresseeID
		}
		if f.Status == model.FriendBlocked && f.RequesterID != me.ID {
			continue // blocks are invisible to the blocked side
		}
		ids = append(ids, other)
		entries = append(entries, gateway.FriendEntry{Friendship: f, UserID: other})
	}
	users, err := s.Store.PublicUsersByIDs(r.Context(), ids)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]any{"friends": entries, "users": users})
}

func (s *Server) handleFriendRequest(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var req struct {
		Username string `json:"username"`
	}
	if !decode(w, r, &req) {
		return
	}
	target, err := s.Store.UserByUsername(r.Context(), req.Username)
	if err != nil {
		notFound(w)
		return
	}
	if target.ID == me.ID {
		badRequest(w, "you cannot friend yourself")
		return
	}
	existing, err := s.Store.FriendshipBetween(r.Context(), me.ID, target.ID)
	if err == nil {
		switch existing.Status {
		case model.FriendAccepted:
			conflict(w, "already friends")
			return
		case model.FriendPending:
			// If they already requested us, accept instead.
			if existing.AddresseeID == me.ID {
				s.acceptFriendship(w, r, existing)
				return
			}
			conflict(w, "request already sent")
			return
		case model.FriendBlocked:
			// Both directions fail quietly the same way (don't leak blocks).
			notFound(w)
			return
		case model.FriendDeclined:
			// Allow re-request: delete old row and fall through.
			s.Store.DeleteFriendship(r.Context(), existing.ID)
		}
	} else if err != store.ErrNotFound {
		serverError(w, s.Log, err)
		return
	}
	f, err := s.Store.CreateFriendRequest(r.Context(), me.ID, target.ID)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToUsers([]string{target.ID}, gateway.EvtFriendRequest, map[string]any{
		"friendship": f, "user": me.Public(),
	})
	s.Hub.PublishToUsers([]string{me.ID}, gateway.EvtFriendUpdate, map[string]any{
		"friendship": f, "user": target.Public(),
	})
	writeJSON(w, 201, map[string]any{"friendship": f, "user": target.Public()})
}

func (s *Server) acceptFriendship(w http.ResponseWriter, r *http.Request, f *model.Friendship) {
	if err := s.Store.UpdateFriendshipStatus(r.Context(), f.ID, model.FriendAccepted); err != nil {
		serverError(w, s.Log, err)
		return
	}
	f.Status = model.FriendAccepted
	users, err := s.Store.PublicUsersByIDs(r.Context(), []string{f.RequesterID, f.AddresseeID})
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	reqUser, addrUser := users[f.RequesterID], users[f.AddresseeID]
	s.Hub.PublishToUsers([]string{f.RequesterID}, gateway.EvtFriendUpdate,
		map[string]any{"friendship": f, "user": addrUser})
	s.Hub.PublishToUsers([]string{f.AddresseeID}, gateway.EvtFriendUpdate,
		map[string]any{"friendship": f, "user": reqUser})
	writeJSON(w, 200, map[string]any{"friendship": f})
}

func (s *Server) handleFriendAccept(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	f, err := s.Store.FriendshipByID(r.Context(), r.PathValue("id"))
	if err != nil || f.AddresseeID != me.ID || f.Status != model.FriendPending {
		notFound(w)
		return
	}
	s.acceptFriendship(w, r, f)
}

func (s *Server) handleFriendDecline(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	f, err := s.Store.FriendshipByID(r.Context(), r.PathValue("id"))
	if err != nil || f.AddresseeID != me.ID || f.Status != model.FriendPending {
		notFound(w)
		return
	}
	if err := s.Store.UpdateFriendshipStatus(r.Context(), f.ID, model.FriendDeclined); err != nil {
		serverError(w, s.Log, err)
		return
	}
	f.Status = model.FriendDeclined
	// Requester just sees the pending request disappear.
	s.Hub.PublishToUsers([]string{f.RequesterID, me.ID}, gateway.EvtFriendUpdate,
		map[string]any{"friendship": f})
	writeJSON(w, 200, map[string]any{"friendship": f})
}

func (s *Server) handleUnfriend(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	other := r.PathValue("userId")
	f, err := s.Store.FriendshipBetween(r.Context(), me.ID, other)
	if err != nil || f.Status == model.FriendBlocked {
		notFound(w)
		return
	}
	// Covers unfriending and cancelling my own outgoing request.
	if f.Status == model.FriendPending && f.RequesterID != me.ID {
		notFound(w)
		return
	}
	if err := s.Store.DeleteFriendship(r.Context(), f.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	f.Status = "removed"
	s.Hub.PublishToUsers([]string{f.RequesterID, f.AddresseeID}, gateway.EvtFriendUpdate,
		map[string]any{"friendship": f})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	other := r.PathValue("userId")
	if other == me.ID {
		badRequest(w, "cannot block yourself")
		return
	}
	if _, err := s.Store.UserByID(r.Context(), other); err != nil {
		notFound(w)
		return
	}
	f, err := s.Store.ReplaceFriendshipAsBlock(r.Context(), me.ID, other)
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	s.Hub.PublishToUsers([]string{me.ID}, gateway.EvtFriendUpdate, map[string]any{"friendship": f})
	// The blocked user sees any friendship silently vanish.
	s.Hub.PublishToUsers([]string{other}, gateway.EvtFriendUpdate,
		map[string]any{"friendship": &model.Friendship{
			ID: f.ID, RequesterID: f.RequesterID, AddresseeID: f.AddresseeID, Status: "removed",
		}})
	writeJSON(w, 200, map[string]any{"friendship": f})
}

func (s *Server) handleUnblock(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	other := r.PathValue("userId")
	f, err := s.Store.FriendshipBetween(r.Context(), me.ID, other)
	if err != nil || f.Status != model.FriendBlocked || f.RequesterID != me.ID {
		notFound(w)
		return
	}
	if err := s.Store.DeleteFriendship(r.Context(), f.ID); err != nil {
		serverError(w, s.Log, err)
		return
	}
	f.Status = "removed"
	s.Hub.PublishToUsers([]string{me.ID}, gateway.EvtFriendUpdate, map[string]any{"friendship": f})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
