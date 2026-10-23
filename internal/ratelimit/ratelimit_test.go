package ratelimit_test

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/ratelimit"
)

// fakeClock is a clock.Clock the test moves by hand. It is safe for
// concurrent use.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

func mustAllow(t *testing.T, l *ratelimit.Limiter, key string) {
	t.Helper()

	if ok, retry := l.Allow(key); !ok {
		t.Fatalf("Allow(%q) = false (retry %v), want true", key, retry)
	}
}

func mustDeny(t *testing.T, l *ratelimit.Limiter, key string, wantRetry time.Duration) {
	t.Helper()

	ok, retry := l.Allow(key)
	if ok {
		t.Fatalf("Allow(%q) = true, want false", key)
	}

	if retry != wantRetry {
		t.Fatalf("Allow(%q) retryAfter = %v, want %v", key, retry, wantRetry)
	}
}

func TestAllowUpToLimit(t *testing.T) {
	t.Parallel()

	clk := newFakeClock()
	l := ratelimit.New(clk, 3, time.Minute)

	for range 3 {
		mustAllow(t, l, "a")
	}

	mustDeny(t, l, "a", time.Minute)

	// Denied attempts don't move the window; the wait shrinks and is
	// rounded up to whole seconds.
	clk.Advance(20*time.Second + 500*time.Millisecond)
	mustDeny(t, l, "a", 40*time.Second)

	// Other keys have their own budget.
	mustAllow(t, l, "b")
}

func TestWindowRollover(t *testing.T) {
	t.Parallel()

	clk := newFakeClock()
	l := ratelimit.New(clk, 2, time.Minute)

	mustAllow(t, l, "a")
	clk.Advance(30 * time.Second)
	mustAllow(t, l, "a")

	// The window started with the first event, not the last one.
	clk.Advance(29*time.Second + 999*time.Millisecond)
	mustDeny(t, l, "a", time.Second)

	clk.Advance(time.Millisecond)
	mustAllow(t, l, "a")
	mustAllow(t, l, "a")
	mustDeny(t, l, "a", time.Minute)
}

func TestReset(t *testing.T) {
	t.Parallel()

	l := ratelimit.New(newFakeClock(), 1, time.Hour)

	mustAllow(t, l, "a")
	mustAllow(t, l, "b")
	mustDeny(t, l, "a", time.Hour)

	l.Reset("a")
	l.Reset("unknown")

	mustAllow(t, l, "a")
	mustDeny(t, l, "b", time.Hour)
}

func TestBlockedDoesNotCount(t *testing.T) {
	t.Parallel()

	clk := newFakeClock()
	l := ratelimit.New(clk, 2, time.Minute)

	for range 10 {
		if blocked, retry := l.Blocked("a"); blocked || retry != 0 {
			t.Fatalf("Blocked(fresh key) = %v, %v; want false, 0", blocked, retry)
		}
	}

	mustAllow(t, l, "a")

	if blocked, _ := l.Blocked("a"); blocked {
		t.Fatal("Blocked after one of two events = true, want false")
	}

	mustAllow(t, l, "a")

	clk.Advance(15 * time.Second)

	blocked, retry := l.Blocked("a")
	if !blocked || retry != 45*time.Second {
		t.Fatalf("Blocked(exhausted) = %v, %v; want true, 45s", blocked, retry)
	}

	clk.Advance(45 * time.Second)

	if blocked, _ := l.Blocked("a"); blocked {
		t.Fatal("Blocked after the window ended = true, want false")
	}
}

func TestAdd(t *testing.T) {
	t.Parallel()

	clk := newFakeClock()
	l := ratelimit.New(clk, 3, 10*time.Minute)

	// Failed logins count without being checked, even beyond the limit.
	for range 5 {
		l.Add("user@example.org")
	}

	blocked, retry := l.Blocked("user@example.org")
	if !blocked || retry != 10*time.Minute {
		t.Fatalf("Blocked = %v, %v; want true, 10m", blocked, retry)
	}

	mustDeny(t, l, "user@example.org", 10*time.Minute)

	clk.Advance(10 * time.Minute)
	mustAllow(t, l, "user@example.org")
}

func TestMaxKeysEvictsEarliestWindow(t *testing.T) {
	t.Parallel()

	clk := newFakeClock()
	l := ratelimit.New(clk, 1, time.Hour)
	l.MaxKeys = 2

	l.Add("a")
	clk.Advance(time.Second)
	l.Add("b")
	clk.Advance(time.Second)

	// Touching "a" again does not make it younger: its window still
	// started first.
	l.Add("a")

	// The table is full; "c" is still admitted and "a" makes room.
	mustAllow(t, l, "c")

	if blocked, _ := l.Blocked("a"); blocked {
		t.Fatal(`"a" should have been evicted`)
	}

	if blocked, _ := l.Blocked("b"); !blocked {
		t.Fatal(`"b" should still be blocked`)
	}

	if blocked, _ := l.Blocked("c"); !blocked {
		t.Fatal(`"c" should be blocked`)
	}
}

func TestMaxKeysUnderFlood(t *testing.T) {
	t.Parallel()

	clk := newFakeClock()
	l := ratelimit.New(clk, 1, time.Hour)
	l.MaxKeys = 10

	if got := ratelimit.New(clk, 1, time.Hour).MaxKeys; got != ratelimit.DefaultMaxKeys {
		t.Fatalf("default MaxKeys = %d, want %d", got, ratelimit.DefaultMaxKeys)
	}

	// An attacker cycling through fresh keys never locks anyone out and
	// the most recent keys keep their state.
	for i := range 1000 {
		mustAllow(t, l, "flood-"+strconv.Itoa(i))
	}

	for i := 990; i < 1000; i++ {
		if blocked, _ := l.Blocked("flood-" + strconv.Itoa(i)); !blocked {
			t.Fatalf("flood-%d should still be tracked", i)
		}
	}
}

func TestConcurrentUse(t *testing.T) {
	t.Parallel()

	clk := newFakeClock()
	shared := ratelimit.New(clk, 500, time.Minute)

	// A second limiter with a small table and a moving clock keeps
	// sweeping and evicting under load.
	churnClk := newFakeClock()
	churn := ratelimit.New(churnClk, 3, time.Minute)
	churn.MaxKeys = 50

	const (
		workers = 16
		calls   = 100
	)

	var (
		allowed atomic.Int64
		wg      sync.WaitGroup
	)

	for w := range workers {
		wg.Go(func() {
			for i := range calls {
				if ok, _ := shared.Allow("shared"); ok {
					allowed.Add(1)
				}

				key := "w" + strconv.Itoa(w) + "-" + strconv.Itoa(i%20)
				churn.Allow(key)
				churn.Add(key)
				churn.Blocked(key)

				if i%7 == 0 {
					churn.Reset(key)
				}

				if i%25 == 0 {
					churnClk.Advance(time.Second)
				}
			}
		})
	}

	wg.Wait()

	if got := allowed.Load(); got != 500 {
		t.Fatalf("allowed %d events on the shared key, want 500", got)
	}
}

func TestNewRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		limit  int
		window time.Duration
	}{
		{"zero limit", 0, time.Minute},
		{"negative limit", -1, time.Minute},
		{"zero window", 1, 0},
		{"negative window", 1, -time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatal("New did not panic")
				}
			}()

			ratelimit.New(newFakeClock(), tc.limit, tc.window)
		})
	}
}
