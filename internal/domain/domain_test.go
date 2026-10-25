package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestRoleValid(t *testing.T) {
	t.Parallel()

	for _, r := range domain.Roles() {
		if !r.Valid() {
			t.Errorf("role %q should be valid", r)
		}
	}

	for _, r := range []domain.Role{"", "root", "Admin"} {
		if r.Valid() {
			t.Errorf("role %q should be invalid", r)
		}
	}
}

func TestRoleAtLeast(t *testing.T) {
	t.Parallel()

	roles := domain.Roles()
	for i, r := range roles {
		for j, minimum := range roles {
			if got, want := r.AtLeast(minimum), i >= j; got != want {
				t.Errorf("%s.AtLeast(%s) = %v, want %v", r, minimum, got, want)
			}
		}

		if got, want := r.Staff(), r.AtLeast(domain.RoleInstructor); got != want {
			t.Errorf("%s.Staff() = %v, want %v", r, got, want)
		}

		if domain.Role("root").AtLeast(r) || domain.Role("").AtLeast(r) {
			t.Errorf("an unknown role is at least %s", r)
		}
	}
}

func TestSettingsValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, func() domain.Settings {
		return domain.Settings{
			SchoolName: "Test School", Timezone: "Europe/Berlin", DefaultLocale: "en",
			Currency: "EUR", ReceiptPrefix: "TS",
			ReleaseWeekday: time.Sunday, ReleaseTime: domain.TimeOfDay{Hour: 18}, ReleaseWeeksAhead: 1,
			BookingCutoff: 0, CancellationCutoff: 2 * time.Hour, DefaultVisibleDays: 14,
			WaitlistInviteExpiry: 12 * time.Hour, RedemptionMode: domain.RedeemOnAttendance,
		}
	}, map[string]func(*domain.Settings){
		"no redemption mode":    func(s *domain.Settings) { s.RedemptionMode = "" },
		"bad redemption mode":   func(s *domain.Settings) { s.RedemptionMode = "never" },
		"bad tax mode":          func(s *domain.Settings) { s.Receipts.TaxMode = "none" },
		"bad issuer email":      func(s *domain.Settings) { s.Receipts.Issuer.Email = "office" },
		"padded issuer name":    func(s *domain.Settings) { s.Receipts.Issuer.Name = " School" },
		"zero invite expiry":    func(s *domain.Settings) { s.WaitlistInviteExpiry = 0 },
		"short invite expiry":   func(s *domain.Settings) { s.WaitlistInviteExpiry = 59 * time.Second },
		"huge invite expiry":    func(s *domain.Settings) { s.WaitlistInviteExpiry = 31 * 24 * time.Hour },
		"empty name":            func(s *domain.Settings) { s.SchoolName = "  " },
		"empty timezone":        func(s *domain.Settings) { s.Timezone = "" },
		"unknown timezone":      func(s *domain.Settings) { s.Timezone = "Mars/Olympus" },
		"local timezone":        func(s *domain.Settings) { s.Timezone = "Local" },
		"empty locale":          func(s *domain.Settings) { s.DefaultLocale = "" },
		"lower-case currency":   func(s *domain.Settings) { s.Currency = "eur" },
		"long currency":         func(s *domain.Settings) { s.Currency = "EURO" },
		"empty receipt prefix":  func(s *domain.Settings) { s.ReceiptPrefix = "" },
		"bad receipt prefix":    func(s *domain.Settings) { s.ReceiptPrefix = "TS 1" },
		"bad release weekday":   func(s *domain.Settings) { s.ReleaseWeekday = -1 },
		"bad release time":      func(s *domain.Settings) { s.ReleaseTime = domain.TimeOfDay{Minute: 60} },
		"zero weeks ahead":      func(s *domain.Settings) { s.ReleaseWeeksAhead = 0 },
		"negative booking cut":  func(s *domain.Settings) { s.BookingCutoff = -time.Minute },
		"negative cancel cut":   func(s *domain.Settings) { s.CancellationCutoff = -time.Minute },
		"zero visible days":     func(s *domain.Settings) { s.DefaultVisibleDays = 0 },
		"visible days negative": func(s *domain.Settings) { s.DefaultVisibleDays = -7 },
		"dash in prefix":        func(s *domain.Settings) { s.ReceiptPrefix = "TS-" },
		"too many weeks ahead":  func(s *domain.Settings) { s.ReleaseWeeksAhead = 53 },
		"huge booking cut":      func(s *domain.Settings) { s.BookingCutoff = 31 * 24 * time.Hour },
		"huge cancel cut":       func(s *domain.Settings) { s.CancellationCutoff = 1<<63 - 1 },
		"too many visible days": func(s *domain.Settings) { s.DefaultVisibleDays = 367 },
		"regional locale":       func(s *domain.Settings) { s.DefaultLocale = "de-DE" },
		"unknown locale":        func(s *domain.Settings) { s.DefaultLocale = "fr" },
		"long name":             func(s *domain.Settings) { s.SchoolName = strings.Repeat("a", 201) },
		"long legal notice":     func(s *domain.Settings) { s.LegalNotice = strings.Repeat("a", 2001) },
	})
}

// The school's texts are limited in characters, as the API's maxLength and
// the forms count them, not in bytes.
func TestSettingsTextLengthsInCharacters(t *testing.T) {
	t.Parallel()

	s := domain.Settings{
		SchoolName: strings.Repeat("ä", domain.MaxSchoolNameLength), Timezone: "Europe/Berlin", DefaultLocale: "de",
		Currency: "EUR", ReceiptPrefix: "TS",
		ReleaseWeekday: time.Sunday, ReleaseTime: domain.TimeOfDay{Hour: 18}, ReleaseWeeksAhead: 1,
		DefaultVisibleDays: 14, WaitlistInviteExpiry: time.Hour, RedemptionMode: domain.RedeemOnAttendance,
		LegalNotice: strings.Repeat("ü", domain.MaxLegalNoticeLength),
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("%d umlauts: %v", domain.MaxSchoolNameLength, err)
	}

	s.SchoolName += "ä"
	if err := s.Validate(); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("one more: %v", err)
	}
}

func TestMatchLocale(t *testing.T) {
	t.Parallel()

	for tag, want := range map[string]string{
		"en": "en", "de": "de", "DE": "de", " de-DE ": "de", "de_AT": "de", "en-US": "en", "en-GB-oxendict": "en",
	} {
		if got, ok := domain.MatchLocale(tag); !ok || got != want {
			t.Errorf("MatchLocale(%q) = %q, %v; want %q", tag, got, ok, want)
		}

		if got := domain.SchoolLocale(tag); got != want {
			t.Errorf("SchoolLocale(%q) = %q, want %q", tag, got, want)
		}
	}

	for _, tag := range []string{"", "fr", "fr-FR", "deu", "-de"} {
		if got, ok := domain.MatchLocale(tag); ok {
			t.Errorf("MatchLocale(%q) = %q, want no match", tag, got)
		}

		if got := domain.SchoolLocale(tag); got != domain.Locales[0] {
			t.Errorf("SchoolLocale(%q) = %q, want the fallback %q", tag, got, domain.Locales[0])
		}
	}
}
