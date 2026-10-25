package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// TestUnknownErrorLogLevel: an unknown error is a 500 logged as an error,
// unless the request was canceled meanwhile (the client went away): then
// the failure is no fault of the app and is logged at Debug only.
func TestUnknownErrorLogLevel(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name  string
		ctx   context.Context
		level string
	}{
		{"live request", context.Background(), "level=ERROR"},
		{"canceled request", canceled, "level=DEBUG"},
	}

	for _, c := range cases {
		var buf bytes.Buffer

		h := &handlers{deps: Deps{
			Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		}}

		err := h.toHTTPError(c.ctx, errors.New("postgres: context canceled"))

		var se huma.StatusError
		if !errors.As(err, &se) || se.GetStatus() != http.StatusInternalServerError {
			t.Fatalf("%s: error = %v, want a 500", c.name, err)
		}

		if line := buf.String(); !strings.Contains(line, c.level) || !strings.Contains(line, "internal error") {
			t.Errorf("%s: logged %q, want %s", c.name, line, c.level)
		}
	}
}
