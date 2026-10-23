package config_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/config"
)

// env returns a lookup function backed by a map, so tests never touch the
// process environment.
func env(vars map[string]string) config.LookupFunc {
	return func(key string) (string, bool) {
		v, ok := vars[key]

		return v, ok
	}
}

func ptr(s string) *string { return &s }

func TestLoadPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		env   map[string]string
		flags config.Flags
		want  config.Config
	}{
		{
			name: "defaults",
			want: config.Config{HTTPAddr: ":8080"},
		},
		{
			name: "env",
			env: map[string]string{
				"DATABASE_URL": "postgres://app@db.example.test/studio",
				"HTTP_ADDR":    "127.0.0.1:9000",
				"BASE_URL":     "https://schedule.example.test",
			},
			want: config.Config{
				DatabaseURL: "postgres://app@db.example.test/studio",
				HTTPAddr:    "127.0.0.1:9000",
				BaseURL:     "https://schedule.example.test",
			},
		},
		{
			name: "flags over env",
			env: map[string]string{
				"DATABASE_URL": "postgres://app@db.example.test/studio",
				"HTTP_ADDR":    "127.0.0.1:9000",
				"BASE_URL":     "https://schedule.example.test",
			},
			flags: config.Flags{
				DatabaseURL: ptr("host=localhost dbname=other"),
				HTTPAddr:    ptr(":7000"),
				BaseURL:     ptr("http://localhost:7000"),
			},
			want: config.Config{
				DatabaseURL: "host=localhost dbname=other",
				HTTPAddr:    ":7000",
				BaseURL:     "http://localhost:7000",
			},
		},
		{
			name:  "flags over defaults",
			flags: config.Flags{HTTPAddr: ptr(":7000")},
			want:  config.Config{HTTPAddr: ":7000"},
		},
		{
			name:  "empty flag clears env",
			env:   map[string]string{"DATABASE_URL": "postgres://app@db.example.test/studio"},
			flags: config.Flags{DatabaseURL: ptr("")},
			want:  config.Config{HTTPAddr: ":8080"},
		},
		{
			name: "empty env falls back to default",
			env:  map[string]string{"HTTP_ADDR": "", "BASE_URL": ""},
			want: config.Config{HTTPAddr: ":8080"},
		},
		{
			name: "values are trimmed",
			env: map[string]string{
				"HTTP_ADDR": " :9000 ",
				"BASE_URL":  " https://schedule.example.test/ ",
			},
			want: config.Config{HTTPAddr: ":9000", BaseURL: "https://schedule.example.test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(env(tt.env), tt.flags)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if got != tt.want {
				t.Fatalf("Load = %+v, want %+v", got.Redacted(), tt.want.Redacted())
			}
		})
	}
}

func TestLoadNilLookup(t *testing.T) {
	t.Parallel()

	got, err := config.Load(nil, config.Flags{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.HTTPAddr != config.DefaultHTTPAddr {
		t.Fatalf("HTTPAddr = %q, want %q", got.HTTPAddr, config.DefaultHTTPAddr)
	}
}

func TestLoadValid(t *testing.T) {
	t.Parallel()

	for _, vars := range []map[string]string{
		{"DATABASE_URL": "postgres://app:change-me@db.example.test:5432/studio?sslmode=require"},
		{"DATABASE_URL": "postgresql://app@db.example.test/studio"},
		{"DATABASE_URL": "POSTGRES://app@db.example.test/studio"},
		{"DATABASE_URL": "postgres:///studio?host=/var/run/postgresql"},
		{"DATABASE_URL": "host=db.example.test user=app password=change-me dbname=studio"},
		{"DATABASE_URL": "dbname = studio"},
		{"DATABASE_URL": "host=db.example.test password=change://me"},
		{"HTTP_ADDR": "localhost:8080"},
		{"HTTP_ADDR": "[::1]:8080"},
		{"HTTP_ADDR": ":0"},
		{"BASE_URL": "http://localhost:8080"},
		{"BASE_URL": "https://schedule.example.test/"},
		{"BASE_URL": "HTTPS://schedule.example.test"},
	} {
		if _, err := config.Load(env(vars), config.Flags{}); err != nil {
			t.Errorf("%v: %v", vars, err)
		}
	}
}

func TestLoadInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string // the env var named in the error
	}{
		{"database url scheme", map[string]string{"DATABASE_URL": "mysql://app@db.example.test/studio"}, "DATABASE_URL"},
		{"database url garbage", map[string]string{"DATABASE_URL": "not a dsn"}, "DATABASE_URL"},
		{"database url unparsable", map[string]string{"DATABASE_URL": "postgres://app:change-me@db.example.test:port/studio"}, "DATABASE_URL"},
		{"http addr without port", map[string]string{"HTTP_ADDR": "localhost"}, "HTTP_ADDR"},
		{"http addr empty port", map[string]string{"HTTP_ADDR": "localhost:"}, "HTTP_ADDR"},
		{"http addr bad port", map[string]string{"HTTP_ADDR": ":http"}, "HTTP_ADDR"},
		{"http addr port range", map[string]string{"HTTP_ADDR": ":70000"}, "HTTP_ADDR"},
		{"base url relative", map[string]string{"BASE_URL": "/schedule"}, "BASE_URL"},
		{"base url no host", map[string]string{"BASE_URL": "https://"}, "BASE_URL"},
		{"base url scheme", map[string]string{"BASE_URL": "ftp://schedule.example.test"}, "BASE_URL"},
		{"base url path", map[string]string{"BASE_URL": "https://schedule.example.test/app"}, "BASE_URL"},
		{"base url query", map[string]string{"BASE_URL": "https://schedule.example.test/?a=b"}, "BASE_URL"},
		{"base url fragment", map[string]string{"BASE_URL": "https://schedule.example.test/#top"}, "BASE_URL"},
		{"base url userinfo", map[string]string{"BASE_URL": "https://admin:change-me@schedule.example.test"}, "BASE_URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(tt.env), config.Flags{})
			if !errors.Is(err, config.ErrInvalid) {
				t.Fatalf("Load error = %v, want ErrInvalid", err)
			}

			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not name %s", err, tt.want)
			}

			if strings.Contains(err.Error(), "change-me") {
				t.Errorf("error %q leaks a secret", err)
			}
		})
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL": "nonsense",
		"HTTP_ADDR":    "nonsense",
		"BASE_URL":     "nonsense",
	}), config.Flags{})

	for _, key := range []string{"DATABASE_URL", "HTTP_ADDR", "BASE_URL"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("error %v does not name %s", err, key)
		}
	}
}

