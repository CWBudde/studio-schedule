package smtp

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		port    int
		tls     TLSMode
	}{
		{
			name: "defaults to starttls on 587",
			cfg:  Config{Host: "smtp.example.org", From: "noreply@example.org"},
			port: 587, tls: TLSStartTLS,
		},
		{
			name: "implicit tls defaults to 465",
			cfg:  Config{Host: "smtp.example.org", From: "noreply@example.org", TLS: TLSImplicit},
			port: 465, tls: TLSImplicit,
		},
		{
			name: "none defaults to 25",
			cfg:  Config{Host: "localhost", From: "noreply@example.org", TLS: TLSNone},
			port: 25, tls: TLSNone,
		},
		{
			name: "explicit port and display name",
			cfg:  Config{Host: "smtp.example.org", Port: 2525, From: "Studio Example <noreply@example.org>"},
			port: 2525, tls: TLSStartTLS,
		},
		{
			name: "auth over starttls",
			cfg:  Config{Host: "smtp.example.org", From: "noreply@example.org", Username: "u", Password: "s3cret-Pa55"},
			port: 587, tls: TLSStartTLS,
		},
		{
			name: "auth without tls on localhost",
			cfg:  Config{Host: "127.0.0.1", Port: 1025, From: "noreply@example.org", TLS: TLSNone, Username: "u", Password: "s3cret-Pa55"},
			port: 1025, tls: TLSNone,
		},
		{name: "no host", cfg: Config{From: "noreply@example.org"}, wantErr: true},
		{name: "host with port", cfg: Config{Host: "smtp.example.org:25", From: "noreply@example.org"}, wantErr: true},
		{name: "negative port", cfg: Config{Host: "smtp.example.org", Port: -1, From: "noreply@example.org"}, wantErr: true},
		{name: "port too large", cfg: Config{Host: "smtp.example.org", Port: 70000, From: "noreply@example.org"}, wantErr: true},
		{name: "no from", cfg: Config{Host: "smtp.example.org"}, wantErr: true},
		{name: "bad from", cfg: Config{Host: "smtp.example.org", From: "noreply"}, wantErr: true},
		{name: "two from", cfg: Config{Host: "smtp.example.org", From: "a@example.org, b@example.org"}, wantErr: true},
		{name: "unknown tls mode", cfg: Config{Host: "smtp.example.org", From: "noreply@example.org", TLS: "ssl"}, wantErr: true},
		{
			name:    "password without username",
			cfg:     Config{Host: "smtp.example.org", From: "noreply@example.org", Password: "s3cret-Pa55"},
			wantErr: true,
		},
		{
			name:    "username without password",
			cfg:     Config{Host: "smtp.example.org", From: "noreply@example.org", Username: "u"},
			wantErr: true,
		},
		{
			name:    "auth without tls on a remote host",
			cfg:     Config{Host: "smtp.example.org", From: "noreply@example.org", TLS: TLSNone, Username: "u", Password: "s3cret-Pa55"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := New(tt.cfg, nil)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("New = %v, want ErrInvalidConfig", err)
				}

				if tt.cfg.Password != "" && strings.Contains(err.Error(), tt.cfg.Password) {
					t.Fatalf("error contains the password: %v", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("New = %v", err)
			}

			if s.cfg.Port != tt.port || s.cfg.TLS != tt.tls {
				t.Fatalf("port, tls = %d, %q, want %d, %q", s.cfg.Port, s.cfg.TLS, tt.port, tt.tls)
			}
		})
	}
}

func TestSecretsMasked(t *testing.T) {
	t.Parallel()

	const password = "s3cret-Pa55"

	cfg := Config{
		Host:     "smtp.example.org",
		Username: "mailer",
		Password: password,
		From:     "noreply@example.org",
	}

	s, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logged, nil))
	logger.Info("smtp", "config", cfg, "sender", s)

	outputs := map[string]string{
		"%v cfg":     fmt.Sprintf("%v", cfg),
		"%+v cfg":    fmt.Sprintf("%+v", cfg),
		"%#v cfg":    fmt.Sprintf("%#v", cfg),
		"String cfg": cfg.String(),
		"%v &cfg":    fmt.Sprintf("%v", &cfg),
		"%v sender":  fmt.Sprintf("%v", s),
		"%+v sender": fmt.Sprintf("%+v", s),
		"%#v sender": fmt.Sprintf("%#v", s),
		"%v *sender": fmt.Sprintf("%v", *s),
		"slog":       logged.String(),
	}

	for name, out := range outputs {
		if strings.Contains(out, password) {
			t.Errorf("%s leaks the password: %s", name, out)
		}

		if !strings.Contains(out, "smtp.example.org") {
			t.Errorf("%s does not show the host: %s", name, out)
		}

		if !strings.Contains(out, "xxxxx") {
			t.Errorf("%s does not show the mask: %s", name, out)
		}
	}

	if got := (Config{}).Redacted().Password; got != "" {
		t.Errorf("empty password redacted to %q, want empty", got)
	}
}
