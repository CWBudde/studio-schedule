// Package receiptfmt holds the wording that receipts share with the data
// that produces them: the language of a receipt, its dates and the
// description of a pass sale (ADR 0012). The receipt service and the seed
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
