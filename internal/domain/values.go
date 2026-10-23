package domain

import (
	"cmp"
	"fmt"
	"time"
)

// Money is an amount in minor units (e.g. cents) of the school currency,
// Settings.Currency. Amounts never use floating point.
type Money int64

// Date is a calendar date without a time zone, e.g. the first day of a term.
// The zero value means "no date".
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

const dateLayout = "2006-01-02"

// ParseDate parses a date in the form "2006-01-02". Errors wrap
// ErrValidation.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil || len(s) != len(dateLayout) {
		return Date{}, invalidf("invalid date %q", s)
	}

	d := Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
	if !d.Valid() {
		return Date{}, invalidf("invalid date %q", s)
	}

	return d, nil
}

// Valid reports whether d is an existing date between the years 1 and 9999.
func (d Date) Valid() bool {
	if d.Year < 1 || d.Year > 9999 {
		return false
	}

	t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)

	return t.Year() == d.Year && t.Month() == d.Month && t.Day() == d.Day
}

// IsZero reports whether d is the zero value.
func (d Date) IsZero() bool { return d == Date{} }

// Compare returns -1, 0 or +1 depending on whether d is before, equal to or
// after o.
func (d Date) Compare(o Date) int {
	return cmp.Or(cmp.Compare(d.Year, o.Year), cmp.Compare(d.Month, o.Month), cmp.Compare(d.Day, o.Day))
}

// String returns the date in the form "2006-01-02".
func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// MarshalText implements encoding.TextMarshaler. Invalid dates, including
// the zero value, are an error.
func (d Date) MarshalText() ([]byte, error) {
	if !d.Valid() {
		return nil, invalidf("invalid date %+v", d)
	}

	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Date) UnmarshalText(b []byte) error {
	parsed, err := ParseDate(string(b))
	if err != nil {
		return err
	}

	*d = parsed

	return nil
}

// TimeOfDay is a wall-clock time in the school's time zone with minute
// precision, e.g. the start time of a weekly class.
type TimeOfDay struct {
	Hour   int
	Minute int
}

const timeOfDayLayout = "15:04"

// ParseTimeOfDay parses a time in the form "15:04". Errors wrap
// ErrValidation.
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	t, err := time.Parse(timeOfDayLayout, s)
	if err != nil || len(s) != len(timeOfDayLayout) {
		return TimeOfDay{}, invalidf("invalid time of day %q", s)
	}

	return TimeOfDay{Hour: t.Hour(), Minute: t.Minute()}, nil
}

// Valid reports whether t lies between 00:00 and 23:59.
func (t TimeOfDay) Valid() bool {
	return t.Hour >= 0 && t.Hour < 24 && t.Minute >= 0 && t.Minute < 60
}

// SinceMidnight returns the offset of t from midnight.
func (t TimeOfDay) SinceMidnight() time.Duration {
	return time.Duration(t.Hour)*time.Hour + time.Duration(t.Minute)*time.Minute
}

// Compare returns -1, 0 or +1 depending on whether t is before, equal to or
// after o.
func (t TimeOfDay) Compare(o TimeOfDay) int {
	return cmp.Or(cmp.Compare(t.Hour, o.Hour), cmp.Compare(t.Minute, o.Minute))
}

// String returns the time in the form "15:04".
func (t TimeOfDay) String() string { return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute) }

// MarshalText implements encoding.TextMarshaler.
func (t TimeOfDay) MarshalText() ([]byte, error) {
	if !t.Valid() {
		return nil, invalidf("invalid time of day %+v", t)
	}

	return []byte(t.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (t *TimeOfDay) UnmarshalText(b []byte) error {
	parsed, err := ParseTimeOfDay(string(b))
	if err != nil {
		return err
	}

	*t = parsed

	return nil
}
