package rtc

import (
	"testing"
	"time"
)

var cam3 = []layerOpt{{"q", 150_000}, {"h", 500_000}, {"f", 1_500_000}}

func req(key, kind string, layers []layerOpt, pref string, prio int) trackReq {
	return trackReq{Key: key, Kind: kind, Layers: layers, Pref: pref, Priority: prio}
}

func TestAllocateFirstPickFitsBudget(t *testing.T) {
	h := map[string]*hystState{}
	now := time.Now()
	got := allocate(1_000_000, 64_000, []trackReq{req("a|cam", "cam", cam3, "auto", 1)}, h, now)
	if got["a|cam"] != "h" {
		t.Fatalf("first pick %q, want h (fits 936 kbps)", got["a|cam"])
	}
	got = allocate(300_000, 0, []trackReq{req("b|cam", "cam", cam3, "auto", 1)}, h, now)
	if got["b|cam"] != "q" {
		t.Fatalf("tight budget first pick %q, want q", got["b|cam"])
	}
}

func TestAllocateNoSignalStartsHighAndHoldsStill(t *testing.T) {
	h := map[string]*hystState{}
	now := time.Now()
	r := []trackReq{req("a|cam", "cam", cam3, "auto", 1), req("b|cam", "cam", cam3, "auto", 0)}
	// No congestion signal (budget 0): everyone gets the top layer at once.
	got := allocate(0, 64_000, r, h, now)
	if got["a|cam"] != "f" || got["b|cam"] != "f" {
		t.Fatalf("no-signal pick a=%q b=%q, want f/f", got["a|cam"], got["b|cam"])
	}
	// Still no signal later: nothing changes.
	got = allocate(0, 64_000, r, h, now.Add(10*time.Second))
	if got["a|cam"] != "f" || got["b|cam"] != "f" {
		t.Fatalf("drifted without a signal: a=%q b=%q", got["a|cam"], got["b|cam"])
	}
	// Congestion arrives: shed the background track first, then recover
	// one step at a time once the signal clears.
	got = allocate(2_000_000, 64_000, r, h, now.Add(11*time.Second))
	if got["a|cam"] != "f" || got["b|cam"] == "f" {
		t.Fatalf("congestion should hit b first: a=%q b=%q", got["a|cam"], got["b|cam"])
	}
	// Signal clears: after the cooldown and the 3 s headroom hold, b steps
	// up one layer (not straight back to the top).
	allocate(0, 64_000, r, h, now.Add(25*time.Second))
	got = allocate(0, 64_000, r, h, now.Add(29*time.Second))
	if got["b|cam"] != "h" {
		t.Fatalf("after cooldown b should step up one layer, got %q", got["b|cam"])
	}
}

func TestAllocateShedsLowPriorityFirst(t *testing.T) {
	h := map[string]*hystState{
		"a|cam": {Current: "h"},
		"b|cam": {Current: "h"},
	}
	now := time.Now()
	// 600 kbps budget cannot carry two h layers (1 Mbps): drop b (priority 0)
	// before touching the focused a.
	got := allocate(600_000, 0, []trackReq{req("a|cam", "cam", cam3, "auto", 2), req("b|cam", "cam", cam3, "auto", 0)}, h, now)
	if got["a|cam"] != "h" || got["b|cam"] != "" {
		t.Fatalf("got a=%q b=%q, want a=h b=paused", got["a|cam"], got["b|cam"])
	}
	if h["b|cam"].LastDowngrade.IsZero() {
		t.Fatal("downgrade should be recorded")
	}
}

