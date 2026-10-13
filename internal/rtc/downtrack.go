package rtc

import (
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// pendingPLIAfter is how long a downtrack waits for a keyframe of its target
// layer before asking the publisher for one again.
const pendingPLIAfter = 500 * time.Millisecond

// downtrack is one subscriber's copy of one published video track. It
// forwards exactly one simulcast layer at a time, switching only on a
// keyframe of the new layer, and rewrites seq/timestamps so the receiver
// sees a single continuous stream.
type downtrack struct {
	pt     *PublishedTrack
	sub    *Peer
	local  *webrtc.TrackLocalStaticRTP
	sender *webrtc.RTPSender

	mu           sync.Mutex
	current      string // layer being forwarded ("" = none yet)
	target       string // layer wanted ("" = paused)
	pendingSince time.Time
	rw           seqTsRewriter
	bytes        int64
}

func newDowntrack(pt *PublishedTrack, sub *Peer) (*downtrack, error) {
	local, err := webrtc.NewTrackLocalStaticRTP(pt.codec, pt.uid, pt.owner.userID+"|"+pt.kind)
	if err != nil {
		return nil, err
	}
	return &downtrack{pt: pt, sub: sub, local: local}, nil
}

// setTarget picks the layer this subscriber should receive. Paused ("")
// stops forwarding; the receiver keeps its last frame.
func (d *downtrack) setTarget(rid string) {
	d.mu.Lock()
	if rid == d.target {
		d.mu.Unlock()
		return
	}
	d.target = rid
	d.pendingSince = time.Now()
	needKey := rid != "" && rid != d.current
	d.mu.Unlock()
	if needKey {
		d.pt.requestKeyframe(rid)
	}
}

// currentLayer reports the layer being forwarded.
func (d *downtrack) currentLayer() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current
}

// push offers a packet of the given layer; the hot path.
func (d *downtrack) push(rid string, p *rtp.Packet, now time.Time) {
	if len(p.Payload) == 0 {
		return // publisher probing/padding
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.target == "" {
		return
	}
	if rid == d.target {
		if d.current != d.target {
			if !isKeyframe(d.pt.mime, p.Payload) {
				if now.Sub(d.pendingSince) > pendingPLIAfter {
					d.pendingSince = now
					go d.pt.requestKeyframe(rid)
				}
				return
			}
			d.rw.onSwitch(&p.Header, now)
			d.current = d.target
		}
		d.writeLocked(p, now)
		return
	}
	// Not the target: keep the old layer flowing until the new one is
	// ready, drop everything else.
	if rid == d.current {
		d.writeLocked(p, now)
	}
}

func (d *downtrack) writeLocked(p *rtp.Packet, now time.Time) {
	h := p.Header
	// Upstream header extensions mean nothing on this leg; the subscriber
	// side's interceptors add their own.
	h.Extension = false
	h.Extensions = nil
	d.rw.mapHeader(&h, now)
	d.bytes += int64(len(p.Payload))
	_ = d.local.WriteRTP(&rtp.Packet{Header: h, Payload: p.Payload})
}

// drainRTCP reads the subscriber's feedback: a PLI from the viewer becomes
// a PLI for the layer it is watching; REMB is kept for bandwidth estimation.
func (d *downtrack) drainRTCP() {
	for {
		pkts, _, err := d.sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, pkt := range pkts {
			switch v := pkt.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				if cur := d.currentLayer(); cur != "" || d.pt.singleLayer() {
					d.pt.requestKeyframe(cur)
				}
			case *rtcp.ReceiverEstimatedMaximumBitrate:
				d.sub.noteREMB(int(v.Bitrate))
			}
		}
	}
}
