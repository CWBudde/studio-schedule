package ical

import (
	"sync"
	"time"
)

// zoneState is what a zone says about an instant: its abbreviation, its
// offset from UTC in seconds, and whether daylight saving time is in effect.
type zoneState struct {
	name   string
	offset int
	dst    bool
}

func stateAt(zone *time.Location, t time.Time) zoneState {
	local := t.In(zone)
	name, offset := local.Zone()

	return zoneState{name: name, offset: offset, dst: local.IsDST()}
}

// transition is a change of a zone's state at an instant.
type transition struct {
	at       time.Time
	from, to zoneState
}

// scanStep is how far apart transitions are looked for. Zones do not change
// twice within a day, so a day finds every transition; a binary search then
// finds its second.
const scanStep = 24 * time.Hour

// transitions returns the transitions of zone in [from, to), in order. Go's
// time package does not expose them, so they are found by scanning.
func transitions(zone *time.Location, from, to time.Time) []transition {
	var out []transition

	prev := stateAt(zone, from)

	for lo := from; lo.Before(to); lo = lo.Add(scanStep) {
		hi := lo.Add(scanStep)
		if hi.After(to) {
			hi = to
		}

		cur := stateAt(zone, hi)
		if cur == prev {
			continue
		}

		// The change lies in (lo, hi]: find its first second.
		a, b := lo, hi
		for b.Sub(a) > time.Second {
			mid := a.Add(b.Sub(a) / 2).Truncate(time.Second)
			if mid.Equal(a) {
				mid = a.Add(time.Second)
			}

			if stateAt(zone, mid) == prev {
				a = mid
			} else {
				b = mid
			}
		}

		out = append(out, transition{at: b, from: prev, to: stateAt(zone, b)})
		prev = stateAt(zone, b)

		if prev != cur {
			// A second change within the step; scan this step again from b.
			lo = b.Add(-scanStep)
		}
	}

	return out
}

// lookBack is how far before the covered years the last transition is
// looked for: it sets the offset in effect when they begin.
const lookBack = 2 * 366 * 24 * time.Hour

// epochOnset is the DTSTART of the one sub-component of a zone without a
// transition near the covered years (no daylight saving time, say).
const epochOnset = "19700101T000000"

// writeTimezone writes the VTIMEZONE of zone for the instants [from, to):
// the last transition before from, which sets the offset in effect at from,
// and every transition up to to, each as a STANDARD or DAYLIGHT
// sub-component of its own with its onset in the local time before it. A
// zone without such a transition gets one STANDARD (or DAYLIGHT)
// sub-component with its offset at from.
func writeTimezone(w *writer, zone *time.Location, from, to time.Time) {
	w.line("BEGIN", "VTIMEZONE")
	w.line("TZID", Text(zone.String()))

	within := observances(zone, from, to)
	if len(within) == 0 {
		s := stateAt(zone, from)
		writeObservance(w, s, s, epochOnset)
	}

	for _, t := range within {
		writeObservance(w, t.from, t.to, t.at.Add(time.Duration(t.from.offset)*time.Second).UTC().Format(localLayout))
	}

	w.line("END", "VTIMEZONE")
}

// observanceKey identifies the observances of a zone in a range.
type observanceKey struct {
	zone     string
	from, to int64
}

// observanceCache keeps the observances of the zones and ranges recently
// written: scanning for transitions costs a few thousand zone lookups, and
// a feed's zone and years rarely change.
var observanceCache = struct {
	sync.Mutex

	m map[observanceKey][]transition
}{m: map[observanceKey][]transition{}}

// maxCachedObservances bounds the cache; it starts over when full.
const maxCachedObservances = 64

// observances returns the transitions a VTIMEZONE of zone for [from, to)
// lists: the last one before from, which sets the offset in effect at from,
// and every one up to to.
func observances(zone *time.Location, from, to time.Time) []transition {
	key := observanceKey{zone: zone.String(), from: from.Unix(), to: to.Unix()}

	observanceCache.Lock()
	cached, ok := observanceCache.m[key]
	observanceCache.Unlock()

	if ok {
		return cached
	}

	before := transitions(zone, from.Add(-lookBack), from)
	within := transitions(zone, from, to)

	if len(before) > 0 {
		within = append(before[len(before)-1:len(before):len(before)], within...)
	}

	observanceCache.Lock()
	if len(observanceCache.m) >= maxCachedObservances {
		clear(observanceCache.m)
	}

	observanceCache.m[key] = within
	observanceCache.Unlock()

	return within
}

// writeObservance writes the sub-component that starts at onset (a local
// time in the offset of from) with the state to.
func writeObservance(w *writer, from, to zoneState, onset string) {
	kind := "STANDARD"
	if to.dst {
		kind = "DAYLIGHT"
	}

	w.line("BEGIN", kind)
	w.line("DTSTART", onset)
	w.line("TZOFFSETFROM", utcOffset(from.offset))
	w.line("TZOFFSETTO", utcOffset(to.offset))

	if to.name != "" {
		w.line("TZNAME", Text(to.name))
	}

	w.line("END", kind)
}
