//go:build js && wasm

// Command wasm builds the complete application as a WebAssembly module for
// the browser demo: the same app.New handler as the server, backed by an
// in-memory store with the fictional demo data. JavaScript calls it through
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

	// The store lives only as long as the page, so the demo is built around
	// the day of the visit (in the school's time zone): its term and
	// workshops never lie in the past.
	loc, err := time.LoadLocation(seed.DemoSettings().Timezone)
	if err != nil {
		logger.Error("loading the demo time zone failed", slog.Any("err", err))
		os.Exit(1)
	}

	st := memory.New()
	if err := seed.LoadDemoAt(ctx, st, domain.DateOf(clk.Now(), loc)); err != nil {
		logger.Error("seeding demo data failed", slog.Any("err", err))
		os.Exit(1)
	}

	deps := app.Deps{
		Store:   st,
		Clock:   clk,
		Version: version,
		Logger:  logger,
		// The worker passes the app's URL (with the hash route prefix) for
		// links in mails, which the demo mailbox shows.
		AppURL:     os.Getenv("APP_URL"),
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

	wasmbridge.Register(handler, version)

	select {}
}
