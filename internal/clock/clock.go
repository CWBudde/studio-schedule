// Package clock abstracts the current time so that time-dependent code can be
// tested deterministically. It is the only package allowed to call time.Now.
package clock

import "time"

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

// System is a Clock backed by the operating system clock.
type System struct{}

// Now returns the current wall-clock time.
func (System) Now() time.Time { return time.Now() }

// Fixed is a Clock that always returns the same instant.
type Fixed struct {
	T time.Time
}

// NewFixed returns a Clock that always reports t.
func NewFixed(t time.Time) Fixed { return Fixed{T: t} }

// Now returns the fixed instant.
func (f Fixed) Now() time.Time { return f.T }
