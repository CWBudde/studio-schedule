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
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/webui"
)

const shutdownTimeout = 10 * time.Second

func newServeCmd(env config.LookupFunc) *cobra.Command {
	var (
		flags serveFlags
		logs  logOptions
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP server (API and web UI)",
		Long: "Run the HTTP server (API and web UI). Every setting can also be given as an " +
			"environment variable (see .env.example); a flag on the command line wins over the " +
			"environment.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(env, flags.explicit(cmd))
			if err != nil {
				return err
			}

			logger, err := logs.logger(cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return serve(ctx, cfg, logger)
		},
	}

	flags.register(cmd)
	logs.register(cmd)

	return cmd
}

// serveFlags binds the config flags. Their defaults only show up in --help;
// config.Load applies the real defaults, so an unset flag never shadows the
// environment.
type serveFlags struct {
	databaseURL string
	httpAddr    string
	baseURL     string
}

func (f *serveFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.httpAddr, "addr", config.DefaultHTTPAddr,
		"listen address host:port (env "+config.EnvHTTPAddr+")")
	cmd.Flags().StringVar(&f.databaseURL, "database-url", "", databaseURLUsage)
	cmd.Flags().StringVar(&f.baseURL, "base-url", "",
		"public origin of the app, e.g. https://schedule.example.org (env "+config.EnvBaseURL+")")
}

// explicit returns the flags given on the command line.
func (f *serveFlags) explicit(cmd *cobra.Command) config.Flags {
	changed := func(name string, v *string) *string {
		if cmd.Flags().Changed(name) {
			return v
		}

		return nil
	}

	return config.Flags{
		DatabaseURL: changed("database-url", &f.databaseURL),
		HTTPAddr:    changed("addr", &f.httpAddr),
		BaseURL:     changed("base-url", &f.baseURL),
	}
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	st, closeStore, err := openStore(ctx, cfg, logger)
	if err != nil {
		return err
	}

	defer closeStore()

	handler, _ := app.New(app.Deps{
		Store:   st,
		Clock:   clock.System{},
		Version: version,
		Logger:  logger,
		UI:      webui.Handler(),
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
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
		logger.Info("listening", slog.String("addr", cfg.HTTPAddr), slog.String("version", version),
			slog.Any("config", cfg))
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

// openStore returns the PostgreSQL store if a database is configured.
// Otherwise it returns an in-memory store with the demo data, whose changes
// are lost on exit.
func openStore(ctx context.Context, cfg config.Config, logger *slog.Logger) (store.Store, func(), error) {
	if cfg.DatabaseURL != "" {
		st, err := openPostgres(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, nil, err
		}

		return st, st.Close, nil
	}

	logger.Warn("no database configured: using the in-memory store with demo data, all changes are lost on exit",
		slog.String("hint", "set "+config.EnvDatabaseURL+" to use PostgreSQL"))

	st := memory.New()
	if err := seed.LoadDemo(ctx, st); err != nil {
		return nil, nil, err
	}

	return st, func() {}, nil
}
