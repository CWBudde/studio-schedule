package main

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/webpush"
)

func TestNewPusher(t *testing.T) {
	t.Parallel()

	clk := clock.NewFixed(time.Date(2026, time.October, 25, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.DiscardHandler)

	if p, err := newPusher(config.Config{}, clk, logger); err != nil || p != nil {
		t.Fatalf("without keys: %v, %v", p, err)
	}

	keys, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	p, err := newPusher(config.Config{Push: config.Push{
		PublicKey: keys.PublicKey(), PrivateKey: keys.PrivateKey(), Subject: "mailto:office@example.test",
	}}, clk, logger)
	if err != nil || p == nil || p.PublicKey() != keys.PublicKey() {
		t.Fatalf("with keys: %v, %v", p, err)
	}
}

// TestPushSenderErrors maps the push service's answers to the service's
// errors: a gone subscription, a refusal, and an error worth a retry.
func TestPushSenderErrors(t *testing.T) {
	t.Parallel()

	keys, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	sub := domain.PushSubscription{}
	p256dh, auth, err := domain.ParsePushKeys(
		"BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4", "BTBZMqHH6r4Tts7J_aSIgg")
	if err != nil {
		t.Fatal(err)
	}

	sub.P256DH, sub.Auth = p256dh, auth

	for status, want := range map[int]error{
		http.StatusCreated:               nil,
		http.StatusGone:                  service.ErrPushGone,
		http.StatusNotFound:              service.ErrPushGone,
		http.StatusRequestEntityTooLarge: service.ErrPushRefused,
		http.StatusInternalServerError:   nil,
	} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))

		sender, err := webpush.New(webpush.Config{Keys: keys, Subject: "mailto:office@example.test", Client: srv.Client()})
		if err != nil {
			t.Fatal(err)
		}

		sub.Endpoint = srv.URL + "/push/x"
		err = pushSender{sender}.Send(t.Context(), sub, []byte(`{"title":"x"}`), time.Minute)

		switch {
		case status == http.StatusCreated && err != nil:
			t.Errorf("201: %v", err)
		case status == http.StatusInternalServerError &&
			(err == nil || errors.Is(err, service.ErrPushGone) || errors.Is(err, service.ErrPushRefused)):
			t.Errorf("500: want a retryable error, got %v", err)
		case want != nil && !errors.Is(err, want):
			t.Errorf("%d: want %v, got %v", status, want, err)
		}

		srv.Close()
	}

	// The default client refuses a private address for good.
	sender, err := webpush.New(webpush.Config{Keys: keys, Subject: "mailto:office@example.test"})
	if err != nil {
		t.Fatal(err)
	}

	sub.Endpoint = "https://127.0.0.1:1/push/x"
	if err := (pushSender{sender}).Send(t.Context(), sub, []byte(`{}`), time.Minute); !errors.Is(err, service.ErrPushRefused) {
		t.Errorf("private address: want ErrPushRefused, got %v", err)
	}
}

// TestNewPusherRejectsBadConfig: keys or a subject that config.Load would
// refuse fail here too, naming web push, instead of a sender that cannot
// send.
func TestNewPusherRejectsBadConfig(t *testing.T) {
	t.Parallel()

	keys, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		push config.Push
		want error
	}{
		"private key not base64url": {
			push: config.Push{PublicKey: keys.PublicKey(), PrivateKey: "not base64!", Subject: "mailto:office@example.test"},
			want: webpush.ErrInvalidKey,
		},
		"public key of another pair": {
			push: config.Push{
				PublicKey:  "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
				PrivateKey: keys.PrivateKey(), Subject: "mailto:office@example.test",
			},
			want: webpush.ErrInvalidKey,
		},
		"no subject": {
			push: config.Push{PublicKey: keys.PublicKey(), PrivateKey: keys.PrivateKey()},
			want: webpush.ErrConfig,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p, err := newPusher(config.Config{Push: tc.push}, clock.System{}, slog.New(slog.DiscardHandler))
			if p != nil || !errors.Is(err, tc.want) || !strings.HasPrefix(err.Error(), "web push: ") {
				t.Errorf("newPusher = %v, %v; want a web push error wrapping %v", p, err, tc.want)
			}
		})
	}
}
