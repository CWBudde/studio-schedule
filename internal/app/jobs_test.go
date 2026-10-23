package app_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
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
		Store:  st,
		Clock:  clock.NewFixed(now),
		IDs:    ids.NewSequence(),
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

func TestJobsWithoutSettings(t *testing.T) {
	t.Parallel()

	// An empty database (PostgreSQL before `seed`) has nothing to generate;
	// the job must not fail every hour because of it.
	runner := app.Jobs(app.Deps{Store: memory.New()})

	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce without settings: %v", err)
	}
}
