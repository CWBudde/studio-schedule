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
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ratelimit"
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

// TestToHTTPError pins the mapping of every domain error to its problem:
// the status, the detail, a Conflict's reason, and the location of a field
// error, also when the field error is wrapped.
func TestToHTTPError(t *testing.T) {
	t.Parallel()

	field := &domain.FieldError{Field: "name", Msg: "the name must not be empty"}

	cases := []struct {
		name     string
		err      error
		status   int
		detail   string
		reason   string // a *Conflict's reason; "" for a plain problem
		location string // the location of the only error detail, if any
		message  string
	}{
		{"not found", fmt.Errorf("room: %w", domain.ErrNotFound), http.StatusNotFound, "resource not found", "", "", ""},
		{"field error", field, http.StatusUnprocessableEntity, "validation failed", "", "body.name", field.Msg},
		{
			"wrapped field error", fmt.Errorf("save room: %w", field), http.StatusUnprocessableEntity,
			"validation failed", "", "body.name", field.Msg,
		},
		{
			"validation", fmt.Errorf("%w: the term must end after it starts", domain.ErrValidation),
			http.StatusUnprocessableEntity, "validation failed: the term must end after it starts", "", "", "",
		},
		{"in use", fmt.Errorf("room: %w", domain.ErrInUse), http.StatusConflict, "the record is still in use", reasonInUse, "", ""},
		{
			"duplicate", fmt.Errorf("room: %w", domain.ErrDuplicate), http.StatusConflict,
			"a record with these values exists already", reasonDuplicate, "", "",
		},
		// A bare conflict has no reason the UI could explain.
		{"conflict", fmt.Errorf("room: %w", domain.ErrConflict), http.StatusConflict, "conflict", "", "", ""},
		{"unauthorized", fmt.Errorf("session: %w", domain.ErrUnauthorized), http.StatusUnauthorized, "not signed in", "", "", ""},
		{"forbidden", fmt.Errorf("lesson: %w", domain.ErrForbidden), http.StatusForbidden, "forbidden", "", "", ""},
		{"unknown", errors.New("postgres: connection refused"), http.StatusInternalServerError, "internal server error", "", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer

			h := &handlers{deps: Deps{Logger: slog.New(slog.NewTextHandler(&logs, nil))}}
			err := h.toHTTPError(context.Background(), c.err)

			var se huma.StatusError
			if !errors.As(err, &se) || se.GetStatus() != c.status {
				t.Fatalf("error = %#v, want a %d", err, c.status)
			}

			var (
				problem  *ErrorModel
				conflict *Conflict
			)

			switch {
			case errors.As(err, &conflict):
				if conflict.Reason != c.reason {
					t.Errorf("reason = %q, want %q", conflict.Reason, c.reason)
				}

				problem = &conflict.ErrorModel
			case errors.As(err, &problem):
				if c.reason != "" {
					t.Errorf("a plain problem, want a Conflict with reason %q", c.reason)
				}
			default:
				t.Fatalf("error %T is no problem document", err)
			}

			if problem.Detail != c.detail || problem.Title != http.StatusText(c.status) {
				t.Errorf("title %q, detail %q, want %q, %q", problem.Title, problem.Detail, http.StatusText(c.status), c.detail)
			}

			switch {
			case c.location == "" && len(problem.Errors) != 0:
				t.Errorf("details %+v, want none", problem.Errors)
			case c.location != "" && (len(problem.Errors) != 1 || problem.Errors[0].Location != c.location ||
				problem.Errors[0].Message != c.message):
				t.Errorf("details %+v, want %q at %s", problem.Errors, c.message, c.location)
			}

			// Only the unknown error is logged; the domain errors are answers.
			if logged := logs.Len() > 0; logged != (c.status == http.StatusInternalServerError) {
				t.Errorf("logged %q", logs.String())
			}
		})
	}
}

// TestNewProblemSkipsNilErrors: like huma.NewError, a nil error among the
// details is left out; an ErrorDetailer gives its own detail, any other
// error its message.
func TestNewProblemSkipsNilErrors(t *testing.T) {
	t.Parallel()

	p := NewProblem(http.StatusBadRequest, "bad request", nil, errors.New("too many fields"), nil,
		&huma.ErrorDetail{Message: "must be a date", Location: "query.from"})

	if p.Status != http.StatusBadRequest || p.Title != "Bad Request" || p.Detail != "bad request" {
		t.Errorf("problem %+v", p.ErrorModel)
	}

	if len(p.Errors) != 2 || p.Errors[0].Message != "too many fields" || p.Errors[0].Location != "" ||
		p.Errors[1].Message != "must be a date" || p.Errors[1].Location != "query.from" {
		t.Errorf("details %+v, want the two non-nil errors", p.Errors)
	}

	if only := NewProblem(http.StatusConflict, "conflict", nil); len(only.Errors) != 0 {
		t.Errorf("details of a nil error: %+v", only.Errors)
	}
}

// TestNilLimiterImposesNoLimit: the zero RateLimits has nil limiters,
// which allow every request: the zero value imposes no limits (Deps).
func TestNilLimiterImposesNoLimit(t *testing.T) {
	t.Parallel()

	var limits RateLimits

	for range 100 {
		if err := allow(limits.Logins, "203.0.113.7"); err != nil {
			t.Fatalf("allow with a nil limiter: %v", err)
		}

		record(limits.PasswordFailures, "ada@example.org")

		if err := notBlocked(limits.PasswordFailures, "ada@example.org"); err != nil {
			t.Fatalf("notBlocked with a nil limiter: %v", err)
		}
	}

	forget(limits.PasswordFailures, "ada@example.org") // no limiter, nothing to forget

	// The same calls with a limiter do limit, so the above is the nil case.
	l := ratelimit.New(clock.NewFixed(time.Date(2026, time.October, 23, 8, 0, 0, 0, time.UTC)), 1, time.Minute)
	if err := allow(l, "203.0.113.7"); err != nil {
		t.Fatal(err)
	}

	var se huma.StatusError
	if err := allow(l, "203.0.113.7"); !errors.As(err, &se) || se.GetStatus() != http.StatusTooManyRequests {
		t.Errorf("second request: %v, want a 429", err)
	}

	if err := notBlocked(l, "203.0.113.7"); !errors.As(err, &se) || se.GetStatus() != http.StatusTooManyRequests {
		t.Errorf("notBlocked after the limit: %v, want a 429", err)
	}
}

// TestUnknownAccessLevel: an access level outside the five admits nobody,
// and Declare refuses it when the operation is registered.
func TestUnknownAccessLevel(t *testing.T) {
	t.Parallel()

	for _, a := range []Access{"", "superuser", "Admin"} {
		if role, ok := a.MinRole(); ok || role != "" {
			t.Errorf("minRole(%q) = %q, %v; want none", a, role, ok)
		}

		func() {
			defer func() {
				want := "httpapi: operation declaredProbe has an unknown access level " + string(a)
				if rec := recover(); rec != want {
					t.Errorf("Declare(%q) panicked with %v, want %q", a, rec, want)
				}
			}()

			Declare(huma.Operation{OperationID: "declaredProbe"}, a)
		}()
	}

	for _, a := range []Access{AccessPublic, AccessSignedIn, AccessStaff, AccessOffice, AccessAdmin} {
		if op := Declare(huma.Operation{OperationID: "declaredProbe"}, a); op.Metadata[accessKey] != a ||
			op.Extensions[accessKey] != string(a) {
			t.Errorf("Declare(%q) = metadata %v, extensions %v", a, op.Metadata, op.Extensions)
		}
	}
}
