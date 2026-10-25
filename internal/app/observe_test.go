package app_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/webui"
)

// The first ID of an ids.Sequence, the request ID tests generate.
const firstSequenceID = "00000000-0000-7000-8000-000000000001"

const unknownLesson = "/api/v1/lessons/00000000-0000-7000-8000-00000000abcd"

func TestRequestID(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	for _, tc := range []struct {
		name   string
		remote string
		header string
		want   string
	}{
		{"generated", "192.0.2.1:1234", "", firstSequenceID},
		{"from a trusted proxy", "10.1.2.3:1234", "proxy-id-42", "proxy-id-42"},
		{"from an untrusted peer", "192.0.2.1:1234", "client-id", firstSequenceID},
		{"invalid from a trusted proxy", "10.1.2.3:1234", "bad id\twith tab", firstSequenceID},
		{"too long from a trusted proxy", "10.1.2.3:1234", strings.Repeat("x", 129), firstSequenceID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, _ := app.New(app.Deps{
				Store: memory.New(), RequestIDs: ids.NewSequence(), TrustedProxies: trusted,
			})

			req := httptest.NewRequest(http.MethodGet, unknownLesson, nil)
			req.RemoteAddr = tc.remote

			if tc.header != "" {
				req.Header.Set("X-Request-Id", tc.header)
			}

			res, body := do(t, h, req)
			if got := res.Header.Get("X-Request-Id"); got != tc.want {
				t.Errorf("X-Request-Id = %q, want %q", got, tc.want)
			}

			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, body %s", res.StatusCode, body)
			}

			if got := decode(t, body)["requestId"]; got != tc.want {
				t.Errorf("problem requestId = %v, want %q", got, tc.want)
			}
		})
	}
}

// TestRequestIDInPlainProblems checks the problems written outside Huma:
// unknown API paths, wrong methods, rejected cross-origin requests and
// failed readiness.
func TestRequestIDInPlainProblems(t *testing.T) {
	t.Parallel()

	h, _ := app.New(app.Deps{
		Store: memory.New(), UI: webui.Handler(),
		Ready: func(context.Context) error { return errors.New("database down") },
	})

	crossOrigin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{}"))
	crossOrigin.Header.Set("Sec-Fetch-Site", "cross-site")

	for name, req := range map[string]*http.Request{
		"unknown API path": httptest.NewRequest(http.MethodGet, "/api/v1/nothing", nil),
		"wrong method":     httptest.NewRequest(http.MethodDelete, "/api/v1/health", nil),
		"cross-origin":     crossOrigin,
		"not ready":        httptest.NewRequest(http.MethodGet, "/readyz", nil),
	} {
		res, body := do(t, h, req)

		id := res.Header.Get("X-Request-Id")
		if id == "" || res.StatusCode < http.StatusBadRequest {
			t.Errorf("%s: status %d, X-Request-Id %q", name, res.StatusCode, id)

			continue
		}

		if got := decode(t, body)["requestId"]; got != id {
			t.Errorf("%s: problem requestId = %v, want %q; body %s", name, got, id, body)
		}
	}
}

func TestAccessLog(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h, _ := app.New(app.Deps{Store: memory.New(), Logger: logger, RequestIDs: ids.NewSequence()})

	do(t, h, httptest.NewRequest(http.MethodGet, unknownLesson, nil))
	do(t, h, httptest.NewRequest(http.MethodGet, "/ical/secret-token.ics", nil))
	do(t, h, httptest.NewRequest(http.MethodGet, "/healthz", nil)) // at Debug

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), logs.String())
	}

	for _, want := range []string{
		"level=INFO", `msg="http request"`, "method=GET", "path=" + unknownLesson, "status=404", "duration=",
		"requestId=" + firstSequenceID,
	} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("access log line %q lacks %q", lines[0], want)
		}
	}

	if strings.Contains(lines[1], "secret-token") || !strings.Contains(lines[1], "/ical/<redacted>.ics") {
		t.Errorf("feed token not redacted: %q", lines[1])
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	failing := func(context.Context) error { return errors.New("database down") }
	// The probe reports the process, not its dependencies; the UI fallback
	// does not swallow it.
	h, _ := app.New(app.Deps{Store: memory.New(), UI: webui.Handler(), Ready: failing})

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Fatalf("/healthz = %d %s", res.StatusCode, body)
	}

	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestReadyz(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		readyErr error
		deadline bool
	)

	clk := &stepClock{t: time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)}
	h, _ := app.New(app.Deps{Store: memory.New(), UI: webui.Handler(), Clock: clk, Ready: func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()

		_, deadline = ctx.Deadline()

		return readyErr
	}})

	res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Fatalf("ready: /readyz = %d %s", res.StatusCode, body)
	}

	if !deadline {
		t.Error("the readiness check runs without a timeout")
	}

	mu.Lock()
	readyErr = errors.New("schema pending: secret detail")
	mu.Unlock()
	clk.advance(3 * time.Second) // past the cached result

	res, body = do(t, h, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("not ready: /readyz = %d %s", res.StatusCode, body)
	}

	if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}

	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}

	if strings.Contains(string(body), "secret detail") {
		t.Errorf("the problem leaks the cause: %s", body)
	}
}

