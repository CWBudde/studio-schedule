package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Settings holds the school-wide configuration.
type Settings struct {
	// SchoolName is the display name of the school.
	SchoolName string
	// Timezone is an IANA time zone name (e.g. "Europe/Berlin") in which all
	// schedule times of the school are interpreted.
	Timezone string
	// DefaultLocale is one of Locales, used when a visitor or an account
	// has not chosen a language. Settings stored before ADR 0013 may hold
	// any language tag; SchoolLocale reads them.
	DefaultLocale string

	// Currency is the ISO 4217 code (e.g. "EUR") of every Money amount.
	Currency string
	// ReceiptPrefix starts every receipt number, e.g. "SD" in "SD-2026-0001".
	ReceiptPrefix string

	// The release rule: every ReleaseWeekday at ReleaseTime (school time)
	// the schedule is released through the same weekday ReleaseWeeksAhead
	// weeks later. Lessons are visible (and, from Phase 4, bookable) once
	// released; see ADR 0003.
	ReleaseWeekday    time.Weekday
	ReleaseTime       TimeOfDay
	ReleaseWeeksAhead int

	// BookingCutoff and CancellationCutoff are how long before a lesson
	// starts booking and canceling close. Zero means "until the start".
	BookingCutoff      time.Duration
	CancellationCutoff time.Duration

	// WaitlistInviteExpiry is how long a waiting-list invitation holds its
	// place, never beyond the booking cutoff (ADR 0010).
	WaitlistInviteExpiry time.Duration

	// DefaultVisibleDays is the early access of signed-in members: they see
	// the lessons they may see up to this many days after today, even if
	// not released yet, unless their account overrides it (ADR 0003).
	DefaultVisibleDays int

	// LegalNotice is the optional imprint shown in the footer.
	LegalNotice string
	// AccentColor is the school's primary color, "#rrggbb" in lower case,
	// or empty for the app's own. It must stand out against the text on it
	// (MinAccentContrast); the dark theme shows DarkAccent of it (ADR 0013).
	AccentColor string
	// Theme is the school's look; zero is Programme (ADR 0018). The
	// background image of themes that show one is stored apart
	// (SettingsRepo.Background).
	Theme Theme

	// RedemptionMode is when bookings use a pass: when they are booked, or
	// when attendance is taken (ADR 0012).
	RedemptionMode RedemptionMode

	// Receipts are the issuer, tax mode and texts of receipts (ADR 0012);
	// incomplete until the school fills them in, and receipts need them
	// complete. The logo is stored apart (SettingsRepo.Logo).
	Receipts ReceiptSettings
}

// Validate checks the settings for consistency. All errors wrap
// ErrValidation.
func (s Settings) Validate() error {
	err := validate(
		on("schoolName", !blank(s.SchoolName), "school name must not be empty"),
		on("schoolName", runes(s.SchoolName) <= MaxSchoolNameLength, "school name is too long"),
		on("legalNotice", runes(s.LegalNotice) <= MaxLegalNoticeLength, "legal notice is too long"),
		on("timezone", s.Timezone != "", "timezone must not be empty"),
		on("defaultLocale", ValidLocale(s.DefaultLocale), "default locale must be en or de"),
		on("currency", validCurrency(s.Currency), "currency must be an ISO 4217 code"),
		on("receiptPrefix", validReceiptPrefix(s.ReceiptPrefix), "receipt prefix must be 1-16 of A-Z and 0-9"),
		on("bookingCutoff", s.BookingCutoff >= 0 && s.BookingCutoff <= maxCutoff, "booking cutoff must be between 0 and 30 days"),
		on(
			"cancellationCutoff", s.CancellationCutoff >= 0 && s.CancellationCutoff <= maxCutoff,
			"cancellation cutoff must be between 0 and 30 days",
		),
		on(
			"waitlistInviteExpiry", s.WaitlistInviteExpiry >= minInviteExpiry && s.WaitlistInviteExpiry <= maxCutoff,
			"waitlist invite expiry must be between 1 minute and 30 days",
		),
		on(
			"defaultVisibleDays", s.DefaultVisibleDays >= 1 && s.DefaultVisibleDays <= maxVisibleDays,
			"default visible days must be between 1 and 366",
		),
		on("redemptionMode", s.RedemptionMode.Valid(), "invalid redemption mode"),
		on("theme", s.Theme.Valid(), "theme must be programme or werkstatt"),
	)
	if err != nil {
		return err
	}

	if err := ValidateReleaseRule(s.ReleaseWeekday, s.ReleaseTime, s.ReleaseWeeksAhead); err != nil {
		return err
	}

	if err := s.validateAccent(); err != nil {
		return err
	}

	if err := s.Receipts.validate(); err != nil {
		return err
	}

	// Loading the zone parses tzdata, so it runs only after the cheap rules.
	if _, err := time.LoadLocation(s.Timezone); err != nil || s.Timezone == "Local" {
		return &FieldError{Field: "timezone", Msg: fmt.Sprintf("unknown timezone %q", s.Timezone)}
	}

	return nil
}

