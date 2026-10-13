package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/skyle/talaria/internal/auth"
	"github.com/skyle/talaria/internal/model"
	"github.com/skyle/talaria/internal/push"
)

// --- Web Push subscriptions ---

func (s *Server) handlePushVAPID(w http.ResponseWriter, r *http.Request) {
	if s.Push == nil {
		writeErr(w, 503, "unavailable", "push notifications are not configured")
		return
	}
	writeJSON(w, 200, map[string]string{"public_key": s.Push.PublicKey()})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !strings.HasPrefix(req.Endpoint, "https://") || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		badRequest(w, "invalid push subscription")
		return
	}
	sub := &model.PushSubscription{
		UserID: me.ID, Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth,
		UserAgent: r.UserAgent(),
	}
	if err := s.Store.UpsertPushSubscription(r.Context(), sub); err != nil {
		serverError(w, s.Log, err)
		return
	}
	n, _ := s.Store.CountPushSubscriptions(r.Context(), me.ID)
	writeJSON(w, 200, map[string]any{"status": "ok", "devices": n})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.Store.DeletePushSubscription(r.Context(), me.ID, req.Endpoint); err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handlePushStatus(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	n, _ := s.Store.CountPushSubscriptions(r.Context(), me.ID)
	writeJSON(w, 200, map[string]any{"enabled": s.Push != nil, "devices": n})
}

// --- Channel mutes ---

func (s *Server) handleMuteChannel(w http.ResponseWriter, r *http.Request) {
	s.setMute(w, r, true)
}

func (s *Server) handleUnmuteChannel(w http.ResponseWriter, r *http.Request) {
	s.setMute(w, r, false)
}

func (s *Server) setMute(w http.ResponseWriter, r *http.Request, muted bool) {
	me := auth.UserFrom(r.Context())
	ch, err := s.Store.ChannelByID(r.Context(), r.PathValue("id"))
	if err != nil {
		notFound(w)
		return
	}
	member, err := s.Store.IsChannelMember(r.Context(), ch, me.ID)
	if err != nil || !member {
		notFound(w)
		return
	}
	if muted {
		err = s.Store.MuteChannel(r.Context(), me.ID, ch.ID)
	} else {
		err = s.Store.UnmuteChannel(r.Context(), me.ID, ch.ID)
	}
	if err != nil {
		serverError(w, s.Log, err)
		return
	}
	writeJSON(w, 200, map[string]any{"channel_id": ch.ID, "muted": muted})
}

// --- Mentions + notification dispatch ---

var mentionRe = regexp.MustCompile(`(?:^|[^\w@])@([a-z0-9_.]{2,32})`)

// resolveMentions maps @username tokens in content to user ids, restricted
// to members of the channel so you can't ping strangers.
func (s *Server) resolveMentions(ctx context.Context, content string, memberIDs []string) []string {
	matches := mentionRe.FindAllStringSubmatch(strings.ToLower(content), -1)
	if len(matches) == 0 {
		return nil
	}
	members := map[string]bool{}
	for _, id := range memberIDs {
		members[id] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		u, err := s.Store.UserByUsername(ctx, strings.TrimSuffix(m[1], "."))
		if err != nil || !members[u.ID] || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		out = append(out, u.ID)
	}
	return out
}

// notifyMessage pushes a notification for a new message to recipients who
// should hear about it: everyone in a DM, and @mentioned people in group
// channels — never the author, never anyone who muted the channel. Runs in
// the background so sending never slows the request.
func (s *Server) notifyMessage(ch *model.Channel, m *model.Message, author *model.User, memberIDs []string) {
	if s.Push == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		mentioned := map[string]bool{}
		for _, id := range m.MentionIDs {
			mentioned[id] = true
		}
		var candidates []string
		for _, id := range memberIDs {
			if id == author.ID {
				continue
			}
			if ch.Kind == model.ChannelDM || mentioned[id] {
				candidates = append(candidates, id)
			}
		}
		if len(candidates) == 0 {
			return
		}
		muted, _ := s.Store.UsersMutingChannel(ctx, ch.ID, candidates)
		var recipients []string
		for _, id := range candidates {
			if !muted[id] {
				recipients = append(recipients, id)
			}
		}
		if len(recipients) == 0 {
			return
		}

		title, url := s.channelLabel(ctx, ch, author)
		body := m.Content
		if body == "" && len(m.Attachments) > 0 {
			body = "Sent an attachment"
		}
		body = truncate(body, 140)
		n := push.Notification{
			Type: "message", Title: title, Body: body, URL: url,
			Tag: "ch-" + ch.ID, ChannelID: ch.ID, FromID: author.ID,
		}
		if author.AvatarFileID != "" {
			n.Icon = "/avatars/" + author.AvatarFileID + "?s=128"
		}
		s.Push.SendToUsers(ctx, recipients, n)
	}()
}

// channelLabel returns the notification title and deep link for a channel.
func (s *Server) channelLabel(ctx context.Context, ch *model.Channel, author *model.User) (string, string) {
	if ch.Kind == model.ChannelDM {
		if ch.Name != "" {
			return author.DisplayName + " in " + ch.Name, "/app/dm/" + ch.ID
		}
		if ids, _ := s.Store.DMParticipantIDs(ctx, ch.ID); len(ids) > 2 {
			return author.DisplayName + " in group chat", "/app/dm/" + ch.ID
		}
		return author.DisplayName, "/app/dm/" + ch.ID
	}
	title := author.DisplayName + " in #" + ch.Name
	if g, err := s.Store.GroupByID(ctx, ch.GroupID); err == nil {
		title += " · " + g.Name
	}
	return title, "/app/g/" + ch.GroupID + "/" + ch.ID
}

// NotifyRing pushes an incoming-call notification (wired into the SFU).
func (s *Server) NotifyRing(channelID, fromUserID string, toUserIDs []string) {
	if s.Push == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		from, err := s.Store.UserByID(ctx, fromUserID)
		if err != nil {
			return
		}
		muted, _ := s.Store.UsersMutingChannel(ctx, channelID, toUserIDs)
		var recipients []string
		for _, id := range toUserIDs {
			if !muted[id] {
				recipients = append(recipients, id)
			}
		}
		n := push.Notification{
			Type: "call", Title: from.DisplayName + " is calling", Body: "Tap to join the call",
			URL: "/app/dm/" + channelID, Tag: "call-" + channelID, ChannelID: channelID, FromID: from.ID,
		}
		if from.AvatarFileID != "" {
			n.Icon = "/avatars/" + from.AvatarFileID + "?s=128"
		}
		s.Push.SendToUsers(ctx, recipients, n)
	}()
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
