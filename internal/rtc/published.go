package rtc

import (
	"errors"
	"io"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// layerInactiveAfter: no packet for this long and the layer is treated as
// paused (browsers stop low layers under CPU pressure, and start them late).
const layerInactiveAfter = time.Second

// PublishedTrack is one logical uplink track (a person's mic, camera, or
// screen) with its simulcast layers. Audio is forwarded through one shared
// local track; video goes through a per-subscriber downtrack that picks a
// layer.
type PublishedTrack struct {
	uid     string
	owner   *Peer
	kind    string
	isVideo bool
	mime    string
	codec   webrtc.RTPCodecCapability

	local *webrtc.TrackLocalStaticRTP // audio only (shared)

	mu     sync.RWMutex
	layers map[string]*layer // rid -> layer ("" when not simulcast)
	subs   []*downtrack

	pliMu   sync.Mutex
	lastPLI map[string]time.Time
}

type layer struct {
	rid    string
	remote *webrtc.TrackRemote
	ssrc   uint32
	meter  bitrateMeter
	active atomic.Bool
}

// layerRank orders simulcast rids from smallest to largest.
func layerRank(rid string) int {
	switch rid {
	case "q":
		return 0
	case "h":
		return 1
	case "f":
		return 2
	}
	return 1
}

type layerInfo struct {
	RID string `json:"rid"`
	Bps int    `json:"bps"`
}

func newPublishedTrack(uid string, owner *Peer, kind string, remote *webrtc.TrackRemote) (*PublishedTrack, error) {
	pt := &PublishedTrack{
		uid: uid, owner: owner, kind: kind,
		isVideo: remote.Kind() == webrtc.RTPCodecTypeVideo,
		mime:    remote.Codec().MimeType,
		codec:   remote.Codec().RTPCodecCapability,
		layers:  map[string]*layer{},
		lastPLI: map[string]time.Time{},
	}
	if !pt.isVideo {
		local, err := webrtc.NewTrackLocalStaticRTP(pt.codec, uid, owner.userID+"|"+kind)
		if err != nil {
			return nil, err
		}
		pt.local = local
	}
	return pt, nil
}

// addLayer registers a simulcast layer (or the single layer of a plain track).
func (pt *PublishedTrack) addLayer(remote *webrtc.TrackRemote) *layer {
	l := &layer{rid: remote.RID(), remote: remote, ssrc: uint32(remote.SSRC())}
	pt.mu.Lock()
	pt.layers[l.rid] = l
	pt.mu.Unlock()
	return l
}

// removeLayer forgets a layer whose reader ended. It only removes the entry
// if it still points at this remote (a replacement may have arrived) and
// reports whether no layers remain.
func (pt *PublishedTrack) removeLayer(l *layer) bool {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if cur, ok := pt.layers[l.rid]; ok && cur == l {
		delete(pt.layers, l.rid)
	}
	return len(pt.layers) == 0
}

func (pt *PublishedTrack) singleLayer() bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	_, ok := pt.layers[""]
	return ok && len(pt.layers) == 1
}

// activeLayers lists layers currently receiving packets, smallest first.
func (pt *PublishedTrack) activeLayers(now time.Time) []layerInfo {
	pt.mu.RLock()
	out := make([]layerInfo, 0, len(pt.layers))
	for _, l := range pt.layers {
		if l.active.Load() {
			out = append(out, layerInfo{RID: l.rid, Bps: l.meter.bps(now)})
		}
	}
	pt.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if layerRank(out[i].RID) != layerRank(out[j].RID) {
			return layerRank(out[i].RID) < layerRank(out[j].RID)
		}
		return out[i].Bps < out[j].Bps
	})
	return out
}

func (pt *PublishedTrack) addSub(dt *downtrack) {
	pt.mu.Lock()
	pt.subs = append(pt.subs, dt)
	pt.mu.Unlock()
}

func (pt *PublishedTrack) removeSub(dt *downtrack) {
	pt.mu.Lock()
	for i, s := range pt.subs {
		if s == dt {
			pt.subs = append(pt.subs[:i:i], pt.subs[i+1:]...)
			break
		}
	}
	pt.mu.Unlock()
}

// requestKeyframe asks the publisher for a keyframe on one layer, at most
// every 300 ms per layer.
func (pt *PublishedTrack) requestKeyframe(rid string) {
	pt.mu.RLock()
	l := pt.layers[rid]
	pt.mu.RUnlock()
	if l == nil {
		return
	}
	pt.pliMu.Lock()
	if time.Since(pt.lastPLI[rid]) < 300*time.Millisecond {
		pt.pliMu.Unlock()
		return
	}
	pt.lastPLI[rid] = time.Now()
	pt.pliMu.Unlock()
	_ = pt.owner.pub.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: l.ssrc}})
}

// layerBps reports a layer's measured bitrate (0 when unknown).
func (pt *PublishedTrack) layerBps(rid string, now time.Time) int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	if l := pt.layers[rid]; l != nil {
		return l.meter.bps(now)
	}
	return 0
}

// runVideoLayer reads one layer until it ends and fans packets out to every
// subscriber's downtrack. onActive runs when the layer (re)starts flowing;
// onEnd runs when the uplink is gone.
func (pt *PublishedTrack) runVideoLayer(l *layer, onActive, onEnd func()) {
	defer onEnd()
	for {
		_ = l.remote.SetReadDeadline(time.Now().Add(layerInactiveAfter))
		pkt, _, err := l.remote.ReadRTP()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				l.active.Store(false)
				continue
			}
			return // EOF / closed: the track is gone
		}
		now := time.Now()
		if !l.active.Swap(true) {
			go onActive()
		}
		l.meter.add(len(pkt.Payload)+12, now)
		pt.mu.RLock()
		subs := pt.subs
		pt.mu.RUnlock()
		for _, dt := range subs {
			dt.push(l.rid, pkt, now)
		}
	}
}

// runAudio copies an audio uplink into the shared local track.
func (pt *PublishedTrack) runAudio(remote *webrtc.TrackRemote, onEnd func()) {
	defer onEnd()
	buf := make([]byte, 1500)
	for {
		n, _, err := remote.Read(buf)
		if err != nil {
			return
		}
		if _, err := pt.local.Write(buf[:n]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			continue // no subscribers yet
		}
	}
}
