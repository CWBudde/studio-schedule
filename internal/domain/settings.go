package domain

import (
	"fmt"
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
	// DefaultLocale is a BCP 47 language tag (e.g. "en", "de") used when a
	// user has not chosen a locale.
	DefaultLocale string
}

// Validate checks the settings for consistency. All errors wrap
// ErrValidation.
func (s Settings) Validate() error {
	if strings.TrimSpace(s.SchoolName) == "" {
		return fmt.Errorf("%w: school name must not be empty", ErrValidation)
	}

	if s.Timezone == "" {
		return fmt.Errorf("%w: timezone must not be empty", ErrValidation)
	}

	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("%w: unknown timezone %q", ErrValidation, s.Timezone)
	}

	if strings.TrimSpace(s.DefaultLocale) == "" {
		return fmt.Errorf("%w: default locale must not be empty", ErrValidation)
	}

	return nil
}
