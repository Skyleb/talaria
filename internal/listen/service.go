// Package listen implements synchronized group listening/watching: one
// server-authoritative playback clock per channel, a shared queue mixing
// YouTube videos and uploaded media, a host who drives transport, and
// saveable named playlists.
//
// Position math: while playing, the true position at server time t is
// position_ms + (t - anchor_server_ms). Clients know the server clock from
// the gateway's ping/pong offset estimate, so everyone lands on the same
// moment regardless of when they joined.
package listen

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

const EvtSyncState = "sync.state"

// Publisher is the slice of the gateway hub this package needs.
type Publisher interface {
	PublishToUsers(userIDs []string, t string, payload any)
	IsOnline(userID string) bool
}

// offlineGrace is how long a disconnected listener keeps their seat. A page
// reload or a phone changing networks reconnects within seconds; only a
// real departure should hand off the host role or end the session.
const offlineGrace = 25 * time.Second

// Error carries an HTTP status for the API layer.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func fail(status int, msg string) error { return &Error{Status: status, Message: msg} }

// State is the full picture of a channel's listening session, as broadcast
// to every channel member (session nil means nothing is playing).
type State struct {
	ChannelID string                 `json:"channel_id"`
	Session   *model.PlaybackSession `json:"session"`
	Items     []*model.PlaylistItem  `json:"items"`
	Members   []string               `json:"members"`
	ServerNow int64                  `json:"server_now"`
}

type Service struct {
	st  *store.Store
	pub Publisher
	log *slog.Logger
	// mu serializes mutations; sessions are few and operations are quick.
	mu sync.Mutex
	// calls, when set, ties listening to the channel's voice call: only
	// callers may start or join, and callers are seated automatically.
	calls CallGate
}

// CallGate is the slice of the voice-call manager this package needs.
type CallGate interface {
	InCall(channelID, userID string) bool
}

// SetCallGate makes listening a part of voice calls (see CallGate).
func (s *Service) SetCallGate(g CallGate) { s.calls = g }

func New(st *store.Store, pub Publisher, log *slog.Logger) *Service {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := st.EndDanglingPlaybackSessions(ctx); err != nil {
		log.Error("end dangling playback sessions", "err", err)
	}
	return &Service{st: st, pub: pub, log: log}
}

func (s *Service) requireInCall(ch *model.Channel, userID string) error {
	if s.calls != nil && !s.calls.InCall(ch.ID, userID) {
		return fail(409, "join the voice call first — listening together happens inside the call")
	}
	return nil
}

// OnCallJoined seats a new caller in the channel's live session, if any.
func (s *Service) OnCallJoined(ctx context.Context, ch *model.Channel, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return
	}
	if err := s.st.JoinPlayback(ctx, ps.ID, userID); err != nil {
		s.log.Error("listen: seat caller", "err", err)
		return
	}
	s.broadcast(ctx, ch)
}

// OnCallLeft removes someone who left the call from the session.
func (s *Service) OnCallLeft(ctx context.Context, ch *model.Channel, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return
	}
	members, err := s.st.PlaybackMemberIDs(ctx, ps.ID)
	if err != nil || !contains(members, userID) {
		return
	}
	if err := s.leaveLocked(ctx, ps, userID); err != nil {
		s.log.Error("listen: unseat caller", "err", err)
		return
	}
	s.broadcast(ctx, ch)
}

// OnCallEnded ends the session when the call is over.
func (s *Service) OnCallEnded(ctx context.Context, ch *model.Channel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return
	}
	if err := s.endLocked(ctx, ps); err != nil {
		s.log.Error("listen: end with call", "err", err)
		return
	}
	s.broadcast(ctx, ch)
}

