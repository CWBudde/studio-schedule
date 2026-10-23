package domain_test

import (
	"errors"
	"testing"

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

func TestSettingsValidate(t *testing.T) {
	t.Parallel()

	valid := domain.Settings{SchoolName: "Test School", Timezone: "Europe/Berlin", DefaultLocale: "en"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid settings: unexpected error %v", err)
	}

	cases := map[string]func(*domain.Settings){
		"empty name":       func(s *domain.Settings) { s.SchoolName = "  " },
		"empty timezone":   func(s *domain.Settings) { s.Timezone = "" },
		"unknown timezone": func(s *domain.Settings) { s.Timezone = "Mars/Olympus" },
		"empty locale":     func(s *domain.Settings) { s.DefaultLocale = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := valid
			mutate(&s)

			if err := s.Validate(); !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}
