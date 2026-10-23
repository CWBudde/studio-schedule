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
	key   string
	start time.Time
	count int
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
	if e.count >= l.limit {
		return false, l.remaining(e, now)
	}

	e.count++

	return true, 0
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
	if l.expired(e, now) || e.count < l.limit {
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

// Reset forgets key, e.g. after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if el, found := l.entries[key]; found {
		l.remove(el)
	}
}

// get returns key's entry for the window containing now, creating it (and
// evicting the earliest entry when the table is full) if necessary.
// l.mu must be held.
func (l *Limiter) get(key string, now time.Time) *entry {
	if el, found := l.entries[key]; found {
		e := el.Value.(*entry)
		if l.expired(e, now) {
			// Only reachable if the clock went backwards and broke the
			// order sweep relies on; start a new window either way.
			e.start, e.count = now, 0
			l.order.MoveToBack(el)
		}

		return e
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
