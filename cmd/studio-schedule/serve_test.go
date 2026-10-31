package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail/smtp"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestStartJobs(t *testing.T) {
	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC) // inside the demo term
	logger := slog.New(slog.DiscardHandler)
	stop := startJobs(t.Context(), app.Deps{Store: st, Clock: clock.NewFixed(now), Logger: logger}, logger)

	// The startup run has finished when startJobs returns.
	var count int

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		lessons, err := tx.Lessons().ListRange(ctx, now, now.AddDate(0, 0, 7))
		count = len(lessons)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if count == 0 {
		t.Fatal("no lessons generated at startup")
	}

	done := make(chan struct{})

	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
}

func TestStartJobsLogsFailures(t *testing.T) {
	var logs bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logs, nil))

	// A canceled context makes the startup run fail; startup goes on.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	stop := startJobs(ctx, app.Deps{Store: st, Logger: logger}, logger)
	stop()

	if !strings.Contains(logs.String(), "background jobs failed at startup") {
		t.Fatalf("startup failure not logged:\n%s", logs.String())
	}
}

func TestIsHTTPS(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]bool{
		"":                            false,
		"http://localhost:8080":       false,
		"https://school.example.test": true,
		"HTTPS://school.example.test": true,
	} {
		if got := isHTTPS(in); got != want {
			t.Errorf("isHTTPS(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSetTransportSecurity(t *testing.T) {
	t.Parallel()

	for baseURL, https := range map[string]bool{
		"":                            false,
		"http://localhost:8080":       false,
		"https://school.example.test": true,
	} {
		for _, subdomains := range []bool{false, true} {
			var deps app.Deps

			setTransportSecurity(&deps, baseURL, subdomains)

			if deps.InsecureCookies == https || deps.HSTS != https || deps.HostCookie != https ||
				deps.HSTSIncludeSubdomains != subdomains {
				t.Errorf("BASE_URL %q, subdomains %v: InsecureCookies = %v, HSTS = %v, HostCookie = %v, HSTSIncludeSubdomains = %v",
					baseURL, subdomains, deps.InsecureCookies, deps.HSTS, deps.HostCookie, deps.HSTSIncludeSubdomains)
			}
		}
	}
}

// serveLogs collects the output of a running serve command. The server logs
// from its goroutines while the test reads, so access is locked.
type serveLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *serveLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.Write(p)
}

func (l *serveLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.String()
}

// serveProcess is a serve command running in the background.
type serveProcess struct {
	addr   string // HTTP_ADDR
	logs   *serveLogs
	cancel context.CancelFunc
	exited chan struct{}
	err    error // serve's result, set before exited closes
}

const (
	// serveStartTimeout bounds the startup (store, startup jobs) until
	// /healthz answers.
	serveStartTimeout = time.Minute
	// serveStopTimeout bounds a shutdown. The first one in a test binary
	// waits for the receipt renderer's compilation (no cache yet), which is
	// slow under the race detector.
	serveStopTimeout = 3 * time.Minute
)

// serveClient never reuses connections, so a shutdown finds none idle, and
// ignores proxies from the environment.
var serveClient = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}

// serveStart runs `serve` with env as its whole environment until the test
// ends or stop is called.
func serveStart(t *testing.T, env map[string]string) *serveProcess {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	p := &serveProcess{addr: env["HTTP_ADDR"], logs: &serveLogs{}, cancel: cancel, exited: make(chan struct{})}

	cmd := newRootCmd(func(key string) (string, bool) {
		v, ok := env[key]

		return v, ok
	})
	cmd.SetOut(p.logs)
	cmd.SetErr(p.logs)
	cmd.SetArgs([]string{"serve"})

	go func() {
		defer close(p.exited)

		p.err = cmd.ExecuteContext(ctx)
	}()

	t.Cleanup(func() {
		cancel()

		if ok, _ := p.wait(serveStopTimeout); !ok {
			t.Errorf("serve did not stop within %v\n%s", serveStopTimeout, p.logs)
		}
	})

	return p
}

// wait waits up to timeout for serve to return and reports whether it
// returned and its result.
func (p *serveProcess) wait(timeout time.Duration) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-p.exited:
		return true, p.err
	case <-timer.C:
		return false, nil
	}
}

// stop cancels serve's context, like SIGTERM, and returns its result.
func (p *serveProcess) stop(t *testing.T) error {
	t.Helper()

	p.cancel()

	ok, err := p.wait(serveStopTimeout)
	if !ok {
		t.Fatalf("serve did not stop within %v\n%s", serveStopTimeout, p.logs)
	}

	return err
}

