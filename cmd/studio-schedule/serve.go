package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/mail/smtp"
	"github.com/cwbudde/studio-schedule/internal/pdf/typstwasm"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/webui"
)

const shutdownTimeout = 10 * time.Second

func newServeCmd(env config.LookupFunc) *cobra.Command {
	var flags serveFlags

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

			logger := newLogger(cmd.ErrOrStderr(), cfg.Log)

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return serve(ctx, cfg, logger)
		},
	}

	flags.register(cmd)

	return cmd
}

// serveFlags binds the config flags. Their defaults only show up in --help;
// config.Load applies the real defaults, so an unset flag never shadows the
// environment.
type serveFlags struct {
	databaseURL string
	httpAddr    string
	baseURL     string
	metricsAddr string
	logFormat   string
	logLevel    string
}

func (f *serveFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.httpAddr, config.FlagHTTPAddr, config.DefaultHTTPAddr,
		"listen address host:port (env "+config.EnvHTTPAddr+")")
	cmd.Flags().StringVar(&f.databaseURL, config.FlagDatabaseURL, "", databaseURLUsage)
	cmd.Flags().StringVar(&f.baseURL, config.FlagBaseURL, "",
		"public origin of the app, e.g. https://schedule.example.org (env "+config.EnvBaseURL+")")
	cmd.Flags().StringVar(&f.metricsAddr, config.FlagMetricsAddr, "",
		"listen address host:port of the Prometheus metrics (/metrics); empty: no metrics (env "+
			config.EnvMetricsAddr+")")
	cmd.Flags().StringVar(&f.logFormat, config.FlagLogFormat, config.DefaultLogFormat,
		"log format: text or json (env "+config.EnvLogFormat+")")
	cmd.Flags().StringVar(&f.logLevel, config.FlagLogLevel, config.DefaultLogLevel,
		"log level: debug, info, warn or error (env "+config.EnvLogLevel+")")
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
		DatabaseURL: changed(config.FlagDatabaseURL, &f.databaseURL),
		HTTPAddr:    changed(config.FlagHTTPAddr, &f.httpAddr),
		BaseURL:     changed(config.FlagBaseURL, &f.baseURL),
		LogFormat:   changed(config.FlagLogFormat, &f.logFormat),
		LogLevel:    changed(config.FlagLogLevel, &f.logLevel),
		MetricsAddr: changed(config.FlagMetricsAddr, &f.metricsAddr),
	}
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	st, closeStore, err := openStore(ctx, cfg, logger)
	if err != nil {
		return err
	}

	defer closeStore()

	clk := clock.System{}

	mailer, err := newMailer(cfg, clk, logger)
	if err != nil {
		return err
	}

	// Without a database the server runs on the demo data, like the web demo.
	demo := cfg.DatabaseURL == ""

	pdf := newPDFRenderer(ctx, clk, logger)
	defer func() { _ = pdf.Close(context.WithoutCancel(ctx)) }()

	deps := app.Deps{
		Store:   st,
		Clock:   clk,
		Version: version,
		Logger:  logger,
		UI:      webui.Handler(),
		AppURL:  appURL(cfg),
		Mailer:  mailer,
		PDF:     pdf,
		Demo:    demo,
		// Requests wake the background jobs started below.
		Wake: jobs.NewWaker(),

		TrustedProxies: cfg.TrustedProxies,
	}
	setTransportSecurity(&deps, cfg.BaseURL, cfg.HSTSIncludeSubdomains)
	if !demo && !isHTTPS(cfg.BaseURL) {
		logger.Warn("BASE_URL is not an https:// origin: mail links point to "+deps.AppURL+
			" and session cookies are not Secure; only acceptable for local development",
			slog.String("hint", "set "+config.EnvBaseURL+" to the public https origin"))
	}
	if demo {
		deps.DemoLogins = seed.DemoLogins()
	}
	// The readiness probe checks the database and its schema.
	if r, ok := st.(readier); ok {
		deps.Ready = r.Ready
	}

	metricsSrv := setUpMetrics(ctx, cfg, &deps, logger)
	handler, _ := app.New(deps)

	stopJobs := startJobs(ctx, deps, logger)
	defer stopJobs() // runs before closeStore: no job outlives the store

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

	logger.Info("listening", slog.String("addr", cfg.HTTPAddr), slog.String("version", version),
		slog.Any("config", cfg))

	if metricsSrv != nil {
		logger.Info("serving metrics", slog.String("addr", metricsSrv.Addr))
	}

	if err := runServers(ctx, logger, srv, metricsSrv); err != nil {
		return err
	}

	logger.Info("stopped")

	return nil
}

// readier is a store that can tell whether it is ready (postgres.Store).
type readier interface {
	Ready(ctx context.Context) error
}

// runServers runs the public server and, unless it is nil, the metrics
// server until ctx is canceled or the public server fails, then shuts both
// down gracefully. A failing metrics server is logged and leaves the app
// serving without metrics: monitoring must not take the app down.
func runServers(ctx context.Context, logger *slog.Logger, public, metrics *http.Server) error {
	publicDone := make(chan error, 1)

	go func() { publicDone <- listenAndServe(public) }()

	metricsDone := make(chan struct{})

	if metrics != nil {
		go func() {
			defer close(metricsDone)

			if err := listenAndServe(metrics); err != nil {
				logger.Error("metrics server failed; serving without metrics", slog.Any("err", err))
			}
		}()
	} else {
		close(metricsDone)
	}

	var errs []error

	publicStopped := false

	select {
	case err := <-publicDone:
		errs = append(errs, err) // the server stopped by itself: it failed
		publicStopped = true
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := public.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("graceful shutdown of %s: %w", public.Addr, err))
	}

	if metrics != nil {
		if err := metrics.Shutdown(shutdownCtx); err != nil {
			logger.Warn("graceful shutdown of the metrics server", slog.Any("err", err))
		}
	}

	if !publicStopped {
		errs = append(errs, <-publicDone)
	}

	<-metricsDone

	return errors.Join(errs...)
}

