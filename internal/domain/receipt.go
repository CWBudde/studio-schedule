package domain

import (
	"slices"
	"time"
)

// ReceiptKind tells a sale from the cancellation of an earlier receipt.
// Receipts are immutable; corrections are cancellation receipts.
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

// Receipt is an issued receipt with its lines. Sequence is gapless per Year;
// Number is the printed form, e.g. "SD-2026-0001".
type Receipt struct {
	ID               ReceiptID
	Number           string
	Year             int
	Sequence         int
	Kind             ReceiptKind
	CancelsReceiptID ReceiptID // set exactly for cancellations
	IssuedAt         time.Time
	PersonID         PersonID // optional
	RecipientName    string
	Total            Money // sum of the line totals
	Lines            []ReceiptLine
}

// ReceiptLine is one position of a receipt. Position counts from 1.
type ReceiptLine struct {
	Position    int
	Description string
	Quantity    int
	UnitPrice   Money
	Total       Money // Quantity × UnitPrice
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
		rule{!blank(r.RecipientName), "receipt recipient must not be empty"},
		rule{len(r.Lines) > 0, "receipt needs at least one line"},
		rule{cancellation || r.Total >= 0, "sale total must not be negative"},
		rule{!cancellation || r.Total <= 0, "cancellation total must not be positive"},
	)
	if err != nil {
		return err
	}

	var sum Money

	for i, l := range r.Lines {
		if err := l.validate(i + 1); err != nil {
			return err
		}

		// |l.Total| <= maxLineTotal, so the sum cannot overflow before the
		// bound check catches it.
		sum += l.Total
		if sum > maxLineTotal || sum < -maxLineTotal {
			return invalidf("receipt total out of range")
		}
	}

	if sum != r.Total {
		return invalidf("receipt total %d does not match the line totals %d", r.Total, sum)
	}

	return nil
}

// Bounds that keep receipt arithmetic far away from int64 overflow:
// maxQuantity × maxUnitPrice = maxLineTotal.
const (
	maxQuantity        = 10_000
	maxUnitPrice Money = 1_000_000_000_000
	maxLineTotal       = maxQuantity * maxUnitPrice
)

func (l ReceiptLine) validate(position int) error {
	return validate(
		rule{l.Position == position, "receipt line positions must count from 1"},
		rule{!blank(l.Description), "receipt line description must not be empty"},
		rule{l.Quantity > 0 && l.Quantity <= maxQuantity, "receipt line quantity out of range"},
		rule{l.UnitPrice >= -maxUnitPrice && l.UnitPrice <= maxUnitPrice, "receipt line unit price out of range"},
		rule{l.Total == Money(l.Quantity)*l.UnitPrice, "receipt line total must be quantity × unit price"},
	)
}
