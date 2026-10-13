package rtc

import (
	"time"

	"github.com/pion/rtp"
)

// seqTsRewriter makes packets from different simulcast layers look like one
// continuous RTP stream to the receiver: sequence numbers continue across a
// switch and timestamps keep advancing by real elapsed time. Within a layer
// the mapping is a constant offset, so reordering and retransmissions still
// map correctly; only switches create new offsets.
type seqTsRewriter struct {
	started    bool
	seqOff     uint16
	tsOff      uint32
	lastOutSeq uint16
	lastOutTs  uint32
	lastWall   time.Time
}

const videoClockRate = 90000

// onSwitch is called with the first packet (a keyframe) of a new layer.
func (r *seqTsRewriter) onSwitch(h *rtp.Header, now time.Time) {
	if !r.started {
		r.started = true
		r.seqOff, r.tsOff = 0, 0
		r.lastOutSeq = h.SequenceNumber - 1
		r.lastOutTs = h.Timestamp
		r.lastWall = now
		return
	}
	r.seqOff = (r.lastOutSeq + 1) - h.SequenceNumber
	elapsed := now.Sub(r.lastWall).Seconds() * videoClockRate
	delta := uint32(elapsed)
	if delta < 1 {
		delta = 1
	}
	if delta > videoClockRate/2 {
		delta = videoClockRate / 2
	}
	r.tsOff = (r.lastOutTs + delta) - h.Timestamp
}

// mapHeader rewrites the header in place.
func (r *seqTsRewriter) mapHeader(h *rtp.Header, now time.Time) {
	h.SequenceNumber += r.seqOff
	h.Timestamp += r.tsOff
	// Track the newest packet emitted; tolerate in-layer reordering.
	if int16(h.SequenceNumber-r.lastOutSeq) > 0 {
		r.lastOutSeq = h.SequenceNumber
		r.lastOutTs = h.Timestamp
		r.lastWall = now
	}
}
