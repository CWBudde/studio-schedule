package service

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/pdf"
	"github.com/cwbudde/studio-schedule/internal/receiptfmt"
)

// The data of the receipt template (tools/receipt-pdf/receipt.typ): every
// text, label, amount and date preformatted in the receipt's language, so the
// template only lays them out. A receipt is printed in the school's default
// language at issuance (Receipt.Locale); English unless that is German.

// receiptDoc is the JSON the template reads as /data.json.
type receiptDoc struct {
	Title          string      `json:"title"`
	Date           docDate     `json:"date"`
	Lang           string      `json:"lang"`
	Heading        string      `json:"heading"`
	Issuer         docIssuer   `json:"issuer"`
	Logo           string      `json:"logo"` // "", "png" or "jpg"
	RecipientLabel string      `json:"recipientLabel"`
	Recipient      []string    `json:"recipient"`
	Meta           [][2]string `json:"meta"`
	Intro          string      `json:"intro"`
	ShowVAT        bool        `json:"showVat"`
	Columns        [6]string   `json:"columns"`
	Lines          []docLine   `json:"lines"`
	Summary        [][2]string `json:"summary"`
	Total          [2]string   `json:"total"`
	Notes          []string    `json:"notes"`
	Footer         []string    `json:"footer"`
}

type docDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type docIssuer struct {
	Name  string   `json:"name"`
	Lines []string `json:"lines"`
}

