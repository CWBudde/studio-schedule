package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/config"
)

// healthServer serves status on /healthz and 404 elsewhere, and returns its
// listen address.
func healthServer(t *testing.T, status int, delay time.Duration) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)

			return
		}

		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}

		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	return srv.Listener.Addr().String()
}

func TestHealthcheck(t *testing.T) {
	t.Parallel()

	healthy := healthServer(t, http.StatusOK, 0)
	unavailable := healthServer(t, http.StatusServiceUnavailable, 0)

	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
		ok   bool
	}{
		"flag":            {args: []string{"--addr", healthy}, ok: true},
		"env":             {env: map[string]string{"HTTP_ADDR": healthy}, ok: true},
		"flag over env":   {env: map[string]string{"HTTP_ADDR": unavailable}, args: []string{"--addr", healthy}, ok: true},
		"not 200":         {args: []string{"--addr", unavailable}},
		"nothing listens": {args: []string{"--addr", "127.0.0.1:1"}},
		// Bad log settings do not matter: the health check writes no log.
		"bad log settings": {env: map[string]string{"HTTP_ADDR": healthy, "LOG_LEVEL": "loud"}, ok: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := execute(tc.env, append([]string{"healthcheck"}, tc.args...)...)
			if tc.ok != (err == nil) {
				t.Fatalf("healthcheck error = %v, want ok %v\n%s", err, tc.ok, out)
			}
		})
	}

	if _, err := execute(map[string]string{"HTTP_ADDR": "nonsense"}, "healthcheck"); !errors.Is(err, config.ErrInvalid) {
		t.Errorf("invalid HTTP_ADDR: error = %v, want config.ErrInvalid", err)
	}
}

func TestHealthcheckTimesOut(t *testing.T) {
	t.Parallel()

	slow := healthServer(t, http.StatusOK, time.Minute)

	_, err := execute(nil, "healthcheck", "--addr", slow)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("healthcheck error = %v, want a timeout after %v", err, healthcheckTimeout)
	}
}

func TestHealthURL(t *testing.T) {
	t.Parallel()

	for addr, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/healthz",
		"[::]:9000":      "http://127.0.0.1:9000/healthz",
		"127.0.0.1:9000": "http://127.0.0.1:9000/healthz",
		"[::1]:9000":     "http://[::1]:9000/healthz",
		"10.0.0.5:8080":  "http://10.0.0.5:8080/healthz",
		"localhost:8080": "http://localhost:8080/healthz",
	} {
		if got := healthURL(addr); got != want {
			t.Errorf("healthURL(%q) = %q, want %q", addr, got, want)
		}
	}
}
