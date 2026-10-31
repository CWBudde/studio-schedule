package service

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// TestReceiptMoneyFormat prints amounts the way each receipt language
// writes them: the symbol or the code, the separators, the minor units of
// the currency and the sign of a cancellation. A code or a trailing symbol
// is kept with the number by a no-break space.
func TestReceiptMoneyFormat(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		locale   string
		amount   domain.Money
		currency string
		want     string
	}{
		{"en", 0, "EUR", "€0.00"},
		{"en", 5, "EUR", "€0.05"},
		{"en", 123456, "EUR", "€1,234.56"},
		{"en", -1500, "EUR", "−€15.00"},
		{"en", 1234567, "CHF", "CHF\u00a012,345.67"},
		{"en", 1234, "JPY", "JPY\u00a01,234"},
		{"en", 12345, "KWD", "KWD\u00a012.345"},
		// Settings.Validate refuses an unknown code; it prints with cents.
		{"en", 1500, "XYZ", "XYZ\u00a015.00"},
		{"de", 123456, "EUR", "1.234,56 €"},
		{"de", 100000000, "USD", "1.000.000,00\u00a0$"},
		{"de", 1234, "JPY", "1.234\u00a0JPY"},
		{"de", -5, "GBP", "−0,05 £"},
	} {
		_, l := receiptLanguage(c.locale)
		if got := l.formatMoney(c.amount, c.currency); got != c.want {
			t.Errorf("%s: %d %s = %q, want %q", c.locale, c.amount, c.currency, got, c.want)
		}
	}
}

// TestReceiptRateFormat prints VAT rates in basis points with the
// language's decimal mark, without trailing zeros.
func TestReceiptRateFormat(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		locale string
		bp     int
		want   string
	}{
		{"en", 0, "0%"},
		{"en", 1900, "19%"},
		{"en", 750, "7.5%"},
		{"en", 1025, "10.25%"},
		{"en", 5, "0.05%"},
		{"de", 1900, "19\u00a0%"},
		{"de", 750, "7,5\u00a0%"},
	} {
		_, l := receiptLanguage(c.locale)
		if got := l.formatRate(c.bp); got != c.want {
			t.Errorf("%s: %d bp = %q, want %q", c.locale, c.bp, got, c.want)
		}
	}
}

// TestReceiptDocumentLogo tells the template which kind of logo to print,
// if any.
func TestReceiptDocumentLogo(t *testing.T) {
	t.Parallel()

	r := domain.Receipt{Number: "SD-2026-0001", Kind: domain.ReceiptSale, Locale: "en", Currency: "EUR"}

	for typ, want := range map[string]string{domain.LogoPNG: "png", domain.LogoJPEG: "jpg", "": ""} {
		if got := receiptDocument(r, nil, domain.Logo{ContentType: typ}).Logo; got != want {
			t.Errorf("logo %q: printed as %q, want %q", typ, got, want)
		}
	}
}