type docLine struct {
	Pos         string `json:"pos"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	Unit        string `json:"unit"`
	VAT         string `json:"vat"`
	Total       string `json:"total"`
}

// receiptLabels are the words of one language on a receipt.
type receiptLabels struct {
	sale, cancellation        string
	receivedFrom, refundedTo  string
	number, date, payment     string
	cancels                   string // "Cancels": "<number> of <date>"
	of                        string
	intro, cancelIntro        string // cancelIntro gets the number and the date
	columns                   [6]string
	net, netNoVAT, vat, total string
	methods, refunds          map[domain.PaymentMethod]string
	methodNames               map[domain.PaymentMethod]string
	smallBusiness             string
	taxID, email, phone       string
	reason                    string
	decimal, thousands        byte
	currencyFirst             bool
	percent                   string // between the number and "%"
}

var receiptLanguages = map[string]receiptLabels{
	"en": {
		sale: "Receipt", cancellation: "Cancellation",
		receivedFrom: "Payment received from", refundedTo: "Refunded to",
		number: "Number", date: "Date", payment: "Payment", cancels: "Cancels", of: "of",
		intro:       "We confirm receipt of the following payment:",
		cancelIntro: "This cancels receipt %s of %s:",
		columns:     [6]string{"Item", "Description", "Qty", "Unit price", "VAT", "Amount"},
		net:         "Net (%s VAT)", netNoVAT: "Without VAT", vat: "VAT %s", total: "Total",
		methods: map[domain.PaymentMethod]string{
			domain.PaymentCash: "Paid in cash.", domain.PaymentTransfer: "Paid by bank transfer.",
			domain.PaymentCard: "Paid by card.",
		},
		refunds: map[domain.PaymentMethod]string{
			domain.PaymentCash: "Refunded in cash.", domain.PaymentTransfer: "Refunded by bank transfer.",
			domain.PaymentCard: "Refunded to the card.",
		},
		methodNames: map[domain.PaymentMethod]string{
			domain.PaymentCash: "Cash", domain.PaymentTransfer: "Bank transfer", domain.PaymentCard: "Card",
		},
		smallBusiness: "No VAT is charged under the small business rule (§ 19 UStG).",
		taxID:         "Tax number", email: "Email", phone: "Phone", reason: "Reason: %s",
		decimal: '.', thousands: ',', currencyFirst: true, percent: "",
	},
	"de": {
		sale: "Quittung", cancellation: "Stornobeleg",
		receivedFrom: "Zahlung erhalten von", refundedTo: "Erstattet an",
		number: "Nummer", date: "Datum", payment: "Zahlungsart", cancels: "Storno zu", of: "vom",
		intro:       "Wir bestätigen den Erhalt folgender Zahlung:",
		cancelIntro: "Hiermit stornieren wir die Quittung %s vom %s:",
		columns:     [6]string{"Pos.", "Beschreibung", "Menge", "Einzelpreis", "USt.", "Betrag"},
		net:         "Nettobetrag (%s USt.)", netNoVAT: "Ohne USt.", vat: "Umsatzsteuer %s", total: "Gesamtbetrag",
		methods: map[domain.PaymentMethod]string{
			domain.PaymentCash: "Betrag bar erhalten.", domain.PaymentTransfer: "Per Überweisung bezahlt.",
			domain.PaymentCard: "Mit Karte bezahlt.",
		},
		refunds: map[domain.PaymentMethod]string{
			domain.PaymentCash: "Betrag bar erstattet.", domain.PaymentTransfer: "Per Überweisung erstattet.",
			domain.PaymentCard: "Auf die Karte erstattet.",
		},
		methodNames: map[domain.PaymentMethod]string{
			domain.PaymentCash: "Bar", domain.PaymentTransfer: "Überweisung", domain.PaymentCard: "Karte",
		},
		smallBusiness: "Gemäß § 19 UStG wird keine Umsatzsteuer berechnet.",
		taxID:         "Steuernummer", email: "E-Mail", phone: "Tel.", reason: "Grund: %s",
		decimal: ',', thousands: '.', currencyFirst: false, percent: " ",
	},
}

// receiptLanguage returns the language of receipts for a locale and its
// words (receiptfmt.Lang).
func receiptLanguage(locale string) (string, receiptLabels) {
	lang := receiptfmt.Lang(locale)

	return lang, receiptLanguages[lang]
}

// currencySymbols are the symbols of the common currencies; others print
// their code.
var currencySymbols = map[string]string{"EUR": "€", "USD": "$", "GBP": "£"}

// formatMoney formats an amount in minor units of currency, e.g.
// "1.234,56 €" in German or "€1,234.56" in English.
func (l receiptLabels) formatMoney(m domain.Money, currency string) string {
	neg := m < 0
	if neg {
		m = -m
	}

	// Settings.Validate refuses unknown currencies; 2 is a sane fallback.
	decimals, ok := domain.CurrencyExponent(currency)
	if !ok {
		decimals = 2
	}

	digits := strconv.FormatInt(int64(m), 10)
	for len(digits) <= decimals {
		digits = "0" + digits
	}

	whole, frac := digits[:len(digits)-decimals], digits[len(digits)-decimals:]

	var b strings.Builder

	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(l.thousands)
		}

		b.WriteRune(c)
	}

	if decimals > 0 {
		b.WriteByte(l.decimal)
		b.WriteString(frac)
	}

	number := b.String()
	symbol := cmp.Or(currencySymbols[currency], currency)

	var out string

	switch {
	case l.currencyFirst && utf8.RuneCountInString(symbol) > 1: // a code
		out = symbol + " " + number
	case l.currencyFirst:
		out = symbol + number
	default:
		out = number + " " + symbol
	}

	if neg {
		return "−" + out
	}

	return out
}

// formatRate formats a VAT rate in basis points, e.g. "19 %" or "7.5%".
func (l receiptLabels) formatRate(bp int) string {
	s := strconv.Itoa(bp / 100)
	if frac := bp % 100; frac != 0 {
		f := strings.TrimRight(fmt.Sprintf("%02d", frac), "0")
		s += string(l.decimal) + f
	}

	return s + l.percent + "%"
}

// receiptInput returns the input of the typst module for a receipt: its
// template data, and the logo. orig is the receipt a cancellation cancels.
func receiptInput(r domain.Receipt, orig *domain.Receipt, logo domain.Logo) ([]byte, error) {
	data, err := json.Marshal(receiptDocument(r, orig, logo))
	if err != nil {
		return nil, fmt.Errorf("receipt data: %w", err)
	}

	return pdf.Input(data, logo.Data)
}

func receiptDocument(r domain.Receipt, orig *domain.Receipt, logo domain.Logo) receiptDoc {
	lang, l := receiptLanguage(r.Locale)
	money := func(m domain.Money) string { return l.formatMoney(m, r.Currency) }
	cancellation := r.Kind == domain.ReceiptCancellation

	doc := receiptDoc{
		Date: docDate{Year: r.IssueDate.Year, Month: int(r.IssueDate.Month), Day: r.IssueDate.Day},
		Lang: lang, Heading: l.sale, RecipientLabel: l.receivedFrom, Intro: l.intro, Columns: l.columns,
		Issuer:    docIssuer{Name: r.Issuer.Name, Lines: lines(r.Issuer.Address)},
		Recipient: append([]string{r.RecipientName}, lines(r.RecipientAddress)...),
		Meta: [][2]string{
			{l.number, r.Number}, {l.date, receiptfmt.Date(lang, r.IssueDate)}, {l.payment, l.methodNames[r.PaymentMethod]},
		},
		ShowVAT: r.TaxMode == domain.TaxStandard,
		Total:   [2]string{l.total, money(r.Gross)},
		// Empty lists, never null: the template maps over them.
		Lines: []docLine{}, Summary: [][2]string{}, Notes: []string{},
	}

	if cancellation {
		doc.Heading, doc.RecipientLabel = l.cancellation, l.refundedTo

		if orig != nil {
			od := receiptfmt.Date(lang, orig.IssueDate)
			doc.Intro = fmt.Sprintf(l.cancelIntro, orig.Number, od)
			doc.Meta = append(doc.Meta, [2]string{l.cancels, orig.Number + " " + l.of + " " + od})
		}
	}

	doc.Title = doc.Heading + " " + r.Number

	switch logo.ContentType {
	case domain.LogoPNG:
		doc.Logo = "png"
	case domain.LogoJPEG:
		doc.Logo = "jpg"
	}

	type group struct{ net, vat domain.Money }

	groups := map[int]*group{}

	for _, line := range r.Lines {
		doc.Lines = append(doc.Lines, docLine{
			Pos: strconv.Itoa(line.Position), Description: line.Description, Quantity: strconv.Itoa(line.Quantity),
			Unit: money(line.UnitPrice), VAT: l.formatRate(line.VATRate), Total: money(line.Gross),
		})

		g := groups[line.VATRate]
		if g == nil {
			g = &group{}
			groups[line.VATRate] = g
		}

		g.net += line.Net
		g.vat += line.VAT
	}

	if doc.ShowVAT {
		rates := make([]int, 0, len(groups))
		for rate := range groups {
			rates = append(rates, rate)
		}

		slices.Sort(rates)

		for _, rate := range rates {
			g := groups[rate]
			if rate == 0 {
				doc.Summary = append(doc.Summary, [2]string{l.netNoVAT, money(g.net)})

				continue
			}

			doc.Summary = append(doc.Summary,
				[2]string{fmt.Sprintf(l.net, l.formatRate(rate)), money(g.net)},
				[2]string{fmt.Sprintf(l.vat, l.formatRate(rate)), money(g.vat)})
		}
	}

	switch {
	case r.TaxNote != "":
		doc.Notes = append(doc.Notes, r.TaxNote)
	case r.TaxMode == domain.TaxSmallBusiness:
		doc.Notes = append(doc.Notes, l.smallBusiness)
	}

	if cancellation {
		doc.Notes = append(doc.Notes, l.refunds[r.PaymentMethod])
		if r.Reason != "" {
			doc.Notes = append(doc.Notes, fmt.Sprintf(l.reason, r.Reason))
		}
	} else {
		doc.Notes = append(doc.Notes, l.methods[r.PaymentMethod])
	}

	doc.Footer = []string{r.Issuer.Name, strings.Join(lines(r.Issuer.Address), ", "), l.taxID + " " + r.Issuer.TaxID}
	if r.Issuer.Email != "" {
		doc.Footer = append(doc.Footer, l.email+" "+r.Issuer.Email)
	}

	if r.Issuer.Phone != "" {
		doc.Footer = append(doc.Footer, l.phone+" "+r.Issuer.Phone)
	}

	if r.Footer != "" {
		doc.Footer = append(doc.Footer, lines(r.Footer)...)
	}

	return doc
}

// lines splits a multi-line text into its non-empty lines.
func lines(s string) []string {
	out := []string{}

	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}

	return out
}

// SampleReceiptInput is the renderer's input for a fictional receipt, sold
// in cash with the VAT shown, for a check that the receipt module renders
// (`studio-schedule doctor`) without touching a real one.
func SampleReceiptInput() []byte {
	r := domain.Receipt{
		Number: "SAMPLE-2026-0001", Kind: domain.ReceiptSale, IssueDate: domain.Date{Year: 2026, Month: time.September, Day: 22},
		Locale: "en", Currency: "EUR", TaxMode: domain.TaxStandard, PaymentMethod: domain.PaymentCash,
		Issuer: domain.ReceiptIssuer{
			Name: "Example Dance School", Address: "1 Example Street\n12345 Example Town", TaxID: "00/000/00000",
			Email: "office@example.org",
		},
		RecipientName: "Erika Example", Net: 8403, VAT: 1597, Gross: 10000,
		Lines: []domain.ReceiptLine{{
			Position: 1, Description: "10-class pass", Quantity: 1, UnitPrice: 10000, VATRate: 1900,
			Net: 8403, VAT: 1597, Gross: 10000,
		}},
	}

	// The document is strings and numbers, which always marshal, and far
	// below the size limit of the input format.
	data, _ := json.Marshal(receiptDocument(r, nil, domain.Logo{}))
	input, _ := pdf.Input(data, nil)

	return input
}
