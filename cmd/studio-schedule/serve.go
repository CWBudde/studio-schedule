package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/webui"
)

const shutdownTimeout = 10 * time.Second

func newServeCmd() *cobra.Command {
	var (
		addr string
		logs logOptions
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP server (API and web UI)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := logs.logger(cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return serve(ctx, addr, logger)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	logs.register(cmd)

	return cmd
}

func serve(ctx context.Context, addr string, logger *slog.Logger) error {
	// Phase 0: in-memory store with demo data. PostgreSQL follows in Phase 1.
	st := memory.New()
	if err := seed.LoadDemo(ctx, st); err != nil {
		return err
	}

	handler, _ := app.New(app.Deps{
		Store:   st,
		Clock:   clock.System{},
		Version: version,
		Logger:  logger,
		UI:      webui.Handler(),
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)

	go func() {
		logger.Info("listening", slog.String("addr", addr), slog.String("version", version))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}

	logger.Info("stopped")

	return nil
}
