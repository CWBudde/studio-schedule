package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

// TestExitCode pins the mapping of errors to exit codes (ADR 0020): what
// cobra returns itself is a usage error, what a command's own code returns
// (ranError) is mapped by its class.
func TestExitCode(t *testing.T) {
	t.Parallel()

	field := &domain.FieldError{Field: "email", Msg: "must be an email address"}

	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"nil":                {err: nil, want: ExitOK},
		"from cobra":         {err: errors.New(`unknown command "nonsense" for "studio-schedule"`), want: ExitUsage},
		"from cobra, wraps":  {err: fmt.Errorf("parse: %w", domain.ErrNotFound), want: ExitUsage},
		"usage":              {err: ranError{Usagef("no %s", "file")}, want: ExitUsage},
		"not confirmed":      {err: ranError{ErrNotConfirmed}, want: ExitUsage},
		"no database URL":    {err: ranError{ErrNoDatabaseURL}, want: ExitUsage},
		"invalid config":     {err: ranError{fmt.Errorf("%w: HTTP_ADDR", config.ErrInvalid)}, want: ExitUsage},
		"validation":         {err: ranError{fmt.Errorf("%w: bad", domain.ErrValidation)}, want: ExitUsage},
		"field error":        {err: ranError{field}, want: ExitUsage},
		"named field error":  {err: ranError{withFields(field, map[string]string{"email": "--email"})}, want: ExitUsage},
		"not found":          {err: ranError{fmt.Errorf("person: %w", domain.ErrNotFound)}, want: ExitNotFound},
		"conflict":           {err: ranError{domain.ErrConflict}, want: ExitConflict},
		"in use":             {err: ranError{fmt.Errorf("room: %w", domain.ErrInUse)}, want: ExitConflict},
		"duplicate":          {err: ranError{domain.ErrDuplicate}, want: ExitConflict},
		"forbidden":          {err: ranError{fmt.Errorf("--as x: %w", domain.ErrForbidden)}, want: ExitConflict},
		"failed":             {err: ranError{errors.New("disk full")}, want: ExitFailed},
		"failed, ran inside": {err: fmt.Errorf("outer: %w", ranError{errors.New("disk full")}), want: ExitFailed},
		"reported":           {err: ranError{Reported(errors.New("3 findings"))}, want: ExitFailed},
		"reported conflict": {
			err: ranError{Reported(fmt.Errorf("the job failed: %w", domain.ErrConflict))}, want: ExitFailed,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := ExitCode(tc.err); got != tc.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()

	if Usage(nil) != nil {
		t.Error("Usage(nil) is not nil")
	}

	base := errors.New("no file")

	err := Usage(base)
	if !errors.Is(err, ErrUsage) || !errors.Is(err, base) || err.Error() != "no file" {
		t.Errorf("Usage(%v) = %v: wraps ErrUsage %t, the error %t", base, err, errors.Is(err, ErrUsage),
			errors.Is(err, base))
	}

	if err := Usagef("--%s: %d", "map", 2); err.Error() != "--map: 2" || !errors.Is(err, ErrUsage) {
		t.Errorf("Usagef = %v", err)
	}

	if err := (ranError{base}); err.Error() != "no file" || !errors.Is(err, base) {
		t.Errorf("ranError = %v", err)
	}
}

// TestReported: a failure the command reported on stdout already exits
// with status 1 and gets its line on stderr in text, but no problem
// document with --output json: it is not an internal error.
func TestReported(t *testing.T) {
	t.Parallel()

	if Reported(nil) != nil {
		t.Error("Reported(nil) is not nil")
	}

	base := errors.New("3 findings")

	err := ranError{Reported(base)}
	if !errors.Is(err, base) || err.Error() != "3 findings" {
		t.Errorf("Reported(%v) = %v", base, err)
	}

	var text bytes.Buffer

	Report(&text, err, false)

	if got := text.String(); got != "error: 3 findings\n" {
		t.Errorf("Report in text = %q", got)
	}

	var js bytes.Buffer

	Report(&js, err, true)

	if js.Len() != 0 {
		t.Errorf("Report with JSON = %q, want nothing", js.String())
	}
}

// TestReportEscapesNoHTML writes a problem document's text as it is, like
// Output.Print: a location "<person-b>" is not "\u003cperson-b\u003e".
func TestReportEscapesNoHTML(t *testing.T) {
	t.Parallel()

	field := &domain.FieldError{Field: "<person-b>", Msg: "must not be <person-a> & itself"}

	var buf bytes.Buffer

	Report(&buf, ranError{field}, true)

	got := buf.String()
	if !strings.Contains(got, `"location": "<person-b>"`) || !strings.Contains(got, "<person-a> & itself") ||
		strings.Contains(got, `\u003c`) || strings.Contains(got, `\u0026`) {
		t.Errorf("Report = %s, want the text unescaped", got)
	}
}

// problem is the problem document Report writes.
type problem struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Errors []struct {
		Message  string `json:"message"`
		Location string `json:"location"`
	} `json:"errors"`
}

