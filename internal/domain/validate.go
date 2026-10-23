package domain

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// rule is one condition checked by validate.
type rule struct {
	ok  bool
	msg string
}

// validate returns an error wrapping ErrValidation for the first rule that
// does not hold, or nil.
func validate(rules ...rule) error {
	for _, r := range rules {
		if !r.ok {
			return fmt.Errorf("%w: %s", ErrValidation, r.msg)
		}
	}

	return nil
}

// invalidf returns an error wrapping ErrValidation.
func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }

// validEmail reports whether s is a bare address such as "a@example.org"
// (no display name, no surrounding spaces). Case-insensitive uniqueness is
// the store's job.
func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)

	return err == nil && a.Name == "" && a.Address == s
}

func validWeekday(d time.Weekday) bool { return d >= time.Sunday && d <= time.Saturday }

// optionalEmail, optionalDate and notBefore treat the zero value as "absent".
func optionalEmail(s string) bool { return s == "" || validEmail(s) }

func optionalDate(d Date) bool { return d.IsZero() || d.Valid() }

func notBefore(t, ref time.Time) bool { return t.IsZero() || !t.Before(ref) }
