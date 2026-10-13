package api

import (
	"net/http"

	"github.com/skyleb/talaria/internal/gateway"
	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

// befriendInviter makes the person who created an invite and the account
// that just registered with it friends straight away: inviting someone is
// a stronger signal than a friend request. Blocks are respected, an
// existing pending request is accepted, and an existing friendship is left
// alone. Failures are logged, never surfaced: registration already
// succeeded.
func (s *Server) befriendInviter(r *http.Request, inviterID, newUserID string) {
	if inviterID == "" || newUserID == "" || inviterID == newUserID {
		return
	}
	ctx := r.Context()
	f, err := s.Store.FriendshipBetween(ctx, inviterID, newUserID)
	switch {
	case err == nil:
		switch f.Status {
		case model.FriendAccepted, model.FriendBlocked:
			return
		case model.FriendPending:
			if err := s.Store.UpdateFriendshipStatus(ctx, f.ID, model.FriendAccepted); err != nil {
				s.Log.Warn("auto-friend on invite", "err", err)
				return
			}
			f.Status = model.FriendAccepted
		default: // declined, removed: start over
			s.Store.DeleteFriendship(ctx, f.ID)
			f = nil
		}
	case err == store.ErrNotFound:
		f = nil
	default:
		s.Log.Warn("auto-friend on invite", "err", err)
		return
	}
	if f == nil {
		f, err = s.Store.CreateFriendRequest(ctx, inviterID, newUserID)
		if err != nil {
			s.Log.Warn("auto-friend on invite", "err", err)
			return
		}
		if err := s.Store.UpdateFriendshipStatus(ctx, f.ID, model.FriendAccepted); err != nil {
			s.Log.Warn("auto-friend on invite", "err", err)
			return
		}
		f.Status = model.FriendAccepted
	}
	users, err := s.Store.PublicUsersByIDs(ctx, []string{inviterID, newUserID})
	if err != nil {
		return
	}
	// The inviter may be online right now; the newcomer gets it in their
	// ready payload anyway, the event just keeps a second tab in sync.
	s.Hub.PublishToUsers([]string{inviterID}, gateway.EvtFriendUpdate,
		map[string]any{"friendship": f, "user": users[newUserID]})
	s.Hub.PublishToUsers([]string{newUserID}, gateway.EvtFriendUpdate,
		map[string]any{"friendship": f, "user": users[inviterID]})
}
