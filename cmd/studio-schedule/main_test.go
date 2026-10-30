package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/webpush"
)

func run(t *testing.T, args ...string) string {
	t.Helper()

	out, err := execute(nil, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}

	return out
}

// execute runs the root command with env as the environment (nil means
// empty) and returns its combined output.
func execute(env map[string]string, args ...string) (string, error) {
	return executeContext(context.Background(), env, args...)
}

// executeContext is execute with ctx, which a long-running command (serve)
// stops at.
func executeContext(ctx context.Context, env map[string]string, args ...string) (string, error) {
	var out bytes.Buffer

	cmd := newRootCmd(func(key string) (string, bool) {
		v, ok := env[key]

		return v, ok
	})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(ctx)

	return out.String(), err
}

func TestOpenAPICommand(t *testing.T) {
	first := run(t, "openapi")
	if second := run(t, "openapi"); first != second {
		t.Fatal("openapi output is not deterministic")
	}

	var spec struct {
		OpenAPI string                               `json:"openapi"`
		Paths   map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(first), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if spec.OpenAPI != "3.1.0" {
		t.Errorf("openapi = %q", spec.OpenAPI)
	}

	for path, id := range map[string]string{
		"/api/v1/health":          "getHealth",
		"/api/v1/settings/public": "getPublicSettings",
	} {
		if got := spec.Paths[path]["get"]["operationId"]; got != id {
			t.Errorf("%s operationId = %v, want %s", path, got, id)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	if got := strings.TrimSpace(run(t, "version")); got != version {
		t.Fatalf("version = %q, want %q", got, version)
	}
}

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer

	newLogger(&buf, config.Log{JSON: true, Level: slog.LevelWarn}).Info("hidden")
	newLogger(&buf, config.Log{JSON: true, Level: slog.LevelWarn}).Warn("shown")

	if got := strings.TrimSpace(buf.String()); !strings.HasPrefix(got, "{") || strings.Contains(got, "hidden") || !strings.Contains(got, "shown") {
		t.Errorf("JSON at warn: %q", got)
	}

	buf.Reset()
	newLogger(&buf, config.Log{}).Info("line")

	if got := buf.String(); !strings.Contains(got, "level=INFO msg=line") {
		t.Errorf("text at info: %q", got)
	}
}

// TestServeLogSettings checks that serve reads the log settings from the
// environment and that a flag wins over it. An invalid HTTP_ADDR keeps the
// server from starting.
func TestServeLogSettings(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		env     map[string]string
		args    []string
		naming  []string
		missing []string
	}{
		"env": {
			env:    map[string]string{"HTTP_ADDR": "nonsense", "LOG_FORMAT": "xml", "LOG_LEVEL": "loud"},
			naming: []string{"LOG_FORMAT", "LOG_LEVEL"},
		},
		"flags over env": {
			env:     map[string]string{"HTTP_ADDR": "nonsense", "LOG_FORMAT": "xml", "LOG_LEVEL": "loud"},
			args:    []string{"--log-format", "json", "--log-level", "debug"},
			naming:  []string{"HTTP_ADDR"},
			missing: []string{"LOG_FORMAT", "LOG_LEVEL"},
		},
		"invalid flags": {
			env:     map[string]string{"HTTP_ADDR": "nonsense"},
			args:    []string{"--log-format", "xml", "--log-level", "loud"},
			naming:  []string{"--log-format", "--log-level"},
			missing: []string{"LOG_FORMAT", "LOG_LEVEL"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := execute(tc.env, append([]string{"serve"}, tc.args...)...)
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("serve error = %v, want ErrInvalid", err)
			}

			for _, key := range tc.naming {
				if !strings.Contains(err.Error(), key) {
					t.Errorf("error %q does not name %s", err, key)
				}
			}

			for _, key := range tc.missing {
				if strings.Contains(err.Error(), key) {
					t.Errorf("error %q names %s", err, key)
				}
			}
		})
	}
}

// TestDatabaseCommandsHidePassword checks that serve and seed use the
// configured database instead of falling back to the memory store, and that
// a connection error does not leak the password.
func TestDatabaseCommandsHidePassword(t *testing.T) {
	t.Parallel()

	const password = "change-me-secret"

	// Nothing listens on port 1, so connecting fails.
	dsn := "postgres://app:" + password + "@127.0.0.1:1/studio?connect_timeout=2"

	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"serve env":  {env: map[string]string{"DATABASE_URL": dsn}, args: []string{"serve", "--addr", "127.0.0.1:0"}},
		"serve flag": {args: []string{"serve", "--addr", "127.0.0.1:0", "--database-url", dsn}},
		"seed env":   {env: map[string]string{"DATABASE_URL": dsn}, args: []string{"seed"}},
		"seed flag":  {args: []string{"seed", "--database-url", dsn}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := execute(tc.env, tc.args...)
			if err == nil {
				t.Fatalf("%v succeeded\n%s", tc.args, out)
			}

			if strings.Contains(err.Error()+out, password) {
				t.Errorf("output leaks the password: %v\n%s", err, out)
			}
		})
	}
}

