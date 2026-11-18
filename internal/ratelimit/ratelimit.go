// Package ratelimit counts events per key in fixed time windows, for
// example login attempts per e-mail address or requests per client IP.
// The state lives in memory, so every process (and the WASM demo) keeps
// its own counters and a restart forgets them.
package ratelimit

import (
	"container/list"
	"sync"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
)

// DefaultMaxKeys is the number of keys a Limiter tracks unless MaxKeys is
// changed.
const DefaultMaxKeys = 100_000

// Limiter allows a fixed number of events per key within each window. A
// key's window starts with its first event and ends window later; after
// that the key starts afresh. A Limiter is safe for concurrent use.
//
// Memory stays bounded under attacker-chosen keys: keys whose window has
// ended are dropped on the next call, and when MaxKeys keys are tracked a
// new key evicts the one whose window started earliest. Eviction forgets
// that key's count, so a flood of fresh keys can reset a victim's counter
// early, but it never turns into a lockout for everyone.
type Limiter struct {
	// MaxKeys caps the number of tracked keys. New sets it to
	// DefaultMaxKeys; change it only before the Limiter is used.
	MaxKeys int

	clk    clock.Clock
	limit  int
	window time.Duration

	mu      sync.Mutex
	entries map[string]*list.Element
	// order holds the *entry values sorted by window start, earliest
	// first, so expired and evictable entries sit at the front.
	order list.List
}

type entry struct {
	key     string
	start   time.Time
	count   int
	pending int
}

// Reservation holds one slot while an operation decides whether to count it.
// The first resolution wins. Resolution after the window ends has no effect.
type Reservation struct {
	slot *reservationSlot
}

type reservationSlot struct {
	limiter *Limiter
	window  *entry
}

// New returns a limiter that allows limit events per key within each
// window. It panics if limit is less than 1 or window is not positive.
func New(clk clock.Clock, limit int, window time.Duration) *Limiter {
	if limit < 1 {
		panic("ratelimit: limit must be at least 1")
	}

	if window <= 0 {
		panic("ratelimit: window must be positive")
	}

	return &Limiter{
		MaxKeys: DefaultMaxKeys,
		clk:     clk,
		limit:   limit,
		window:  window,
		entries: make(map[string]*list.Element),
	}
}

// Allow counts one event for key and reports whether it is within the
// limit. When it is not, the event is not counted and retryAfter is the
// time until the key's window ends, rounded up to whole seconds.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clk.Now()
	l.sweep(now)

	e := l.get(key, now)
	if e.count+e.pending >= l.limit {
		return false, l.remaining(e, now)
	}

	e.count++

	return true, 0
}

// Reserve admits an operation without committing its slot to the count yet.
// A denied reservation returns nil and the retry delay. The caller must resolve
// an admitted reservation even when its operation fails.
func (l *Limiter) Reserve(key string) (*Reservation, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clk.Now()
	l.sweep(now)
	e := l.get(key, now)
	if e.count+e.pending >= l.limit {
		return nil, l.remaining(e, now)
	}
	e.pending++

	return &Reservation{slot: &reservationSlot{limiter: l, window: e}}, 0
}

// Count settles the reservation as an event in its admission window.
func (r *Reservation) Count() { r.finish(countEvent) }

// Release settles the reservation without counting an event.
func (r *Reservation) Release() { r.finish(releaseSlot) }

// ClearFailures clears completed failures after a successful login. Other
// in-flight operations keep their reservations and can still count failures.
func (r *Reservation) ClearFailures() { r.finish(clearEvents) }

type resolution uint8

const (
	releaseSlot resolution = iota
	countEvent
	clearEvents
)

func (r *Reservation) finish(how resolution) {
	if r == nil || r.slot == nil {
		return // a nil limiter imposes no limit
	}

	l := r.slot.limiter
	l.mu.Lock()
	defer l.mu.Unlock()

	e := r.slot.window
	if e == nil {
		return
	}
	r.slot.window = nil
	el := l.entries[e.key]
	if el == nil || el.Value != e {
		return
	}
	if l.expired(e, l.clk.Now()) {
		l.remove(el)

		return
	}

	e.pending--
	switch how {
	case countEvent:
		e.count++
	case clearEvents:
		e.count = 0
	}
	if e.count == 0 && e.pending == 0 {
		l.remove(el)
	}
}

// Blocked reports, without counting, whether key has used up its limit in
// the current window, and the time until that window ends, rounded up to
// whole seconds.
func (l *Limiter) Blocked(key string) (blocked bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clk.Now()
	l.sweep(now)

	el, found := l.entries[key]
	if !found {
		return false, 0
	}

	e := el.Value.(*entry)
	if l.expired(e, now) || e.count+e.pending < l.limit {
		return false, 0
	}

	return true, l.remaining(e, now)
}

// Add counts one event for key without checking the limit, e.g. a failed
// login.
func (l *Limiter) Add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clk.Now()
	l.sweep(now)

	l.get(key, now).count++
}

// Reset forgets completed events of key without releasing in-flight slots.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if el, found := l.entries[key]; found {
		e := el.Value.(*entry)
		e.count = 0
		if e.pending == 0 {
			l.remove(el)
		}
	}
}

// get returns key's entry for the window containing now, creating it (and
// evicting the earliest entry when the table is full) if necessary.
// l.mu must be held.
func (l *Limiter) get(key string, now time.Time) *entry {
	if el, found := l.entries[key]; found {
		e := el.Value.(*entry)
		if !l.expired(e, now) {
			return e
		}
		// A clock change can leave an expired entry in the middle of the
		// list. Replace it so its reservations cannot affect the new window.
		l.remove(el)
	}

	for len(l.entries) >= max(l.MaxKeys, 1) {
		l.remove(l.order.Front())
	}

	e := &entry{key: key, start: now}
	l.entries[key] = l.order.PushBack(e)

	return e
}

// sweep drops the entries whose window has ended. Entries are ordered by
// window start, so it stops at the first one still running; the cost is
// proportional to the number of entries dropped. l.mu must be held.
func (l *Limiter) sweep(now time.Time) {
	for el := l.order.Front(); el != nil; el = l.order.Front() {
		if !l.expired(el.Value.(*entry), now) {
			return
		}

		l.remove(el)
	}
}

func (l *Limiter) remove(el *list.Element) {
	e := l.order.Remove(el).(*entry)
	delete(l.entries, e.key)
}

func (l *Limiter) expired(e *entry, now time.Time) bool {
	return !now.Before(e.start.Add(l.window))
}

// remaining returns the time until e's window ends, rounded up to whole
// seconds so that it fits a Retry-After header.
func (l *Limiter) remaining(e *entry, now time.Time) time.Duration {
	d := e.start.Add(l.window).Sub(now)

	return (d + time.Second - 1).Truncate(time.Second)
}
