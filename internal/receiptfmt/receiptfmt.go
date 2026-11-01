// Package receiptfmt holds the wording that receipts share with the data
// that produces them: the language of a receipt, its dates and the
// descriptions of a pass sale (ADR 0012) and an event fee (ADR 0019). The receipt service and the seed
// both use it, so a seeded receipt reads like one the app issues.
package receiptfmt

import (
	"fmt"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Lang returns the language receipts are printed in for a locale: "de" for
// German and its regional forms, "en" otherwise.
func Lang(locale string) string {
	if lang, _, _ := strings.Cut(strings.ToLower(locale), "-"); lang == "de" {
		return "de"
	}

	return "en"
}

// Date formats a school date in a language of Lang, e.g. "24.10.2026" or
// "24 Oct 2026".
func Date(lang string, d domain.Date) string {
	if lang == "de" {
		return fmt.Sprintf("%02d.%02d.%04d", d.Day, int(d.Month), d.Year)
	}

	return fmt.Sprintf("%d %s %d", d.Day, d.Month.String()[:3], d.Year)
}

// passLines are the words of a pass line by language: the pass for its
// owner, and its validity with and without a last day.
var passLines = map[string][3]string{
	"en": {"%s for %s", "valid %s – %s", "valid from %s"},
	"de": {"%s für %s", "gültig %s – %s", "gültig ab %s"},
}

// PassLine describes the receipt line of a pass sale in the language of
// locale, e.g. "10-visit pass for Lea Example, valid 1 Oct 2026 – 29 Mar
// 2027", or "…, valid from 1 Oct 2026" for a pass without a last day.
func PassLine(locale, passType, owner string, from, until domain.Date) string {
	lang := Lang(locale)
	w := passLines[lang]

	desc := fmt.Sprintf(w[0], passType, owner)
	if until.IsZero() {
		return desc + ", " + fmt.Sprintf(w[2], Date(lang, from))
	}

	return desc + ", " + fmt.Sprintf(w[1], Date(lang, from), Date(lang, until))
}

// eventLines are the words of an event line by language: the event for its
// participant.
var eventLines = map[string]string{"en": "%s for %s", "de": "%s für %s"}

// EventLine describes the receipt line of an event fee in the language of
// locale, e.g. "Ballet intensive for Mia Example, 3 Jul 2026 – 5 Jul 2026",
// or "…, 3 Jul 2026" for an event on one day. Without dates (an event
// without sessions) it names the event and the participant only.
func EventLine(locale, title, participant string, first, last domain.Date) string {
	lang := Lang(locale)
	desc := fmt.Sprintf(eventLines[lang], title, participant)

	switch {
	case first.IsZero():
		return desc
	case last.IsZero() || last == first:
		return desc + ", " + Date(lang, first)
	}

	return desc + ", " + Date(lang, first) + " – " + Date(lang, last)
}