// TestReadyzCached checks that a readiness result answers the probes for a
// moment, that concurrent probes share one check, and that only a change of
// the state is logged.
func TestReadyzCached(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		calls    int
		readyErr error
		logs     syncBuffer
	)

	release := make(chan struct{})
	close(release)

	gate := func() chan struct{} {
		mu.Lock()
		defer mu.Unlock()

		return release
	}

	clk := &stepClock{t: time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)}
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h, _ := app.New(app.Deps{Store: memory.New(), Clock: clk, Logger: logger, Ready: func(context.Context) error {
		<-gate()

		mu.Lock()
		defer mu.Unlock()

		calls++

		return readyErr
	}})

	probe := func() int {
		res, _ := do(t, h, httptest.NewRequest(http.MethodGet, "/readyz", nil))

		return res.StatusCode
	}

	setReady := func(err error) {
		mu.Lock()
		defer mu.Unlock()

		readyErr = err
	}

	wantCalls := func(name string, want int) {
		t.Helper()

		mu.Lock()
		defer mu.Unlock()

		if calls != want {
			t.Errorf("%s: %d checks, want %d", name, calls, want)
		}
	}

	// Concurrent probes share one check.
	mu.Lock()
	release = make(chan struct{})
	mu.Unlock()

	var wg sync.WaitGroup

	statuses := make(chan int, 8)
	for range 8 {
		wg.Go(func() { statuses <- probe() })
	}

	time.Sleep(50 * time.Millisecond) // let the probes reach the check
	close(gate())
	wg.Wait()
	close(statuses)

	for status := range statuses {
		if status != http.StatusOK {
			t.Errorf("concurrent probe = %d", status)
		}
	}

	wantCalls("concurrent", 1)

	// The result answers until it is readyCacheTTL old.
	setReady(errors.New("database down"))

	if status := probe(); status != http.StatusOK {
		t.Errorf("cached probe = %d", status)
	}

	wantCalls("cached", 1)

	for range 3 {
		clk.advance(3 * time.Second)

		if status := probe(); status != http.StatusServiceUnavailable {
			t.Errorf("failing probe = %d", status)
		}
	}

	setReady(nil)
	clk.advance(3 * time.Second)

	if status := probe(); status != http.StatusOK {
		t.Errorf("recovered probe = %d", status)
	}

	wantCalls("after recovery", 5)

	if n := strings.Count(logs.String(), `msg="not ready"`); n != 1 {
		t.Errorf("logged %d times not ready, want once:\n%s", n, logs.String())
	}

	if n := strings.Count(logs.String(), `msg="ready again"`); n != 1 {
		t.Errorf("logged %d times ready again, want once:\n%s", n, logs.String())
	}
}

// TestProbesHead checks that the probes answer HEAD like GET.
func TestProbesHead(t *testing.T) {
	t.Parallel()

	failing := func(context.Context) error { return errors.New("database down") }

	for _, tc := range []struct {
		path  string
		ready func(context.Context) error
		want  int
	}{
		{"/healthz", failing, http.StatusOK},
		{"/readyz", nil, http.StatusOK},
		{"/readyz", failing, http.StatusServiceUnavailable},
	} {
		h, _ := app.New(app.Deps{Store: memory.New(), UI: webui.Handler(), Ready: tc.ready})

		if res, body := do(t, h, httptest.NewRequest(http.MethodHead, tc.path, nil)); res.StatusCode != tc.want {
			t.Errorf("HEAD %s = %d, want %d; %s", tc.path, res.StatusCode, tc.want, body)
		}
	}
}

