package rtc

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

// Event names (server -> client). Command names arrive via the gateway's
// reserved call.* / rtc.* namespaces (see internal/gateway/protocol.go).
const (
	EvtCallState = "call.state"
	EvtCallRing  = "call.ring"
	EvtRTCOffer  = "rtc.offer"
	EvtRTCAnswer = "rtc.answer"
	EvtRTCIce    = "rtc.ice"
)

// Publisher is the slice of the gateway hub the SFU needs (avoids an import cycle).
type Publisher interface {
	PublishToUsers(userIDs []string, t string, payload any)
}

type Manager struct {
	api   *webrtc.API
	store *store.Store
	pub   Publisher
	log   *slog.Logger

	mu       sync.Mutex
	rooms    map[string]*Room // channelID -> room
	userRoom map[string]*Room // userID -> the one room they're in

	// ring, when set, is told about DM calls so offline users can be
	// notified (push) in addition to the gateway call.ring event.
	ring func(channelID, fromUserID string, toUserIDs []string)
}

// SetRingNotifier installs an out-of-band notifier for incoming DM calls.
func (m *Manager) SetRingNotifier(fn func(channelID, fromUserID string, toUserIDs []string)) {
	m.ring = fn
}

func NewManager(st *store.Store, pub Publisher, log *slog.Logger, udpPort int, publicIP string) (*Manager, error) {
	api, err := newEngine(udpPort, publicIP, log)
	if err != nil {
		return nil, err
	}
	// Clean up rows left open by a previous unclean shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := st.EndDanglingCalls(ctx); err != nil {
		log.Error("end dangling calls", "err", err)
	}
	return &Manager{
		api: api, store: st, pub: pub, log: log,
		rooms: map[string]*Room{}, userRoom: map[string]*Room{},
	}, nil
}

func (m *Manager) sendToUser(userID, t string, payload any) {
	m.pub.PublishToUsers([]string{userID}, t, payload)
}

// publishCallState notifies everyone who can see the channel. An empty
// participant list means the call ended.
func (m *Manager) publishCallState(ch *model.Channel, callID string, participants []participantState) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ids, err := m.store.ChannelMemberIDs(ctx, ch)
	if err != nil {
		m.log.Error("call state audience", "err", err)
		return
	}
	m.pub.PublishToUsers(ids, EvtCallState, map[string]any{
		"channel_id": ch.ID, "call_id": callID, "participants": participants,
	})
}

// HandleCommand routes call.* / rtc.* gateway commands. Implements the
// gateway.CallHandler interface.
func (m *Manager) HandleCommand(ctx context.Context, userID, t string, d json.RawMessage) {
	switch t {
	case "call.join":
		var req struct {
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(d, &req) != nil || req.ChannelID == "" {
			return
		}
		if err := m.Join(ctx, userID, req.ChannelID); err != nil {
			m.log.Error("call join", "user", userID, "err", err)
			m.sendToUser(userID, EvtCallState, map[string]any{
				"channel_id": req.ChannelID, "error": "could not join call",
			})
		}

	case "call.leave":
		m.Leave(userID)

	case "call.update":
		var req struct {
			Muted  bool `json:"muted"`
			Camera bool `json:"camera"`
			Screen bool `json:"screen"`
		}
		if json.Unmarshal(d, &req) != nil {
			return
		}
		m.mu.Lock()
		r := m.userRoom[userID]
		m.mu.Unlock()
		if r == nil {
			return
		}
		r.mu.Lock()
		if p := r.peers[userID]; p != nil {
			p.muted, p.camera, p.screen = req.Muted, req.Camera, req.Screen
		}
		r.mu.Unlock()
		r.broadcastState()

	case "rtc.offer":
		var req struct {
			ChannelID string            `json:"channel_id"`
			SDP       json.RawMessage   `json:"sdp"`
			Meta      map[string]string `json:"meta"`
		}
		if json.Unmarshal(d, &req) != nil {
			return
		}
		if p := m.peerFor(userID, req.ChannelID); p != nil {
			if sdp, ok := parseSDP(req.SDP); ok {
				p.handlePubOffer(sdp, req.Meta)
			}
		}

	case "rtc.answer":
		var req struct {
			ChannelID string          `json:"channel_id"`
			SDP       json.RawMessage `json:"sdp"`
		}
		if json.Unmarshal(d, &req) != nil {
			return
		}
		if p := m.peerFor(userID, req.ChannelID); p != nil {
			if sdp, ok := parseSDP(req.SDP); ok {
				p.handleSubAnswer(sdp)
			}
		}

	case "rtc.ice":
		var req struct {
			ChannelID string                  `json:"channel_id"`
			Target    string                  `json:"target"`
			Candidate webrtc.ICECandidateInit `json:"candidate"`
		}
		if json.Unmarshal(d, &req) != nil || req.Candidate.Candidate == "" {
			return
		}
		if p := m.peerFor(userID, req.ChannelID); p != nil {
			p.handleICE(req.Target, req.Candidate)
		}
	}
}

func (m *Manager) peerFor(userID, channelID string) *Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rooms[channelID]
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peers[userID]
}

