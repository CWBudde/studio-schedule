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
	_, tzErr := time.LoadLocation(s.Timezone)

	return validate(
		rule{!blank(s.SchoolName), "school name must not be empty"},
		rule{s.Timezone != "", "timezone must not be empty"},
		rule{tzErr == nil && s.Timezone != "Local", "unknown timezone " + s.Timezone},
		rule{!blank(s.DefaultLocale), "default locale must not be empty"},
		rule{validCurrency(s.Currency), "currency must be an ISO 4217 code"},
		rule{validReceiptPrefix(s.ReceiptPrefix), "receipt prefix must be 1-16 of A-Z, 0-9 and -"},
		rule{validWeekday(s.ReleaseWeekday), "invalid release weekday"},
		rule{s.ReleaseTime.Valid(), "invalid release time"},
		rule{s.ReleaseWeeksAhead >= 1, "release weeks ahead must be at least 1"},
		rule{s.BookingCutoff >= 0, "booking cutoff must not be negative"},
		rule{s.CancellationCutoff >= 0, "cancellation cutoff must not be negative"},
		rule{s.DefaultVisibleDays >= 1, "default visible days must be at least 1"},
	)
}

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
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}

	return true
}
