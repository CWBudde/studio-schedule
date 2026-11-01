// Package jobs runs periodic background jobs, such as lesson generation.
//
// The same Runner serves both targets: the server binary and the WebAssembly
// demo. It uses one Go time.Ticker per job, and Go timers work in js/wasm
// (the runtime yields to the browser's event loop while goroutines wait), so
// the demo needs no JavaScript timer bridge. Both targets call RunOnce at
// startup, before they serve requests, and then Run in a goroutine. A
// Waker lets a request run a job before its next tick (Job.Wake), and
// RunNow runs one job on request (the command line).
//
// Every run takes the job's lock (store.Store.TryLockJob) and is recorded
// in the store (ADR 0020): its start, end, result and error. While another
// holder has the lock (a second server, or a command running the job), the
// run does not happen and is recorded as skipped, so two processes never
// run the same job at once. Statuses reads each job's latest run.
//
// Jobs run in-process and share the WASM build's single thread, so a job
// must stay cheap.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Job is a named function that runs every Every.
type Job struct {
	// Name identifies the job in logs, errors, its recorded runs and its
	// lock. It is unique within a Runner and at most domain.MaxJobNameLen
	// bytes long.
	Name string
	// Every is the interval between two runs; it must be positive.
	Every time.Duration
	// Run does the work. It should return soon after ctx is canceled.
	Run func(ctx context.Context) error
	// SkipAtStartup leaves the job out of RunOnce, for jobs that may block
	// on the network (such as sending mail) and must not delay startup.
	SkipAtStartup bool
	// Wake, if set, runs the job before its next tick when it receives (see
	// Waker.Chan): the application has work for it now.
	Wake <-chan struct{}
}

// Waker lets the application ask for a run of a job before its next tick,
// such as emailing an announcement right after it is published. The
// handler that asks returns at once; the job runs in the background. A nil
// *Waker wakes nothing.
type Waker struct {
	mu    sync.Mutex
	chans map[string]chan struct{}
}

// NewWaker returns a Waker without pending wakes.
func NewWaker() *Waker { return &Waker{chans: map[string]chan struct{}{}} }

// Chan returns the channel that wakes the job name, for Job.Wake (nil for
// a nil Waker, which never receives).
func (w *Waker) Chan(name string) <-chan struct{} {
	if w == nil {
		return nil
	}

	return w.channel(name)
}

func (w *Waker) channel(name string) chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()

	c, ok := w.chans[name]
	if !ok {
		// One pending wake is enough: the run it causes does all the work
		// there is.
		c = make(chan struct{}, 1)
		w.chans[name] = c
	}

	return c
}

// Wake asks for a run of the job name soon. It never blocks; wakes while
// one is pending coalesce.
func (w *Waker) Wake(name string) {
	if w == nil {
		return
	}

	select {
	case w.channel(name) <- struct{}{}:
	default:
	}
}

// Config holds what a Runner needs besides its jobs.
type Config struct {
	// Logger receives the runs' errors and panics; nil discards them.
	Logger *slog.Logger
	// Store records the runs and holds the jobs' locks.
	Store store.Store
	// Clock gives the runs' start and end.
	Clock clock.Clock
	// IDs gives the IDs of the recorded runs.
	IDs ids.Source
}

// Runner runs a fixed set of jobs.
type Runner struct {
	logger *slog.Logger
	store  store.Store
	clock  clock.Clock
	ids    ids.Source
	jobs   []Job

	// newTicker returns a channel that delivers ticks every d and a function
	// that stops it; tests replace it.
	newTicker func(d time.Duration) (<-chan time.Time, func())
}

// NewRunner returns a Runner for jobs, in this order. It panics without a
// Store, a Clock or IDs in cfg, on a job without a name (or with a longer
// one than domain.MaxJobNameLen), a positive interval or a function, and
// on two jobs with the same name.
func NewRunner(cfg Config, jobs ...Job) *Runner {
	if cfg.Store == nil || cfg.Clock == nil || cfg.IDs == nil {
		panic("jobs: the Runner needs a Store, a Clock and IDs")
	}

	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}

	names := make(map[string]bool, len(jobs))

	for _, j := range jobs {
		if strings.TrimSpace(j.Name) == "" || len(j.Name) > domain.MaxJobNameLen || j.Every <= 0 || j.Run == nil {
			panic(fmt.Sprintf("jobs: invalid job %q: needs a name of at most %d bytes, a positive interval and a function",
				j.Name, domain.MaxJobNameLen))
		}

		if names[j.Name] {
			panic(fmt.Sprintf("jobs: two jobs named %q", j.Name))
		}

		names[j.Name] = true
	}

	return &Runner{
		logger: cfg.Logger, store: cfg.Store, clock: cfg.Clock, ids: cfg.IDs, jobs: jobs,
		newTicker: newTimeTicker,
	}
}

func newTimeTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)

	return t.C, t.Stop
}

// Info describes a configured job.
type Info struct {
	Name  string
	Every time.Duration
}

// Jobs returns the configured jobs in the order they were given.
func (r *Runner) Jobs() []Info {
	out := make([]Info, len(r.jobs))
	for i, j := range r.jobs {
		out[i] = Info{Name: j.Name, Every: j.Every}
	}

	return out
}

// Status is a configured job and its latest recorded run.
type Status struct {
	Info
	// Last is the latest recorded run; its ID is zero if the job has none.
	Last domain.JobRun
}

