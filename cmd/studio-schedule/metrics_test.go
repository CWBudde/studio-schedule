package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestNoMetricsWithoutAddr(t *testing.T) {
	t.Parallel()

	deps := app.Deps{Store: memory.New()}
	if srv := setUpMetrics(t.Context(), config.Config{HTTPAddr: ":8080"}, &deps, slog.New(slog.DiscardHandler)); srv != nil {
		t.Fatalf("metrics server %s without METRICS_ADDR", srv.Addr)
	}

	if deps.Metrics != nil {
		t.Error("metrics are collected without METRICS_ADDR")
	}
}

func TestMetrics(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := memory.New()

	if err := seed.LoadDemo(ctx, st); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	addPendingMail(t, st, now)

	logger := slog.New(slog.DiscardHandler)
	deps := app.Deps{Store: st, Clock: clock.NewFixed(now), Logger: logger}

	srv := setUpMetrics(ctx, config.Config{MetricsAddr: "127.0.0.1:9464"}, &deps, logger)
	if srv == nil || srv.Addr != "127.0.0.1:9464" || deps.Metrics == nil {
		t.Fatalf("setUpMetrics = %v, deps.Metrics = %v", srv, deps.Metrics)
	}

	handler, _ := app.New(deps)
	get(t, handler, "/api/v1/health")
	get(t, handler, "/api/v1/lessons/00000000-0000-7000-8000-00000000abcd")

	if err := app.Jobs(deps).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	status, body := get(t, srv.Handler, "/metrics")
	if status != http.StatusOK {
		t.Fatalf("/metrics = %d %s", status, body)
	}

	for _, want := range []string{
		`studio_schedule_http_requests_total{method="GET",route="/api/v1/health",status="200"} 1`,
		`studio_schedule_http_requests_total{method="GET",route="/api/v1/lessons/{lessonId}",status="404"} 1`,
		`studio_schedule_http_request_duration_seconds_count{method="GET",route="/api/v1/health"} 1`,
		`studio_schedule_job_runs_total{job="generate-lessons",result="success"} 1`,
		`studio_schedule_job_duration_seconds_count{job="generate-lessons"} 1`,
		`studio_schedule_job_last_success_timestamp_seconds{job="generate-lessons"}`,
		`studio_schedule_outbox_mails{status="pending"} 1`,
		`studio_schedule_outbox_mails{status="failed"} 0`,
		`studio_schedule_build_info{version="` + version + `"} 1`,
		"go_goroutines ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}

	// Only /metrics, and the metrics never on the public handler.
	if status, _ := get(t, srv.Handler, "/"); status != http.StatusNotFound {
		t.Errorf("metrics server / = %d, want 404", status)
	}

	if status, body := get(t, handler, "/metrics"); status == http.StatusOK || strings.Contains(body, "studio_schedule_") {
		t.Errorf("the app serves /metrics: %d %s", status, body)
	}
}

func addPendingMail(t *testing.T, st store.Store, now time.Time) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Outbox().Create(ctx, domain.OutboxMail{
			ID: ids.New[domain.OutboxMailID](ids.V7{}), To: "ada@example.org", Subject: "Hello", Text: "Hi",
			CreatedAt: now, NextAttemptAt: now.Add(time.Hour),
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

	body, _ := io.ReadAll(rec.Body)

	return rec.Code, string(body)
}

func TestRunServersStopsWithContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		done <- runServers(ctx, slog.New(slog.DiscardHandler), &http.Server{Addr: "127.0.0.1:0"},
			&http.Server{Addr: "127.0.0.1:0"})
	}()

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("runServers = %v, want nil after cancel", err)
	}
}

func TestRunServersFailsWithThePublicServer(t *testing.T) {
	t.Parallel()

	taken := takePort(t)

	err := runServers(t.Context(), slog.New(slog.DiscardHandler), &http.Server{Addr: taken},
		&http.Server{Addr: "127.0.0.1:0"})
	if err == nil || !strings.Contains(err.Error(), taken) {
		t.Fatalf("runServers = %v, want the error of the server on the taken port", err)
	}
}

