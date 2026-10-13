package rtc

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"

	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

const (
	kindMic    = "mic"
	kindCam    = "cam"
	kindScreen = "screen"
)

// Room is one active call, keyed by channel ID.
type Room struct {
	mu       sync.Mutex
	mgr      *Manager
	channel  *model.Channel
	callID   string
	peers    map[string]*Peer // userID -> peer
	closed   bool
	stopTick chan struct{}
}

// Peer is one participant: a publisher PC (client->server media) and a
// subscriber PC (server->client media).
type Peer struct {
	userID string
	room   *Room

	pub *webrtc.PeerConnection
	sub *webrtc.PeerConnection
	// ICE candidates that arrived before the remote description.
	pubPending []webrtc.ICECandidateInit
	subPending []webrtc.ICECandidateInit
	// subDirty: the sub PC needs another offer once signaling is stable.
	subDirty bool

	// streamMeta maps the client's uplink stream IDs to mic|cam|screen.
	streamMeta map[string]string
	published  map[string]*PublishedTrack   // by forwarded track UID
	senders    map[string]*webrtc.RTPSender // forwarded track UID -> sender on sub PC

	muted  bool
	camera bool
	screen bool
	closed bool
}

// PublishedTrack is one uplink track being forwarded to other peers.
type PublishedTrack struct {
	uid     string
	owner   *Peer
	kind    string
	local   *webrtc.TrackLocalStaticRTP
	remote  *webrtc.TrackRemote
	isVideo bool
}

type participantState struct {
	UserID string `json:"user_id"`
	Muted  bool   `json:"muted"`
	Camera bool   `json:"camera"`
	Screen bool   `json:"screen"`
}

func (r *Room) stateLocked() []participantState {
	out := make([]participantState, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, participantState{UserID: p.userID, Muted: p.muted, Camera: p.camera, Screen: p.screen})
	}
	return out
}

// broadcastState publishes call.state to everyone who can see the channel.
func (r *Room) broadcastState() {
	r.mu.Lock()
	state := r.stateLocked()
	r.mu.Unlock()
	r.mgr.publishCallState(r.channel, r.callID, state)
}

// join creates the peer and its PCs. Caller (manager) holds no room lock.
func (r *Room) join(userID string) (*Peer, error) {
	p := &Peer{
		userID:     userID,
		room:       r,
		streamMeta: map[string]string{},
		published:  map[string]*PublishedTrack{},
		senders:    map[string]*webrtc.RTPSender{},
	}

	pub, err := r.mgr.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	sub, err := r.mgr.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		pub.Close()
		return nil, err
	}
	p.pub, p.sub = pub, sub

	pub.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		r.mgr.sendToUser(userID, EvtRTCIce, map[string]any{
			"channel_id": r.channel.ID, "target": "pub", "candidate": c.ToJSON(),
		})
	})
	sub.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		r.mgr.sendToUser(userID, EvtRTCIce, map[string]any{
			"channel_id": r.channel.ID, "target": "sub", "candidate": c.ToJSON(),
		})
	})

	pub.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		r.addPublished(p, remote)
	})

	// Either PC failing means the participant is gone (tab closed, network died).
	// Leave runs on its own goroutine: it closes the PCs, which must not happen
	// from inside their own state callback.
	onState := func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			go r.mgr.Leave(p.userID)
		}
	}
	pub.OnConnectionStateChange(onState)
	sub.OnConnectionStateChange(onState)

	r.mu.Lock()
	r.peers[userID] = p
	r.mu.Unlock()
	return p, nil
}

// addPublished registers an uplink track and fans it out.
func (r *Room) addPublished(p *Peer, remote *webrtc.TrackRemote) {
	kind := p.streamMeta[remote.StreamID()]
	if kind == "" {
		if remote.Kind() == webrtc.RTPCodecTypeAudio {
			kind = kindMic
		} else {
			kind = kindCam
		}
	}
	uid := store.NewID()
	// Downlink stream ID encodes owner and kind: the client parses "user|kind".
	local, err := webrtc.NewTrackLocalStaticRTP(remote.Codec().RTPCodecCapability, uid, p.userID+"|"+kind)
	if err != nil {
		r.mgr.log.Error("create local track", "err", err)
		return
	}
	pt := &PublishedTrack{
		uid: uid, owner: p, kind: kind, local: local, remote: remote,
		isVideo: remote.Kind() == webrtc.RTPCodecTypeVideo,
	}
	r.mu.Lock()
	if p.closed {
		r.mu.Unlock()
		return
	}
	p.published[uid] = pt
	r.syncLocked()
	r.mu.Unlock()
	r.keyframeAll()

	// Forward RTP until the uplink ends.
	go func() {
		buf := make([]byte, 1500)
		for {
			n, _, err := remote.Read(buf)
			if err != nil {
				break
			}
			if _, err := local.Write(buf[:n]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
				// No subscribers yet or transient write issue: keep forwarding.
				continue
			}
		}
		r.mu.Lock()
		delete(p.published, uid)
		r.syncLocked()
		r.mu.Unlock()
	}()
}

