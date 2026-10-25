package mail_test

import (
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/mail"
)

func receiptData(cancellation bool) mail.ReceiptData {
	d := mail.ReceiptData{
		SchoolName: "Studio Example", GivenName: "Clara", Number: "SD-2026-0007", Amount: "150,00 €",
		Item: "10-visit pass for Ella <Example>, valid 1 Oct 2026 – 29 Mar 2027",
	}
	if cancellation {
		d.Number, d.Cancellation, d.Cancels = "SD-2026-0008", true, "SD-2026-0007"
	}

	return d
}

// TestRenderReceiptGolden pins the receipt mail and the cancellation mail in
// both locales.
func TestRenderReceiptGolden(t *testing.T) {
	t.Parallel()

	r := newRenderer(t)

	for _, locale := range []string{"en", "de"} {
		for name, cancellation := range map[string]bool{"receipt": false, "receipt-cancellation": true} {
			t.Run(locale+"/"+name, func(t *testing.T) {
				t.Parallel()

				got, err := r.Render(locale, mail.TemplateReceipt, receiptData(cancellation))
				if err != nil {
					t.Fatalf("Render: %v", err)
				}

				golden(t, locale+"/"+name+".subject.golden", got.Subject+"\n")
				golden(t, locale+"/"+name+".txt.golden", got.Text)
				golden(t, locale+"/"+name+".html.golden", got.HTML)

				if !strings.Contains(got.Text, "SD-2026-000") || strings.Contains(got.HTML, "<Example>") {
					t.Errorf("number missing or HTML not escaped:\n%s\n%s", got.Text, got.HTML)
				}
			})
		}
	}
}
