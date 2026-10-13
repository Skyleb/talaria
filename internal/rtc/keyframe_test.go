package rtc

import "testing"

func TestVP8Keyframe(t *testing.T) {
	// Descriptor: X=0, S=1, PID=0 → 0x10; frame tag first byte with P=0 (keyframe).
	key := []byte{0x10, 0x00, 0x11, 0x22}
	if !isKeyframe("video/VP8", key) {
		t.Fatal("expected keyframe")
	}
	// Same but inter frame (P=1).
	inter := []byte{0x10, 0x01, 0x11, 0x22}
	if isKeyframe("video/VP8", inter) {
		t.Fatal("inter frame flagged as key")
	}
	// Not the start of a partition.
	mid := []byte{0x00, 0x00, 0x11}
	if isKeyframe("video/VP8", mid) {
		t.Fatal("non-start packet flagged as key")
	}
	// With extension bytes: X=1,S=1 (0x90); I=1 (0x80); PictureID 7-bit (0x05); then frame tag P=0.
	ext := []byte{0x90, 0x80, 0x05, 0x00, 0xAA}
	if !isKeyframe("video/VP8", ext) {
		t.Fatal("extended descriptor keyframe missed")
	}
}

func TestH264Keyframe(t *testing.T) {
	if !isKeyframe("video/H264", []byte{0x65, 0x88}) { // IDR
		t.Fatal("IDR missed")
	}
	if !isKeyframe("video/H264", []byte{0x67, 0x42}) { // SPS
		t.Fatal("SPS missed")
	}
	if isKeyframe("video/H264", []byte{0x41, 0x9A}) { // non-IDR slice
		t.Fatal("P slice flagged")
	}
	// STAP-A containing a PPS then an SPS.
	stap := []byte{0x78, 0x00, 0x02, 0x68, 0xCE, 0x00, 0x02, 0x67, 0x42}
	if !isKeyframe("video/H264", stap) {
		t.Fatal("STAP-A with SPS missed")
	}
	// FU-A start of IDR vs continuation.
	if !isKeyframe("video/H264", []byte{0x7C, 0x85, 0x00}) {
		t.Fatal("FU-A IDR start missed")
	}
	if isKeyframe("video/H264", []byte{0x7C, 0x05, 0x00}) {
		t.Fatal("FU-A continuation flagged")
	}
}

func TestVP9Keyframe(t *testing.T) {
	// I=0 P=0 L=0 F=0 B=1 E=0 V=0 Z=0 → 0x08
	if !isKeyframe("video/VP9", []byte{0x08, 0x00}) {
		t.Fatal("VP9 keyframe missed")
	}
	// P=1 (inter) B=1 → 0x48
	if isKeyframe("video/VP9", []byte{0x48, 0x00}) {
		t.Fatal("VP9 inter flagged")
	}
}

func TestUnknownCodecNeverKey(t *testing.T) {
	if isKeyframe("video/AV1", []byte{0x10, 0x00}) {
		t.Fatal("AV1 should never be treated as switchable")
	}
}
