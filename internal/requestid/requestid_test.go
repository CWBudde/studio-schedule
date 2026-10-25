package requestid_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/requestid"
)

func TestValid(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"0190a6e0-0000-7000-8000-000000000001", true},
		{"Root=1-67891233-abcdef012345678912345678", true},
		{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", true},
		{"a", true},
		{strings.Repeat("a", requestid.MaxLen), true},
		{"", false},
		{strings.Repeat("a", requestid.MaxLen+1), false},
		{"with space", false},
		{"line\nbreak", false},
		{`quote"`, false},
		{"semi;colon", false},
		{"ümlaut", false},
	} {
		if got := requestid.Valid(tc.id); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestContext(t *testing.T) {
	t.Parallel()

	if got := requestid.From(t.Context()); got != "" {
		t.Errorf("From(empty context) = %q", got)
	}

	ctx := requestid.NewContext(t.Context(), "abc")
	if got := requestid.From(ctx); got != "abc" {
		t.Errorf("From = %q, want abc", got)
	}
}

func TestLogHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := slog.New(requestid.NewLogHandler(slog.NewTextHandler(&buf, nil))).With("app", "test")

	logger.InfoContext(requestid.NewContext(t.Context(), "req-1"), "with id")
	logger.InfoContext(t.Context(), "without id")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q", len(lines), buf.String())
	}

	if !strings.Contains(lines[0], "requestId=req-1") || !strings.Contains(lines[0], "app=test") {
		t.Errorf("line with id = %q", lines[0])
	}

	if strings.Contains(lines[1], "requestId") {
		t.Errorf("line without id = %q", lines[1])
	}
}
