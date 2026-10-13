package rtc

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor/pkg/cc"
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
	published  map[string]*PublishedTrack // uid -> every track this peer publishes
	byKind     map[string]*PublishedTrack // "kind/video"|"kind/audio" -> track (groups simulcast layers)

	// What this peer receives from others.
	audioSenders map[string]*webrtc.RTPSender // uid -> sender for a shared audio track
	downtracks   map[string]*downtrack        // uid -> per-subscriber video track
	prefs        map[string]subPref           // "user|kind" -> viewer's quality preference
	hyst         map[string]*hystState        // "user|kind" -> layer switching history
	lastLayers   string                       // last rtc.layers payload sent (JSON)
	bwe          cc.BandwidthEstimator        // downlink estimate from the viewer's TWCC feedback
	videoSince   time.Time                    // when video first flowed to this viewer
	allocPending atomic.Bool
	rembBps      int
	rembAt       time.Time

	muted  bool
	camera bool
	screen bool
	closed bool
}

// subPref is a viewer's wish for one stream: quality auto|q|h|f|off and a
// priority (2 focused, 1 visible, 0 hidden).
type subPref struct {
	Quality  string `json:"quality"`
	Priority int    `json:"priority"`
}

func (p *Peer) noteREMB(bps int) {
	p.room.mu.Lock()
	p.rembBps, p.rembAt = bps, time.Now()
	p.room.mu.Unlock()
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
		userID:       userID,
		room:         r,
		streamMeta:   map[string]string{},
		published:    map[string]*PublishedTrack{},
		byKind:       map[string]*PublishedTrack{},
		audioSenders: map[string]*webrtc.RTPSender{},
		downtracks:   map[string]*downtrack{},
		prefs:        map[string]subPref{},
		hyst:         map[string]*hystState{},
	}
	if err := r.attachPCs(p); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.peers[userID] = p
	r.mu.Unlock()
	return p, nil
}

// rejoin gives an existing participant fresh PeerConnections (their old ones
// died with a network change) without them ever leaving the call: membership,
// listening seat and preferences all survive. A no-op when both PCs are
// healthy, so a reconnecting client can call it freely.
func (r *Room) rejoin(p *Peer) error {
	r.mu.Lock()
	if p.closed {
		r.mu.Unlock()
		return nil
	}
	healthy := p.pub.ConnectionState() == webrtc.PeerConnectionStateConnected &&
		p.sub.ConnectionState() == webrtc.PeerConnectionStateConnected
	if healthy {
		r.mu.Unlock()
		return nil
	}
	oldPub, oldSub := p.pub, p.sub
	// Forget everything bound to the old PCs; the readers of the old uplink
	// tracks end when the PC closes and remove the published tracks.
	for _, dt := range p.downtracks {
		dt.pt.removeSub(dt)
	}
	p.downtracks = map[string]*downtrack{}
	p.audioSenders = map[string]*webrtc.RTPSender{}
	p.pubPending, p.subPending = nil, nil
	p.subDirty = false
	p.lastLayers = ""
	p.videoSince = time.Time{}
	p.hyst = map[string]*hystState{}
	err := r.attachPCs(p)
	r.mu.Unlock()
	oldPub.Close()
	oldSub.Close()
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.syncLocked()
	r.mu.Unlock()
	return nil
}

// attachPCs creates the publisher and subscriber PCs for p and wires their
// callbacks. Callbacks compare against the peer's *current* PCs so a PC that
// was deliberately replaced cannot end the call when it closes.
func (r *Room) attachPCs(p *Peer) error {
	pub, err := r.mgr.eng.pubAPI.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	sub, bwe, err := r.mgr.eng.newSubPC()
	if err != nil {
		pub.Close()
		return err
	}
	p.pub, p.sub, p.bwe = pub, sub, bwe
	userID := p.userID
	if bwe != nil {
		// Re-pick layers soon after the viewer's estimate moves (debounced;
		// the ticker also re-allocates every second).
		bwe.OnTargetBitrateChange(func(int) {
			if p.allocPending.CompareAndSwap(false, true) {
				time.AfterFunc(250*time.Millisecond, func() {
					p.allocPending.Store(false)
					r.mu.Lock()
					if !p.closed {
						r.allocateLocked(p)
						r.publishLayersLocked(p)
					}
					r.mu.Unlock()
				})
			}
		})
	}

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

	// A current PC failing or closing means the participant is gone (tab
	// closed, network died). A replaced PC closing is expected and ignored.
	// Leave runs on its own goroutine: it closes the PCs, which must not
	// happen from inside their own state callback.
	watch := func(pc *webrtc.PeerConnection, isPub bool) func(webrtc.PeerConnectionState) {
		return func(s webrtc.PeerConnectionState) {
			if s != webrtc.PeerConnectionStateFailed && s != webrtc.PeerConnectionStateClosed {
				return
			}
			r.mu.Lock()
			current := (isPub && p.pub == pc) || (!isPub && p.sub == pc)
			closed := p.closed
			r.mu.Unlock()
			if current && !closed {
				go r.mgr.Leave(p.userID)
			}
		}
	}
	pub.OnConnectionStateChange(watch(pub, true))
	sub.OnConnectionStateChange(watch(sub, false))
	return nil
}

