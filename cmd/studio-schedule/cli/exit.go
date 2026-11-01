package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

// The exit codes of the binary (ADR 0020).
const (
	// ExitOK: the command did what it was asked.
	ExitOK = 0
	// ExitFailed: it failed, including a check with findings and an
	// import with failed rows.
	ExitFailed = 1
	// ExitUsage: an unknown flag or command, a missing argument, invalid
	// input before anything ran, or a refused confirmation.
	ExitUsage = 2
	// ExitNotFound: what the command names does not exist
	// (domain.ErrNotFound), also an unknown --as.
	ExitNotFound = 3
	// ExitConflict: the change conflicts with the data (domain.ErrConflict
	// and what wraps it, a refused guard), or the --as account's role is
	// too low for the command (domain.ErrForbidden).
	ExitConflict = 4
)

// ErrUsage is wrapped by every usage error (ExitUsage).
var ErrUsage = errors.New("usage")

// ErrNoDatabaseURL is returned by the commands that need a database when
// neither DATABASE_URL nor --database-url names one.
var ErrNoDatabaseURL = Usage(errors.New("no database URL: set " + config.EnvDatabaseURL + " or --database-url"))

// usageError is an error that is a usage error without saying so.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }

func (e usageError) Unwrap() []error { return []error{e.err, ErrUsage} }

// Usage marks err as a usage error (ExitUsage), keeping its message; nil
// stays nil.
func Usage(err error) error {
	if err == nil {
		return nil
	}

	return usageError{err}
}

// Usagef is Usage(fmt.Errorf(format, args...)).
func Usagef(format string, args ...any) error { return Usage(fmt.Errorf(format, args...)) }

// ranError is an error a command's own code returned. Everything else that
// cobra returns (an unknown command or flag, wrong arguments) is a usage
// error.
type ranError struct{ err error }

func (e ranError) Error() string { return e.err.Error() }

func (e ranError) Unwrap() error { return e.err }

// flagError names the flag (or setting) of a field error the way the
// command spells it: "--email: must be an email address".
type flagError struct {
	name string
	err  *domain.FieldError
}

func (e flagError) Error() string { return e.name + ": " + e.err.Msg }

func (e flagError) Unwrap() error { return e.err }

// ExitCode is the exit status of a command that returned err: the one
// mapping of errors to exit codes (ADR 0020).
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case !errors.As(err, new(ranError)),
		errors.Is(err, ErrUsage),
		errors.Is(err, config.ErrInvalid),
		errors.Is(err, domain.ErrValidation):
		return ExitUsage
	case errors.Is(err, domain.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrForbidden):
		return ExitConflict
	default:
		return ExitFailed
	}
}

// Report writes err to w (stderr): the line "error: …", or with asJSON the
// error as a problem document like the API's (title, status, detail,
// errors). Its status is the HTTP status the API answers such an error
// with; a field error names its flag (or field) as the location.
func Report(w io.Writer, err error, asJSON bool) {
	if !asJSON {
		_, _ = fmt.Fprintln(w, "error:", err)

		return
	}

	problem := httpapi.NewProblem(problemStatus(err), err.Error())

	var (
		named flagError
		field *domain.FieldError
	)

	switch {
	case errors.As(err, &named):
		problem.Errors = []*huma.ErrorDetail{{Message: named.err.Msg, Location: named.name}}
	case errors.As(err, &field):
		problem.Errors = []*huma.ErrorDetail{{Message: field.Msg, Location: field.Field}}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// As Output.Print: a location "<person-b>" stays "<person-b>".
	enc.SetEscapeHTML(false)
	_ = enc.Encode(problem)
}

// problemStatus is the HTTP status the API answers err with, by its exit
// code.
func problemStatus(err error) int {
	switch ExitCode(err) {
	case ExitUsage:
		if errors.Is(err, domain.ErrValidation) {
			return http.StatusUnprocessableEntity
		}

		return http.StatusBadRequest
	case ExitNotFound:
		return http.StatusNotFound
	case ExitConflict:
		if errors.Is(err, domain.ErrForbidden) {
			return http.StatusForbidden
		}

		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
