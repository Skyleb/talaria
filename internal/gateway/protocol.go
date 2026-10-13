// Package gateway implements the realtime WebSocket protocol.
//
// Envelope: {"op":"evt"|"cmd"|"hello"|"resume"|"resumed"|"reset"|"ping"|"pong"|"err",
//
//	"t":"message.create", "seq":123, "n":"nonce", "d":{...}}
//
// Event namespaces reserved for later phases (do not reuse):
//
//	Phase 2: call.*, rtc.*   Phase 3: presence.activity   Phase 4: sync.*
package gateway

import "encoding/json"

type Op string

const (
	OpHello   Op = "hello"
	OpResume  Op = "resume"
	OpResumed Op = "resumed"
	OpReset   Op = "reset"
	OpEvt     Op = "evt"
	OpCmd     Op = "cmd"
	OpPing    Op = "ping"
	OpPong    Op = "pong"
	OpErr     Op = "err"
)

// Server -> client event types (Phase 1).
const (
	EvtReady           = "ready"
	EvtMessageCreate   = "message.create"
	EvtMessageUpdate   = "message.update"
	EvtMessageDelete   = "message.delete"
	EvtMessageReaction = "message.reaction"
	EvtChannelCreate   = "channel.create"
	EvtChannelUpdate   = "channel.update"
	EvtChannelDelete   = "channel.delete"
	EvtGroupCreate     = "group.create"
	EvtGroupUpdate     = "group.update"
	EvtGroupDelete     = "group.delete"
	EvtMemberAdd       = "group.member_add"
	EvtMemberRemove    = "group.member_remove"
	EvtDMCreate        = "dm.create"
	EvtFriendRequest   = "friend.request"
	EvtFriendUpdate    = "friend.update"
	EvtUserUpdate      = "user.update"
	EvtTypingStart     = "typing.start"
	EvtReadUpdate      = "read.update"
	EvtPresence        = "presence.update"
)

// Client -> server command types (Phase 1).
const (
	CmdTypingStart = "typing.start"
	CmdReadUpdate  = "read.update"
)

// Reserved names, defined now so phases 2-4 slot in without renames.
const (
	// Phase 2
	EvtCallRing  = "call.ring"
	EvtCallState = "call.state"
	CmdCallJoin  = "call.join"
	CmdCallLeave = "call.leave"
	CmdRTCOffer  = "rtc.offer"
	CmdRTCAnswer = "rtc.answer"
	CmdRTCICE    = "rtc.ice"
	// Phase 3
	EvtPresenceActivity = "presence.activity"
	// Phase 4 — every playback event carries {position_ms, anchor_server_ms, item_id}
	EvtSyncState   = "sync.state"
	CmdSyncPlay    = "sync.play"
	CmdSyncPause   = "sync.pause"
	CmdSyncSeek    = "sync.seek"
	CmdSyncItem    = "sync.item_change"
	CmdSyncQueue   = "sync.queue_update"
	CmdSyncHostXfr = "sync.host_transfer"
)

type Frame struct {
	Op  Op              `json:"op"`
	T   string          `json:"t,omitempty"`
	Seq int64           `json:"seq,omitempty"`
	N   string          `json:"n,omitempty"`
	D   json.RawMessage `json:"d,omitempty"`
}

func evt(t string, seq int64, payload any) ([]byte, error) {
	d, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Frame{Op: OpEvt, T: t, Seq: seq, D: d})
}

type helloPayload struct {
	ResumeID     string `json:"session_resume_id"`
	HeartbeatMS  int64  `json:"heartbeat_ms"`
	ServerTimeMS int64  `json:"server_time_ms"`
}

type pingPayload struct {
	C int64 `json:"c"` // client send time (client clock, ms)
}

type pongPayload struct {
	C int64 `json:"c"` // echoed
	S int64 `json:"s"` // server time at receipt (ms) — client derives clock offset
}
