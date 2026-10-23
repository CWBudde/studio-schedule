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
	_ "time/tzdata" // the browser has no zoneinfo database

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/wasmbridge"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	ctx := context.Background()

	st := memory.New()
	if err := seed.LoadDemo(ctx, st); err != nil {
		logger.Error("seeding demo data failed", slog.Any("err", err))
		os.Exit(1)
	}

	deps := app.Deps{
		Store:   st,
		Clock:   clock.System{},
		Version: version,
		Logger:  logger,
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
