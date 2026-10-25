package config_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"reflect"
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
			name: "metrics from env",
			env:  map[string]string{"METRICS_ADDR": "127.0.0.1:9090"},
			want: config.Config{HTTPAddr: ":8080", MetricsAddr: "127.0.0.1:9090"},
		},
		{
			name:  "metrics flag over env",
			env:   map[string]string{"METRICS_ADDR": "127.0.0.1:9090"},
			flags: config.Flags{MetricsAddr: ptr(":9100")},
			want:  config.Config{HTTPAddr: ":8080", MetricsAddr: ":9100"},
		},
		{
			name:  "empty metrics flag turns metrics off",
			env:   map[string]string{"METRICS_ADDR": "127.0.0.1:9090"},
			flags: config.Flags{MetricsAddr: ptr("")},
			want:  config.Config{HTTPAddr: ":8080"},
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

			if !reflect.DeepEqual(got, tt.want) {
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
		{"metrics addr without port", map[string]string{"METRICS_ADDR": "localhost"}, "METRICS_ADDR"},
		{"metrics addr bad port", map[string]string{"METRICS_ADDR": ":metrics"}, "METRICS_ADDR"},
		{"metrics addr is the http addr", map[string]string{"METRICS_ADDR": ":8080"}, "METRICS_ADDR"},
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

// TestMetricsAddrConflict checks that METRICS_ADDR may not take the socket
// of HTTP_ADDR, whatever the spelling of the host.
func TestMetricsAddrConflict(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		http, metrics string
		conflict      bool
	}{
		{":8080", ":8080", true},
		{":8080", "0.0.0.0:8080", true},
		{":8080", "127.0.0.1:8080", true},
		{"0.0.0.0:8080", "[::]:8080", true},
		{"[::]:8080", "10.0.0.5:8080", true},
		{"127.0.0.1:8080", ":8080", true},
		{"localhost:8080", "127.0.0.1:8080", true},
		{"[::1]:8080", "LOCALHOST:8080", true},
		{"[::ffff:127.0.0.1]:8080", "127.0.0.1:8080", true},
		{"app.example.test:8080", "APP.example.test:8080", true},
		{":8080", ":08080", true},
		{":8080", ":9090", false},
		{"127.0.0.1:8080", "10.0.0.5:8080", false},
		{"localhost:8080", "10.0.0.5:8080", false},
		{"app.example.test:8080", "10.0.0.5:8080", false},
		{":0", ":0", false},
	} {
		_, err := config.Load(env(map[string]string{"HTTP_ADDR": tc.http, "METRICS_ADDR": tc.metrics}), config.Flags{})

		switch {
		case tc.conflict && (!errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), "METRICS_ADDR")):
			t.Errorf("HTTP_ADDR %q, METRICS_ADDR %q: error = %v, want a conflict", tc.http, tc.metrics, err)
		case !tc.conflict && err != nil:
			t.Errorf("HTTP_ADDR %q, METRICS_ADDR %q: %v", tc.http, tc.metrics, err)
		}
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

func TestLoadTrustedProxies(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{"TRUSTED_PROXIES": "10.0.0.0/8, 192.0.2.7"}), config.Flags{})
	if err != nil {
		t.Fatal(err)
	}

	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.0.2.7/32")}
	if !reflect.DeepEqual(cfg.TrustedProxies, want) {
		t.Errorf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}

	if _, err := config.Load(env(map[string]string{"TRUSTED_PROXIES": "proxy.example.test"}), config.Flags{}); !errors.Is(err, config.ErrInvalid) {
		t.Errorf("a host name: want ErrInvalid, got %v", err)
	}
}

func TestLoadLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		env   map[string]string
		flags config.Flags
		want  config.Log
	}{
		{name: "default", want: config.Log{Level: slog.LevelInfo}},
		{
			name: "env",
			env:  map[string]string{"LOG_FORMAT": "json", "LOG_LEVEL": "debug"},
			want: config.Log{JSON: true, Level: slog.LevelDebug},
		},
		{
			name: "case-insensitive",
			env:  map[string]string{"LOG_FORMAT": "JSON", "LOG_LEVEL": "WARN"},
			want: config.Log{JSON: true, Level: slog.LevelWarn},
		},
		{
			name:  "flags over env",
			env:   map[string]string{"LOG_FORMAT": "json", "LOG_LEVEL": "debug"},
			flags: config.Flags{LogFormat: ptr("text"), LogLevel: ptr("error")},
			want:  config.Log{Level: slog.LevelError},
		},
		{
			name:  "empty flags mean the default",
			env:   map[string]string{"LOG_FORMAT": "json", "LOG_LEVEL": "debug"},
			flags: config.Flags{LogFormat: ptr(""), LogLevel: ptr("")},
			want:  config.Log{Level: slog.LevelInfo},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(env(tt.env), tt.flags)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if got.Log != tt.want {
				t.Fatalf("Log = %+v, want %+v", got.Log, tt.want)
			}
		})
	}

	for name, tc := range map[string]struct {
		env           map[string]string
		flags         config.Flags
		want, notWant string
	}{
		"env format":  {env: map[string]string{"LOG_FORMAT": "xml"}, want: "LOG_FORMAT:", notWant: "--log"},
		"env level":   {env: map[string]string{"LOG_LEVEL": "loud"}, want: "LOG_LEVEL:", notWant: "--log"},
		"flag format": {flags: config.Flags{LogFormat: ptr("xml")}, want: "--log-format:", notWant: "LOG_FORMAT"},
		"flag level":  {flags: config.Flags{LogLevel: ptr("loud")}, want: "--log-level:", notWant: "LOG_LEVEL"},
		"flag over a bad env value": {
			env:   map[string]string{"LOG_FORMAT": "yaml"},
			flags: config.Flags{LogFormat: ptr("xml")}, want: "--log-format:", notWant: "LOG_FORMAT",
		},
	} {
		_, err := config.Load(env(tc.env), tc.flags)
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), tc.want) ||
			strings.Contains(err.Error(), tc.notWant) {
			t.Errorf("%s: error %v, want ErrInvalid naming %s and not %s", name, err, tc.want, tc.notWant)
		}
	}
}

// TestLoadWithoutLog checks that the commands without a log are not blocked
// by bad log settings, while the other settings are still validated.
func TestLoadWithoutLog(t *testing.T) {
	t.Parallel()

	bad := map[string]string{"LOG_FORMAT": "xml", "LOG_LEVEL": "loud", "DATABASE_URL": "host=db.example.test"}

	cfg, err := config.LoadWithoutLog(env(bad), config.Flags{LogFormat: ptr("yaml")})
	if err != nil {
		t.Fatalf("LoadWithoutLog: %v", err)
	}

	if cfg.DatabaseURL != "host=db.example.test" || cfg.Log != (config.Log{}) {
		t.Errorf("config = %#v, log %+v", cfg, cfg.Log)
	}

	bad["HTTP_ADDR"] = "nonsense"
	if _, err := config.LoadWithoutLog(env(bad), config.Flags{}); !errors.Is(err, config.ErrInvalid) ||
		strings.Contains(err.Error(), "LOG_") {
		t.Errorf("bad HTTP_ADDR: error %v, want ErrInvalid without the log settings", err)
	}
}

// TestLoadNamesTheSource checks that an error names the flag when a flag
// supplied the bad value, and the environment variable otherwise.
func TestLoadNamesTheSource(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		env           map[string]string
		flags         config.Flags
		want, notWant string
	}{
		"env addr":      {env: map[string]string{"HTTP_ADDR": "nonsense"}, want: "HTTP_ADDR:", notWant: "--addr"},
		"flag addr":     {flags: config.Flags{HTTPAddr: ptr("nonsense")}, want: "--addr:", notWant: "HTTP_ADDR"},
		"env base url":  {env: map[string]string{"BASE_URL": "/relative"}, want: "BASE_URL:", notWant: "--base-url"},
		"flag base url": {flags: config.Flags{BaseURL: ptr("/relative")}, want: "--base-url:", notWant: "BASE_URL"},
		"flag database url": {
			flags: config.Flags{DatabaseURL: ptr("mysql://db.example.test")}, want: "--database-url:", notWant: "DATABASE_URL",
		},
	} {
		_, err := config.Load(env(tc.env), tc.flags)
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), tc.want) ||
			strings.Contains(err.Error(), tc.notWant) {
			t.Errorf("%s: error %v, want ErrInvalid naming %s and not %s", name, err, tc.want, tc.notWant)
		}
	}
}