// Join puts the user in the channel's call, leaving any other call first.
func (m *Manager) Join(ctx context.Context, userID, channelID string) error {
	ch, err := m.store.ChannelByID(ctx, channelID)
	if err != nil {
		return err
	}
	ok, err := m.store.IsChannelMember(ctx, ch, userID)
	if err != nil || !ok {
		return store.ErrNotFound
	}

	m.Leave(userID) // one call at a time

	m.mu.Lock()
	r := m.rooms[channelID]
	created := false
	if r == nil {
		callID, err := m.store.CreateCallSession(ctx, channelID, "sfu", userID)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		r = &Room{
			mgr: m, channel: ch, callID: callID,
			peers: map[string]*Peer{}, stopTick: make(chan struct{}),
		}
		m.rooms[channelID] = r
		created = true
		go r.runTicker()
	}
	m.mu.Unlock()

	p, err := r.join(userID)
	if err != nil {
		if created {
			m.closeRoomIfEmpty(r)
		}
		return err
	}
	_ = p

	m.mu.Lock()
	m.userRoom[userID] = r
	m.mu.Unlock()

	if err := m.store.AddCallParticipant(ctx, r.callID, userID); err != nil {
		m.log.Error("record call participant", "err", err)
	}

	// Ring the other side of a DM when a call starts there.
	if created && ch.Kind == model.ChannelDM {
		if ids, err := m.store.DMParticipantIDs(ctx, ch.ID); err == nil {
			var others []string
			for _, id := range ids {
				if id != userID {
					others = append(others, id)
					m.sendToUser(id, EvtCallRing, map[string]any{
						"channel_id": ch.ID, "from_user_id": userID,
					})
				}
			}
			if m.ring != nil && len(others) > 0 {
				m.ring(ch.ID, userID, others)
			}
		}
	}

	r.broadcastState()
	// Send the new peer a subscriber offer for any already-published tracks.
	r.mu.Lock()
	r.syncLocked()
	r.mu.Unlock()
	r.keyframeAll()
	return nil
}

// Leave removes the user from their current call (idempotent).
func (m *Manager) Leave(userID string) {
	m.mu.Lock()
	r := m.userRoom[userID]
	if r == nil {
		m.mu.Unlock()
		return
	}
	delete(m.userRoom, userID)
	m.mu.Unlock()

	empty := r.removePeer(userID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.store.MarkCallParticipantLeft(ctx, r.callID, userID); err != nil {
		m.log.Error("mark participant left", "err", err)
	}
	if empty {
		m.closeRoom(r)
	}
	r.broadcastState()
}

func (m *Manager) closeRoomIfEmpty(r *Room) {
	r.mu.Lock()
	empty := len(r.peers) == 0
	r.mu.Unlock()
	if empty {
		m.closeRoom(r)
	}
}

func (m *Manager) closeRoom(r *Room) {
	m.mu.Lock()
	if m.rooms[r.channel.ID] == r {
		delete(m.rooms, r.channel.ID)
	}
	m.mu.Unlock()
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.stopTick)
	}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.store.EndCallSession(ctx, r.callID); err != nil {
		m.log.Error("end call session", "err", err)
	}
}

// OnUserOffline implements gateway.CallHandler: a user with no gateway
// connections left is removed from their call.
func (m *Manager) OnUserOffline(userID string) {
	m.Leave(userID)
}

// ActiveCallsFor returns call.state payloads for every active call the user
// can see — included in the gateway's ready payload so fresh connections
// render in-progress calls.
func (m *Manager) ActiveCallsFor(ctx context.Context, userID string) []any {
	m.mu.Lock()
	rooms := make([]*Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		rooms = append(rooms, r)
	}
	m.mu.Unlock()

	var out []any
	for _, r := range rooms {
		ok, err := m.store.IsChannelMember(ctx, r.channel, userID)
		if err != nil || !ok {
			continue
		}
		r.mu.Lock()
		state := r.stateLocked()
		r.mu.Unlock()
		out = append(out, map[string]any{
			"channel_id": r.channel.ID, "call_id": r.callID, "participants": state,
		})
	}
	return out
}
