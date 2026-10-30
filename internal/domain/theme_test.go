package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestThemeValid(t *testing.T) {
	t.Parallel()

	for _, th := range []domain.Theme{"", domain.ThemeProgramme, domain.ThemeWerkstatt} {
		if !th.Valid() {
			t.Errorf("theme %q should be valid", th)
		}
	}

	for _, th := range []domain.Theme{"neon", "Programme", " werkstatt"} {
		if th.Valid() {
			t.Errorf("theme %q should be invalid", th)
		}
	}

	if got := domain.Themes(); len(got) != 2 || got[0] != domain.ThemeProgramme || got[1] != domain.ThemeWerkstatt {
		t.Errorf("Themes() = %v", got)
	}
}

func TestThemeOrDefault(t *testing.T) {
	t.Parallel()

	for in, want := range map[domain.Theme]domain.Theme{
		"":                    domain.ThemeProgramme,
		domain.ThemeProgramme: domain.ThemeProgramme,
		domain.ThemeWerkstatt: domain.ThemeWerkstatt,
	} {
		if got := in.OrDefault(); got != want {
			t.Errorf("%q.OrDefault() = %q, want %q", in, got, want)
		}
	}
}

func TestSettingsValidateTheme(t *testing.T) {
	t.Parallel()

	s := domain.Settings{
		SchoolName: "Test School", Timezone: "Europe/Berlin", DefaultLocale: "en",
		Currency: "EUR", ReceiptPrefix: "TS", ReleaseWeeksAhead: 1, DefaultVisibleDays: 14,
		WaitlistInviteExpiry: time.Hour, RedemptionMode: domain.RedeemOnAttendance,
	}

	for _, th := range []domain.Theme{"", domain.ThemeProgramme, domain.ThemeWerkstatt} {
		s.Theme = th
		if err := s.Validate(); err != nil {
			t.Errorf("theme %q: %v", th, err)
		}
	}

	s.Theme = "neon"

	var fe *domain.FieldError
	if err := s.Validate(); !errors.As(err, &fe) || fe.Field != "theme" || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("theme neon: want a field error on theme, got %v", err)
	}
}
