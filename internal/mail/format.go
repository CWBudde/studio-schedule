package mail

import (
	"fmt"
	"strings"
	"time"
)

// Locale-dependent formatting for the templates. Go's time package knows
// only English names, so the names of every template locale are listed
// here; a test checks that each locale has them.

// dateFormat writes a date in the words of one locale.
type dateFormat struct {
	weekdays [7]string  // Sunday first, as time.Weekday
	months   [12]string // January first
	// layout gets the weekday, the day of the month, the month and the
	// year.
	layout string
}

var dateFormats = map[string]dateFormat{
	"en": {
		weekdays: [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		months: [12]string{
			"January", "February", "March", "April", "May", "June", "July", "August", "September", "October",
			"November", "December",
		},
		layout: "%s, %d %s %d",
	},
	"de": {
		weekdays: [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
		months: [12]string{
			"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober",
			"November", "Dezember",
		},
		layout: "%s, %d. %s %d",
	},
}

// funcs returns the template functions of a locale:
//
//   - date formats the date of an instant in the instant's own location,
//     e.g. "Tuesday, 27 October 2026" or "Dienstag, 27. Oktober 2026".
//   - clock formats its time of day, e.g. "18:00".
//   - join joins names with ", ".
//   - lines splits a text into its lines, for line breaks in HTML.
func funcs(locale string) map[string]any {
	f, ok := dateFormats[locale]

	return map[string]any{
		"date": func(t time.Time) (string, error) {
			if !ok {
				return "", fmt.Errorf("no date format for locale %q", locale)
			}

			return fmt.Sprintf(f.layout, f.weekdays[t.Weekday()], t.Day(), f.months[t.Month()-1], t.Year()), nil
		},
		"clock": func(t time.Time) string { return t.Format("15:04") },
		"join":  func(names []string) string { return strings.Join(names, ", ") },
		"lines": func(text string) []string { return strings.Split(text, "\n") },
	}
}
