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

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// t0 is the first reading of the test clock, step the time between two.
var (
	t0   = time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	step = time.Second
)

// stepClock is a clock.Clock that moves on by step on every reading.
type stepClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now
	c.now = c.now.Add(c.step)

	return now
}

// newConfig returns a Config with a fresh memory store, a stepping clock
// from t0 and sequential IDs.
func newConfig(logger *slog.Logger) Config {
	return Config{Logger: logger, Store: memory.New(), Clock: &stepClock{now: t0, step: step}, IDs: ids.NewSequence()}
}

// lastRuns returns the latest recorded run of each of r's jobs.
func lastRuns(t *testing.T, r *Runner) map[string]domain.JobRun {
	t.Helper()

	statuses, err := r.Statuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	last := make(map[string]domain.JobRun, len(statuses))
	for _, s := range statuses {
		last[s.Name] = s.Last
	}

	return last
}

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
	r := NewRunner(newConfig(nil), job("a", errA), job("b", nil), Job{Name: "p", Every: time.Hour, Run: func(context.Context) error {
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

	// Each run is recorded, from its start to its end (the test clock
	// steps a second per reading), with its result and error; a panic
	// without the stack, which only the log has.
	last := lastRuns(t, r)
	for name, want := range map[string]domain.JobRun{
		"a": {Result: domain.JobFailed, Error: `job "a": a failed`},
		"b": {Result: domain.JobOK},
		"p": {Result: domain.JobFailed, Error: `job "p": panic: boom`},
		"c": {Result: domain.JobFailed, Error: `job "c": c failed`},
	} {
		got := last[name]
		if got.ID.IsZero() || got.Job != name || got.Result != want.Result || got.Error != want.Error ||
			got.EndedAt.Sub(got.StartedAt) != step || got.StartedAt.Before(t0) {
			t.Errorf("run of %s = %+v, want %s %q over %s", name, got, want.Result, want.Error, step)
		}
	}

	if late := last["late"]; !late.ID.IsZero() {
		t.Errorf("the job skipped at startup has a run: %+v", late)
	}

	if err := NewRunner(newConfig(nil), job("b", nil)).RunOnce(t.Context()); err != nil {
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
	r := NewRunner(newConfig(slog.New(slog.NewTextHandler(logs, nil))),
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
	r := NewRunner(newConfig(nil), Job{Name: "tick", Every: time.Millisecond, Run: func(context.Context) error {
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
		"blank name":  {Name: " ", Every: time.Hour, Run: run},
		"long name":   {Name: strings.Repeat("x", domain.MaxJobNameLen+1), Every: time.Hour, Run: run},
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

			NewRunner(newConfig(nil), j)
		})
	}

	ok := Job{Name: "x", Every: time.Hour, Run: run}
	full := newConfig(nil)
	noStore, noClock, noIDs := full, full, full
	noStore.Store, noClock.Clock, noIDs.IDs = nil, nil, nil

	for name, build := range map[string]func(){
		"two jobs of one name": func() { NewRunner(full, ok, ok) },
		"no store":             func() { NewRunner(noStore, ok) },
		"no clock":             func() { NewRunner(noClock, ok) },
		"no ids":               func() { NewRunner(noIDs, ok) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatal("NewRunner did not panic")
				}
			}()

			build()
		})
	}

	if NewRunner(Config{Store: full.Store, Clock: full.Clock, IDs: full.IDs}, ok) == nil {
		t.Fatal("no Runner without a logger")
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
	r := NewRunner(newConfig(nil), job("announcements"), job("other"))
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

// counter is a job that counts its runs, and sends its name to ran if
// that is set.
type counter struct {
	mu  sync.Mutex
	n   int
	ran chan string
}

func (c *counter) job(name string, err error) Job {
	return Job{Name: name, Every: time.Hour, Run: func(context.Context) error {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()

		if c.ran != nil {
			c.ran <- name
		}

		return err
	}}
}

func (c *counter) runs() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.n
}

// TestJobs lists the configured jobs in their order.
func TestJobs(t *testing.T) {
	t.Parallel()

	var c counter

	r := NewRunner(newConfig(nil), c.job("b", nil), Job{Name: "a", Every: time.Minute, Run: c.job("a", nil).Run})

	got := r.Jobs()
	if want := []Info{{"b", time.Hour}, {"a", time.Minute}}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Jobs() = %v, want %v", got, want)
	}
}

// TestStatuses: every configured job in order, without a run before it
// ran and with its latest afterwards; runs of other jobs are left out.
func TestStatuses(t *testing.T) {
	t.Parallel()

	var c counter

	cfg := newConfig(nil)
	r := NewRunner(cfg, c.job("b", nil), c.job("a", errors.New("a failed")))

	retired := domain.JobRun{
		ID: ids.New[domain.JobRunID](cfg.IDs), Job: "retired", StartedAt: t0, EndedAt: t0, Result: domain.JobOK,
	}

	err := cfg.Store.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.JobRuns().Create(ctx, retired)
	})
	if err != nil {
		t.Fatal(err)
	}

	before, err := r.Statuses(t.Context())
	if err != nil || len(before) != 2 || before[0].Name != "b" || before[1].Name != "a" ||
		before[0].Every != time.Hour || !before[0].Last.ID.IsZero() || !before[1].Last.ID.IsZero() {
		t.Fatalf("before any run: %+v, %v", before, err)
	}

	first, _ := r.RunNow(t.Context(), "a")
	second, _ := r.RunNow(t.Context(), "a")
	b, _ := r.RunNow(t.Context(), "b")

	after, err := r.Statuses(t.Context())
	if err != nil || len(after) != 2 || after[0].Last != b || after[1].Last != second || first == second {
		t.Fatalf("after the runs: %+v, %v; want b's run %+v and a's latest %+v", after, err, b, second)
	}
}

