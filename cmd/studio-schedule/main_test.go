package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/config"
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
	var out bytes.Buffer

	cmd := newRootCmd(func(key string) (string, bool) {
		v, ok := env[key]

		return v, ok
	})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

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

func TestLogOptions(t *testing.T) {
	for _, o := range []logOptions{{"text", "info"}, {"json", "debug"}, {"JSON", "warn"}} {
		if _, err := o.logger(&bytes.Buffer{}); err != nil {
			t.Errorf("%+v: %v", o, err)
		}
	}

	for _, o := range []logOptions{{"xml", "info"}, {"text", "loud"}} {
		if _, err := o.logger(&bytes.Buffer{}); err == nil {
			t.Errorf("%+v: want error", o)
		}
	}
}

func TestServeRejectsDatabaseURL(t *testing.T) {
	t.Parallel()

	const dsn = "postgres://app:change-me@db.example.test/studio"

	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"env":  {env: map[string]string{"DATABASE_URL": dsn}},
		"flag": {args: []string{"--database-url", dsn}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := execute(tc.env, append([]string{"serve"}, tc.args...)...)
			if !errors.Is(err, errNoPostgres) {
				t.Fatalf("serve error = %v, want errNoPostgres\n%s", err, out)
			}

			if strings.Contains(err.Error()+out, "change-me") {
				t.Errorf("output leaks the password: %v\n%s", err, out)
			}
		})
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