func contains(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func nowMS() int64 { return time.Now().UnixMilli() }

// position returns the session's true position right now.
func position(ps *model.PlaybackSession) int64 {
	if ps.State == "playing" {
		return ps.PositionMS + (nowMS() - ps.AnchorServerMS)
	}
	return ps.PositionMS
}

func hydrateItems(items []*model.PlaylistItem) {
	for _, it := range items {
		refs := map[string]string{}
		if it.RefsJSON != "" {
			json.Unmarshal([]byte(it.RefsJSON), &refs)
		}
		it.Refs = refs
	}
}

// State assembles the current state for a channel.
func (s *Service) State(ctx context.Context, ch *model.Channel) (*State, error) {
	st := &State{ChannelID: ch.ID, Items: []*model.PlaylistItem{}, Members: []string{}, ServerNow: nowMS()}
	ps, err := s.st.ActivePlaybackSession(ctx, ch.ID)
	if errors.Is(err, store.ErrNotFound) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	st.Session = ps
	if st.Items, err = s.st.ListPlaylistItems(ctx, ps.PlaylistID); err != nil {
		return nil, err
	}
	hydrateItems(st.Items)
	if st.Members, err = s.st.PlaybackMemberIDs(ctx, ps.ID); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Service) broadcast(ctx context.Context, ch *model.Channel) *State {
	st, err := s.State(ctx, ch)
	if err != nil {
		s.log.Error("listen state", "channel", ch.ID, "err", err)
		return nil
	}
	ids, err := s.st.ChannelMemberIDs(ctx, ch)
	if err == nil {
		s.pub.PublishToUsers(ids, EvtSyncState, st)
	}
	return st
}

func (s *Service) active(ctx context.Context, ch *model.Channel) (*model.PlaybackSession, error) {
	ps, err := s.st.ActivePlaybackSession(ctx, ch.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fail(404, "nobody is listening together here right now")
	}
	return ps, err
}

func (s *Service) requireMember(ctx context.Context, ps *model.PlaybackSession, userID string) ([]string, error) {
	members, err := s.st.PlaybackMemberIDs(ctx, ps.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range members {
		if id == userID {
			return members, nil
		}
	}
	return nil, fail(403, "join the session first")
}

func (s *Service) requireHost(ps *model.PlaybackSession, userID string) error {
	if ps.HostUserID != userID {
		return fail(403, "only the host can do that")
	}
	return nil
}

// Start creates a session for the channel, optionally seeded from a saved
// playlist, and joins the starter as host.
func (s *Service) Start(ctx context.Context, ch *model.Channel, userID, fromPlaylistID string) (*State, error) {
	if err := s.requireInCall(ch, userID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.st.ActivePlaybackSession(ctx, ch.ID); err == nil {
		return nil, fail(409, "a listening session is already running here; join it instead")
	}
	var queue *model.Playlist
	var err error
	if fromPlaylistID != "" {
		src, err := s.st.PlaylistByID(ctx, fromPlaylistID)
		if err != nil || src.ChannelID != ch.ID {
			return nil, fail(404, "playlist not found")
		}
		queue, err = s.st.ClonePlaylist(ctx, src.ID, userID, ch.ID, "")
	} else {
		queue, err = s.st.CreatePlaylist(ctx, userID, ch.ID, "")
	}
	if err != nil {
		return nil, err
	}
	items, err := s.st.ListPlaylistItems(ctx, queue.ID)
	if err != nil {
		return nil, err
	}
	first := ""
	if len(items) > 0 {
		first = items[0].ID
	}
	ps, err := s.st.CreatePlaybackSession(ctx, ch.ID, userID, queue.ID, first)
	if err != nil {
		return nil, err
	}
	if err := s.st.JoinPlayback(ctx, ps.ID, userID); err != nil {
		return nil, err
	}
	return s.broadcast(ctx, ch), nil
}

func (s *Service) Join(ctx context.Context, ch *model.Channel, userID string) (*State, error) {
	if err := s.requireInCall(ch, userID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, err
	}
	if err := s.st.JoinPlayback(ctx, ps.ID, userID); err != nil {
		return nil, err
	}
	return s.broadcast(ctx, ch), nil
}

// Leave removes the user; the host role passes to the longest-tenured
// remaining listener, and the session ends when nobody is left.
func (s *Service) Leave(ctx context.Context, ch *model.Channel, userID string) (*State, error) {
	s.log.Info("listen: leave requested", "user", userID, "channel", ch.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, err
	}
	if err := s.leaveLocked(ctx, ps, userID); err != nil {
		return nil, err
	}
	return s.broadcast(ctx, ch), nil
}

func (s *Service) leaveLocked(ctx context.Context, ps *model.PlaybackSession, userID string) error {
	if err := s.st.LeavePlayback(ctx, ps.ID, userID); err != nil {
		return err
	}
	members, err := s.st.PlaybackMemberIDs(ctx, ps.ID)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return s.endLocked(ctx, ps)
	}
	if ps.HostUserID == userID {
		return s.st.SetPlaybackHost(ctx, ps.ID, members[0])
	}
	return nil
}

func (s *Service) endLocked(ctx context.Context, ps *model.PlaybackSession) error {
	// The ended session keeps referencing its queue playlist (history), so
	// the queue row stays; it is unnamed and never listed.
	return s.st.EndPlaybackSession(ctx, ps.ID)
}

// End stops the session for everyone (host only).
func (s *Service) End(ctx context.Context, ch *model.Channel, userID string) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, err
	}
	if err := s.requireHost(ps, userID); err != nil {
		return nil, err
	}
	if err := s.endLocked(ctx, ps); err != nil {
		return nil, err
	}
	return s.broadcast(ctx, ch), nil
}