// Statuses returns every configured job, in the order they were given,
// with its latest recorded run. Runs of jobs this Runner does not have
// are left out.
func (r *Runner) Statuses(ctx context.Context) ([]Status, error) {
	var latest []domain.JobRun

	err := r.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		latest, err = tx.JobRuns().Latest(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("job statuses: %w", err)
	}

	last := make(map[string]domain.JobRun, len(latest))
	for _, run := range latest {
		last[run.Job] = run
	}

	out := make([]Status, len(r.jobs))
	for i, info := range r.Jobs() {
		out[i] = Status{Info: info, Last: last[info.Name]}
	}

	return out, nil
}

// RunNow runs the job name now, in the caller's goroutine, under its lock
// like every run, also a job with SkipAtStartup, and returns the run as
// recorded. An unknown name is domain.ErrNotFound (and no run). A run
// skipped because another holder has the lock is returned with an error
// wrapping domain.ErrConflict; a failed run with the job's error.
func (r *Runner) RunNow(ctx context.Context, name string) (domain.JobRun, error) {
	i := slices.IndexFunc(r.jobs, func(j Job) bool { return j.Name == name })
	if i < 0 {
		return domain.JobRun{}, fmt.Errorf("%w: no job %q", domain.ErrNotFound, name)
	}

	run, err := r.run(ctx, r.jobs[i])
	if err == nil && run.Result == domain.JobSkipped {
		return run, fmt.Errorf("%w: job %q is running elsewhere", domain.ErrConflict, name)
	}

	return run, err
}

// RunOnce runs every job once (except those with SkipAtStartup), one after
// the other, and returns their errors joined. A failing or panicking job
// does not stop the others, and a skipped one is no error.
func (r *Runner) RunOnce(ctx context.Context) error {
	var errs []error

	for _, j := range r.jobs {
		if !j.SkipAtStartup {
			_, err := r.run(ctx, j)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// Run runs each job every Job.Every until ctx is canceled, and returns once
// all jobs have stopped. The first run of a job happens one interval after
// Run starts (use RunOnce for a run at startup). Errors and panics are
// logged with the job name; they do not stop the job's schedule.
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup

	for _, j := range r.jobs {
		wg.Go(func() { r.loop(ctx, j) })
	}

	wg.Wait()
}

// loop runs j on every tick and wake until ctx is canceled. Ticks that
// arrive while j runs are dropped, so runs never overlap.
func (r *Runner) loop(ctx context.Context, j Job) {
	ticks, stop := r.newTicker(j.Every)
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		case <-j.Wake:
		}

		if ctx.Err() != nil {
			return
		}

		if _, err := r.run(ctx, j); err != nil {
			r.logger.ErrorContext(ctx, "background job failed", slog.String("job", j.Name), slog.Any("err", err))
		}
	}
}

// run runs j once under its lock and records the run, which it returns
// with the job's error. A run whose lock another holder has is skipped:
// recorded, and no error. A lock that cannot be taken fails the run
// without running the job.
func (r *Runner) run(ctx context.Context, j Job) (domain.JobRun, error) {
	release, ok, err := r.store.TryLockJob(ctx, j.Name)
	if err != nil {
		err = fmt.Errorf("job %q: lock: %w", j.Name, err)
		now := r.clock.Now()

		return r.record(ctx, ended(j, now, now, err)), err
	}

	if !ok {
		r.logger.DebugContext(ctx, "background job skipped: another holder runs it", slog.String("job", j.Name))
		now := r.clock.Now()

		return r.record(ctx, domain.JobRun{Job: j.Name, StartedAt: now, EndedAt: now, Result: domain.JobSkipped}), nil
	}

	defer release()

	start := r.clock.Now()
	err = r.call(ctx, j)

	return r.record(ctx, ended(j, start, r.clock.Now(), err)), err
}

// ended returns the run of j from start to end that ended with err: ok
// without one, failed with its message otherwise.
func ended(j Job, start, end time.Time, err error) domain.JobRun {
	run := domain.JobRun{Job: j.Name, StartedAt: start, EndedAt: end, Result: domain.JobOK}
	if err != nil {
		run.Result, run.Error = domain.JobFailed, domain.JobRunError(err.Error())
	}

	return run
}

// record gives run an ID, stores it and returns it. It stores the run
// even if ctx is canceled (a run that shutdown interrupted), and a failure
// to store it is only logged: it changes neither the run nor its result.
func (r *Runner) record(ctx context.Context, run domain.JobRun) domain.JobRun {
	run.ID = ids.New[domain.JobRunID](r.ids)

	err := run.Validate()
	if err == nil {
		err = r.store.InTx(context.WithoutCancel(ctx), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			return tx.JobRuns().Create(ctx, run)
		})
	}

	if err != nil {
		r.logger.ErrorContext(ctx, "recording a job run failed", slog.String("job", run.Job), slog.Any("err", err))
	}

	return run
}

// call runs j and turns a panic into an error.
func (r *Runner) call(ctx context.Context, j Job) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.ErrorContext(ctx, "background job panicked", slog.String("job", j.Name),
				slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
			err = fmt.Errorf("job %q: panic: %v", j.Name, rec)
		}
	}()

	if err := j.Run(ctx); err != nil {
		return fmt.Errorf("job %q: %w", j.Name, err)
	}

	return nil
}