// addPublished registers an uplink track (or one simulcast layer of one)
// and fans it out. pion calls OnTrack once per rid, so the layers of a
// camera arrive as separate tracks sharing the stream id.
func (r *Room) addPublished(p *Peer, remote *webrtc.TrackRemote) {
	kind := p.streamMeta[remote.StreamID()]
	if kind == "" {
		if remote.Kind() == webrtc.RTPCodecTypeAudio {
			kind = kindMic
		} else {
			kind = kindCam
		}
	}
	isVideo := remote.Kind() == webrtc.RTPCodecTypeVideo
	gk := kind + "/audio"
	if isVideo {
		gk = kind + "/video"
	}

	r.mu.Lock()
	if p.closed {
		r.mu.Unlock()
		return
	}
	pt := p.byKind[gk]
	if pt == nil || !isVideo {
		npt, err := newPublishedTrack(store.NewID(), p, kind, remote)
		if err != nil {
			r.mu.Unlock()
			r.mgr.log.Error("create published track", "err", err)
			return
		}
		pt = npt
		p.published[pt.uid] = pt
		p.byKind[gk] = pt
		r.syncLocked()
	}
	r.mu.Unlock()

	// end forgets the track once its uplink is gone (the pointer checks guard
	// against a replacement that arrived in the meantime).
	end := func() {
		r.mu.Lock()
		if p.published[pt.uid] == pt {
			delete(p.published, pt.uid)
		}
		if p.byKind[gk] == pt {
			delete(p.byKind, gk)
		}
		r.syncLocked()
		r.mu.Unlock()
	}
	if !isVideo {
		go pt.runAudio(remote, end)
		return
	}
	l := pt.addLayer(remote)
	go pt.runVideoLayer(l, r.allocateAll, func() {
		if pt.removeLayer(l) {
			end()
		} else {
			r.allocateAll()
		}
	})
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
		for uid, sender := range p.audioSenders {
			if _, ok := desired[uid]; !ok {
				if err := p.sub.RemoveTrack(sender); err == nil {
					changed = true
				}
				delete(p.audioSenders, uid)
			}
		}
		for uid, dt := range p.downtracks {
			if _, ok := desired[uid]; !ok {
				if err := p.sub.RemoveTrack(dt.sender); err == nil {
					changed = true
				}
				dt.pt.removeSub(dt)
				delete(p.downtracks, uid)
			}
		}
		for uid, pt := range desired {
			if pt.isVideo {
				if _, ok := p.downtracks[uid]; ok {
					continue
				}
				dt, err := newDowntrack(pt, p)
				if err != nil {
					r.mgr.log.Error("create downtrack", "err", err)
					continue
				}
				sender, err := p.sub.AddTrack(dt.local)
				if err != nil {
					r.mgr.log.Error("add video track to subscriber", "err", err)
					continue
				}
				dt.sender = sender
				pt.addSub(dt)
				p.downtracks[uid] = dt
				changed = true
				go dt.drainRTCP()
				continue
			}
			if _, ok := p.audioSenders[uid]; ok {
				continue
			}
			sender, err := p.sub.AddTrack(pt.local)
			if err != nil {
				r.mgr.log.Error("add audio track to subscriber", "err", err)
				continue
			}
			p.audioSenders[uid] = sender
			changed = true
			go drainAudioRTCP(sender)
		}
		if changed || p.subDirty {
			r.offerSubLocked(p)
		}
		r.allocateLocked(p)
	}
}