// waitHealthy waits until /healthz answers 200 OK; serve returning first
// fails the test.
func (p *serveProcess) waitHealthy(t *testing.T) {
	t.Helper()

	p.poll(t, serveStartTimeout, "/healthz to answer", func() bool {
		resp, err := serveClient.Get("http://" + p.addr + "/healthz")
		if err != nil {
			return false
		}

		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	})
}

// waitLog waits until the logs contain s.
func (p *serveProcess) waitLog(t *testing.T, s string, timeout time.Duration) {
	t.Helper()

	p.poll(t, timeout, "the log line "+strconv.Quote(s), func() bool { return strings.Contains(p.logs.String(), s) })
}

// poll calls done every few milliseconds until it is true, failing the
// test when timeout passes or serve returns first.
func (p *serveProcess) poll(t *testing.T, timeout time.Duration, what string, done func() bool) {
	t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for !done() {
		select {
		case <-p.exited:
			t.Fatalf("serve returned %v while waiting for %s\n%s", p.err, what, p.logs)
		case <-deadline.C:
			t.Fatalf("no %s within %v\n%s", what, timeout, p.logs)
		case <-tick.C:
		}
	}
}

// serveFreeAddr returns a loopback address with a port that was free a
// moment ago.
func serveFreeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	return addr
}

// serveOccupiedAddr returns the address of a listener that stays open until
// the test ends, so nothing else can listen there.
func serveOccupiedAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	return l.Addr().String()
}

// serveGet requests url and returns the status, headers and body.
func serveGet(t *testing.T, url string) (int, http.Header, string) {
	t.Helper()

	resp, err := serveClient.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	return resp.StatusCode, resp.Header, string(body)
}

// serveSchoolName returns the school name of /api/v1/settings/public.
func serveSchoolName(t *testing.T, addr string) string {
	t.Helper()

	status, _, body := serveGet(t, "http://"+addr+"/api/v1/settings/public")
	if status != http.StatusOK {
		t.Fatalf("/api/v1/settings/public = %d %s", status, body)
	}

	var settings struct {
		SchoolName string `json:"schoolName"`
	}
	if err := json.Unmarshal([]byte(body), &settings); err != nil {
		t.Fatalf("/api/v1/settings/public: %v\n%s", err, body)
	}

	return settings.SchoolName
}

// serveLacks fails the test for every s the logs contain.
func serveLacks(t *testing.T, logs string, unwanted ...string) {
	t.Helper()

	for _, s := range unwanted {
		if strings.Contains(logs, s) {
			t.Errorf("logs contain %q:\n%s", s, logs)
		}
	}
}

// serveHas fails the test for every s the logs lack.
func serveHas(t *testing.T, logs string, wanted ...string) {
	t.Helper()

	for _, s := range wanted {
		if !strings.Contains(logs, s) {
			t.Errorf("logs lack %q:\n%s", s, logs)
		}
	}
}

// Log lines of serve that the tests look for.
const (
	serveLogHTTPWarning     = "BASE_URL is not an https:// origin"
	serveLogNoMailer        = "no mail server configured"
	serveLogNoSettings      = "the database has no school settings"
	serveLogMemoryStore     = "using the in-memory store with demo data"
	serveLogMetricsFailed   = "metrics server failed; serving without metrics"
	serveLogRendererReady   = "receipt renderer ready"
	serveLogShuttingDown    = "msg=\"shutting down\""
	serveLogStopped         = "msg=stopped"
	serveLogListening       = "msg=listening"
	serveLogStartupJobsFail = "background jobs failed at startup"
)

