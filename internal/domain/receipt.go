package domain

import (
	"bytes"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Receipts (ADR 0012) are payment confirmations for pass sales. They are
// immutable: a mistake is corrected by a cancellation receipt (Storno) that
// negates the original.

// ReceiptKind tells a sale from the cancellation of an earlier receipt.
type ReceiptKind string

// Known receipt kinds.
const (
	ReceiptSale         ReceiptKind = "sale"
	ReceiptCancellation ReceiptKind = "cancellation"
)

// ReceiptKinds returns all receipt kinds.
func ReceiptKinds() []ReceiptKind { return []ReceiptKind{ReceiptSale, ReceiptCancellation} }

// Valid reports whether k is a known receipt kind.
func (k ReceiptKind) Valid() bool { return slices.Contains(ReceiptKinds(), k) }

// TaxMode is how the school charges VAT, a school setting snapshotted on
// every receipt (not legal advice; ADR 0012).
type TaxMode string

// Known tax modes.
const (
	// TaxSmallBusiness charges no VAT under the small business rule
	// (Kleinunternehmerregelung, § 19 UStG); the receipt says so.
	TaxSmallBusiness TaxMode = "small_business"
	// TaxExempt charges no VAT because the lessons are exempt; the receipt
	// prints the school's note with the legal basis.
	TaxExempt TaxMode = "exempt"
	// TaxStandard charges the VAT rate of each pass type, included in the
	// gross price; the receipt shows net and VAT per rate.
	TaxStandard TaxMode = "standard"
)

// TaxModes returns all tax modes.
func TaxModes() []TaxMode { return []TaxMode{TaxSmallBusiness, TaxExempt, TaxStandard} }

// Valid reports whether m is a known tax mode.
func (m TaxMode) Valid() bool { return slices.Contains(TaxModes(), m) }

// PaymentMethod is how a receipt's amount was paid. The app records it; it is
// not a cash register.
type PaymentMethod string

// Known payment methods.
const (
	PaymentCash     PaymentMethod = "cash"
	PaymentTransfer PaymentMethod = "transfer"
	PaymentCard     PaymentMethod = "card"
)

// PaymentMethods returns all payment methods.
func PaymentMethods() []PaymentMethod {
	return []PaymentMethod{PaymentCash, PaymentTransfer, PaymentCard}
}

// Valid reports whether m is a known payment method.
func (m PaymentMethod) Valid() bool { return slices.Contains(PaymentMethods(), m) }

// Bounds of the texts on receipts and in the receipt settings, in characters.
const (
	MaxIssuerNameLength    = 200
	MaxIssuerAddressLength = 500
	MaxTaxIDLength         = 50
	MaxIssuerPhoneLength   = 50
	MaxTaxNoteLength       = 500
	MaxReceiptFooterLength = 500
	MaxRecipientLength     = 200
	MaxReceiptReasonLength = 200
)

// ReceiptIssuer is who issues the receipts: the school's legal name,
// address, tax number or VAT ID and contact.
type ReceiptIssuer struct {
	Name string
	// Address has one line per line of the printed address.
	Address string
	// TaxID is the tax number or the VAT ID.
	TaxID string
	Email string // optional
	Phone string // optional
}

// ReceiptSettings are the school's settings for receipts (ADR 0012): the
// issuer, the tax mode and the texts every receipt prints. They are
// snapshotted on each receipt.
type ReceiptSettings struct {
	Issuer ReceiptIssuer
	// TaxMode is zero until the school chose one.
	TaxMode TaxMode
	// TaxNote is printed with every receipt: required in the exempt mode
	// (the legal basis); in the small business mode it replaces the
	// default note.
	TaxNote string
	// Footer is an optional closing text, e.g. the bank details.
	Footer string
}

// validate checks the fields; they may be incomplete (see Complete).
func (s ReceiptSettings) validate() error {
	return validate(
		on("issuerName", trimmed(s.Issuer.Name) && runes(s.Issuer.Name) <= MaxIssuerNameLength,
			"the issuer name is too long or starts or ends with spaces"),
		on("issuerAddress", trimmed(s.Issuer.Address) && runes(s.Issuer.Address) <= MaxIssuerAddressLength,
			"the address is too long or starts or ends with spaces"),
		on("taxNumber", trimmed(s.Issuer.TaxID) && runes(s.Issuer.TaxID) <= MaxTaxIDLength && !strings.Contains(s.Issuer.TaxID, "\n"),
			"the tax number must be one line of at most 50 characters"),
		on("email", optionalEmail(s.Issuer.Email), "invalid email address"),
		on("phone", trimmed(s.Issuer.Phone) && runes(s.Issuer.Phone) <= MaxIssuerPhoneLength && !strings.Contains(s.Issuer.Phone, "\n"),
			"the phone number must be one line of at most 50 characters"),
		on("taxMode", s.TaxMode == "" || s.TaxMode.Valid(), "invalid tax mode"),
		on("taxNote", trimmed(s.TaxNote) && runes(s.TaxNote) <= MaxTaxNoteLength, "the tax note is too long"),
		on("footer", trimmed(s.Footer) && runes(s.Footer) <= MaxReceiptFooterLength, "the footer is too long"),
	)
}

// Complete reports, as an error wrapping ErrValidation that names the first
// missing field, whether receipts can be issued with the settings: they need
// the issuer's name, address and tax number, a tax mode, and in the exempt
// mode the note with its legal basis.
func (s ReceiptSettings) Complete() error {
	return validate(
		on("issuerName", !blank(s.Issuer.Name), "the issuer's name is missing"),
		on("issuerAddress", !blank(s.Issuer.Address), "the issuer's address is missing"),
		on("taxNumber", !blank(s.Issuer.TaxID), "the tax number or VAT ID is missing"),
		on("taxMode", s.TaxMode.Valid(), "the tax mode is missing"),
		on("taxNote", s.TaxMode != TaxExempt || !blank(s.TaxNote), "the exempt tax mode needs a note with its legal basis"),
	)
}

// Receipt is an issued receipt with its lines. Sequence is gapless per Year;
// Number is the printed form, e.g. "SD-2026-0001". Amounts are in minor
// units of Currency; a cancellation negates every amount of the receipt it
// cancels.
type Receipt struct {
	ID               ReceiptID
	Number           string
	Year             int
	Sequence         int
	Kind             ReceiptKind
	CancelsReceiptID ReceiptID // set exactly for cancellations
	IssuedAt         time.Time
	// IssueDate is the school date of IssuedAt; Year is its year.
	IssueDate Date
	// Locale is the language the receipt is printed in: the school's
	// default language at issuance.
	Locale string
	// Issuer, TaxMode, TaxNote and Footer are the school's receipt
	// settings at issuance; a cancellation keeps the tax mode of the
	// receipt it cancels.
	Issuer  ReceiptIssuer
	TaxMode TaxMode
	TaxNote string // optional
	Footer  string // optional
	// PayerID is the person who paid (optional: the person may be deleted
	// later only if nothing refers to them, so it stays set in practice).
	PayerID          PersonID
	RecipientName    string
	RecipientAddress string // optional
	RecipientEmail   string // optional: where the receipt was mailed if the payer has no account
	PaymentMethod    PaymentMethod
	Currency         string
	// Net, VAT and Gross are the sums of the lines'.
	Net   Money
	VAT   Money
	Gross Money
	// Reason is why a cancellation was issued (optional, cancellations
	// only).
	Reason    string
	CreatedBy AccountID // optional: the office account that issued it
	Lines     []ReceiptLine
}

// ReceiptLine is one position of a receipt. Position counts from 1.
type ReceiptLine struct {
	Position    int
	Description string
	Quantity    int
	// UnitPrice is the gross price of one unit.
	UnitPrice Money
	// VATRate is the rate charged, in basis points; zero unless the tax
	// mode is standard.
	VATRate int
	// Gross is Quantity × UnitPrice, Net + VAT.
	Net   Money
	VAT   Money
	Gross Money
}

// ReceiptNumber returns the printed number of a receipt, e.g.
// "SD-2026-0001".
func ReceiptNumber(prefix string, year, sequence int) string {
	return fmt.Sprintf("%s-%04d-%04d", prefix, year, sequence)
}

// NewReceiptLine returns a line of quantity units at the gross unit price,
// taxed in the tax mode: in the standard mode the gross amount includes VAT at
// vatRate, and net is gross × 10000 / (10000 + vatRate) rounded to the
// nearest minor unit (half away from zero), VAT the rest; in the other modes
// no VAT is charged and the rate is zero. Rounding per line keeps each line
// exact in itself, and a negated line (a cancellation) rounds to the negated
// amounts.
func NewReceiptLine(position int, description string, quantity int, unitPrice Money, vatRate int, mode TaxMode) ReceiptLine {
	if mode != TaxStandard {
		vatRate = 0
	}

	gross := Money(quantity) * unitPrice
	net := grossToNet(gross, vatRate)

	return ReceiptLine{
		Position: position, Description: description, Quantity: quantity, UnitPrice: unitPrice,
		VATRate: vatRate, Net: net, VAT: gross - net, Gross: gross,
	}
}

// grossToNet returns gross / (1 + rate/10000) rounded half away from zero.
// big.Int keeps the intermediate product of large amounts exact.
func grossToNet(gross Money, rate int) Money {
	if rate == 0 {
		return gross
	}

	num := new(big.Int).Mul(big.NewInt(int64(gross)), big.NewInt(10_000))
	den := big.NewInt(int64(10_000 + rate))

	neg := num.Sign() < 0
	num.Abs(num)

	// (2·num + den) / (2·den) rounds half up for non-negative values.
	num.Mul(num, big.NewInt(2)).Add(num, den)
	q := num.Quo(num, den.Mul(den, big.NewInt(2)))

	if neg {
		q.Neg(q)
	}

	return Money(q.Int64())
}

// Negated returns the line with every amount negated, as a cancellation
// prints it.
func (l ReceiptLine) Negated() ReceiptLine {
	l.UnitPrice, l.Net, l.VAT, l.Gross = -l.UnitPrice, -l.Net, -l.VAT, -l.Gross

	return l
}

// Validate checks the receipt and its lines for consistency. All errors wrap
// ErrValidation.
func (r Receipt) Validate() error {
	cancellation := r.Kind == ReceiptCancellation

	err := validate(
		rule{!r.ID.IsZero(), "receipt id must be set"},
		rule{!blank(r.Number), "receipt number must not be empty"},
		rule{r.Year > 0, "receipt year must be positive"},
		rule{r.Sequence > 0, "receipt sequence must be positive"},
		rule{r.Kind.Valid(), "invalid receipt kind"},
		rule{cancellation != r.CancelsReceiptID.IsZero(), "exactly cancellation receipts reference the receipt they cancel"},
		rule{r.CancelsReceiptID != r.ID, "a receipt cannot cancel itself"},
		rule{!r.IssuedAt.IsZero(), "receipt issue time must be set"},
		rule{r.IssueDate.Valid() && r.IssueDate.Year == r.Year, "the issue date must lie in the receipt's year"},
		rule{!blank(r.Locale), "receipt language must be set"},
		rule{r.TaxMode.Valid(), "invalid tax mode"},
		on("paymentMethod", r.PaymentMethod.Valid(), "invalid payment method"),
		rule{validCurrency(r.Currency), "currency must be an ISO 4217 code"},
		on("recipientName", !blank(r.RecipientName) && trimmed(r.RecipientName) && runes(r.RecipientName) <= MaxRecipientLength,
			"the recipient's name must be 1 to 200 characters"),
		on("recipientAddress", trimmed(r.RecipientAddress) && runes(r.RecipientAddress) <= MaxIssuerAddressLength,
			"the recipient's address is too long"),
		on("recipientEmail", optionalEmail(r.RecipientEmail), "invalid email address"),
		on("reason", trimmed(r.Reason) && runes(r.Reason) <= MaxReceiptReasonLength, "the reason is too long"),
		rule{cancellation || r.Reason == "", "only a cancellation has a reason"},
		rule{len(r.Lines) > 0, "receipt needs at least one line"},
	)
	if err != nil {
		return err
	}

	issuer := ReceiptSettings{Issuer: r.Issuer, TaxMode: r.TaxMode, TaxNote: r.TaxNote, Footer: r.Footer}
	if err := issuer.validate(); err != nil {
		return err
	}

	if err := issuer.Complete(); err != nil {
		return err
	}

	var net, vat, gross Money

	for i, l := range r.Lines {
		if err := l.validate(i+1, r.TaxMode, cancellation); err != nil {
			return err
		}

		// |l.Gross| <= maxLineTotal, so the sums cannot overflow before the
		// bound check catches them.
		net, vat, gross = net+l.Net, vat+l.VAT, gross+l.Gross
		if gross > maxLineTotal || gross < -maxLineTotal {
			return invalidf("receipt total out of range")
		}
	}

	return validate(
		rule{gross == r.Gross, fmt.Sprintf("receipt total %d does not match the line totals %d", r.Gross, gross)},
		rule{net == r.Net && vat == r.VAT, "receipt net and VAT must be the sums of the lines'"},
	)
}

// Bounds that keep receipt arithmetic far away from int64 overflow:
// maxQuantity × maxUnitPrice = maxLineTotal.
const (
	maxQuantity        = 10_000
	maxUnitPrice Money = 1_000_000_000_000
	maxLineTotal       = maxQuantity * maxUnitPrice
)

func (l ReceiptLine) validate(position int, mode TaxMode, cancellation bool) error {
	err := validate(
		rule{l.Position == position, "receipt line positions must count from 1"},
		rule{!blank(l.Description), "receipt line description must not be empty"},
		rule{l.Quantity > 0 && l.Quantity <= maxQuantity, "receipt line quantity out of range"},
		rule{l.UnitPrice >= -maxUnitPrice && l.UnitPrice <= maxUnitPrice, "receipt line unit price out of range"},
		rule{l.VATRate >= 0 && l.VATRate <= MaxVATRate, "receipt line VAT rate out of range"},
		rule{cancellation || l.UnitPrice >= 0, "a sale line must not be negative"},
		rule{!cancellation || l.UnitPrice <= 0, "a cancellation line must not be positive"},
	)
	if err != nil {
		return err
	}

	if l != NewReceiptLine(l.Position, l.Description, l.Quantity, l.UnitPrice, l.VATRate, mode) {
		return invalidf("receipt line %d does not follow the tax computation", position)
	}

	return nil
}

// Logo is the school's logo printed on receipts: a PNG or JPEG image of at
// most MaxLogoBytes.
type Logo struct {
	ContentType string
	Data        []byte
}

// MaxLogoBytes is the largest logo, 512 KiB.
const MaxLogoBytes = 512 << 10

// Logo content types.
const (
	LogoPNG  = "image/png"
	LogoJPEG = "image/jpeg"
)

// IsZero reports whether there is no logo.
func (l Logo) IsZero() bool { return len(l.Data) == 0 }

// Validate checks that the logo is a PNG or JPEG image within the size
// limit whose bytes match its type. All errors wrap ErrValidation.
func (l Logo) Validate() error {
	var magic []byte

	switch l.ContentType {
	case LogoPNG:
		magic = []byte("\x89PNG\r\n\x1a\n")
	case LogoJPEG:
		magic = []byte{0xFF, 0xD8, 0xFF}
	default:
		return &FieldError{Field: "contentType", Msg: "the logo must be a PNG or JPEG image"}
	}

	return validate(
		on("data", len(l.Data) > 0 && len(l.Data) <= MaxLogoBytes, "the logo must be at most 512 KiB"),
		on("data", bytes.HasPrefix(l.Data, magic), "the logo's data does not match its type"),
	)
}

// trimmed reports whether s does not start or end with white space.
func trimmed(s string) bool { return s == strings.TrimSpace(s) }

func runes(s string) int { return utf8.RuneCountInString(s) }
