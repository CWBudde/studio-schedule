package jobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe for concurrent use by a logger.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestRunOnce(t *testing.T) {
	t.Parallel()

	errA, errC := errors.New("a failed"), errors.New("c failed")

	var order []string

	job := func(name string, err error) Job {
		return Job{Name: name, Every: time.Hour, Run: func(context.Context) error {
			order = append(order, name)

			return err
		}}
	}
	r := NewRunner(nil, job("a", errA), job("b", nil), Job{Name: "p", Every: time.Hour, Run: func(context.Context) error {
		order = append(order, "p")

		panic("boom")
	}}, job("c", errC), Job{Name: "late", Every: time.Hour, SkipAtStartup: true, Run: func(context.Context) error {
		order = append(order, "late")

		return nil
	}})

	err := r.RunOnce(t.Context())
	if !errors.Is(err, errA) || !errors.Is(err, errC) {
		t.Fatalf("err = %v, want both job errors", err)
	}

	for _, s := range []string{`job "a"`, `job "c"`, `job "p": panic: boom`} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not mention %q", err, s)
		}
	}

	if got := strings.Join(order, ","); got != "a,b,p,c" {
		t.Fatalf("ran %s, want a,b,p,c in order and not the job skipped at startup", got)
	}

	if err := NewRunner(nil, job("b", nil)).RunOnce(t.Context()); err != nil {
		t.Fatalf("succeeding job: %v", err)
	}
}

// fakeTickers hands out manually driven tickers, one per Every value.
type fakeTickers struct {
	mu      sync.Mutex
	ticks   map[time.Duration]chan time.Time
	stopped map[time.Duration]bool
}

func newFakeTickers() *fakeTickers {
	return &fakeTickers{ticks: map[time.Duration]chan time.Time{}, stopped: map[time.Duration]bool{}}
}

func (f *fakeTickers) channel(d time.Duration) chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.ticks[d]
	if !ok {
		c = make(chan time.Time)
		f.ticks[d] = c
	}

	return c
}

func (f *fakeTickers) new(d time.Duration) (<-chan time.Time, func()) {
	return f.channel(d), func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.stopped[d] = true
	}
}

func (f *fakeTickers) tick(t *testing.T, d time.Duration) {
	t.Helper()

	select {
	case f.channel(d) <- time.Time{}:
	case <-time.After(5 * time.Second):
		t.Fatalf("ticker %s not read", d)
	}
}

func receive(t *testing.T, c <-chan string) string {
	t.Helper()

	select {
	case s := <-c:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("job did not run")

		return ""
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	ran := make(chan string, 10)

	var panics int

	logs := &syncBuffer{}
	r := NewRunner(slog.New(slog.NewTextHandler(logs, nil)),
		Job{Name: "fast", Every: time.Minute, Run: func(context.Context) error {
			ran <- "fast"

			return errors.New("fast failed")
		}},
		Job{Name: "flaky", Every: time.Hour, Run: func(context.Context) error {
			panics++
			if panics == 1 {
				panic("first run panics")
			}

			ran <- "flaky"

			return nil
		}},
	)
	tickers := newFakeTickers()
	r.newTicker = tickers.new

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		r.Run(ctx)
		close(done)
	}()

	tickers.tick(t, time.Minute)

	if got := receive(t, ran); got != "fast" {
		t.Fatalf("ran %s, want fast", got)
	}

	// The panic is recovered and logged; the next tick runs the job again.
	tickers.tick(t, time.Hour)
	tickers.tick(t, time.Hour)

	if got := receive(t, ran); got != "flaky" {
		t.Fatalf("ran %s, want flaky", got)
	}

	tickers.tick(t, time.Minute)

	if got := receive(t, ran); got != "fast" {
		t.Fatalf("ran %s, want fast", got)
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}

	tickers.mu.Lock()
	if !tickers.stopped[time.Minute] || !tickers.stopped[time.Hour] {
		t.Errorf("tickers not stopped: %v", tickers.stopped)
	}
	tickers.mu.Unlock()

	out := logs.String()
	for _, s := range []string{"job=fast", "fast failed", "job=flaky", "first run panics"} {
		if !strings.Contains(out, s) {
			t.Errorf("log does not contain %q:\n%s", s, out)
		}
	}
}

func TestRunWithRealTicker(t *testing.T) {
	t.Parallel()

	ran := make(chan string, 1)
	r := NewRunner(nil, Job{Name: "tick", Every: time.Millisecond, Run: func(context.Context) error {
		select {
		case ran <- "tick":
		default:
		}

		return nil
	}})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		r.Run(ctx)
		close(done)
	}()

	receive(t, ran)
	cancel()
	<-done
}

func TestNewRunnerRejectsInvalidJobs(t *testing.T) {
	t.Parallel()

	run := func(context.Context) error { return nil }

	for name, j := range map[string]Job{
		"no name":     {Every: time.Hour, Run: run},
		"no interval": {Name: "x", Run: run},
		"no func":     {Name: "x", Every: time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatal("NewRunner did not panic")
				}
			}()

			NewRunner(nil, j)
		})
	}
}

// TestWake runs a job before its tick when it is woken, once for wakes that
// pile up while it waits, and not for another job's wake.
func TestWake(t *testing.T) {
	t.Parallel()

	ran := make(chan string, 10)
	w := NewWaker()
	job := func(name string) Job {
		return Job{Name: name, Every: time.Hour, Wake: w.Chan(name), Run: func(context.Context) error {
			ran <- name

			return nil
		}}
	}
	r := NewRunner(nil, job("announcements"), job("other"))
	r.newTicker = newFakeTickers().new

	// Wakes before Run starts coalesce into one run.
	w.Wake("announcements")
	w.Wake("announcements")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go r.Run(ctx)

	if got := receive(t, ran); got != "announcements" {
		t.Fatalf("ran %s, want announcements", got)
	}

	select {
	case got := <-ran:
		t.Fatalf("ran %s again, want one run for the coalesced wakes", got)
	case <-time.After(50 * time.Millisecond):
	}

	w.Wake("other")

	if got := receive(t, ran); got != "other" {
		t.Fatalf("ran %s, want other", got)
	}

	// A nil Waker wakes nothing and does not block.
	var none *Waker
	none.Wake("announcements")

	if none.Chan("announcements") != nil {
		t.Error("a nil Waker has a channel")
	}
}