// TestRunServersSurvivesTheMetricsServer checks that a metrics server that
// cannot listen is logged and leaves the public server serving.
func TestRunServersSurvivesTheMetricsServer(t *testing.T) {
	t.Parallel()

	taken := takePort(t)
	public := freePort(t)
	records := make(chan slog.Record, 8)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		done <- runServers(ctx, slog.New(recordHandler(records)),
			&http.Server{Addr: public, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			})},
			&http.Server{Addr: taken})
	}()

	select {
	case rec := <-records:
		if rec.Level != slog.LevelError || !strings.Contains(rec.Message, "metrics server failed") {
			t.Errorf("logged %s %q, want the metrics server's failure", rec.Level, rec.Message)
		}
	case err := <-done:
		t.Fatalf("runServers returned %v after the metrics server failed", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the metrics server's failure was not logged")
	}

	if status := getStatus(t, "http://"+public+"/"); status != http.StatusTeapot {
		t.Errorf("public server answered %d", status)
	}

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("runServers = %v, want nil after cancel", err)
	}
}

// takePort returns the address of a port held for the whole test.
func takePort(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	return l.Addr().String()
}

// freePort returns the address of a port that was free a moment ago.
func freePort(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := l.Addr().String()
	_ = l.Close()

	return addr
}

// getStatus GETs url until the server answers (it starts concurrently) and
// returns the status.
func getStatus(t *testing.T, url string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}

		res, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = res.Body.Close()

			return res.StatusCode
		}

		if ctx.Err() != nil {
			t.Fatalf("GET %s: %v", url, err)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// recordHandler is a slog.Handler that sends every record to ch.
type recordHandler chan<- slog.Record

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	h <- r.Clone()

	return nil
}

func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }

// TestMetricsFailures: a failed job run counts as a failure and leaves its
// last success alone; an outbox that cannot be counted leaves the gauge out
// of a scrape that still succeeds, and logs a warning.
func TestMetricsFailures(t *testing.T) {
	t.Parallel()

	records := make(chan slog.Record, 8)
	down := errors.New("the database is down")

	m, reg := newMetrics(version, func(context.Context) (service.MailCounts, error) {
		return service.MailCounts{Pending: 3}, down
	}, slog.New(recordHandler(records)))

	m.ObserveJob("deliver-mail", errors.New("the mail server refused"), 2*time.Second)
	m.ObserveJob("purge-mail", nil, time.Second)

	status, body := get(t, metricsHandler(reg, slog.New(slog.DiscardHandler)), "/metrics")
	if status != http.StatusOK {
		t.Fatalf("/metrics = %d %s", status, body)
	}

	for _, want := range []string{
		`studio_schedule_job_runs_total{job="deliver-mail",result="failure"} 1`,
		`studio_schedule_job_duration_seconds_sum{job="deliver-mail"} 2`,
		`studio_schedule_job_runs_total{job="purge-mail",result="success"} 1`,
		`studio_schedule_job_last_success_timestamp_seconds{job="purge-mail"}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics lacks %s", want)
		}
	}

	for _, unwanted := range []string{
		`studio_schedule_job_runs_total{job="deliver-mail",result="success"}`,
		`studio_schedule_job_last_success_timestamp_seconds{job="deliver-mail"}`,
		"studio_schedule_outbox_mails{",
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("/metrics has %s", unwanted)
		}
	}

	select {
	case rec := <-records:
		var logged error

		rec.Attrs(func(a slog.Attr) bool {
			if a.Key == "err" {
				logged, _ = a.Value.Any().(error)
			}

			return true
		})

		if rec.Level != slog.LevelWarn || rec.Message != "metrics: counting the outbox failed" || !errors.Is(logged, down) {
			t.Errorf("logged %s %q (err %v)", rec.Level, rec.Message, logged)
		}
	default:
		t.Error("the failed count was not logged")
	}
}
