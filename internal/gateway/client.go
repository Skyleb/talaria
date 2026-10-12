package gateway

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/skyle/lantern/internal/model"
)

// Client is one WebSocket connection.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	userID   string
	resumeID string
	session  *resumeSession

	out    chan []byte
	closed chan struct{}
}

func newResumeID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ServeWS upgrades the request (already authenticated; userID supplied by caller).
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, userID string) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Same-origin is enforced by cookie SameSite; the embedded client is
		// same-origin. Dev mode uses the Vite proxy (same origin to the browser).
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	c := &Client{
		hub: h, conn: conn, userID: userID,
		resumeID: newResumeID(),
		out:      make(chan []byte, 256),
		closed:   make(chan struct{}),
	}
	go c.writePump()
	c.run(r.Context())
}

func (c *Client) send(data []byte) {
	select {
	case c.out <- data:
	default:
		// Slow consumer: drop the connection; client reconnects and resumes.
		c.closeAsync("send buffer full")
	}
}

func (c *Client) closeAsync(reason string) {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	go c.conn.Close(websocket.StatusPolicyViolation, reason)
}

func (c *Client) writePump() {
	for {
		select {
		case data := <-c.out:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := c.conn.Write(ctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		case <-c.closed:
			return
		}
	}
}

func (c *Client) writeFrame(f Frame) {
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	c.send(data)
}

func (c *Client) run(ctx context.Context) {
	defer func() {
		c.hub.detach(c)
		select {
		case <-c.closed:
		default:
			close(c.closed)
		}
		c.conn.Close(websocket.StatusNormalClosure, "")
	}()

	hello, _ := json.Marshal(helloPayload{
		ResumeID: c.resumeID, HeartbeatMS: heartbeatMS, ServerTimeMS: time.Now().UnixMilli(),
	})
	c.writeFrame(Frame{Op: OpHello, D: hello})

	// First frame must be a resume request (empty resume_id = fresh connect).
	firstCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	var first Frame
	err := readJSON(firstCtx, c.conn, &first)
	cancel()
	if err != nil || first.Op != OpResume {
		return
	}
	var req struct {
		ResumeID string `json:"session_resume_id"`
		LastSeq  int64  `json:"last_seq"`
	}
	json.Unmarshal(first.D, &req)

	replay, resumed := c.hub.attach(c, req.ResumeID, req.LastSeq)
	if resumed {
		// Adopt the resumed session's identity so future resumes keep working.
		c.resumeID = req.ResumeID
		c.writeFrame(Frame{Op: OpResumed})
		for _, data := range replay {
			c.send(data)
		}
	} else {
		if req.ResumeID != "" {
			// Client hoped to resume but the buffer is gone: tell it to refetch.
			c.writeFrame(Frame{Op: OpReset})
		}
		ready, err := c.hub.buildReady(ctx, c.userID)
		if err != nil {
			c.hub.log.Error("build ready", "err", err)
			return
		}
		c.hub.mu.Lock()
		rs := c.session
		rs.seq++
		data, merr := evt(EvtReady, rs.seq, ready)
		if merr == nil {
			rs.ring = append(rs.ring, storedFrame{seq: rs.seq, data: data})
		}
		c.hub.mu.Unlock()
		if merr != nil {
			return
		}
		c.send(data)
	}

	for {
		readCtx, cancel := context.WithTimeout(ctx, 70*time.Second) // ~2.5 missed heartbeats
		var f Frame
		err := readJSON(readCtx, c.conn, &f)
		cancel()
		if err != nil {
			return
		}
		switch f.Op {
		case OpPing:
			var p pingPayload
			json.Unmarshal(f.D, &p)
			pong, _ := json.Marshal(pongPayload{C: p.C, S: time.Now().UnixMilli()})
			c.writeFrame(Frame{Op: OpPong, D: pong})
		case OpCmd:
			cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			c.hub.handleCommand(cmdCtx, c, &f)
			cancel()
		}
	}
}

func readJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// ReadyPayload is the full bootstrap state sent on fresh connects.
type ReadyPayload struct {
	User        *model.User              `json:"user"`
	Groups      []GroupSummary           `json:"groups"`
	DMChannels  []*model.Channel         `json:"dm_channels"`
	Friends     []FriendEntry            `json:"friends"`
	ReadStates  []*model.ReadState       `json:"read_states"`
	LatestIDs   map[string]string        `json:"latest_message_ids"`
	OnlineUsers []string                 `json:"online_user_ids"`
	Users       map[string]model.PublicUser `json:"users"`
	// ActiveCalls carries call.state payloads for in-progress calls the user
	// can see, so a fresh client renders them immediately.
	ActiveCalls []any `json:"active_calls,omitempty"`
}

type GroupSummary struct {
	Group    *model.Group          `json:"group"`
	Channels []*model.Channel      `json:"channels"`
	Members  []*model.GroupMember  `json:"members"`
}

type FriendEntry struct {
	Friendship *model.Friendship `json:"friendship"`
	UserID     string            `json:"user_id"` // the other user
}

func (h *Hub) buildReady(ctx context.Context, userID string) (*ReadyPayload, error) {
	st := h.store
	user, err := st.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	groups, err := st.ListGroupsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	userIDs := map[string]struct{}{userID: {}}
	var channelIDs []string
	summaries := make([]GroupSummary, 0, len(groups))
	for _, g := range groups {
		chs, err := st.ListChannelsByGroup(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		members, err := st.ListGroupMembers(ctx, g.ID)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			userIDs[m.UserID] = struct{}{}
		}
		for _, c := range chs {
			channelIDs = append(channelIDs, c.ID)
		}
		summaries = append(summaries, GroupSummary{Group: g, Channels: chs, Members: members})
	}
	dms, err := st.ListDMChannels(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, c := range dms {
		ids, err := st.DMParticipantIDs(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		channelIDs = append(channelIDs, c.ID)
		for _, id := range ids {
			userIDs[id] = struct{}{}
		}
	}
	friendships, err := st.ListFriendships(ctx, userID)
	if err != nil {
		return nil, err
	}
	friends := make([]FriendEntry, 0, len(friendships))
	for _, f := range friendships {
		other := f.RequesterID
		if other == userID {
			other = f.AddresseeID
		}
		// Hide blocks created by the other side.
		if f.Status == model.FriendBlocked && f.RequesterID != userID {
			continue
		}
		userIDs[other] = struct{}{}
		friends = append(friends, FriendEntry{Friendship: f, UserID: other})
	}
	reads, err := st.ListReadStates(ctx, userID)
	if err != nil {
		return nil, err
	}
	latest, err := st.LatestMessageIDs(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	idList := make([]string, 0, len(userIDs))
	for id := range userIDs {
		idList = append(idList, id)
	}
	users, err := st.PublicUsersByIDs(ctx, idList)
	if err != nil {
		return nil, err
	}
	// Hydrate DM participants for the client.
	for _, c := range dms {
		ids, _ := st.DMParticipantIDs(ctx, c.ID)
		for _, id := range ids {
			if u, ok := users[id]; ok {
				c.Participants = append(c.Participants, u)
			}
		}
	}
	ready := &ReadyPayload{
		User: user, Groups: summaries, DMChannels: dms, Friends: friends,
		ReadStates: reads, LatestIDs: latest,
		OnlineUsers: h.OnlineAmong(idList), Users: users,
	}
	if h.call != nil {
		ready.ActiveCalls = h.call.ActiveCallsFor(ctx, userID)
	}
	return ready, nil
}
