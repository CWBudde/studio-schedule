package domain

import "errors"

// Sentinel errors shared across layers. Wrap them with fmt.Errorf("...: %w")
// and test with errors.Is; the HTTP layer maps them to status codes.
var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrForbidden  = errors.New("forbidden")
	ErrValidation = errors.New("validation failed")
)
