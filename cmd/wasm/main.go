//go:build js && wasm

// Command wasm builds the complete application as a WebAssembly module for
// the browser demo: the same app.New handler as the server, backed by an
// in-memory store with the fictional demo data, which the worker saves in the
// browser between visits. JavaScript calls it through
// globalThis.StudioScheduleAPI.fetch (see internal/wasmbridge).
//
//	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w -X main.version=..." -o app.wasm ./cmd/wasm
package main

import (
	"context"
	"log/slog"
	"os"
	"time"
	_ "time/tzdata" // the browser has no zoneinfo database

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/wasmbridge"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx := context.Background()
	clk := clock.System{}

	// The worker keeps the data of earlier visits (docs/demo-mode.md): restore
	// it before anything reads the store. A snapshot of another build is
	// refused, and the demo starts from the seed.
	st := memory.New()
	// Every committed change, by a request or a background job, is saved.
	st.OnCommit(wasmbridge.NotifyChanged)

	restored := false

	data, err := wasmbridge.TakeSnapshot()
	if err == nil && data != nil {
		err = st.Restore(data, version)
		restored = err == nil
	}

	if err != nil {
		logger.Warn("discarding the saved demo data", slog.Any("err", err))
	}

	if !restored {
		// A fresh demo is built around the day of the visit (in the school's
		// time zone): its term and workshops never lie in the past.
		loc, err := time.LoadLocation(seed.DemoSettings().Timezone)
		if err != nil {
			logger.Error("loading the demo time zone failed", slog.Any("err", err))
			os.Exit(1)
		}

		if err := seed.LoadDemoAt(ctx, st, domain.DateOf(clk.Now(), loc)); err != nil {
			logger.Error("seeding demo data failed", slog.Any("err", err))
			os.Exit(1)
		}
	}

	deps := app.Deps{
		Store:   st,
		Clock:   clk,
		Version: version,
		Logger:  logger,
		// The worker passes the app's URL (with the hash route prefix) for
		// links in mails, which the demo mailbox shows.
		AppURL: os.Getenv("APP_URL"),
		// Receipts render with the typst module in the worker.
		PDF:        wasmbridge.PDFRenderer{},
		Demo:       true,
		DemoLogins: seed.DemoLogins(),
	}
	handler, _ := app.New(deps)

	// Run the jobs once before the bridge answers the first request, so the
	// demo has lessons from the start; Go timers keep them running.
	runner := app.Jobs(deps)

	if err := runner.RunOnce(ctx); err != nil {
		logger.Error("background jobs failed", slog.Any("err", err))
	}

	go runner.Run(ctx)

	wasmbridge.Register(handler, version, wasmbridge.Persistence{
		Snapshot: func() ([]byte, error) { return st.Snapshot(version) },
		Restored: restored,
	})

	select {}
}
