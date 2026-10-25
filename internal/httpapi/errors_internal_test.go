package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// TestUnknownErrorLogLevel: an unknown error is a 500 logged as an error,
// unless it is the cancellation of the request (the client went away):
// then the failure is no fault of the app and is logged at Debug only.
func TestUnknownErrorLogLevel(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	timedOut, cancelTimeout := context.WithTimeout(context.Background(), 0)
	defer cancelTimeout()

	cancellation := fmt.Errorf("postgres: %w", context.Canceled)

	cases := []struct {
		name  string
		ctx   context.Context
		err   error
		level string
	}{
		{"live request", context.Background(), errors.New("postgres: connection refused"), "level=ERROR"},
		{"canceled request", canceled, cancellation, "level=DEBUG"},
		{"timed-out request", timedOut, fmt.Errorf("postgres: %w", context.DeadlineExceeded), "level=DEBUG"},
		// The request was canceled, but the error has another cause.
		{"unrelated error of a canceled request", canceled, errors.New("postgres: constraint violated"), "level=ERROR"},
		// The error says canceled, but the request is live: a bug.
		{"cancellation of a live request", context.Background(), cancellation, "level=ERROR"},
	}

	for _, c := range cases {
		var buf bytes.Buffer

		h := &handlers{deps: Deps{
			Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		}}

		err := h.toHTTPError(c.ctx, c.err)

		var se huma.StatusError
		if !errors.As(err, &se) || se.GetStatus() != http.StatusInternalServerError {
			t.Fatalf("%s: error = %v, want a 500", c.name, err)
		}

		if line := buf.String(); !strings.Contains(line, c.level) || !strings.Contains(line, "internal error") {
			t.Errorf("%s: logged %q, want %s", c.name, line, c.level)
		}
	}
}
