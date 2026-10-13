package rtc

import (
	"sort"
	"time"
)

// Layer allocation for one viewer.
//
// The bandwidth estimate for a viewer is a send-side estimate: it can only
// rise while we actually send more, so it hovers a little above what we are
// sending. Treating it as a hard ceiling would pin everyone to the lowest
// layer forever. Instead the allocator uses it as a congestion signal:
// drop a layer the moment the estimate falls below what we send, and step
// one track up a layer at a time when the estimate shows headroom and has
// done so for a while. A failed step is undone by the next drop and then
// held off for a cooldown.

const (
	upgradeHold      = 3 * time.Second
	upgradeHeadroom  = 1.15 // estimate must exceed current sending by this factor
	downgradeCooldwn = 10 * time.Second
	minSwitchGap     = time.Second
)

type layerOpt struct {
	RID string
	Bps int
}

type trackReq struct {
	Key      string
	Kind     string     // cam | screen
	Layers   []layerOpt // active layers, smallest first
	Pref     string     // auto | q | h | f | off
	Priority int        // 2 focused, 1 visible, 0 hidden
}

// hystState remembers per-track switching history across allocations.
type hystState struct {
	Current       string
	HeadroomSince time.Time // when the estimate first showed room to upgrade
	LastSwitch    time.Time
	LastDowngrade time.Time
}

func rankOf(layers []layerOpt, rid string) int {
	for i, l := range layers {
		if l.RID == rid {
			return i
		}
	}
	return -1
}

// allocate returns the layer each track should carry (""=paused). hyst is
// updated in place. budgetBps <= 0 means "no congestion signal": tracks
// start on their best active layer and only step down when a real budget
// says so.
// updated in place.
func allocate(budgetBps, audioBps int, reqs []trackReq, hyst map[string]*hystState, now time.Time) map[string]string {
	out := map[string]string{}
	limited := budgetBps > 0
	budget := budgetBps - audioBps
	if budget < 0 {
		budget = 0
	}

	// Fixed choices come first and are never budgeted away; Auto tracks sort
	// by priority, screens before cameras.
	var auto []trackReq
	for _, t := range reqs {
		h := hyst[t.Key]
		if h == nil {
			h = &hystState{}
			hyst[t.Key] = h
		}
		switch t.Pref {
		case "", "auto":
			auto = append(auto, t)
		default:
			rid := pickFixed(t.Layers, t.Pref)
			apply(h, rid, now)
			out[t.Key] = rid
			if limited {
				budget -= bpsOf(t.Layers, rid)
			}
		}
	}
	sort.SliceStable(auto, func(i, j int) bool {
		if auto[i].Priority != auto[j].Priority {
			return auto[i].Priority > auto[j].Priority
		}
		if auto[i].Kind != auto[j].Kind {
			return auto[i].Kind == "screen"
		}
		return auto[i].Key < auto[j].Key
	})

	// Start from what each track carries now, clamped to an active layer.
	cur := map[string]string{}
	for _, t := range auto {
		h := hyst[t.Key]
		rid := h.Current
		if rankOf(t.Layers, rid) < 0 {
			// Not active any more (or nothing yet): with no congestion signal
			// take the best layer; otherwise the best that fits the budget.
			rid = ""
			if len(t.Layers) > 0 {
				rid = t.Layers[0].RID
				for _, l := range t.Layers {
					if !limited || l.Bps <= budget {
						rid = l.RID
					}
				}
			}
		}
		cur[t.Key] = rid
	}
	total := func() int {
		n := 0
		for _, t := range auto {
			n += bpsOf(t.Layers, cur[t.Key])
		}
		return n
	}

	// Congestion: shed from the lowest priority upwards until we fit.
	for limited && total() > budget {
		shed := false
		for i := len(auto) - 1; i >= 0; i-- {
			t := auto[i]
			rid := cur[t.Key]
			r := rankOf(t.Layers, rid)
			if r > 0 {
				cur[t.Key] = t.Layers[r-1].RID
				shed = true
				break
			}
			if r == 0 && t.Priority < 2 {
				cur[t.Key] = "" // pause hidden/background tracks before starving the focused one
				shed = true
				break
			}
		}
		if !shed {
			break // only focused tracks at their lowest layer remain
		}
	}
	downgraded := map[string]bool{}
	for _, t := range auto {
		if rankOf(t.Layers, cur[t.Key]) < rankOf(t.Layers, hyst[t.Key].Current) && hyst[t.Key].Current != "" {
			downgraded[t.Key] = true
		}
	}

	// Headroom: step the most important track up one layer after a hold. A
	// track that has never been congested on a link with no signal goes
	// straight to the top instead: browsers start the big layers a few
	// seconds late, and nothing has suggested they will not fit.
	if len(downgraded) == 0 && total() > 0 && (!limited || float64(budget) >= upgradeHeadroom*float64(total())) {
		for _, t := range auto {
			h := hyst[t.Key]
			r := rankOf(t.Layers, cur[t.Key])
			if r < 0 || r >= len(t.Layers)-1 {
				continue
			}
			if !limited && h.LastDowngrade.IsZero() {
				if now.Sub(h.LastSwitch) >= minSwitchGap {
					cur[t.Key] = t.Layers[len(t.Layers)-1].RID
					h.HeadroomSince = time.Time{}
				}
				continue // every such track, not one step at a time
			}
			if h.HeadroomSince.IsZero() {
				h.HeadroomSince = now
			}
			if now.Sub(h.HeadroomSince) >= upgradeHold && now.Sub(h.LastSwitch) >= minSwitchGap && now.Sub(h.LastDowngrade) >= downgradeCooldwn {
				cur[t.Key] = t.Layers[r+1].RID
				h.HeadroomSince = time.Time{}
			}
			break // one step at a time, highest priority first
		}
	} else {
		for _, t := range auto {
			hyst[t.Key].HeadroomSince = time.Time{}
		}
	}

	for _, t := range auto {
		h := hyst[t.Key]
		if downgraded[t.Key] {
			h.LastDowngrade = now
		}
		apply(h, cur[t.Key], now)
		out[t.Key] = cur[t.Key]
	}
	return out
}

func apply(h *hystState, rid string, now time.Time) {
	if h.Current != rid {
		h.Current = rid
		h.LastSwitch = now
	}
}

func bpsOf(layers []layerOpt, rid string) int {
	for _, l := range layers {
		if l.RID == rid {
			return l.Bps
		}
	}
	return 0
}

// pickFixed honours an explicit choice if that layer is active, else the
// nearest smaller active layer, else the smallest.
func pickFixed(layers []layerOpt, pref string) string {
	if pref == "off" || len(layers) == 0 {
		return ""
	}
	want := layerRank(pref)
	best := layers[0].RID
	for _, l := range layers {
		if layerRank(l.RID) <= want {
			best = l.RID
		}
	}
	return best
}
