// Package jobs runs periodic background jobs, such as lesson generation.
//
// The same Runner serves both targets: the server binary and the WebAssembly
// demo. It uses one Go time.Ticker per job, and Go timers work in js/wasm
// (the runtime yields to the browser's event loop while goroutines wait), so
// the demo needs no JavaScript timer bridge. Both targets call RunOnce at
// startup, before they serve requests, and then Run in a goroutine.
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

// RunOnce runs every job once, one after the other, and returns their
// errors joined. A failing or panicking job does not stop the others.
func (r *Runner) RunOnce(ctx context.Context) error {
	var errs []error

	for _, j := range r.jobs {
		errs = append(errs, r.run(ctx, j))
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

// loop runs j on every tick until ctx is canceled. Ticks that arrive while
// j runs are dropped, so runs never overlap.
func (r *Runner) loop(ctx context.Context, j Job) {
	ticks, stop := r.newTicker(j.Every)
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
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
