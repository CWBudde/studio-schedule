// Package jobs runs periodic background jobs, such as lesson generation.
//
// The same Runner serves both targets: the server binary and the WebAssembly
// demo. It uses one Go time.Ticker per job, and Go timers work in js/wasm
// (the runtime yields to the browser's event loop while goroutines wait), so
// the demo needs no JavaScript timer bridge. Both targets call RunOnce at
// startup, before they serve requests, and then Run in a goroutine. A
// Waker lets a request run a job before its next tick (Job.Wake).
//
// Jobs run in-process and share the WASM build's single thread, so a job
// must stay cheap and must not assume it is the only instance running.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Job is a named function that runs every Every.
type Job struct {
	// Name identifies the job in logs and errors.
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

// Runner runs a fixed set of jobs.
type Runner struct {
	logger *slog.Logger
	jobs   []Job

	// newTicker returns a channel that delivers ticks every d and a function
	// that stops it; tests replace it.
	newTicker func(d time.Duration) (<-chan time.Time, func())
}

// NewRunner returns a Runner for jobs. A nil logger discards everything.
// It panics on a job without a name, a positive interval or a function.
func NewRunner(logger *slog.Logger, jobs ...Job) *Runner {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	for _, j := range jobs {
		if j.Name == "" || j.Every <= 0 || j.Run == nil {
			panic(fmt.Sprintf("jobs: invalid job %q: needs a name, a positive interval and a function", j.Name))
		}
	}

	return &Runner{logger: logger, jobs: jobs, newTicker: newTimeTicker}
}

func newTimeTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)

	return t.C, t.Stop
}

// RunOnce runs every job once (except those with SkipAtStartup), one after
// the other, and returns their errors joined. A failing or panicking job does not stop the others.
func (r *Runner) RunOnce(ctx context.Context) error {
	var errs []error

	for _, j := range r.jobs {
		if !j.SkipAtStartup {
			errs = append(errs, r.run(ctx, j))
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

		if err := r.run(ctx, j); err != nil {
			r.logger.ErrorContext(ctx, "background job failed", slog.String("job", j.Name), slog.Any("err", err))
		}
	}
}

// run runs j once and turns a panic into an error.
func (r *Runner) run(ctx context.Context, j Job) (err error) {
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