// transport applies a host transport command.
func (s *Service) transport(ctx context.Context, ch *model.Channel, userID string, apply func(ps *model.PlaybackSession) error) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, err
	}
	if err := s.requireHost(ps, userID); err != nil {
		return nil, err
	}
	if err := apply(ps); err != nil {
		return nil, err
	}
	if err := s.st.UpdatePlayback(ctx, ps.ID, ps.State, ps.PositionMS, ps.AnchorServerMS, ps.CurrentItemID); err != nil {
		return nil, err
	}
	return s.broadcast(ctx, ch), nil
}

// Play resumes (optionally from an explicit position).
func (s *Service) Play(ctx context.Context, ch *model.Channel, userID string, posMS *int64) (*State, error) {
	return s.transport(ctx, ch, userID, func(ps *model.PlaybackSession) error {
		if ps.CurrentItemID == "" {
			return fail(400, "add something to the queue first")
		}
		if posMS != nil {
			ps.PositionMS = max(0, *posMS)
		} else {
			ps.PositionMS = position(ps)
		}
		ps.AnchorServerMS = nowMS()
		ps.State = "playing"
		return nil
	})
}

func (s *Service) Pause(ctx context.Context, ch *model.Channel, userID string, posMS *int64) (*State, error) {
	return s.transport(ctx, ch, userID, func(ps *model.PlaybackSession) error {
		if posMS != nil {
			ps.PositionMS = max(0, *posMS)
		} else {
			ps.PositionMS = position(ps)
		}
		ps.AnchorServerMS = nowMS()
		ps.State = "paused"
		return nil
	})
}

func (s *Service) Seek(ctx context.Context, ch *model.Channel, userID string, posMS int64) (*State, error) {
	return s.transport(ctx, ch, userID, func(ps *model.PlaybackSession) error {
		ps.PositionMS = max(0, posMS)
		ps.AnchorServerMS = nowMS()
		return nil
	})
}

// Select jumps to a specific queue item and starts it from the top.
func (s *Service) Select(ctx context.Context, ch *model.Channel, userID, itemID string) (*State, error) {
	return s.transport(ctx, ch, userID, func(ps *model.PlaybackSession) error {
		it, err := s.st.PlaylistItemByID(ctx, itemID)
		if err != nil || it.PlaylistID != ps.PlaylistID {
			return fail(404, "that item is not in the queue")
		}
		ps.CurrentItemID = it.ID
		ps.PositionMS = 0
		ps.AnchorServerMS = nowMS()
		ps.State = "playing"
		return nil
	})
}

// Next advances to the following item. fromItemID makes it idempotent: the
// host's player reports "ended" for a specific item, and a stale report must
// not skip a track that was already advanced to.
func (s *Service) Next(ctx context.Context, ch *model.Channel, userID, fromItemID string) (*State, error) {
	return s.transport(ctx, ch, userID, func(ps *model.PlaybackSession) error {
		if fromItemID != "" && ps.CurrentItemID != fromItemID {
			return nil
		}
		items, err := s.st.ListPlaylistItems(ctx, ps.PlaylistID)
		if err != nil {
			return err
		}
		next := ""
		for i, it := range items {
			if it.ID == ps.CurrentItemID && i+1 < len(items) {
				next = items[i+1].ID
				break
			}
		}
		ps.PositionMS = 0
		ps.AnchorServerMS = nowMS()
		if next == "" {
			// End of queue: park at the start of the last item, paused.
			ps.State = "paused"
			return nil
		}
		ps.CurrentItemID = next
		ps.State = "playing"
		return nil
	})
}

func (s *Service) Previous(ctx context.Context, ch *model.Channel, userID string) (*State, error) {
	return s.transport(ctx, ch, userID, func(ps *model.PlaybackSession) error {
		items, err := s.st.ListPlaylistItems(ctx, ps.PlaylistID)
		if err != nil {
			return err
		}
		// Within the first few seconds, go to the previous track; otherwise
		// restart the current one (the convention every player uses).
		if position(ps) < 4000 {
			for i, it := range items {
				if it.ID == ps.CurrentItemID && i > 0 {
					ps.CurrentItemID = items[i-1].ID
					break
				}
			}
		}
		ps.PositionMS = 0
		ps.AnchorServerMS = nowMS()
		ps.State = "playing"
		return nil
	})
}

