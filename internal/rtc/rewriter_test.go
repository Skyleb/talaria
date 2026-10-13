package rtc

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

func hdr(seq uint16, ts uint32) rtp.Header { return rtp.Header{SequenceNumber: seq, Timestamp: ts} }

func TestRewriterFirstPacketMapsToItself(t *testing.T) {
	var r seqTsRewriter
	now := time.Now()
	h := hdr(1000, 90000)
	r.onSwitch(&h, now)
	r.mapHeader(&h, now)
	if h.SequenceNumber != 1000 || h.Timestamp != 90000 {
		t.Fatalf("first packet changed: %+v", h)
	}
}

func TestRewriterContinuityAcrossSwitchWithWrap(t *testing.T) {
	var r seqTsRewriter
	now := time.Now()
	// Layer A ends at seq 65535; layer B starts at seq 100.
	for i, seq := range []uint16{65533, 65534, 65535} {
		h := hdr(seq, uint32(1000+i*3000))
		if i == 0 {
			r.onSwitch(&h, now)
		}
		r.mapHeader(&h, now.Add(time.Duration(i)*33*time.Millisecond))
		if h.SequenceNumber != seq {
			t.Fatalf("in-layer seq changed: %d -> %d", seq, h.SequenceNumber)
		}
	}
	later := now.Add(100 * time.Millisecond)
	h := hdr(100, 777777)
	r.onSwitch(&h, later)
	r.mapHeader(&h, later)
	if h.SequenceNumber != 0 {
		t.Fatalf("seq after wrap = %d, want 0", h.SequenceNumber)
	}
	// Timestamp advanced by the real elapsed time (~33 ms since the last
	// packet at +66 ms), never backwards.
	delta := int32(h.Timestamp - 7000)
	if delta < 1 || delta > videoClockRate/2 {
		t.Fatalf("ts delta %d out of range", delta)
	}
	// Next packet of layer B keeps the same offsets.
	h2 := hdr(101, 777777+3000)
	r.mapHeader(&h2, later.Add(33*time.Millisecond))
	if h2.SequenceNumber != 1 || h2.Timestamp != h.Timestamp+3000 {
		t.Fatalf("second packet of new layer: %+v (first ts %d)", h2, h.Timestamp)
	}
}

func TestRewriterTimestampWrap(t *testing.T) {
	var r seqTsRewriter
	now := time.Now()
	var base uint32 = 0xFFFFFF00
	h := hdr(10, base)
	r.onSwitch(&h, now)
	r.mapHeader(&h, now)
	h2 := hdr(11, base+3000) // wraps past 2^32
	r.mapHeader(&h2, now.Add(33*time.Millisecond))
	if h2.Timestamp != base+3000 {
		t.Fatalf("in-layer wrap altered ts: %d", h2.Timestamp)
	}
	if h2.SequenceNumber != 11 {
		t.Fatalf("seq %d", h2.SequenceNumber)
	}
}

func TestRewriterInLayerReorderKeepsMapping(t *testing.T) {
	var r seqTsRewriter
	now := time.Now()
	first := hdr(50, 100)
	r.onSwitch(&first, now)
	r.mapHeader(&first, now)
	// 52 arrives before 51.
	a := hdr(52, 6100)
	r.mapHeader(&a, now.Add(60*time.Millisecond))
	b := hdr(51, 3100)
	r.mapHeader(&b, now.Add(61*time.Millisecond))
	if a.SequenceNumber != 52 || b.SequenceNumber != 51 || a.Timestamp != 6100 || b.Timestamp != 3100 {
		t.Fatalf("reorder broke mapping: %+v %+v", a, b)
	}
	if r.lastOutSeq != 52 {
		t.Fatalf("lastOutSeq %d, want 52 (newest, not latest)", r.lastOutSeq)
	}
}

func TestRewriterSwitchClampsLongGap(t *testing.T) {
	var r seqTsRewriter
	now := time.Now()
	h := hdr(1, 1000)
	r.onSwitch(&h, now)
	r.mapHeader(&h, now)
	// Ten seconds of nothing, then a new layer: timestamp jumps by at most
	// half a second so the receiver's jitter buffer doesn't stall.
	n := hdr(500, 42)
	r.onSwitch(&n, now.Add(10*time.Second))
	r.mapHeader(&n, now.Add(10*time.Second))
	if n.SequenceNumber != 2 {
		t.Fatalf("seq %d want 2", n.SequenceNumber)
	}
	if d := n.Timestamp - 1000; d != videoClockRate/2 {
		t.Fatalf("ts delta %d, want clamp %d", d, videoClockRate/2)
	}
}
