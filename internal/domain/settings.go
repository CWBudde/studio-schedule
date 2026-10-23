package domain

import (
	"time"
)

// Settings holds the school-wide configuration.
type Settings struct {
	// SchoolName is the display name of the school.
	SchoolName string
	// Timezone is an IANA time zone name (e.g. "Europe/Berlin") in which all
	// schedule times of the school are interpreted.
	Timezone string
	// DefaultLocale is a BCP 47 language tag (e.g. "en", "de") used when a
	// user has not chosen a locale.
	DefaultLocale string

	// Currency is the ISO 4217 code (e.g. "EUR") of every Money amount.
	Currency string
	// ReceiptPrefix starts every receipt number, e.g. "SD" in "SD-2026-0001".
	ReceiptPrefix string

	// The release rule: every ReleaseWeekday at ReleaseTime (school time),
	// lessons of the next ReleaseWeeksAhead weeks become bookable.
	ReleaseWeekday    time.Weekday
	ReleaseTime       TimeOfDay
	ReleaseWeeksAhead int

	// BookingCutoff and CancellationCutoff are how long before a lesson
	// starts booking and canceling close. Zero means "until the start".
	BookingCutoff      time.Duration
	CancellationCutoff time.Duration

	// DefaultVisibleDays is how many days ahead members see lessons, unless
	// their account overrides it.
	DefaultVisibleDays int

	// LegalNotice is the optional imprint shown in the footer.
	LegalNotice string
}

// Validate checks the settings for consistency. All errors wrap
// ErrValidation.
func (s Settings) Validate() error {
	err := validate(
		rule{!blank(s.SchoolName), "school name must not be empty"},
		rule{s.Timezone != "", "timezone must not be empty"},
		rule{!blank(s.DefaultLocale), "default locale must not be empty"},
		rule{validCurrency(s.Currency), "currency must be an ISO 4217 code"},
		rule{validReceiptPrefix(s.ReceiptPrefix), "receipt prefix must be 1-16 of A-Z and 0-9"},
		rule{validWeekday(s.ReleaseWeekday), "invalid release weekday"},
		rule{s.ReleaseTime.Valid(), "invalid release time"},
		rule{s.ReleaseWeeksAhead >= 1 && s.ReleaseWeeksAhead <= maxWeeksAhead, "release weeks ahead must be between 1 and 52"},
		rule{s.BookingCutoff >= 0 && s.BookingCutoff <= maxCutoff, "booking cutoff must be between 0 and 30 days"},
		rule{s.CancellationCutoff >= 0 && s.CancellationCutoff <= maxCutoff, "cancellation cutoff must be between 0 and 30 days"},
		rule{s.DefaultVisibleDays >= 1 && s.DefaultVisibleDays <= maxVisibleDays, "default visible days must be between 1 and 366"},
	)
	if err != nil {
		return err
	}

	// Loading the zone parses tzdata, so it runs only after the cheap rules.
	if _, err := time.LoadLocation(s.Timezone); err != nil || s.Timezone == "Local" {
		return invalidf("unknown timezone %q", s.Timezone)
	}

	return nil
}

// Upper bounds that keep schedule arithmetic sane.
const (
	maxWeeksAhead  = 52
	maxVisibleDays = 366
	maxCutoff      = 30 * 24 * time.Hour
)

func validCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}

	for _, c := range []byte(s) {
		if c < 'A' || c > 'Z' {
			return false
		}
	}

	return true
}

func validReceiptPrefix(s string) bool {
	if s == "" || len(s) > 16 {
		return false
	}

	for _, c := range []byte(s) {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}

	return true
}