// ValidateReleaseRule checks a weekly release rule (ADR 0003): a weekday,
// a valid time of day and 1 to 52 weeks ahead. Errors are FieldErrors on
// releaseWeekday, releaseTime and releaseWeeksAhead.
func ValidateReleaseRule(weekday time.Weekday, at TimeOfDay, weeksAhead int) error {
	return validate(
		on("releaseWeekday", validWeekday(weekday), "invalid release weekday"),
		on("releaseTime", at.Valid(), "invalid release time"),
		on(
			"releaseWeeksAhead", weeksAhead >= 1 && weeksAhead <= maxWeeksAhead,
			"release weeks ahead must be between 1 and 52",
		),
	)
}

// validateAccent checks the accent color: absent, or "#rrggbb" in lower
// case that reaches MinAccentContrast against the text on it and the page
// in the light theme. Its dark variant always reaches it (DarkAccent).
func (s Settings) validateAccent() error {
	if s.AccentColor == "" {
		return nil
	}

	c, ok := ParseColor(s.AccentColor)

	return validate(
		on("accentColor", ok && c.Hex() == s.AccentColor, "accent color must be #rrggbb in lower case"),
		on("accentColor", AccentContrast(c) >= MinAccentContrast,
			"accent color must reach a contrast of 4.5:1 against the text on it"),
	)
}

// Locales are the languages of the app, its mails and documents; the first
// is the fallback.
var Locales = []string{"en", "de"}

// ValidLocale reports whether s is one of Locales.
func ValidLocale(s string) bool {
	return slices.Contains(Locales, s)
}

// MatchLocale returns the locale of Locales that a language tag asks for:
// the tag itself or its base language ("de-AT" and "de_at" are "de"),
// ignoring case and surrounding space. It reports false if none matches.
func MatchLocale(tag string) (string, bool) {
	tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
	base, _, _ := strings.Cut(tag, "-")

	for _, l := range []string{tag, base} {
		if ValidLocale(l) {
			return l, true
		}
	}

	return "", false
}

// SchoolLocale is the locale of a stored default language: its match
// (MatchLocale), else the fallback Locales[0]. Before ADR 0013 the default
// language could be any tag; reading it this way keeps such settings
// usable and lets every card save them again.
func SchoolLocale(tag string) string {
	if l, ok := MatchLocale(tag); ok {
		return l
	}

	return Locales[0]
}

// Upper bounds of the school's texts, in characters.
const (
	MaxSchoolNameLength  = 200
	MaxLegalNoticeLength = 2000
)

// Upper bounds that keep schedule arithmetic sane.
const (
	maxWeeksAhead  = 52
	maxVisibleDays = 366
	maxCutoff      = 30 * 24 * time.Hour
	// minInviteExpiry keeps an invitation answerable at all.
	minInviteExpiry = time.Minute
)

func validCurrency(s string) bool {
	_, ok := CurrencyExponent(s)

	return ok
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
