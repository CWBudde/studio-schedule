package domain

import "slices"

// Theme is the school's look: its colors, fonts and the layouts of the
// app's main screens (ADR 0018), a school setting. The zero Theme is
// Programme, the app's own.
type Theme string

// The themes.
const (
	// ThemeProgramme is the app's own look.
	ThemeProgramme Theme = "programme"
	// ThemeWerkstatt is the look of the previous dance-school app, with the
	// school's background image.
	ThemeWerkstatt Theme = "werkstatt"
)

// Themes returns all themes, the default first.
func Themes() []Theme { return []Theme{ThemeProgramme, ThemeWerkstatt} }

// Valid reports whether t is a known theme; the zero Theme (Programme) is
// valid.
func (t Theme) Valid() bool { return t == "" || slices.Contains(Themes(), t) }

// OrDefault returns t, or ThemeProgramme for the zero Theme.
func (t Theme) OrDefault() Theme {
	if t == "" {
		return ThemeProgramme
	}

	return t
}