// AddItem appends a YouTube link or an uploaded attachment to the queue. Any
// listener may add; if nothing is selected yet, the new item becomes current.
func (s *Service) AddItem(ctx context.Context, ch *model.Channel, userID, rawURL, attachmentID string) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, err
	}
	if _, err := s.requireMember(ctx, ps, userID); err != nil {
		return nil, err
	}
	it := &model.PlaylistItem{PlaylistID: ps.PlaylistID, AddedBy: userID}
	switch {
	case attachmentID != "":
		att, err := s.st.AttachmentByID(ctx, attachmentID)
		if err != nil || att.UploaderID != userID {
			return nil, fail(404, "upload not found")
		}
		if !strings.HasPrefix(att.MIME, "audio/") && !strings.HasPrefix(att.MIME, "video/") {
			return nil, fail(400, "only audio and video files can be queued")
		}
		it.Kind = "upload"
		it.Title = strings.TrimSuffix(att.FileName, ext(att.FileName))
		it.DurationMS = att.DurationMS
		it.AttachmentID = att.ID
		it.RefsJSON = `{"upload":"` + att.ID + `","mime":"` + att.MIME + `"}`
	case rawURL != "":
		id, ok := ParseYouTubeID(rawURL)
		if !ok {
			return nil, fail(400, "paste a YouTube link (other services are coming)")
		}
		meta := FetchYouTubeMeta(ctx, id)
		it.Kind = "video"
		it.Title = meta.Title
		it.Artist = meta.Author
		refs, _ := json.Marshal(map[string]string{"youtube": id, "thumbnail": meta.Thumbnail})
		it.RefsJSON = string(refs)
	default:
		return nil, fail(400, "nothing to add")
	}
	if err := s.st.AppendPlaylistItem(ctx, it); err != nil {
		return nil, err
	}
	if ps.CurrentItemID == "" {
		ps.CurrentItemID = it.ID
		ps.PositionMS = 0
		ps.AnchorServerMS = nowMS()
		if err := s.st.UpdatePlayback(ctx, ps.ID, ps.State, ps.PositionMS, ps.AnchorServerMS, ps.CurrentItemID); err != nil {
			return nil, err
		}
	}
	return s.broadcast(ctx, ch), nil
}

// AddEntries appends many YouTube videos at once (a pasted playlist or
// album). Everything is inserted in order and announced with one broadcast.
func (s *Service) AddEntries(ctx context.Context, ch *model.Channel, userID string, entries []PlaylistEntry) (*State, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, 0, err
	}
	if _, err := s.requireMember(ctx, ps, userID); err != nil {
		return nil, 0, err
	}
	added := 0
	for _, e := range entries {
		refs, _ := json.Marshal(map[string]string{"youtube": e.ID, "thumbnail": e.Thumbnail})
		it := &model.PlaylistItem{
			PlaylistID: ps.PlaylistID, AddedBy: userID, Kind: "video",
			Title: e.Title, Artist: e.Author, DurationMS: e.DurationMS, RefsJSON: string(refs),
		}
		if it.Title == "" {
			it.Title = "YouTube video"
		}
		if err := s.st.AppendPlaylistItem(ctx, it); err != nil {
			return nil, added, err
		}
		added++
		if ps.CurrentItemID == "" {
			ps.CurrentItemID = it.ID
			ps.PositionMS = 0
			ps.AnchorServerMS = nowMS()
			if err := s.st.UpdatePlayback(ctx, ps.ID, ps.State, ps.PositionMS, ps.AnchorServerMS, ps.CurrentItemID); err != nil {
				return nil, added, err
			}
		}
	}
	return s.broadcast(ctx, ch), added, nil
}

func ext(name string) string {
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[i:]
	}
	return ""
}