// TestNoSecretPathInLogs requests every route of the API, and paths near
// the routes with a secret parameter that match none, with recognizable
// parameter values, and checks that no secret value reaches the log or a
// response, while the other values stay in the log.
func TestNoSecretPathInLogs(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h, api := app.New(app.Deps{Store: memory.New(), Logger: logger, UI: webui.Handler()})

	param := regexp.MustCompile(`\{([^}]+)\}`)
	secret := func(name string) bool { return name == "token" || strings.HasSuffix(name, "Token") }

	var (
		paths   []string
		kept    []string
		secrets int
	)

	for pattern, item := range api.OpenAPI().Paths {
		isSecret := false
		path := param.ReplaceAllStringFunc(pattern, func(m string) string {
			name := strings.Trim(m, "{}")
			if secret(name) {
				isSecret = true

				return "SECRET" + name
			}

			kept = append(kept, "kept"+name)

			return "kept" + name
		})

		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil {
				continue
			}

			res, body := do(t, h, httptest.NewRequest(op.Method, path, strings.NewReader("{}")))
			if strings.Contains(string(body), "SECRET") {
				t.Errorf("%s %s: the response names the secret: %d %s", op.Method, pattern, res.StatusCode, body)
			}
		}

		paths = append(paths, path)

		if isSecret {
			secrets++
			// Near misses: a trailing segment, the secret segment alone.
			end := strings.LastIndex(path, "SECRET")
			end += strings.IndexAny(path[end:]+"/", "./")

			for _, miss := range []string{path + "/more", path[:end], path[:end] + "/"} {
				res, body := do(t, h, httptest.NewRequest(http.MethodGet, miss, nil))
				if strings.Contains(string(body), "SECRET") {
					t.Errorf("GET %s: the response names the secret: %d %s", miss, res.StatusCode, body)
				}
			}
		}
	}

	if secrets < 2 {
		t.Fatalf("only %d routes with a secret parameter; the test lost them", secrets)
	}

	out := logs.String()
	if strings.Contains(out, "SECRET") {
		t.Errorf("a secret reached the log:\n%s", grepLines(out, "SECRET"))
	}

	for _, k := range kept {
		if !strings.Contains(out, k) {
			t.Errorf("the log lacks the parameter value %s", k)
		}
	}

	if n := strings.Count(out, `msg="http request"`); n < len(paths) {
		t.Errorf("%d access log lines for %d paths", n, len(paths))
	}
}

// grepLines returns the lines of s that contain sub.
func grepLines(s, sub string) string {
	var out []string

	for line := range strings.Lines(s) {
		if strings.Contains(line, sub) {
			out = append(out, line)
		}
	}

	return strings.Join(out, "")
}

