package receiptfmt_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/receiptfmt"
)

func TestPassLine(t *testing.T) {
	t.Parallel()

	from, until := domain.Date{Year: 2026, Month: 10, Day: 1}, domain.Date{Year: 2027, Month: 3, Day: 29}

	for _, c := range []struct {
		locale string
		until  domain.Date
		want   string
	}{
		{"en", until, "10-visit pass for Lea Example, valid 1 Oct 2026 – 29 Mar 2027"},
		{"de-AT", until, "10-visit pass für Lea Example, gültig 01.10.2026 – 29.03.2027"},
		{"fr", domain.Date{}, "10-visit pass for Lea Example, valid from 1 Oct 2026"},
		{"de", domain.Date{}, "10-visit pass für Lea Example, gültig ab 01.10.2026"},
	} {
		if got := receiptfmt.PassLine(c.locale, "10-visit pass", "Lea Example", from, c.until); got != c.want {
			t.Errorf("%s: %q, want %q", c.locale, got, c.want)
		}
	}
}

func TestEventLine(t *testing.T) {
	t.Parallel()

	first, last := domain.Date{Year: 2026, Month: 7, Day: 3}, domain.Date{Year: 2026, Month: 7, Day: 5}

	for _, c := range []struct {
		locale      string
		first, last domain.Date
		want        string
	}{
		{"en", first, last, "Ballet intensive for Mia Example, 3 Jul 2026 – 5 Jul 2026"},
		{"de-DE", first, last, "Ballet intensive für Mia Example, 03.07.2026 – 05.07.2026"},
		{"en", first, first, "Ballet intensive for Mia Example, 3 Jul 2026"},
		{"de", first, domain.Date{}, "Ballet intensive für Mia Example, 03.07.2026"},
		{"fr", domain.Date{}, domain.Date{}, "Ballet intensive for Mia Example"},
	} {
		if got := receiptfmt.EventLine(c.locale, "Ballet intensive", "Mia Example", c.first, c.last); got != c.want {
			t.Errorf("%s %v–%v: %q, want %q", c.locale, c.first, c.last, got, c.want)
		}
	}
}