// TestSkippedWhileLocked: while another holder has a job's lock, its runs
// do not happen and are recorded as skipped. That is no error at startup
// or in the loop; RunNow reports it as a conflict.
func TestSkippedWhileLocked(t *testing.T) {
	t.Parallel()

	var c counter

	logs := &syncBuffer{}
	cfg := newConfig(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	r := NewRunner(cfg, c.job("held", nil))

	release, ok, err := cfg.Store.TryLockJob(t.Context(), "held")
	if err != nil || !ok {
		t.Fatalf("taking the lock: %v, %v", ok, err)
	}

	if err := r.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce while locked: %v", err)
	}

	run, err := r.RunNow(t.Context(), "held")
	if !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), `job "held" is running elsewhere`) {
		t.Fatalf("RunNow while locked: %v, want a conflict", err)
	}

	if run.ID.IsZero() || run.Result != domain.JobSkipped || run.Error != "" || !run.StartedAt.Equal(run.EndedAt) {
		t.Errorf("skipped run %+v, want a skipped run without an error that ends as it starts", run)
	}

	if c.runs() != 0 {
		t.Fatalf("the job ran %d times while locked", c.runs())
	}

	if last := lastRuns(t, r)["held"]; last != run {
		t.Errorf("latest run %+v, want the skipped one %+v", last, run)
	}

	if out := logs.String(); strings.Count(out, `level=DEBUG msg="background job skipped: another holder runs it" job=held`) != 2 {
		t.Errorf("want both skipped runs logged at debug level:\n%s", out)
	}

	release()

	run, err = r.RunNow(t.Context(), "held")
	if err != nil || run.Result != domain.JobOK || c.runs() != 1 {
		t.Fatalf("RunNow after the release: %+v, %v (ran %d times)", run, err, c.runs())
	}

	// The run released the lock again.
	release, ok, err = cfg.Store.TryLockJob(t.Context(), "held")
	if err != nil || !ok {
		t.Fatalf("lock after the run: %v, %v", ok, err)
	}

	release()
}

// TestRunNow runs a job on request, also one left out at startup, and
// returns the run as recorded with the job's error; an unknown job is not
// found and records nothing.
func TestRunNow(t *testing.T) {
	t.Parallel()

	var c counter

	failure := errors.New("no luck")
	late := c.job("late", nil)
	late.SkipAtStartup = true
	r := NewRunner(newConfig(nil), late, c.job("failing", failure))

	run, err := r.RunNow(t.Context(), "late")
	if err != nil || run.Job != "late" || run.Result != domain.JobOK || c.runs() != 1 {
		t.Fatalf("RunNow(late) = %+v, %v (ran %d times)", run, err, c.runs())
	}

	run, err = r.RunNow(t.Context(), "failing")
	if !errors.Is(err, failure) || run.Result != domain.JobFailed || run.Error != `job "failing": no luck` {
		t.Fatalf("RunNow(failing) = %+v, %v", run, err)
	}

	if last := lastRuns(t, r)["failing"]; last != run {
		t.Errorf("recorded %+v, want the returned run %+v", last, run)
	}

	run, err = r.RunNow(t.Context(), "unknown")
	if !errors.Is(err, domain.ErrNotFound) || !run.ID.IsZero() || c.runs() != 2 {
		t.Fatalf("RunNow(unknown) = %+v, %v", run, err)
	}

	var runs []domain.JobRun

	err = r.store.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		runs, err = tx.JobRuns().Latest(ctx)

		return err
	})
	if err != nil || len(runs) != 2 {
		t.Errorf("recorded runs %+v, %v; want those of late and failing only", runs, err)
	}
}