func TestReportText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	Report(&buf, ranError{errors.New("disk full")}, false)

	if got := buf.String(); got != "error: disk full\n" {
		t.Errorf("Report = %q", got)
	}

	// A field error names the flag.
	buf.Reset()

	field := &domain.FieldError{Field: "email", Msg: "must be an email address"}
	Report(&buf, ranError{withFields(field, map[string]string{"email": "--email"})}, false)

	if got := buf.String(); got != "error: --email: must be an email address\n" {
		t.Errorf("Report of a field error = %q", got)
	}
}

// TestReportJSON: with --output json the error is a problem document with
// the status the API answers such an error with.
func TestReportJSON(t *testing.T) {
	t.Parallel()

	field := &domain.FieldError{Field: "email", Msg: "must be an email address"}

	type location struct{ message, location string }

	for name, tc := range map[string]struct {
		err    error
		status int
		detail string
		errs   []location
	}{
		"from cobra": {
			err: errors.New("unknown flag: --loud"), status: http.StatusBadRequest,
			detail: "unknown flag: --loud",
		},
		"usage": {
			err: ranError{ErrNotConfirmed}, status: http.StatusBadRequest,
			detail: "not confirmed; nothing was changed",
		},
		"named field": {
			err:    ranError{withFields(field, map[string]string{"email": "--email"})},
			status: http.StatusUnprocessableEntity, detail: "--email: must be an email address",
			errs: []location{{"must be an email address", "--email"}},
		},
		"plain field": {
			err:    ranError{fmt.Errorf("register: %w", field)},
			status: http.StatusUnprocessableEntity, detail: "register: validation failed: must be an email address",
			errs: []location{{"must be an email address", "email"}},
		},
		"not found": {err: ranError{domain.ErrNotFound}, status: http.StatusNotFound, detail: "not found"},
		"in use":    {err: ranError{domain.ErrInUse}, status: http.StatusConflict, detail: "conflict: in use"},
		"forbidden": {err: ranError{domain.ErrForbidden}, status: http.StatusForbidden, detail: "forbidden"},
		"failed": {
			err: ranError{errors.New("disk full")}, status: http.StatusInternalServerError,
			detail: "disk full",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			Report(&buf, tc.err, true)

			var p problem
			if err := json.Unmarshal(buf.Bytes(), &p); err != nil {
				t.Fatalf("not JSON: %v\n%s", err, buf.String())
			}

			if p.Status != tc.status || p.Title != http.StatusText(tc.status) || p.Detail != tc.detail {
				t.Errorf("problem = %+v, want status %d, detail %q", p, tc.status, tc.detail)
			}

			if len(p.Errors) != len(tc.errs) {
				t.Fatalf("errors = %+v, want %v", p.Errors, tc.errs)
			}

			for i, want := range tc.errs {
				if got := p.Errors[i]; got.Message != want.message || got.Location != want.location {
					t.Errorf("errors[%d] = %+v, want %v", i, got, want)
				}
			}
		})
	}
}

// TestWithFields: a field error the Spec names is reported under its flag;
// another error, or a field the Spec does not name, stays as it is.
func TestWithFields(t *testing.T) {
	t.Parallel()

	fields := map[string]string{"mapping": "--map"}
	named := &domain.FieldError{Field: "mapping", Msg: "the file has no column \"Nr\""}

	err := withFields(fmt.Errorf("import: %w", named), fields)
	if err.Error() != `--map: the file has no column "Nr"` || !errors.Is(err, domain.ErrValidation) {
		t.Errorf("named = %v", err)
	}

	other := &domain.FieldError{Field: "content", Msg: "empty"}
	if err := withFields(other, fields); err != other { //nolint:errorlint // the very error
		t.Errorf("unnamed field = %v", err)
	}

	plain := errors.New("disk full")
	if err := withFields(plain, fields); err != plain { //nolint:errorlint // the very error
		t.Errorf("plain = %v", err)
	}

	if withFields(nil, fields) != nil {
		t.Error("nil is not nil")
	}
}
