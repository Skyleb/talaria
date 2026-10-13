package rtc

import (
	"sync"
	"time"
)

// bitrateMeter measures a stream's recent bitrate: 100 ms buckets over a
// 2 s window. Measured rates matter more than nominal caps, since encoders
// send far below their maximum on static scenes.
type bitrateMeter struct {
	mu      sync.Mutex
	bytes   [20]int
	buckets [20]int64 // which 100 ms slot each entry belongs to
}

func (m *bitrateMeter) add(n int, now time.Time) {
	slot := now.UnixMilli() / 100
	i := int(slot % int64(len(m.bytes)))
	m.mu.Lock()
	if m.buckets[i] != slot {
		m.buckets[i] = slot
		m.bytes[i] = 0
	}
	m.bytes[i] += n
	m.mu.Unlock()
}

// bps returns bits per second over the last two seconds.
func (m *bitrateMeter) bps(now time.Time) int {
	slot := now.UnixMilli() / 100
	total := 0
	m.mu.Lock()
	for i := range m.bytes {
		if m.buckets[i] > slot-int64(len(m.bytes)) {
			total += m.bytes[i]
		}
	}
	m.mu.Unlock()
	return total * 8 / 2
}
