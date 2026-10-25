package domain

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// check is one condition checked by validate: a rule, or a fieldRule that
// names the field it is about.
type check interface {
	err() error
}

// rule is a condition about the entity as a whole.
type rule struct {
	ok  bool
	msg string
}

func (r rule) err() error {
	if r.ok {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrValidation, r.msg)
}

// fieldRule is a condition about one field a client sends, so the API can
// point at it (ADR 0007).
type fieldRule struct {
	rule

	field string
}

// on returns a rule about the field with the given JSON name.
func on(field string, ok bool, msg string) fieldRule {
	return fieldRule{rule: rule{ok: ok, msg: msg}, field: field}
}

func (r fieldRule) err() error {
	if r.ok {
		return nil
	}

	return &FieldError{Field: r.field, Msg: r.msg}
}

// validate returns an error wrapping ErrValidation for the first check that
// does not hold, or nil. A failed fieldRule is a *FieldError.
func validate(checks ...check) error {
	for _, c := range checks {
		if err := c.err(); err != nil {
			return err
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

// ValidEmail reports whether s is an email address the entities take: a
// bare address such as "a@example.org", without spaces around it.
func ValidEmail(s string) bool { return validEmail(s) }

func validWeekday(d time.Weekday) bool { return d >= time.Sunday && d <= time.Saturday }

// optionalEmail, optionalDate and notBefore treat the zero value as "absent".
func optionalEmail(s string) bool { return s == "" || validEmail(s) }

func optionalDate(d Date) bool { return d.IsZero() || d.Valid() }

func notBefore(t, ref time.Time) bool { return t.IsZero() || !t.Before(ref) }