// TestPanicLoggedAndCounted checks that a request whose handler panics is
// logged and counted as a 500, also when it aborts the connection.
func TestPanicLoggedAndCounted(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	metrics := &fakeMetrics{}
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h, api := app.New(app.Deps{Store: memory.New(), Logger: logger, Metrics: metrics})

	for path, cause := range map[string]any{"/api/v1/test-panic": "boom", "/api/v1/test-abort": http.ErrAbortHandler} {
		huma.Register(api, httpapi.Declare(huma.Operation{
			OperationID: strings.TrimPrefix(path, "/api/v1/"), Method: http.MethodGet, Path: path,
		}, httpapi.AccessPublic), func(context.Context, *struct{}) (*struct{}, error) {
			panic(cause)
		})
	}

	if res, _ := do(t, h, httptest.NewRequest(http.MethodGet, "/api/v1/test-panic", nil)); res.StatusCode != http.StatusInternalServerError {
		t.Errorf("panic: status %d", res.StatusCode)
	}

	func() {
		defer func() {
			if rec := recover(); rec != http.ErrAbortHandler { //nolint:errorlint // the very value
				t.Errorf("abort: recovered %v, want http.ErrAbortHandler", rec)
			}
		}()

		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/test-abort", nil))
	}()

	for _, path := range []string{"/api/v1/test-panic", "/api/v1/test-abort"} {
		if !strings.Contains(logs.String(), "path="+path+" status=500") {
			t.Errorf("no access log line for %s:\n%s", path, logs.String())
		}
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	want := "GET /api/v1/test-panic Internal Server Error\nGET /api/v1/test-abort Internal Server Error"
	if got := strings.Join(metrics.requests, "\n"); got != want {
		t.Errorf("requests =\n%s\nwant\n%s", got, want)
	}
}

// TestAccessLogLevels checks that the API logs at Info and the web app's
// pages and files, like the probes, at Debug.
func TestAccessLogLevels(t *testing.T) {
	t.Parallel()

	var logs syncBuffer

	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h, _ := app.New(app.Deps{Store: memory.New(), Logger: logger, UI: webui.Handler()})

	for _, p := range []string{"/", "/schedule/some/page", "/assets/missing.js", "/healthz", "/readyz"} {
		do(t, h, httptest.NewRequest(http.MethodGet, p, nil))
	}

	if logs.String() != "" {
		t.Errorf("the web app and the probes logged at Info:\n%s", logs.String())
	}

	for _, p := range []string{"/api/v1/health", "/api/v1/nothing/here"} {
		do(t, h, httptest.NewRequest(http.MethodGet, p, nil))

		if !strings.Contains(logs.String(), "path="+p+" ") {
			t.Errorf("%s not logged at Info:\n%s", p, logs.String())
		}
	}
}

// TestReadyzMemory checks that an app without a readiness check (the
// memory store) is always ready.
func TestReadyzMemory(t *testing.T) {
	t.Parallel()

	h, _ := app.New(app.Deps{Store: memory.New()})

	if res, body := do(t, h, httptest.NewRequest(http.MethodGet, "/readyz", nil)); res.StatusCode != http.StatusOK {
		t.Fatalf("/readyz = %d %s", res.StatusCode, body)
	}
}

// fakeMetrics records what the app reports.
type fakeMetrics struct {
	mu       sync.Mutex
	requests []string // "METHOD route status"
	jobs     map[string][]error
}

func (m *fakeMetrics) ObserveRequest(method, route string, status int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if d < 0 {
		panic("negative duration")
	}

	m.requests = append(m.requests, method+" "+route+" "+http.StatusText(status))
}

func (m *fakeMetrics) ObserveJob(name string, err error, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.jobs == nil {
		m.jobs = map[string][]error{}
	}

	m.jobs[name] = append(m.jobs[name], err)
}

func TestRequestMetrics(t *testing.T) {
	t.Parallel()

	metrics := &fakeMetrics{}
	h, _ := app.New(app.Deps{Store: memory.New(), UI: webui.Handler(), Metrics: metrics})

	crossOrigin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("{}"))
	crossOrigin.Header.Set("Sec-Fetch-Site", "cross-site")

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, unknownLesson, nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/health", nil),
		httptest.NewRequest(http.MethodGet, "/healthz", nil),
		httptest.NewRequest(http.MethodGet, "/schedule/some/page", nil), // the web app
		httptest.NewRequest(http.MethodGet, "/api/v1/nothing/here", nil),
		httptest.NewRequest("BREW", "/api/v1/health", nil),
		crossOrigin,
	} {
		do(t, h, req)
	}

	want := []string{
		"GET /api/v1/lessons/{lessonId} Not Found",
		"GET /api/v1/health OK",
		"GET /healthz OK",
		"GET other OK",
		"GET other Not Found",
		"OTHER other Method Not Allowed",
		"POST other Forbidden",
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	if strings.Join(metrics.requests, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests =\n%s\nwant\n%s", strings.Join(metrics.requests, "\n"), strings.Join(want, "\n"))
	}
}

func TestJobMetrics(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	metrics := &fakeMetrics{}
	now := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)

	runner := app.Jobs(app.Deps{Store: st, Clock: clock.NewFixed(now), Metrics: metrics})
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	errs, ok := metrics.jobs["generate-lessons"]
	if !ok || len(errs) != 1 || errs[0] != nil {
		t.Errorf("generate-lessons runs = %v (reported %v)", errs, ok)
	}

	if len(metrics.jobs) < 3 {
		t.Errorf("only %d jobs reported: %v", len(metrics.jobs), metrics.jobs)
	}
}

// TestUnsentMails checks the outbox counts the metrics read.
func TestUnsentMails(t *testing.T) {
	t.Parallel()

	st := memory.New()
	now := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)
	src := ids.NewSequence()

	mail := func(next, sent time.Time) domain.OutboxMail {
		return domain.OutboxMail{
			ID: ids.New[domain.OutboxMailID](src), To: "ada@example.org", Subject: "Hello", Text: "Hi",
			CreatedAt: now, NextAttemptAt: next, SentAt: sent,
		}
	}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for _, m := range []domain.OutboxMail{
			mail(now, time.Time{}), mail(now.Add(time.Hour), time.Time{}), // pending
			mail(time.Time{}, time.Time{}), // failed
			mail(time.Time{}, now),         // sent
		} {
			if err := tx.Outbox().Create(ctx, m); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	counts, err := app.UnsentMails(app.Deps{Store: st})(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if counts.Pending != 2 || counts.Failed != 1 {
		t.Errorf("counts = %+v, want 2 pending and 1 failed", counts)
	}
}

// syncBuffer is a bytes.Buffer safe for the concurrent writes of a logger.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}