// TestServeStartup runs the server in-process on PostgreSQL and on the demo
// memory store, the way `studio-schedule serve` does, and checks what it
// serves, logs and how it stops. The subtests share one receipt renderer
// cache in a temporary HOME, so only the first one compiles the module.
func TestServeStartup(t *testing.T) {
	// The databases come first: the PostgreSQL container starts on first
	// use and needs the real HOME (Docker's configuration).
	seeded := newTestDatabase(t)
	if err := seed.LoadDemo(t.Context(), openTestStore(t, seeded)); err != nil {
		t.Fatal(err)
	}

	empty := newTestDatabase(t)

	// The receipt renderer caches its compiled code in the user's cache
	// directory (os.UserCacheDir: $HOME/Library/Caches on macOS,
	// $XDG_CACHE_HOME on Linux); keep it out of the developer's.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	cacheDir, err := os.UserCacheDir()
	if err != nil || !strings.HasPrefix(cacheDir, home) {
		t.Fatalf("os.UserCacheDir() = %q, %v; want a directory in %s", cacheDir, err, home)
	}

	wantSchool := seed.DemoSettings().SchoolName

	t.Run("postgres", func(t *testing.T) {
		addr, metricsAddr := serveFreeAddr(t), serveFreeAddr(t)

		const smtpPassword = "smtp-password-of-the-tests"

		p := serveStart(t, map[string]string{
			"DATABASE_URL":            seeded,
			"HTTP_ADDR":               addr,
			"METRICS_ADDR":            metricsAddr,
			"BASE_URL":                "https://schedule.example.test",
			"HSTS_INCLUDE_SUBDOMAINS": "false",
			// A valid mail server: newMailer only builds the sender,
			// nothing connects to it.
			"SMTP_HOST":     "mail.example.test",
			"SMTP_FROM":     "Studio Demo <noreply@example.test>",
			"SMTP_USERNAME": "studio",
			"SMTP_PASSWORD": smtpPassword,
			"LOG_LEVEL":     "debug",
		})
		p.waitHealthy(t)

		// The container health check works against the real server.
		if out, err := execute(nil, "healthcheck", "--addr", addr); err != nil {
			t.Errorf("healthcheck: %v\n%s", err, out)
		}

		status, header, body := serveGet(t, "http://"+addr+"/readyz")
		if status != http.StatusOK {
			t.Errorf("/readyz = %d %s", status, body)
		}

		// An https BASE_URL turns on HSTS, here without the subdomains.
		if hsts := header.Get("Strict-Transport-Security"); hsts == "" || strings.Contains(hsts, "includeSubDomains") {
			t.Errorf("Strict-Transport-Security = %q, want HSTS without includeSubDomains", hsts)
		}

		if got := serveSchoolName(t, addr); got != wantSchool {
			t.Errorf("school name = %q, want %q (the seeded database)", got, wantSchool)
		}

		// A server on a database is no demo: no published logins.
		if status, _, body := serveGet(t, "http://"+addr+"/api/v1/demo/logins"); status != http.StatusNotFound {
			t.Errorf("/api/v1/demo/logins = %d %s, want 404", status, body)
		}

		// The metrics have their own listener, which saw the request above
		// and the startup run of the jobs; the public server has none.
		status, _, body = serveGet(t, "http://"+metricsAddr+"/metrics")
		if status != http.StatusOK {
			t.Fatalf("/metrics = %d %s", status, body)
		}

		for _, want := range []string{
			`studio_schedule_build_info{version="` + version + `"} 1`,
			`studio_schedule_http_requests_total{method="GET",route="/api/v1/settings/public",status="200"} 1`,
			`studio_schedule_job_runs_total{job="generate-lessons",result="success"} 1`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("/metrics lacks %s", want)
			}
		}

		if _, _, body := serveGet(t, "http://"+addr+"/metrics"); strings.Contains(body, "studio_schedule_build_info") {
			t.Error("the public server serves the metrics")
		}

		// The renderer compiles in the background and says so at debug.
		p.waitLog(t, serveLogRendererReady, serveStopTimeout)

		// The readiness probe is the store's schema check: a migration the
		// binary does not know makes the running server unready.
		execSQL(t, seeded, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999, true)")
		p.poll(t, 15*time.Second, "/readyz to answer 503", func() bool {
			status, _, _ := serveGet(t, "http://"+addr+"/readyz")

			return status == http.StatusServiceUnavailable
		})

		if err := p.stop(t); err != nil {
			t.Fatalf("serve = %v after cancel, want nil\n%s", err, p.logs)
		}

		if resp, err := serveClient.Get("http://" + addr + "/healthz"); err == nil {
			_ = resp.Body.Close()

			t.Errorf("the server still answers after stopping: %s", resp.Status)
		}

		logs := p.logs.String()
		serveHas(t, logs, serveLogListening, "msg=\"serving metrics\" addr="+metricsAddr, serveLogShuttingDown, serveLogStopped)
		serveLacks(t, logs, smtpPassword, serveLogHTTPWarning, serveLogNoMailer, serveLogNoSettings,
			serveLogMemoryStore, serveLogStartupJobsFail)

		if strings.Index(logs, serveLogShuttingDown) > strings.Index(logs, serveLogStopped) {
			t.Errorf("stopped is logged before shutting down:\n%s", logs)
		}

		// The compiled renderer stays in the user's cache directory for the
		// next start.
		entries, err := os.ReadDir(filepath.Join(cacheDir, "studio-schedule", "wazero"))
		if err != nil || len(entries) == 0 {
			t.Errorf("renderer cache: %d entries, %v; want the compiled module", len(entries), err)
		}
	})

	t.Run("demo", func(t *testing.T) {
		addr := serveFreeAddr(t)
		p := serveStart(t, map[string]string{"DEMO_MODE": "true", "HTTP_ADDR": addr})
		p.waitHealthy(t)

		status, header, body := serveGet(t, "http://"+addr+"/api/v1/demo/logins")
		if status != http.StatusOK {
			t.Fatalf("/api/v1/demo/logins = %d %s", status, body)
		}

		var out struct {
			Logins []struct {
				Email string `json:"email"`
				Role  string `json:"role"`
			} `json:"logins"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("/api/v1/demo/logins: %v\n%s", err, body)
		}

		want := seed.DemoLogins()
		if len(out.Logins) != len(want) {
			t.Fatalf("%d demo logins, want %d: %s", len(out.Logins), len(want), body)
		}

		for i, l := range out.Logins {
			if l.Email != want[i].Email || l.Role != string(want[i].Role) {
				t.Errorf("login %d = %s (%s), want %s (%s)", i, l.Email, l.Role, want[i].Email, want[i].Role)
			}
		}

		// Plain HTTP: no HSTS.
		if hsts := header.Get("Strict-Transport-Security"); hsts != "" {
			t.Errorf("Strict-Transport-Security = %q without an https BASE_URL", hsts)
		}

		if got := serveSchoolName(t, addr); got != wantSchool {
			t.Errorf("school name = %q, want %q (the demo data)", got, wantSchool)
		}

		// The memory store has no readiness check: always ready.
		if status, _, body := serveGet(t, "http://"+addr+"/readyz"); status != http.StatusOK {
			t.Errorf("/readyz = %d %s", status, body)
		}

		if err := p.stop(t); err != nil {
			t.Fatalf("serve = %v after cancel, want nil\n%s", err, p.logs)
		}

		// The demo warns about its memory store, but neither about plain
		// HTTP nor the missing mail server, which it does not need.
		logs := p.logs.String()
		serveHas(t, logs, serveLogMemoryStore, serveLogStopped)
		serveLacks(t, logs, serveLogHTTPWarning, serveLogNoMailer, serveLogNoSettings)
	})

	t.Run("database without settings", func(t *testing.T) {
		addr := serveFreeAddr(t)
		p := serveStart(t, map[string]string{"DATABASE_URL": empty, "HTTP_ADDR": addr})
		p.waitHealthy(t)

		if status, _, body := serveGet(t, "http://"+addr+"/readyz"); status != http.StatusOK {
			t.Errorf("/readyz = %d %s; the schema is complete", status, body)
		}

		if err := p.stop(t); err != nil {
			t.Fatalf("serve = %v after cancel, want nil\n%s", err, p.logs)
		}

		// It starts but says what is missing. Without BASE_URL, mail links
		// point to the listen port on localhost.
		_, port, _ := net.SplitHostPort(addr)
		serveHas(t, p.logs.String(), serveLogNoSettings, serveLogNoMailer,
			serveLogHTTPWarning+": mail links point to http://localhost:"+port+"/ ")
	})

	t.Run("HTTP port in use", func(t *testing.T) {
		addr := serveOccupiedAddr(t)
		p := serveStart(t, map[string]string{"DEMO_MODE": "true", "HTTP_ADDR": addr})

		// serve returns by itself, without a cancel.
		ok, err := p.wait(serveStopTimeout)
		if !ok {
			t.Fatalf("serve keeps running on an address in use\n%s", p.logs)
		}

		if !errors.Is(err, syscall.EADDRINUSE) || !strings.Contains(err.Error(), "http server "+addr) {
			t.Errorf("serve = %v, want EADDRINUSE naming %s", err, addr)
		}

		serveLacks(t, p.logs.String(), serveLogShuttingDown, serveLogStopped)
	})

	t.Run("metrics port in use", func(t *testing.T) {
		addr, metricsAddr := serveFreeAddr(t), serveOccupiedAddr(t)
		p := serveStart(t, map[string]string{"DEMO_MODE": "true", "HTTP_ADDR": addr, "METRICS_ADDR": metricsAddr})
		p.waitHealthy(t)

		// Monitoring must not take the app down: it logs and serves on.
		p.waitLog(t, serveLogMetricsFailed, serveStartTimeout)

		if status, _, body := serveGet(t, "http://"+addr+"/healthz"); status != http.StatusOK {
			t.Errorf("/healthz = %d %s after the metrics server failed", status, body)
		}

		if err := p.stop(t); err != nil {
			t.Fatalf("serve = %v after cancel, want nil\n%s", err, p.logs)
		}

		serveHas(t, p.logs.String(), serveLogStopped)
	})
}

// serveRefused runs serve with env, which must make it fail before it
// listens, and returns the output and the error. A serve that does listen
// runs until the deadline and then returns nil, failing the test.
func serveRefused(t *testing.T, env map[string]string) (string, error) {
	t.Helper()

	env["HTTP_ADDR"] = "127.0.0.1:0"

	ctx, cancel := context.WithTimeout(t.Context(), serveStartTimeout)
	defer cancel()

	out, err := executeContext(ctx, env, "serve")
	if err == nil {
		t.Fatalf("serve started\n%s", out)
	}

	serveLacks(t, out, serveLogListening)

	return out, err
}

// TestServeRefusesToStart: a server that would run on the wrong schema, an
// unreadable database or an unusable mail server stops at startup with an
// error, instead of failing later.
func TestServeRefusesToStart(t *testing.T) {
	t.Parallel()

	t.Run("outdated schema", func(t *testing.T) {
		t.Parallel()

		out, err := serveRefused(t, map[string]string{"DATABASE_URL": newUnmigratedDatabase(t)})
		if !errors.Is(err, errSchemaOutdated) {
			t.Errorf("serve = %v, want errSchemaOutdated\n%s", err, out)
		}
	})

	t.Run("newer schema", func(t *testing.T) {
		t.Parallel()

		url := newTestDatabase(t)
		execSQL(t, url, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999, true)")

		out, err := serveRefused(t, map[string]string{"DATABASE_URL": url})
		if !errors.Is(err, errSchemaNewer) {
			t.Errorf("serve = %v, want errSchemaNewer\n%s", err, out)
		}
	})

	// The schema check counts migrations, not tables: a settings table
	// that cannot be read is a startup error, not "no settings".
	t.Run("unreadable settings", func(t *testing.T) {
		t.Parallel()

		url := newTestDatabase(t)
		execSQL(t, url, "ALTER TABLE settings RENAME TO settings_elsewhere")

		out, err := serveRefused(t, map[string]string{"DATABASE_URL": url})
		if err == nil || !strings.HasPrefix(err.Error(), "read settings: ") || errors.Is(err, domain.ErrNotFound) {
			t.Errorf("serve = %v, want a read settings error\n%s", err, out)
		}

		serveLacks(t, out, serveLogNoSettings)
	})

	// config.Load only checks what it parses; the SMTP sender checks the
	// rest at startup.
	const smtpPassword = "smtp-password-of-the-tests"

	for name, smtpEnv := range map[string]map[string]string{
		"unknown TLS mode": {"SMTP_TLS": "always"},
		"invalid sender":   {"SMTP_FROM": "Studio Demo"},
		"host with port":   {"SMTP_HOST": "mail.example.test:2525"},
		"username only":    {"SMTP_USERNAME": "studio"},
		"password only":    {"SMTP_PASSWORD": smtpPassword},
		"plain text login": {"SMTP_TLS": "none", "SMTP_USERNAME": "studio", "SMTP_PASSWORD": smtpPassword},
	} {
		t.Run("mail server: "+name, func(t *testing.T) {
			t.Parallel()

			env := map[string]string{
				"DEMO_MODE": "true", "SMTP_HOST": "mail.example.test", "SMTP_FROM": "noreply@example.test",
			}
			maps.Copy(env, smtpEnv)

			out, err := serveRefused(t, env)
			if !errors.Is(err, smtp.ErrInvalidConfig) || !strings.HasPrefix(err.Error(), "mail server: ") {
				t.Errorf("serve = %v, want a mail server error wrapping smtp.ErrInvalidConfig\n%s", err, out)
			}

			if strings.Contains(err.Error()+out, smtpPassword) {
				t.Errorf("the error or the logs show the SMTP password: %v\n%s", err, out)
			}
		})
	}
}

func TestAppURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		baseURL, httpAddr, want string
	}{
		{"https://schedule.example.test", ":8080", "https://schedule.example.test/"},
		{"http://localhost:5173", ":8080", "http://localhost:5173/"},
		{"", ":8080", "http://localhost:8080/"},
		{"", "0.0.0.0:9000", "http://localhost:9000/"},
		{"", "[::1]:8443", "http://localhost:8443/"},
		{"", "127.0.0.1:8081", "http://localhost:8081/"},
	} {
		if got := appURL(config.Config{BaseURL: tc.baseURL, HTTPAddr: tc.httpAddr}); got != tc.want {
			t.Errorf("appURL(BASE_URL %q, HTTP_ADDR %q) = %q, want %q", tc.baseURL, tc.httpAddr, got, tc.want)
		}
	}
}
