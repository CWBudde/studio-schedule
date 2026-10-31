package app_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// TestNewWithoutStore: the store is the one dependency without a default;
// New and Jobs refuse to build without it.
func TestNewWithoutStore(t *testing.T) {
	t.Parallel()

	for name, build := range map[string]func(){
		"New":  func() { app.New(app.Deps{}) },
		"Jobs": func() { app.Jobs(app.Deps{}) },
	} {
		func() {
			defer func() {
				if rec := recover(); rec != "app: Deps.Store is required" {
					t.Errorf("%s without a store panicked with %v", name, rec)
				}
			}()

			build()
		}()
	}
}

// TestOperationWithoutAccessLevel: an operation registered on the app's
// API without an access level (huma.Register instead of httpapi.Declare)
// fails closed: its handler never runs, the request is a 500, and the
// mistake is logged with the operation's ID.
func TestOperationWithoutAccessLevel(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	logger := slog.New(slog.NewTextHandler(&logs, nil))
	h, api := app.New(app.Deps{Store: memory.New(), Logger: logger})

	var ran atomic.Bool

	huma.Register(api, huma.Operation{
		OperationID: "undeclaredOperation", Method: http.MethodGet, Path: "/api/v1/undeclared-operation",
	}, func(context.Context, *struct{}) (*struct{}, error) {
		ran.Store(true)

		return &struct{}{}, nil
	})

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/undeclared-operation", nil))
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	if got := decode(t, body)["detail"]; got != "internal server error" {
		t.Errorf("detail = %v", got)
	}

	if ran.Load() {
		t.Error("the handler of an operation without access level ran")
	}

	line := grepLines(logs.String(), "operation without access level")
	if !strings.Contains(line, "level=ERROR") || !strings.Contains(line, "operation=undeclaredOperation") {
		t.Errorf("log:\n%s", logs.String())
	}
}

// TestReadyzProbeGivesUp: a probe that finds another probe's check running
// waits for it, but only as long as its own request lasts: then it answers
// 503 (without the context's error), starts no check of its own and logs
// nothing, while the running check goes on and answers the first probe.
func TestReadyzProbeGivesUp(t *testing.T) {
	t.Parallel()

	var (
		calls atomic.Int32
		logs  syncBuffer
	)

	started, release := make(chan struct{}), make(chan struct{})

	clk := &stepClock{t: time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)}
	h, _ := app.New(app.Deps{
		Store: memory.New(), Clock: clk, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		Ready: func(context.Context) error {
			if calls.Add(1) == 1 {
				close(started)
			}

			<-release

			return nil
		},
	})

	first := make(chan *httptest.ResponseRecorder)

	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		first <- rec
	}()

	<-started

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(ctx))
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("waiting probe: /readyz = %d %s", res.StatusCode, body)
	}

	if got := decode(t, body)["detail"]; got != "not ready" || strings.Contains(string(body), "deadline") {
		t.Errorf("waiting probe: %s", body)
	}

	close(release)

	if rec := <-first; rec.Code != http.StatusOK {
		t.Errorf("first probe: /readyz = %d %s", rec.Code, rec.Body)
	}

	if n := calls.Load(); n != 1 {
		t.Errorf("%d checks, want the first probe's only", n)
	}

	if out := logs.String(); strings.Contains(out, "not ready") {
		t.Errorf("a probe that gave up logged the app unready:\n%s", out)
	}
}

// TestUnmatchedFeedPathRedacted: a path under the feed prefix with the
// feed suffix that matches no route (chi takes a feed token up to its first
// dot) is redacted in the 404 and the access log with the suffix kept.
func TestUnmatchedFeedPathRedacted(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h, _ := app.New(app.Deps{Store: memory.New(), Logger: logger})

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/ical/c3secret.part.ics", nil))
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	if got := decode(t, body)["detail"]; got != "no route for /ical/<redacted>.ics" {
		t.Errorf("detail = %v", got)
	}

	out := logs.String()
	if line := grepLines(out, `msg="http request"`); !strings.Contains(line, "path=/ical/<redacted>.ics") ||
		!strings.Contains(line, "status=404") {
		t.Errorf("access log:\n%s", out)
	}

	if strings.Contains(out, "c3secret") {
		t.Errorf("the secret reached the log:\n%s", out)
	}
}

// TestPanicInUnmatchedRoute: a panic of the web app's handler, which serves
// the paths no route matches, is logged with the request's path (redacted
// by the path redactor, which leaves a path without a secret alone) and
// answered with a 500 problem.
func TestPanicInUnmatchedRoute(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	ui := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("the web app broke") })
	h, _ := app.New(app.Deps{Store: memory.New(), Logger: slog.New(slog.NewTextHandler(&logs, nil)), UI: ui})

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/web-page/kept", nil))
	if res.StatusCode != http.StatusInternalServerError || strings.Contains(string(body), "the web app broke") {
		t.Fatalf("status = %d, body %s", res.StatusCode, body)
	}

	line := grepLines(logs.String(), `msg="panic in handler"`)
	if !strings.Contains(line, "path=/web-page/kept") || !strings.Contains(line, `panic="the web app broke"`) {
		t.Errorf("log:\n%s", logs.String())
	}
}
