package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestStartJobs(t *testing.T) {
	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC) // inside the demo term
	logger := slog.New(slog.DiscardHandler)
	stop := startJobs(t.Context(), app.Deps{Store: st, Clock: clock.NewFixed(now), Logger: logger}, logger)

	// The startup run has finished when startJobs returns.
	var count int

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		lessons, err := tx.Lessons().ListRange(ctx, now, now.AddDate(0, 0, 7))
		count = len(lessons)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if count == 0 {
		t.Fatal("no lessons generated at startup")
	}

	done := make(chan struct{})

	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
}

func TestStartJobsLogsFailures(t *testing.T) {
	var logs bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logs, nil))

	// A canceled context makes the startup run fail; startup goes on.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	stop := startJobs(ctx, app.Deps{Store: st, Logger: logger}, logger)
	stop()

	if !strings.Contains(logs.String(), "background jobs failed at startup") {
		t.Fatalf("startup failure not logged:\n%s", logs.String())
	}
}
