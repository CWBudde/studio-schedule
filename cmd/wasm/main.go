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

	st := memory.New()
	if err := seed.LoadDemo(context.Background(), st); err != nil {
		logger.Error("seeding demo data failed", slog.Any("err", err))
		os.Exit(1)
	}

	handler, _ := app.New(app.Deps{
		Store:   st,
		Clock:   clock.System{},
		Version: version,
		Logger:  logger,
	})

	wasmbridge.Register(handler, version)

	select {}
}
