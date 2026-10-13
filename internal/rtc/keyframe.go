package rtc

import (
	"strings"

	"github.com/pion/rtp/codecs"
)

// isKeyframe reports whether an RTP payload starts a decodable keyframe.
// Layer switches only happen on keyframes of the new layer, otherwise the
// receiver decodes garbage until the next one.
func isKeyframe(mime string, payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	switch strings.ToLower(mime) {
	case "video/vp8":
		var v codecs.VP8Packet
		frame, err := v.Unmarshal(payload)
		if err != nil || v.S != 1 || v.PID != 0 || len(frame) == 0 {
			return false
		}
		// VP8 frame tag: bit 0 of the first byte is 0 for a keyframe.
		return frame[0]&0x01 == 0
	case "video/vp9":
		var v codecs.VP9Packet
		if _, err := v.Unmarshal(payload); err != nil {
			return false
		}
		return v.B && !v.P && (!v.L || v.SID == 0)
	case "video/h264":
		return h264Keyframe(payload)
	}
	return false
}

func h264Keyframe(payload []byte) bool {
	const (
		naluIDR  = 5
		naluSPS  = 7
		naluSTAP = 24
		naluFUA  = 28
	)
	isKey := func(t byte) bool { return t == naluIDR || t == naluSPS }
	typ := payload[0] & 0x1F
	switch {
	case isKey(typ):
		return true
	case typ == naluSTAP:
		// STAP-A: [hdr][len:2][nalu]...
		off := 1
		for off+2 < len(payload) {
			n := int(payload[off])<<8 | int(payload[off+1])
			off += 2
			if n == 0 || off >= len(payload) {
				break
			}
			if isKey(payload[off] & 0x1F) {
				return true
			}
			off += n
		}
	case typ == naluFUA:
		if len(payload) < 2 {
			return false
		}
		start := payload[1]&0x80 != 0
		return start && isKey(payload[1]&0x1F)
	}
	return false
}