func TestRedacted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"postgres://app@db.example.test/studio", "postgres://app@db.example.test/studio"},
		{"postgres://app:change-me@db.example.test/studio", "postgres://app:xxxxx@db.example.test/studio"},
		{
			"postgres://app@db.example.test/studio?password=change-me&sslmode=require",
			"postgres://app@db.example.test/studio?password=xxxxx&sslmode=require",
		},
		{
			"host=db.example.test password=change-me dbname=studio",
			"host=db.example.test password=xxxxx dbname=studio",
		},
		{
			`host=db.example.test password = 'change me \' too' sslpassword=change-me`,
			`host=db.example.test password = xxxxx sslpassword=xxxxx`,
		},
		{"password=change-me", "password=xxxxx"},
		{"host=db.example.test password=change://me", "host=db.example.test password=xxxxx"},
		{"password='change me", "password=xxxxx"},
	}

	for _, tt := range tests {
		got := config.Config{DatabaseURL: tt.in}.Redacted().DatabaseURL
		if got != tt.want {
			t.Errorf("Redacted(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConfigNeverPrintsSecrets(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://app:change-me@db.example.test/studio",
		"BASE_URL":      "https://schedule.example.test",
		"SMTP_HOST":     "mail.example.test",
		"SMTP_USERNAME": "schedule",
		"SMTP_PASSWORD": "smtp-secret",
		"SMTP_FROM":     "Studio <schedule@example.test>",
	}), config.Flags{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var text, json bytes.Buffer

	slog.New(slog.NewTextHandler(&text, nil)).Info("start", slog.Any("config", cfg))
	slog.New(slog.NewJSONHandler(&json, nil)).Info("start", "config", cfg)

	for name, out := range map[string]string{
		"text":   text.String(),
		"json":   json.String(),
		"%v":     fmt.Sprintf("%v", cfg),
		"%+v":    fmt.Sprintf("%+v", cfg),
		"String": cfg.String(),
		"%#v":    fmt.Sprintf("%#v", cfg),
		"%v ptr": fmt.Sprintf("%v", &cfg),
		"SMTP":   fmt.Sprintf("%v %+v %#v %s", cfg.SMTP, cfg.SMTP, cfg.SMTP, cfg.SMTP),
	} {
		if strings.Contains(out, "change-me") || strings.Contains(out, "smtp-secret") {
			t.Errorf("%s output leaks a password: %s", name, out)
		}

		if !strings.Contains(out, "xxxxx") {
			t.Errorf("%s output lacks the redacted URL: %s", name, out)
		}
	}

	if !strings.Contains(text.String(), "config.httpAddr=:8080") {
		t.Errorf("text log lacks httpAddr: %s", text.String())
	}
}

func TestLoadSMTP(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		"SMTP_HOST": "mail.example.test", "SMTP_PORT": "2525", "SMTP_USERNAME": "u", "SMTP_PASSWORD": "p",
		"SMTP_FROM": "schedule@example.test", "SMTP_TLS": "none",
	}), config.Flags{})
	if err != nil {
		t.Fatal(err)
	}

	want := config.SMTP{Host: "mail.example.test", Port: 2525, Username: "u", Password: "p", From: "schedule@example.test", TLS: "none"}
	if cfg.SMTP != want {
		t.Fatalf("SMTP = %#v, want %#v", cfg.SMTP, want)
	}

	if cfg, err := config.Load(env(nil), config.Flags{}); err != nil || cfg.SMTP != (config.SMTP{}) {
		t.Fatalf("no SMTP: %#v, %v", cfg.SMTP, err)
	}

	for name, vars := range map[string]map[string]string{
		"bad port":    {"SMTP_HOST": "mail.example.test", "SMTP_FROM": "a@example.test", "SMTP_PORT": "smtp"},
		"zero port":   {"SMTP_HOST": "mail.example.test", "SMTP_FROM": "a@example.test", "SMTP_PORT": "0"},
		"no from":     {"SMTP_HOST": "mail.example.test"},
		"no host":     {"SMTP_FROM": "a@example.test"},
		"only secret": {"SMTP_PASSWORD": "p"},
	} {
		if _, err := config.Load(env(vars), config.Flags{}); !errors.Is(err, config.ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}