// drainAudioRTCP reads (and ignores) subscriber feedback for a shared audio
// track; reading is required so the interceptors keep running.
func drainAudioRTCP(sender *webrtc.RTPSender) {
	for {
		if _, _, err := sender.ReadRTCP(); err != nil {
			return
		}
	}
}

// allocateLocked picks the layer each of p's downtracks should carry from
// the viewer's preferences and bandwidth budget (see allocator.go). Must
// hold r.mu.
func (r *Room) allocateLocked(p *Peer) {
	now := time.Now()
	if len(p.downtracks) == 0 {
		return
	}
	if p.videoSince.IsZero() {
		p.videoSince = now
	}
	reqs := make([]trackReq, 0, len(p.downtracks))
	byKey := map[string]*downtrack{}
	for _, dt := range p.downtracks {
		key := dt.pt.owner.userID + "|" + dt.pt.kind
		byKey[key] = dt
		pref := p.prefs[key]
		var layers []layerOpt
		for _, l := range dt.pt.activeLayers(now) {
			bps := l.Bps
			if bps <= 0 {
				bps = nominalBps(dt.pt.kind, l.RID)
			}
			layers = append(layers, layerOpt{RID: l.RID, Bps: bps})
		}
		reqs = append(reqs, trackReq{Key: key, Kind: dt.pt.kind, Layers: layers, Pref: pref.Quality, Priority: pref.Priority})
	}
	budget := r.budgetLocked(p, now)
	targets := allocate(budget, 64_000*len(p.audioSenders), reqs, p.hyst, now)
	for key, rid := range targets {
		byKey[key].setTarget(rid)
	}
}

// budgetLocked is the viewer's downlink estimate: TWCC-driven GCC once video
// has flowed for a couple of seconds, REMB from browsers that send it as a
// fallback, else a 1 Mbps opening assumption.
func (r *Room) budgetLocked(p *Peer, now time.Time) int {
	if p.bwe != nil && now.Sub(p.videoSince) > 2*time.Second {
		if t := p.bwe.GetTargetBitrate(); t > 0 {
			return t
		}
	}
	if p.rembBps > 0 && now.Sub(p.rembAt) < 3*time.Second {
		return p.rembBps
	}
	return 1_000_000
}

// nominalBps is the expected rate of a layer that has not been measured yet.
func nominalBps(kind, rid string) int {
	if kind == kindScreen {
		if rid == "h" {
			return 500_000
		}
		return 2_000_000
	}
	switch rid {
	case "q":
		return 150_000
	case "h":
		return 500_000
	}
	return 1_200_000
}

func (r *Room) allocateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.peers {
		if !p.closed {
			r.allocateLocked(p)
		}
	}
}

// handleSubscribe stores a viewer's per-stream preferences and re-allocates.
func (p *Peer) handleSubscribe(streams map[string]subPref) {
	r := p.room
	r.mu.Lock()
	p.prefs = streams
	if p.prefs == nil {
		p.prefs = map[string]subPref{}
	}
	r.allocateLocked(p)
	r.publishLayersLocked(p)
	r.mu.Unlock()
}

// publishLayersLocked tells the viewer which layer each stream is on, when
// that changed. Must hold r.mu.
func (r *Room) publishLayersLocked(p *Peer) {
	now := time.Now()
	streams := map[string]layerInfo{}
	for _, dt := range p.downtracks {
		rid := dt.currentLayer()
		streams[dt.pt.owner.userID+"|"+dt.pt.kind] = layerInfo{RID: rid, Bps: dt.pt.layerBps(rid, now)}
	}
	raw, _ := json.Marshal(streams)
	if string(raw) == p.lastLayers {
		return
	}
	p.lastLayers = string(raw)
	r.mgr.sendToUser(p.userID, EvtRTCLayers, map[string]any{"channel_id": r.channel.ID, "streams": streams})
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

// runTicker is the safety net: it retries dirty renegotiations, re-picks
// layers as they appear and disappear, and reports layer changes.
func (r *Room) runTicker() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.stopTick:
			return
		case <-t.C:
			r.mu.Lock()
			r.syncLocked()
			for _, p := range r.peers {
				if !p.closed {
					r.publishLayersLocked(p)
				}
			}
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
	for _, dt := range p.downtracks {
		dt.pt.removeSub(dt)
	}
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
	if p.subDirty {
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
