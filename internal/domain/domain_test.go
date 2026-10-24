package domain_test

import (
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
			WaitlistInviteExpiry: 12 * time.Hour,
		}
	}, map[string]func(*domain.Settings){
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
	})
}