func TestAllocateFixedPrefBeatsBudget(t *testing.T) {
	h := map[string]*hystState{}
	got := allocate(100_000, 0, []trackReq{req("a|cam", "cam", cam3, "f", 1)}, h, time.Now())
	if got["a|cam"] != "f" {
		t.Fatalf("fixed pref ignored: %q", got["a|cam"])
	}
	// Fixed layer not active: nearest smaller active one.
	got = allocate(100_000, 0, []trackReq{req("a|cam", "cam", []layerOpt{{"q", 150_000}, {"h", 500_000}}, "f", 1)}, h, time.Now())
	if got["a|cam"] != "h" {
		t.Fatalf("fallback for inactive f: %q", got["a|cam"])
	}
	got = allocate(100_000, 0, []trackReq{req("a|cam", "cam", cam3, "off", 1)}, h, time.Now())
	if got["a|cam"] != "" {
		t.Fatalf("off should pause: %q", got["a|cam"])
	}
}

func TestAllocateUpgradeNeedsHoldAndHeadroom(t *testing.T) {
	h := map[string]*hystState{"a|cam": {Current: "q", LastSwitch: time.Now().Add(-time.Minute)}}
	now := time.Now()
	r := []trackReq{req("a|cam", "cam", cam3, "auto", 1)}
	// Headroom present (estimate 300k vs sending 150k) but no hold yet.
	if got := allocate(300_000, 0, r, h, now); got["a|cam"] != "q" {
		t.Fatalf("upgraded before the hold: %q", got["a|cam"])
	}
	if got := allocate(300_000, 0, r, h, now.Add(2*time.Second)); got["a|cam"] != "q" {
		t.Fatalf("upgraded at 2 s: %q", got["a|cam"])
	}
	if got := allocate(300_000, 0, r, h, now.Add(3100*time.Millisecond)); got["a|cam"] != "h" {
		t.Fatalf("no upgrade after the hold: %q", got["a|cam"])
	}
	// No headroom (estimate barely above sending): hold resets, stays put.
	h2 := map[string]*hystState{"a|cam": {Current: "q", LastSwitch: now.Add(-time.Minute)}}
	allocate(160_000, 0, r, h2, now)
	if got := allocate(160_000, 0, r, h2, now.Add(5*time.Second)); got["a|cam"] != "q" {
		t.Fatalf("upgraded without headroom: %q", got["a|cam"])
	}
}

func TestAllocateDowngradeIsImmediateAndCoolsDown(t *testing.T) {
	now := time.Now()
	h := map[string]*hystState{"a|cam": {Current: "f", LastSwitch: now.Add(-time.Minute)}}
	r := []trackReq{req("a|cam", "cam", cam3, "auto", 1)}
	if got := allocate(400_000, 0, r, h, now); got["a|cam"] != "q" {
		t.Fatalf("congestion should drop to the layer that fits: %q", got["a|cam"])
	}
	// Plenty of headroom right after a drop: the cooldown blocks an upgrade.
	for _, dt := range []time.Duration{4 * time.Second, 8 * time.Second} {
		if got := allocate(2_000_000, 0, r, h, now.Add(dt)); got["a|cam"] != "q" {
			t.Fatalf("upgraded during cooldown at %v: %q", dt, got["a|cam"])
		}
	}
	// Cooldown over, hold satisfied: one step up (q -> h), not straight to f.
	if got := allocate(2_000_000, 0, r, h, now.Add(14*time.Second)); got["a|cam"] != "h" {
		t.Fatalf("after cooldown: %q, want h", got["a|cam"])
	}
}

func TestAllocateScreenBeforeCamera(t *testing.T) {
	now := time.Now()
	h := map[string]*hystState{
		"a|cam":    {Current: "q", LastSwitch: now.Add(-time.Minute)},
		"b|screen": {Current: "h", LastSwitch: now.Add(-time.Minute)},
	}
	screen := []layerOpt{{"h", 400_000}, {"f", 2_000_000}}
	r := []trackReq{req("a|cam", "cam", cam3, "auto", 1), req("b|screen", "screen", screen, "auto", 1)}
	allocate(2_000_000, 0, r, h, now)
	got := allocate(2_000_000, 0, r, h, now.Add(4*time.Second))
	if got["b|screen"] != "f" || got["a|cam"] != "q" {
		t.Fatalf("screen should step up first: screen=%q cam=%q", got["b|screen"], got["a|cam"])
	}
}
