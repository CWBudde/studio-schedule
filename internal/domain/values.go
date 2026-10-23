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

// utc returns midnight UTC of d, for calendar arithmetic only.
func (d Date) utc() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// AddDays returns the date n calendar days after d (before d if n < 0).
func (d Date) AddDays(n int) Date {
	t := d.utc().AddDate(0, 0, n)

	return Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

// Weekday returns the day of the week of d.
func (d Date) Weekday() time.Weekday { return d.utc().Weekday() }

// DateOf returns the calendar date of the instant t in loc.
func DateOf(t time.Time, loc *time.Location) Date {
	t = t.In(loc)

	return Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

// At returns the instant at which the wall clock in loc shows tod on d.
//
// Daylight saving time makes this ambiguous twice a year. A wall-clock time
// that happens twice (when clocks go back) resolves to the earlier instant;
// one that does not exist (when clocks go forward) moves forward by the size
// of the gap, e.g. 02:30 becomes 03:30.
func (d Date) At(tod TimeOfDay, loc *time.Location) time.Time {
	// The requested wall clock read as UTC; subtracting an offset of loc
	// gives an instant that shows this wall clock if that offset applies.
	wall := time.Date(d.Year, d.Month, d.Day, tod.Hour, tod.Minute, 0, 0, time.UTC)

	var earliest time.Time

	// The offsets in effect half a day before and after cover both sides of
	// any transition on this day.
	for _, probe := range []time.Time{wall.Add(-12 * time.Hour), wall.Add(12 * time.Hour)} {
		_, offset := probe.In(loc).Zone()
		candidate := wall.Add(-time.Duration(offset) * time.Second)

		local := candidate.In(loc)
		if local.Hour() != tod.Hour || local.Minute() != tod.Minute || DateOf(candidate, loc) != d {
			continue // this offset does not apply at that wall clock
		}

		if earliest.IsZero() || candidate.Before(earliest) {
			earliest = candidate
		}
	}

	if earliest.IsZero() {
		// A gap. With the offset from before the transition, the wall clock
		// lands after it, moved forward by the size of the gap (time.Date
		// does not guarantee which way it normalises).
		_, before := wall.Add(-12 * time.Hour).In(loc).Zone()

		return wall.Add(-time.Duration(before) * time.Second)
	}

	return earliest
}

// MarshalText implements encoding.TextMarshaler. The zero value ("no date")
// encodes as an empty string; other invalid dates are an error.
func (d Date) MarshalText() ([]byte, error) {
	if d.IsZero() {
		return []byte{}, nil
	}

	if !d.Valid() {
		return nil, invalidf("invalid date %+v", d)
	}

	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. An empty string
// decodes to the zero value.
func (d *Date) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Date{}

		return nil
	}

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
