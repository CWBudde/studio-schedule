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
