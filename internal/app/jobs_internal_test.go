package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/jobs"
)

type jobObservation struct {
	name string
	err  error
	d    time.Duration
}

type recordingMetrics struct {
	noMetrics

	jobs []jobObservation
}

func (m *recordingMetrics) ObserveJob(name string, err error, d time.Duration) {
	m.jobs = append(m.jobs, jobObservation{name, err, d})
}

// manualClock is a clock that tests move.
type manualClock struct{ now time.Time }

func (c *manualClock) Now() time.Time { return c.now }

// TestMeasured checks that a job's runs are reported with their result,
// panics included, which still reach the runner.
func TestMeasured(t *testing.T) {
	t.Parallel()

	clk := &manualClock{now: time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)}
	metrics := &recordingMetrics{}
	deps := Deps{Clock: clk, Metrics: metrics}
	failure := errors.New("boom")

	ok := measured(jobs.Job{Name: "ok", Run: func(context.Context) error {
		clk.now = clk.now.Add(time.Second)

		return nil
	}}, deps)
	failing := measured(jobs.Job{Name: "failing", Run: func(context.Context) error { return failure }}, deps)
	panicking := measured(jobs.Job{Name: "panicking", Run: func(context.Context) error { panic("bad") }}, deps)

	if err := ok(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := failing(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("failing job returned %v", err)
	}

	func() {
		defer func() {
			if rec := recover(); rec != "bad" {
				t.Errorf("recovered %v, want the job's panic", rec)
			}
		}()

		_ = panicking(t.Context())
	}()

	want := []jobObservation{{"ok", nil, time.Second}, {"failing", failure, 0}, {"panicking", errJobPanicked, 0}}
	if len(metrics.jobs) != len(want) {
		t.Fatalf("observations = %v, want %v", metrics.jobs, want)
	}

	for i, got := range metrics.jobs {
		if got.name != want[i].name || !errors.Is(got.err, want[i].err) || got.d != want[i].d {
			t.Errorf("observation %d = %+v, want %+v", i, got, want[i])
		}
	}
}