// RemoveItem deletes a queue item (host, or whoever added it).
func (s *Service) RemoveItem(ctx context.Context, ch *model.Channel, userID, itemID string) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, err
	}
	it, err := s.st.PlaylistItemByID(ctx, itemID)
	if err != nil || it.PlaylistID != ps.PlaylistID {
		return nil, fail(404, "that item is not in the queue")
	}
	if ps.HostUserID != userID && it.AddedBy != userID {
		return nil, fail(403, "only the host or whoever added it can remove this")
	}
	if ps.CurrentItemID == it.ID {
		// Move off the item before deleting it.
		items, _ := s.st.ListPlaylistItems(ctx, ps.PlaylistID)
		next := ""
		for i, x := range items {
			if x.ID == it.ID {
				if i+1 < len(items) {
					next = items[i+1].ID
				} else if i > 0 {
					next = items[i-1].ID
				}
			}
		}
		ps.CurrentItemID = next
		ps.PositionMS = 0
		ps.AnchorServerMS = nowMS()
		if next == "" {
			ps.State = "paused"
		}
		if err := s.st.UpdatePlayback(ctx, ps.ID, ps.State, ps.PositionMS, ps.AnchorServerMS, ps.CurrentItemID); err != nil {
			return nil, err
		}
	}
	if err := s.st.DeletePlaylistItem(ctx, it.ID); err != nil {
		return nil, err
	}
	return s.broadcast(ctx, ch), nil
}

func (s *Service) TransferHost(ctx context.Context, ch *model.Channel, userID, toUserID string) (*State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, err
	}
	if err := s.requireHost(ps, userID); err != nil {
		return nil, err
	}
	members, _ := s.st.PlaybackMemberIDs(ctx, ps.ID)
	found := false
	for _, id := range members {
		if id == toUserID {
			found = true
		}
	}
	if !found {
		return nil, fail(400, "they need to be listening to become host")
	}
	if err := s.st.SetPlaybackHost(ctx, ps.ID, toUserID); err != nil {
		return nil, err
	}
	return s.broadcast(ctx, ch), nil
}

// Save stores the current queue as a named playlist for this channel.
func (s *Service) Save(ctx context.Context, ch *model.Channel, userID, name string) (*model.Playlist, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.active(ctx, ch)
	if err != nil {
		return nil, err
	}
	if _, err := s.requireMember(ctx, ps, userID); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return nil, fail(400, "give the playlist a name (up to 80 characters)")
	}
	return s.st.ClonePlaylist(ctx, ps.PlaylistID, userID, ch.ID, name)
}

// ReportDuration lets the first client that learns an item's length record it.
func (s *Service) ReportDuration(ctx context.Context, ch *model.Channel, userID, itemID string, durationMS int64) error {
	ps, err := s.active(ctx, ch)
	if err != nil {
		return err
	}
	if _, err := s.requireMember(ctx, ps, userID); err != nil {
		return err
	}
	it, err := s.st.PlaylistItemByID(ctx, itemID)
	if err != nil || it.PlaylistID != ps.PlaylistID || durationMS <= 0 {
		return fail(400, "bad duration report")
	}
	if it.DurationMS > 0 {
		return nil
	}
	if err := s.st.SetPlaylistItemDuration(ctx, it.ID, durationMS); err != nil {
		return err
	}
	s.broadcast(ctx, ch)
	return nil
}

// OnUserOffline schedules removal of a disconnected user from any session
// they were in, unless they reconnect within the grace period.
func (s *Service) OnUserOffline(userID string) {
	s.log.Info("listen: user offline, grace timer started", "user", userID)
	time.AfterFunc(offlineGrace, func() {
		if s.pub.IsOnline(userID) {
			s.log.Info("listen: user back online, keeping seat", "user", userID)
			return
		}
		s.log.Info("listen: grace expired, removing", "user", userID)
		s.removeEverywhere(userID)
	})
}

func (s *Service) removeEverywhere(userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	ids, err := s.st.SessionsForUser(ctx, userID)
	if err != nil {
		return
	}
	for _, sid := range ids {
		ps, err := s.st.PlaybackSessionByID(ctx, sid)
		if err != nil {
			continue
		}
		if err := s.leaveLocked(ctx, ps, userID); err != nil {
			s.log.Error("listen offline leave", "err", err)
			continue
		}
		if ch, err := s.st.ChannelByID(ctx, ps.ChannelID); err == nil {
			s.broadcast(ctx, ch)
		}
	}
}

// ActiveListeningFor returns sync.state payloads for every live session in a
// channel the user can see, so a fresh client renders them immediately.
func (s *Service) ActiveListeningFor(ctx context.Context, userID string) []any {
	sessions, err := s.st.ActivePlaybackSessions(ctx)
	if err != nil {
		return nil
	}
	var out []any
	for _, ps := range sessions {
		ch, err := s.st.ChannelByID(ctx, ps.ChannelID)
		if err != nil {
			continue
		}
		if ok, err := s.st.IsChannelMember(ctx, ch, userID); err != nil || !ok {
			continue
		}
		if st, err := s.State(ctx, ch); err == nil && st.Session != nil {
			out = append(out, st)
		}
	}
	return out
}