// errLock and errTx are the failures of a faultyStore.
var (
	errLock = errors.New("lock service down")
	errTx   = errors.New("database down")
)

// faultyStore fails the next lockFails calls of TryLockJob and the next
// txFails transactions.
type faultyStore struct {
	store.Store

	mu                 sync.Mutex
	lockFails, txFails int
}

func (s *faultyStore) fail(lock, tx int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lockFails, s.txFails = lock, tx
}

func (s *faultyStore) take(n *int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if *n == 0 {
		return false
	}

	*n--

	return true
}

func (s *faultyStore) TryLockJob(ctx context.Context, name string) (func(), bool, error) {
	if s.take(&s.lockFails) {
		return nil, false, errLock
	}

	return s.Store.TryLockJob(ctx, name)
}

func (s *faultyStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	if s.take(&s.txFails) {
		return errTx
	}

	return s.Store.InTx(ctx, mode, fn)
}

// TestLockFailure: a lock that cannot be taken fails the run without
// running the job; the failed run is recorded, and the loop goes on.
func TestLockFailure(t *testing.T) {
	t.Parallel()

	var c counter

	logs := &syncBuffer{}
	cfg := newConfig(slog.New(slog.NewTextHandler(logs, nil)))
	st := &faultyStore{Store: cfg.Store}
	cfg.Store = st
	r := NewRunner(cfg, c.job("a", nil))

	st.fail(1, 0)

	run, err := r.RunNow(t.Context(), "a")
	if !errors.Is(err, errLock) || !strings.Contains(err.Error(), `job "a": lock: lock service down`) {
		t.Fatalf("RunNow with a failing lock: %v", err)
	}

	if c.runs() != 0 || run.Result != domain.JobFailed || run.Error != err.Error() || !run.StartedAt.Equal(run.EndedAt) {
		t.Fatalf("run %+v (job ran %d times), want a failed run with the lock error", run, c.runs())
	}

	if last := lastRuns(t, r)["a"]; last != run {
		t.Errorf("recorded %+v, want %+v", last, run)
	}

	st.fail(1, 0)

	c.ran = make(chan string, 2) // before the loop starts
	tickers := newFakeTickers()
	r.newTicker = tickers.new
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		r.Run(ctx)
		close(done)
	}()

	// The first tick fails to lock; the loop runs the job on the second,
	// which it reads only after the first run. Run returns after the run
	// in progress, so the cancel ends the loop after that one.
	tickers.tick(t, time.Hour)
	tickers.tick(t, time.Hour)
	receive(t, c.ran)
	cancel()
	<-done

	if c.runs() != 1 {
		t.Errorf("the job ran %d times, want once after the failed lock", c.runs())
	}

	if out := logs.String(); !strings.Contains(out, `msg="background job failed" job=a`) ||
		!strings.Contains(out, "lock service down") {
		t.Errorf("want the lock failure logged:\n%s", out)
	}
}

// TestRecordFailure: a run that cannot be recorded keeps its result; the
// failure is logged, and the loop goes on.
func TestRecordFailure(t *testing.T) {
	t.Parallel()

	var c counter

	logs := &syncBuffer{}
	cfg := newConfig(slog.New(slog.NewTextHandler(logs, nil)))
	st := &faultyStore{Store: cfg.Store}
	cfg.Store = st
	r := NewRunner(cfg, c.job("a", nil))

	st.fail(0, 1)

	run, err := r.RunNow(t.Context(), "a")
	if err != nil || run.Result != domain.JobOK || c.runs() != 1 {
		t.Fatalf("RunNow with a failing record: %+v, %v", run, err)
	}

	if last := lastRuns(t, r)["a"]; !last.ID.IsZero() {
		t.Fatalf("recorded %+v, want no run", last)
	}

	if out := logs.String(); !strings.Contains(out, `msg="recording a job run failed" job=a err="database down"`) {
		t.Errorf("want the failed record logged:\n%s", out)
	}

	st.fail(0, 1)

	if _, err := r.Statuses(t.Context()); !errors.Is(err, errTx) {
		t.Errorf("Statuses with a failing store: %v", err)
	}

	st.fail(0, 1)

	c.ran = make(chan string, 2) // before the loop starts
	tickers := newFakeTickers()
	r.newTicker = tickers.new
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		r.Run(ctx)
		close(done)
	}()

	tickers.tick(t, time.Hour)
	receive(t, c.ran)
	tickers.tick(t, time.Hour)
	receive(t, c.ran)
	cancel()
	<-done

	if c.runs() != 3 {
		t.Errorf("the job ran %d times, want twice more in the loop", c.runs())
	}

	if last := lastRuns(t, r)["a"]; last.Result != domain.JobOK {
		t.Errorf("latest run %+v, want the loop's second run recorded", last)
	}
}

