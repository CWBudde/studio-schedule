// Package requestid carries the ID of an HTTP request through its context,
// so that log lines and problem responses can name the request. The
// application's middleware (internal/app) assigns the ID; this package only
// stores, validates and logs it.
package requestid

import (
	"context"
	"log/slog"
)

// Header is the request and response header that carries the ID.
const Header = "X-Request-Id"

// MaxLen is the longest ID accepted from a proxy.
const MaxLen = 128

// LogKey is the attribute key of the ID in log records and the JSON field
// of problem responses.
const LogKey = "requestId"

type ctxKey struct{}

// NewContext returns ctx carrying id.
func NewContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From returns the ID in ctx, or "" if there is none.
func From(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)

	return id
}

// Valid reports whether s is an acceptable ID from a proxy: 1 to MaxLen
// characters, each an ASCII letter, digit or one of "-._~:+/=@". This
// covers UUIDs, the IDs of common proxies and trace IDs, and keeps control
// characters, spaces and quotes out of logs and headers.
func Valid(s string) bool {
	if s == "" || len(s) > MaxLen {
		return false
	}

	for i := range len(s) {
		if !idChar(s[i]) {
			return false
		}
	}

	return true
}

func idChar(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	}

	switch c {
	case '-', '.', '_', '~', ':', '+', '/', '=', '@':
		return true
	}

	return false
}

// LogHandler wraps a slog.Handler so that every record logged with a
// context that carries an ID (logger.InfoContext and friends) gets the
// attribute LogKey.
type LogHandler struct {
	next slog.Handler
}

// NewLogHandler returns a LogHandler around next.
func NewLogHandler(next slog.Handler) *LogHandler {
	return &LogHandler{next: next}
}

// Enabled implements slog.Handler.
func (h *LogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle implements slog.Handler.
func (h *LogHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := From(ctx); id != "" {
		r = r.Clone()
		r.AddAttrs(slog.String(LogKey, id))
	}

	return h.next.Handle(ctx, r)
}

// WithAttrs implements slog.Handler.
func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup implements slog.Handler; the ID of a grouped logger's records
// lands inside the group.
func (h *LogHandler) WithGroup(name string) slog.Handler {
	return &LogHandler{next: h.next.WithGroup(name)}
}
