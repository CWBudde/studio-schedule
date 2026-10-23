package ratelimit

import (
	"strconv"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
)

// steppingClock returns an instant that advances by step on each call
// after the first, so every event lands later than the one before.
type steppingClock struct {
	t    time.Time
	step time.Duration
}

func (c *steppingClock) Now() time.Time {
	t := c.t
	c.t = c.t.Add(c.step)

	return t
}

func TestExpiredWindowsAreDropped(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clk := &steppingClock{t: start, step: time.Second}
	l := New(clk, 1, 10*time.Second)

	for i := range 100 {
		l.Add("k" + strconv.Itoa(i))

		// At most one window's worth of keys is ever kept.
		if n := l.size(); n > 10 {
			t.Fatalf("after %d keys the table holds %d entries, want <= 10", i+1, n)
		}
	}

	// Blocked does not create entries, so after all windows ended and one
	// call swept them, the table is empty.
	l.clk = clock.NewFixed(clk.t.Add(time.Hour))
	l.Blocked("other")

	if n := l.size(); n != 0 {
		t.Fatalf("table holds %d entries after all windows ended, want 0", n)
	}
}

func (l *Limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.entries)
}
