package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors shared across layers. Wrap them with fmt.Errorf("...: %w")
// and test with errors.Is; the HTTP layer maps them to status codes.
var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrForbidden  = errors.New("forbidden")
	ErrValidation = errors.New("validation failed")
	// ErrUnauthorized means the request is not signed in (or the
	// credentials are wrong); ErrForbidden means it is signed in but may
	// not do this.
	ErrUnauthorized = errors.New("unauthorized")
)

// Conflicts with a reason the UI can explain (ADR 0007). Both wrap
// ErrConflict.
var (
	// ErrInUse means a record cannot be deleted while others refer to it.
	ErrInUse = fmt.Errorf("%w: in use", ErrConflict)
	// ErrDuplicate means a name that must be unique is taken.
	ErrDuplicate = fmt.Errorf("%w: duplicate", ErrConflict)
)

// FieldError is a validation error about one field of a request, named by
// its JSON name (e.g. "name", "locationId"). It wraps ErrValidation.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string { return ErrValidation.Error() + ": " + e.Msg }

func (e *FieldError) Unwrap() error { return ErrValidation }

// FieldErrors returns the field errors in err's tree, in order: err itself,
// what it wraps, and every error of a join (errors.Join, fmt.Errorf with
// several %w), so the HTTP layer can report several fields of one request at
// once. A FieldError's own ErrValidation is not searched.
func FieldErrors(err error) []*FieldError {
	switch e := err.(type) { //nolint:errorlint // walks the tree errors.As would
	case nil:
		return nil
	case *FieldError:
		return []*FieldError{e}
	case interface{ Unwrap() []error }:
		var out []*FieldError
		for _, inner := range e.Unwrap() {
			out = append(out, FieldErrors(inner)...)
		}

		return out
	case interface{ Unwrap() error }:
		return FieldErrors(e.Unwrap())
	default:
		return nil
	}
}