// syncLocked reconciles every peer's sub PC with the set of published tracks
// from other peers, then renegotiates where membership changed. Must hold r.mu.
func (r *Room) syncLocked() {
	if r.closed {
		return
	}
	for _, p := range r.peers {
		if p.closed {
			continue
		}
		desired := map[string]*PublishedTrack{}
		for _, other := range r.peers {
			if other == p {
				continue
			}
			for uid, pt := range other.published {
				desired[uid] = pt
			}
		}
		changed := false
		for uid, sender := range p.senders {
			if _, ok := desired[uid]; !ok {
				if err := p.sub.RemoveTrack(sender); err == nil {
					changed = true
				}
				delete(p.senders, uid)
			}
		}
		for uid, pt := range desired {
			if _, ok := p.senders[uid]; ok {
				continue
			}
			sender, err := p.sub.AddTrack(pt.local)
			if err != nil {
				r.mgr.log.Error("add track to subscriber", "err", err)
				continue
			}
			p.senders[uid] = sender
			changed = true
			// Drain subscriber RTCP (required so interceptors run); forward
			// PLI from the viewer straight to the publisher for fast keyframes.
			go r.drainSenderRTCP(pt, sender)
		}
		if changed || p.subDirty {
			r.offerSubLocked(p)
		}
	}
}

// offerSubLocked (re)offers the peer's subscriber PC. Must hold r.mu.
func (r *Room) offerSubLocked(p *Peer) {
	if p.sub.SignalingState() != webrtc.SignalingStateStable {
		p.subDirty = true // re-offer when the pending answer lands
		return
	}
	p.subDirty = false
	offer, err := p.sub.CreateOffer(nil)
	if err != nil {
		r.mgr.log.Error("create sub offer", "err", err)
		return
	}
	if err := p.sub.SetLocalDescription(offer); err != nil {
		r.mgr.log.Error("set sub local description", "err", err)
		return
	}
	r.mgr.sendToUser(p.userID, EvtRTCOffer, map[string]any{
		"channel_id": r.channel.ID, "sdp": p.sub.LocalDescription(),
	})
}

func (r *Room) drainSenderRTCP(pt *PublishedTrack, sender *webrtc.RTPSender) {
	for {
		pkts, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		if !pt.isVideo {
			continue
		}
		for _, pkt := range pkts {
			if _, ok := pkt.(*rtcp.PictureLossIndication); ok {
				pt.owner.pub.WriteRTCP([]rtcp.Packet{
					&rtcp.PictureLossIndication{MediaSSRC: uint32(pt.remote.SSRC())},
				})
				break
			}
		}
	}
}

// keyframeAll asks every publisher for a keyframe (joins/changes).
func (r *Room) keyframeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.peers {
		for _, pt := range p.published {
			if pt.isVideo {
				p.pub.WriteRTCP([]rtcp.Packet{
					&rtcp.PictureLossIndication{MediaSSRC: uint32(pt.remote.SSRC())},
				})
			}
		}
	}
}

// runTicker periodically requests keyframes and retries dirty renegotiations —
// a safety net against lost signaling races.
func (r *Room) runTicker() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.stopTick:
			return
		case <-t.C:
			r.keyframeAll()
			r.mu.Lock()
			r.syncLocked()
			r.mu.Unlock()
		}
	}
}

// removePeer tears down one participant. Returns true when the room is now empty.
func (r *Room) removePeer(userID string) bool {
	r.mu.Lock()
	p, ok := r.peers[userID]
	if !ok {
		r.mu.Unlock()
		return len(r.peers) == 0
	}
	p.closed = true
	delete(r.peers, userID)
	empty := len(r.peers) == 0
	r.syncLocked()
	r.mu.Unlock()

	p.pub.Close()
	p.sub.Close()
	return empty
}

// --- signaling handlers (called by Manager) ---

func (p *Peer) handlePubOffer(sdp webrtc.SessionDescription, meta map[string]string) {
	r := p.room
	r.mu.Lock()
	for k, v := range meta {
		switch v {
		case kindMic, kindCam, kindScreen:
			p.streamMeta[k] = v
		}
	}
	r.mu.Unlock()

	if err := p.pub.SetRemoteDescription(sdp); err != nil {
		r.mgr.log.Error("pub set remote", "err", err)
		return
	}
	r.mu.Lock()
	pending := p.pubPending
	p.pubPending = nil
	r.mu.Unlock()
	for _, c := range pending {
		p.pub.AddICECandidate(c)
	}
	answer, err := p.pub.CreateAnswer(nil)
	if err != nil {
		r.mgr.log.Error("pub answer", "err", err)
		return
	}
	if err := p.pub.SetLocalDescription(answer); err != nil {
		r.mgr.log.Error("pub set local", "err", err)
		return
	}
	r.mgr.sendToUser(p.userID, EvtRTCAnswer, map[string]any{
		"channel_id": r.channel.ID, "sdp": p.pub.LocalDescription(),
	})
}

func (p *Peer) handleSubAnswer(sdp webrtc.SessionDescription) {
	r := p.room
	if err := p.sub.SetRemoteDescription(sdp); err != nil {
		r.mgr.log.Error("sub set remote", "err", err)
		return
	}
	r.mu.Lock()
	pending := p.subPending
	p.subPending = nil
	dirty := p.subDirty
	if dirty {
		r.offerSubLocked(p)
	}
	r.mu.Unlock()
	for _, c := range pending {
		p.sub.AddICECandidate(c)
	}
}

func (p *Peer) handleICE(target string, cand webrtc.ICECandidateInit) {
	r := p.room
	pc := p.pub
	if target == "sub" {
		pc = p.sub
	}
	if pc.RemoteDescription() == nil {
		r.mu.Lock()
		if target == "sub" {
			p.subPending = append(p.subPending, cand)
		} else {
			p.pubPending = append(p.pubPending, cand)
		}
		r.mu.Unlock()
		return
	}
	pc.AddICECandidate(cand)
}

// parseSDP decodes the {type, sdp} JSON the browser produces.
func parseSDP(raw json.RawMessage) (webrtc.SessionDescription, bool) {
	var sd webrtc.SessionDescription
	if err := json.Unmarshal(raw, &sd); err != nil || strings.TrimSpace(sd.SDP) == "" {
		return sd, false
	}
	return sd, true
}
