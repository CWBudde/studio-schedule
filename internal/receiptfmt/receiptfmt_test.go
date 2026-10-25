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
