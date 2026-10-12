package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/skyle/lantern/internal/model"
	"github.com/skyle/lantern/internal/store"
)

const (
	heartbeatMS      = 25000
	resumeBufferSize = 512
	resumeRetention  = 5 * time.Minute
	typingExpiryMS   = 10000
)

// Hub is the connection registry and fan-out engine.
type Hub struct {
	store *store.Store
	log   *slog.Logger

	mu       sync.RWMutex
	byUser   map[string]map[*Client]struct{}
	sessions map[string]*resumeSession // resumeID -> buffer

	typingMu sync.Mutex
	typing   map[string]int64 // userID:channelID -> last sent (ms), for throttling
}

type resumeSession struct {
	userID   string
	seq      int64
	ring     []storedFrame // last N marshaled evt frames
	client   *Client       // nil while disconnected
	detachAt time.Time     // when client became nil
}

type storedFrame struct {
	seq  int64
	data []byte
}

func NewHub(st *store.Store, log *slog.Logger) *Hub {
	h := &Hub{
		store:    st,
		log:      log,
		byUser:   map[string]map[*Client]struct{}{},
		sessions: map[string]*resumeSession{},
		typing:   map[string]int64{},
	}
	go h.janitor()
	return h
}

func (h *Hub) janitor() {
	t := time.NewTicker(time.Minute)
	for range t.C {
		h.mu.Lock()
		for id, rs := range h.sessions {
			if rs.client == nil && time.Since(rs.detachAt) > resumeRetention {
				delete(h.sessions, id)
			}
		}
		h.mu.Unlock()
	}
}

// attach registers a client under a (possibly resumed) session.
// Returns replay frames (resume path) or nil (fresh), and whether resume succeeded.
func (h *Hub) attach(c *Client, resumeID string, lastSeq int64) (replay [][]byte, resumed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if resumeID != "" {
		if rs, ok := h.sessions[resumeID]; ok && rs.userID == c.userID {
			// Valid only if we still hold every event after lastSeq.
			oldest := int64(0)
			if len(rs.ring) > 0 {
				oldest = rs.ring[0].seq
			}
			if lastSeq >= oldest-1 && lastSeq <= rs.seq {
				if rs.client != nil {
					rs.client.closeAsync("replaced by resume")
				}
				rs.client = c
				c.session = rs
				for _, f := range rs.ring {
					if f.seq > lastSeq {
						replay = append(replay, f.data)
					}
				}
				h.addConn(c)
				return replay, true
			}
		}
		// Fall through: stale resume -> fresh session (client gets reset).
	}
	rs := &resumeSession{userID: c.userID, client: c}
	h.sessions[c.resumeID] = rs
	c.session = rs
	h.addConn(c)
	return nil, false
}

func (h *Hub) addConn(c *Client) {
	set := h.byUser[c.userID]
	wasOffline := len(set) == 0
	if set == nil {
		set = map[*Client]struct{}{}
		h.byUser[c.userID] = set
	}
	set[c] = struct{}{}
	if wasOffline {
		go h.broadcastPresence(c.userID, "online")
	}
}

func (h *Hub) detach(c *Client) {
	h.mu.Lock()
	if c.session != nil && c.session.client == c {
		c.session.client = nil
		c.session.detachAt = time.Now()
	}
	if set, ok := h.byUser[c.userID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.byUser, c.userID)
			h.mu.Unlock()
			h.broadcastPresence(c.userID, "offline")
			return
		}
	}
	h.mu.Unlock()
}

// IsOnline reports whether a user has any live gateway connection.
func (h *Hub) IsOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byUser[userID]) > 0
}

func (h *Hub) OnlineAmong(ids []string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []string
	for _, id := range ids {
		if len(h.byUser[id]) > 0 {
			out = append(out, id)
		}
	}
	return out
}

// PublishToUsers fans an event out to every connection of the given users.
func (h *Hub) PublishToUsers(userIDs []string, t string, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[string]bool{}
	for _, uid := range userIDs {
		if seen[uid] {
			continue
		}
		seen[uid] = true
		for c := range h.byUser[uid] {
			rs := c.session
			rs.seq++
			data, err := evt(t, rs.seq, payload)
			if err != nil {
				h.log.Error("marshal event", "type", t, "err", err)
				return
			}
			rs.ring = append(rs.ring, storedFrame{seq: rs.seq, data: data})
			if len(rs.ring) > resumeBufferSize {
				rs.ring = rs.ring[len(rs.ring)-resumeBufferSize:]
			}
			c.send(data)
		}
	}
}

// PublishToChannel resolves the channel audience and publishes.
func (h *Hub) PublishToChannel(ctx context.Context, ch *model.Channel, t string, payload any) {
	ids, err := h.store.ChannelMemberIDs(ctx, ch)
	if err != nil {
		h.log.Error("resolve channel audience", "channel", ch.ID, "err", err)
		return
	}
	h.PublishToUsers(ids, t, payload)
}

// broadcastPresence notifies friends and co-group-members of online/offline.
func (h *Hub) broadcastPresence(userID, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ids, err := h.store.VisibleUserIDs(ctx, userID)
	if err != nil {
		h.log.Error("presence audience", "err", err)
		return
	}
	h.PublishToUsers(ids, EvtPresence, map[string]any{"user_id": userID, "status": status})
}

// handleCommand processes a client->server cmd frame.
func (h *Hub) handleCommand(ctx context.Context, c *Client, f *Frame) {
	switch f.T {
	case CmdTypingStart:
		var d struct {
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(f.D, &d) != nil || d.ChannelID == "" {
			return
		}
		// Throttle to one broadcast per 8s per user+channel.
		key := c.userID + ":" + d.ChannelID
		nowMs := time.Now().UnixMilli()
		h.typingMu.Lock()
		last := h.typing[key]
		if nowMs-last < 8000 {
			h.typingMu.Unlock()
			return
		}
		h.typing[key] = nowMs
		h.typingMu.Unlock()

		ch, err := h.store.ChannelByID(ctx, d.ChannelID)
		if err != nil {
			return
		}
		ok, err := h.store.IsChannelMember(ctx, ch, c.userID)
		if err != nil || !ok {
			return
		}
		h.PublishToChannel(ctx, ch, EvtTypingStart, map[string]any{
			"channel_id": d.ChannelID, "user_id": c.userID, "expires_ms": typingExpiryMS,
		})

	case CmdReadUpdate:
		var d struct {
			ChannelID string `json:"channel_id"`
			MessageID string `json:"last_read_message_id"`
		}
		if json.Unmarshal(f.D, &d) != nil || d.ChannelID == "" {
			return
		}
		ch, err := h.store.ChannelByID(ctx, d.ChannelID)
		if err != nil {
			return
		}
		ok, err := h.store.IsChannelMember(ctx, ch, c.userID)
		if err != nil || !ok {
			return
		}
		if err := h.store.UpsertReadState(ctx, c.userID, d.ChannelID, d.MessageID); err != nil {
			return
		}
		// Sync the user's other devices.
		h.PublishToUsers([]string{c.userID}, EvtReadUpdate, map[string]any{
			"channel_id": d.ChannelID, "last_read_message_id": d.MessageID,
		})
	}
}
