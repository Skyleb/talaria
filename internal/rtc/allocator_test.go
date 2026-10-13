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

func TestAllocateNeverCongestedJumpsToTop(t *testing.T) {
	now := time.Now()
	// Browsers start the small layer first; once the big ones flow, a track
	// with no congestion history goes straight to the top (no 3 s hold).
	h := map[string]*hystState{"a|cam": {Current: "q", LastSwitch: now.Add(-2 * time.Second)}}
	r := []trackReq{req("a|cam", "cam", cam3, "auto", 1)}
	if got := allocate(0, 0, r, h, now); got["a|cam"] != "f" {
		t.Fatalf("never-congested track should jump to f, got %q", got["a|cam"])
	}
	// Right after a switch it waits the minimum gap.
	h2 := map[string]*hystState{"a|cam": {Current: "q", LastSwitch: now.Add(-200 * time.Millisecond)}}
	if got := allocate(0, 0, r, h2, now); got["a|cam"] != "q" {
		t.Fatalf("switched again inside the minimum gap: %q", got["a|cam"])
	}
	// A real budget still applies: with 400 kbps it stays on q.
	h3 := map[string]*hystState{"a|cam": {Current: "q", LastSwitch: now.Add(-2 * time.Second)}}
	if got := allocate(400_000, 0, r, h3, now); got["a|cam"] != "q" {
		t.Fatalf("jumped over a budget: %q", got["a|cam"])
	}
}

func TestEstimatorCongested(t *testing.T) {
	if c, _ := estimatorCongested(map[string]any{"state": "increase", "averageLoss": 0.0}); c {
		t.Fatal("increase with no loss is not congestion")
	}
	if c, _ := estimatorCongested(map[string]any{"state": "hold", "averageLoss": 0.01}); c {
		t.Fatal("hold with a little loss is not congestion")
	}
	if c, _ := estimatorCongested(map[string]any{"state": "decrease", "averageLoss": 0.0}); !c {
		t.Fatal("decrease is congestion")
	}
	if c, _ := estimatorCongested(map[string]any{"state": "increase", "averageLoss": 0.2}); !c {
		t.Fatal("heavy loss is congestion")
	}
	if c, _ := estimatorCongested(map[string]any{}); c {
		t.Fatal("missing stats are not congestion")
	}
}

func TestCongestionBudget(t *testing.T) {
	now := time.Now()
	if b := congestionBudget(time.Time{}, 0, 4_000_000, now); b != 0 {
		t.Fatalf("no verdict yet should be unlimited, got %d", b)
	}
	// An estimate below what we send is NOT a budget on its own (it lags
	// every upgrade by design).
	if b := congestionBudget(time.Time{}, 0, 700_000, now); b != 0 {
		t.Fatalf("a low estimate alone must not become a budget, got %d", b)
	}
	// A verdict stays in force for congestionHold, at the lower of its
	// estimate and the current one.
	at := now.Add(-time.Second)
	if b := congestionBudget(at, 2_000_000, 2_200_000, now); b != 2_000_000 {
		t.Fatalf("budget during hold: %d, want 2000000", b)
	}
	if b := congestionBudget(at, 2_000_000, 1_500_000, now); b != 1_500_000 {
		t.Fatalf("current estimate lower than verdict: %d, want 1500000", b)
	}
	if b := congestionBudget(now.Add(-congestionHold-time.Second), 2_000_000, 2_200_000, now); b != 0 {
		t.Fatalf("expired verdict should be unlimited, got %d", b)
	}
}