// backwardClock is a clock that goes back a second on every reading.
type backwardClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *backwardClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(-time.Second)

	return c.now
}

// TestInvalidRunNotRecorded: a run that ends before it started (a clock
// set back during the run) is invalid; it is logged, not recorded, and
// keeps its result.
func TestInvalidRunNotRecorded(t *testing.T) {
	t.Parallel()

	var c counter

	logs := &syncBuffer{}
	cfg := newConfig(slog.New(slog.NewTextHandler(logs, nil)))
	cfg.Clock = &backwardClock{now: t0}
	r := NewRunner(cfg, c.job("a", nil))

	run, err := r.RunNow(t.Context(), "a")
	if err != nil || run.Result != domain.JobOK || c.runs() != 1 {
		t.Fatalf("RunNow: %+v, %v", run, err)
	}

	if last := lastRuns(t, r)["a"]; !last.ID.IsZero() {
		t.Errorf("recorded %+v, want no run", last)
	}

	if out := logs.String(); !strings.Contains(out, `msg="recording a job run failed" job=a`) ||
		!strings.Contains(out, "cannot end before it started") {
		t.Errorf("want the invalid run logged:\n%s", out)
	}
}

// TestRunNowWhileLoopRuns: RunNow does not run a job the loop is running
// (the lock), and a run that shutdown interrupts is still recorded.
func TestRunNowWhileLoopRuns(t *testing.T) {
	t.Parallel()

	var (
		mu             sync.Mutex
		running, peak  int
		started        = make(chan struct{}, 1)
		blockFirstOnly = true
	)

	job := Job{Name: "slow", Every: time.Hour, Run: func(ctx context.Context) error {
		mu.Lock()
		running++
		peak = max(peak, running)
		block := blockFirstOnly
		blockFirstOnly = false
		mu.Unlock()

		defer func() {
			mu.Lock()
			running--
			mu.Unlock()
		}()

		if !block {
			return nil
		}

		started <- struct{}{}
		<-ctx.Done()

		return ctx.Err()
	}}
	r := NewRunner(newConfig(nil), job)
	tickers := newFakeTickers()
	r.newTicker = tickers.new

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		r.Run(ctx)
		close(done)
	}()

	tickers.tick(t, time.Hour)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not run the job")
	}

	run, err := r.RunNow(t.Context(), "slow")
	if !errors.Is(err, domain.ErrConflict) || run.Result != domain.JobSkipped {
		t.Fatalf("RunNow while the loop runs the job: %+v, %v", run, err)
	}

	cancel()
	<-done

	run, err = r.RunNow(t.Context(), "slow")
	if err != nil || run.Result != domain.JobOK {
		t.Fatalf("RunNow after the loop stopped: %+v, %v", run, err)
	}

	mu.Lock()
	defer mu.Unlock()

	if peak != 1 {
		t.Errorf("%d runs at once, want 1", peak)
	}
}

// TestInterruptedRunRecorded: a run that shutdown cancels is recorded,
// although its context is canceled.
func TestInterruptedRunRecorded(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	r := NewRunner(newConfig(nil), Job{Name: "slow", Every: time.Hour, Run: func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()

		return ctx.Err()
	}})
	tickers := newFakeTickers()
	r.newTicker = tickers.new

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		r.Run(ctx)
		close(done)
	}()

	tickers.tick(t, time.Hour)
	<-started
	cancel()
	<-done

	if last := lastRuns(t, r)["slow"]; last.Result != domain.JobFailed ||
		last.Error != `job "slow": context canceled` {
		t.Errorf("latest run %+v, want the interrupted run recorded as failed", last)
	}
}