// listenAndServe runs s until it fails or is shut down (nil).
func listenAndServe(s *http.Server) error {
	if err := s.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server %s: %w", s.Addr, err)
	}

	return nil
}

// setUpMetrics returns the server of the Prometheus metrics at
// cfg.MetricsAddr and reports the app's measurements to it (deps.Metrics),
// or returns nil without METRICS_ADDR. The metrics have a server of their
// own, never the public one: operators expose them to their monitoring
// only.
func setUpMetrics(ctx context.Context, cfg config.Config, deps *app.Deps, logger *slog.Logger) *http.Server {
	if cfg.MetricsAddr == "" {
		return nil
	}

	metrics, reg := newMetrics(version, app.UnsentMails(*deps), logger)
	deps.Metrics = metrics

	return &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           metricsHandler(reg, logger),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
}

// setTransportSecurity sets Secure session cookies with the __Host- prefix
// and HSTS (for the subdomains too with subdomains, HSTS_INCLUDE_SUBDOMAINS)
// when BASE_URL is https. Plain HTTP is only for local development, where
// BASE_URL is unset or http://: browsers drop Secure cookies there, and HSTS
// must not pin the host to HTTPS.
func setTransportSecurity(deps *app.Deps, baseURL string, subdomains bool) {
	https := isHTTPS(baseURL)
	deps.InsecureCookies = !https
	deps.HostCookie = https
	deps.HSTS = https
	deps.HSTSIncludeSubdomains = subdomains
}

// isHTTPS reports whether the validated BASE_URL uses https.
func isHTTPS(baseURL string) bool {
	u, err := url.Parse(baseURL)

	return err == nil && u.Scheme == "https" // Parse lower-cases the scheme
}

// appURL is the root of the web app for links in mails: BASE_URL, or the
// listen address on localhost.
func appURL(cfg config.Config) string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL + "/"
	}

	_, port, _ := net.SplitHostPort(cfg.HTTPAddr) // validated by config.Load

	return "http://localhost:" + port + "/"
}

// newMailer returns the SMTP sender of cfg, or nil without SMTP_HOST: then
// mails stay in the outbox.
func newMailer(cfg config.Config, clk clock.Clock, logger *slog.Logger) (mail.Mailer, error) {
	if cfg.SMTP.Host == "" {
		if cfg.DatabaseURL != "" {
			logger.Warn("no mail server configured: mails stay in the outbox and are never sent",
				slog.String("hint", "set "+config.EnvSMTPHost+" and "+config.EnvSMTPFrom))
		}

		return nil, nil // no mailer is a valid configuration
	}

	sender, err := smtp.New(smtp.Config{
		Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username, Password: cfg.SMTP.Password,
		From: cfg.SMTP.From, TLS: smtp.TLSMode(cfg.SMTP.TLS),
	}, clk)
	if err != nil {
		return nil, fmt.Errorf("mail server: %w", err)
	}

	return sender, nil
}

// startJobs runs the background jobs once, so that lesson generation is
// done before the server listens, and then on their intervals. A failed job
// is logged and does not prevent startup. The returned function stops the
// jobs and waits for them; they also stop when ctx is canceled.
func startJobs(ctx context.Context, deps app.Deps, logger *slog.Logger) (stop func()) {
	runner := app.Jobs(deps)

	if err := runner.RunOnce(ctx); err != nil {
		logger.Error("background jobs failed at startup", slog.Any("err", err))
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)

		runner.Run(ctx)
	}()

	return func() {
		cancel()
		<-done
	}
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

		if err := warnWithoutSettings(ctx, st, logger); err != nil {
			st.Close()

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

// warnWithoutSettings logs a warning if the database has no school settings:
// the UI cannot load without them, and until the admin area exists only
// `studio-schedule seed` creates them.
func warnWithoutSettings(ctx context.Context, st store.Store, logger *slog.Logger) error {
	err := st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, err := tx.Settings().Get(ctx)

		return err
	})

	switch {
	case errors.Is(err, domain.ErrNotFound):
		logger.Warn("the database has no school settings, so the UI cannot load",
			slog.String("hint", "run `studio-schedule seed` to load the demo data"))
	case err != nil:
		return fmt.Errorf("read settings: %w", err)
	}

	return nil
}

// newPDFRenderer starts compiling the receipt renderer (typst run by wazero)
// in the background, with the compiled code cached in the user's cache
// directory if there is one, so a restart is quick. Renders wait for the
// compilation; a failure is logged at once and fails every receipt.
func newPDFRenderer(ctx context.Context, clk clock.Clock, logger *slog.Logger) *typstwasm.Renderer {
	opts := typstwasm.Options{}
	if dir, err := os.UserCacheDir(); err == nil {
		opts.CacheDir = filepath.Join(dir, "studio-schedule", "wazero")
	}

	r := typstwasm.New(opts)

	go func() {
		start := clk.Now()
		if err := r.Ready(ctx); err != nil && ctx.Err() == nil {
			logger.Error("the receipt renderer is unavailable: receipts and pass sales fail", slog.Any("err", err))

			return
		}

		logger.Debug("receipt renderer ready", slog.Duration("took", clk.Now().Sub(start)))
	}()

	return r
}
