package app_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestJobsGenerateLessons(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer

	now := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC) // inside the demo term
	runner := app.Jobs(app.Deps{
		Store: st,
		Clock: clock.NewFixed(now),
		// As in production: UUIDv7 never collide with the seed's sequence IDs.
		IDs:    ids.V7{},
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})

	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}

	var count int

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		lessons, err := tx.Lessons().ListRange(ctx, now, now.AddDate(0, 0, 7))
		count = len(lessons)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// Every weekly demo class has a lesson in the coming seven days.
	if want := len(seed.Demo().Classes); count != want {
		t.Fatalf("%d lessons in the next week, want %d", count, want)
	}

	if !strings.Contains(logs.String(), "lessons generated") {
		t.Errorf("no log of the generated lessons:\n%s", logs.String())
	}
}

// TestJobsRecordRuns: every run is recorded (ADR 0020). After the startup
// run, each job has an ok run; a job runs on request (the command line),
// and an unknown one is not found.
func TestJobsRecordRuns(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	runner := app.Jobs(app.Deps{Store: st, Clock: clock.NewFixed(now), IDs: ids.V7{}})

	statuses, err := runner.Statuses(t.Context())
	if err != nil || len(statuses) != len(runner.Jobs()) {
		t.Fatalf("statuses before the first run: %+v, %v", statuses, err)
	}

	for _, s := range statuses {
		if !s.Last.ID.IsZero() {
			t.Errorf("%s has a run before the first: %+v", s.Name, s.Last)
		}
	}

	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}

	statuses, err = runner.Statuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	var names []string

	for _, s := range statuses {
		names = append(names, s.Name)

		if s.Every <= 0 || s.Last.Job != s.Name || s.Last.Result != domain.JobOK || !s.Last.StartedAt.Equal(now) {
			t.Errorf("%s (every %s): latest run %+v, want an ok run at startup", s.Name, s.Every, s.Last)
		}
	}

	if !slices.Contains(names, "generate-lessons") || !slices.Contains(names, "purge-personal-data") {
		t.Errorf("jobs %v, want generate-lessons and purge-personal-data among them", names)
	}

	run, err := runner.RunNow(t.Context(), "generate-lessons")
	if err != nil || run.Job != "generate-lessons" || run.Result != domain.JobOK || run.ID == statuses[0].Last.ID {
		t.Fatalf("RunNow(generate-lessons) = %+v, %v", run, err)
	}

	if _, err := runner.RunNow(t.Context(), "no-such-job"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RunNow of an unknown job: %v, want not found", err)
	}
}

func TestJobsWithoutSettings(t *testing.T) {
	t.Parallel()

	// An empty database (PostgreSQL before `seed`) has nothing to generate;
	// the job must not fail every hour because of it.
	runner := app.Jobs(app.Deps{Store: memory.New()})

	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce without settings: %v", err)
	}
}

// TestJobsWaitlistExpiry ends the open waiting-list entries of lessons that
// have started: the demo's full workshop (2026-10-02 for the fixed anchor)
// is over at the test's time.
func TestJobsWaitlistExpiry(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer

	runner := app.Jobs(app.Deps{
		Store: st, Clock: clock.NewFixed(time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)), IDs: ids.V7{},
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})

	for range 2 { // the second run finds nothing to do
		if err := runner.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		for _, want := range seed.Demo().WaitlistEntries {
			e, err := tx.Waitlist().Get(ctx, want.ID)
			if err != nil {
				return err
			}

			if e.Status != domain.WaitlistExpired {
				t.Errorf("entry %+v, want it expired", e)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(logs.String(), "waiting lists settled") != 1 {
		t.Errorf("want one log of the settled lists:\n%s", logs.String())
	}
}