// TestServeNeedsDatabaseOrDemoMode: serve without a database refuses to
// start instead of silently becoming the public demo. Should the check go,
// the deadline stops the server instead of the test timeout.
func TestServeNeedsDatabaseOrDemoMode(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	out, err := executeContext(ctx, nil, "serve", "--addr", "127.0.0.1:0")
	if err == nil {
		t.Fatalf("serve without DATABASE_URL succeeded\n%s", out)
	}

	if !strings.Contains(err.Error(), "DEMO_MODE") {
		t.Errorf("serve error = %v, want it to name DEMO_MODE\n%s", err, out)
	}
}

func TestSeedNeedsDatabaseURL(t *testing.T) {
	t.Parallel()

	out, err := execute(nil, "seed")
	if !errors.Is(err, errNoDatabaseURL) {
		t.Fatalf("seed error = %v, want errNoDatabaseURL\n%s", err, out)
	}
}

// TestSeedAnchor: --anchor takes a date or "today" in the demo school's
// time zone; anything else fails before the database is needed.
func TestSeedAnchor(t *testing.T) {
	t.Parallel()

	// 23:30 UTC on 2026-10-25 is already the 26th in Europe/Berlin (UTC+1).
	clk := clock.NewFixed(time.Date(2026, time.October, 25, 23, 30, 0, 0, time.UTC))

	for value, want := range map[string]string{
		"today":                       "2026-10-26",
		"2027-09-14":                  "2027-09-14",
		seed.FixedDemoAnchor.String(): seed.FixedDemoAnchor.String(),
	} {
		got, err := demoAnchor(value, clk)
		if err != nil || got.String() != want {
			t.Errorf("demoAnchor(%q) = %v, %v; want %s", value, got, err, want)
		}
	}

	for _, value := range []string{"", "Today", "tomorrow", "2026-13-01", "2026-9-22", "22.09.2026"} {
		if got, err := demoAnchor(value, clk); err == nil {
			t.Errorf("demoAnchor(%q) = %v, want an error", value, got)
		}

		// Without a database URL: the anchor is checked first.
		out, err := execute(nil, "seed", "--anchor", value)
		if err == nil || errors.Is(err, errNoDatabaseURL) || !strings.Contains(err.Error(), "--anchor") {
			t.Errorf("seed --anchor %q: error = %v, want one naming --anchor\n%s", value, err, out)
		}
	}
}

// TestSeedSchool: --school takes a demo school, programme by default;
// anything else fails before the database is needed, naming the schools.
func TestSeedSchool(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"programme", "werkstatt"} {
		got, err := demoSchool(value)
		if err != nil || string(got) != value {
			t.Errorf("demoSchool(%q) = %q, %v; want %s", value, got, err, value)
		}
	}

	cmd := newSeedCmd(nil)
	if def := cmd.Flags().Lookup("school").DefValue; def != string(seed.SchoolProgramme) {
		t.Errorf("--school defaults to %q, want %q", def, seed.SchoolProgramme)
	}

	for _, value := range []string{"", "x", "Werkstatt", "programme,werkstatt"} {
		if got, err := demoSchool(value); err == nil {
			t.Errorf("demoSchool(%q) = %q, want an error", value, got)
		}

		// Without a database URL: the school is checked first.
		out, err := execute(nil, "seed", "--school", value)
		if err == nil || errors.Is(err, errNoDatabaseURL) ||
			!strings.Contains(err.Error(), "--school") || !strings.Contains(err.Error(), "programme or werkstatt") {
			t.Errorf("seed --school %q: error = %v, want one naming --school and the schools\n%s", value, err, out)
		}
	}
}

func TestServeRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"env addr":      {env: map[string]string{"HTTP_ADDR": "nonsense"}},
		"flag addr":     {args: []string{"--addr", "nonsense"}},
		"flag base url": {args: []string{"--base-url", "/relative"}},
		"flag over valid env": {
			env:  map[string]string{"BASE_URL": "https://schedule.example.test"},
			args: []string{"--base-url", "ftp://schedule.example.test"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := execute(tc.env, append([]string{"serve"}, tc.args...)...)
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("serve error = %v, want config.ErrInvalid\n%s", err, out)
			}
		})
	}
}

func TestServeFlagHelpNamesEnv(t *testing.T) {
	t.Parallel()

	out := run(t, "serve", "--help")

	for _, want := range []string{"--addr", "HTTP_ADDR", "--database-url", "DATABASE_URL", "--base-url", "BASE_URL"} {
		if !strings.Contains(out, want) {
			t.Errorf("serve --help lacks %q:\n%s", want, out)
		}
	}
}

func TestVAPIDKeysCommand(t *testing.T) {
	out := run(t, "vapid-keys")

	vars := map[string]string{}
	for line := range strings.Lines(out) {
		name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			t.Fatalf("line %q is no assignment", line)
		}

		vars[name] = value
	}

	if _, err := webpush.ParseKeys(vars[config.EnvVAPIDPublicKey], vars[config.EnvVAPIDPrivateKey]); err != nil {
		t.Fatalf("the printed pair does not parse: %v\n%s", err, out)
	}

	if again := run(t, "vapid-keys"); again == out {
		t.Error("two runs print the same keys")
	}

	// The printed lines configure push.
	vars[config.EnvVAPIDSubject] = "mailto:office@example.test"

	cfg, err := config.Load(func(key string) (string, bool) {
		v, ok := vars[key]

		return v, ok
	}, config.Flags{})
	if err != nil || !cfg.Push.Enabled() {
		t.Fatalf("Load: %v, %#v", err, cfg.Push)
	}
}
