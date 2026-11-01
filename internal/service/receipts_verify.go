package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// ReceiptCheck names a rule of the receipts that Verify checks.
type ReceiptCheck string

// The checks of Verify. The stores themselves refuse a second receipt with
// the same number, or with the same year and sequence, a second
// cancellation of a receipt and a cancellation of a receipt that does not
// exist (unique and foreign-key constraints in PostgreSQL, the same refusals
// in the memory store), so Verify does not look for duplicates.
const (
	// CheckReceiptSequence: the sequence numbers of a year have a gap.
	CheckReceiptSequence ReceiptCheck = "sequence"
	// CheckReceiptNumber: the printed number does not end in the year and
	// sequence of the receipt.
	CheckReceiptNumber ReceiptCheck = "number"
	// CheckReceiptCancellation: a cancellation (Storno) cancels no sale
	// receipt, or its amounts do not negate the original's.
	CheckReceiptCancellation ReceiptCheck = "cancellation"
	// CheckReceiptTotals: the totals of a receipt are not the sums of its
	// lines.
	CheckReceiptTotals ReceiptCheck = "totals"
)

// ReceiptFinding is something Verify found wrong.
type ReceiptFinding struct {
	Year int
	// Number is the receipt the finding is about; empty for missing
	// numbers.
	Number  string
	Check   ReceiptCheck
	Message string
}

// ReceiptVerification is the result of Verify.
type ReceiptVerification struct {
	// Years are the years checked, in order; Receipts counts the receipts
	// checked.
	Years    []int
	Receipts int
	// Findings are in the order of the years and numbers; none when the
	// receipts are in order.
	Findings []ReceiptFinding
}

// Verify checks the receipts of a year (zero: of every year): the sequence
// numbers of each year run from 1 without a gap, every printed number ends
// in its year and sequence, every cancellation cancels a sale receipt
// (which may be of an earlier year) and negates its amounts, and the totals
// of every receipt are the sums of its lines. It changes nothing. A year
// outside 0 to 9999 is a validation error on "year".
func (s *ReceiptService) Verify(ctx context.Context, year int) (ReceiptVerification, error) {
	if year < 0 || year > 9999 {
		return ReceiptVerification{}, &domain.FieldError{Field: "year", Msg: "the year must lie between 1 and 9999, or be 0 for every year"}
	}

	var out ReceiptVerification

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		receipts, err := allReceipts(ctx, tx, year)
		if err != nil {
			return err
		}

		slices.Reverse(receipts) // by year and sequence

		originals, err := receiptOriginals(ctx, tx, receipts)
		if err != nil {
			return err
		}

		out = verifyReceipts(receipts, originals)

		return nil
	})
	if err != nil {
		return ReceiptVerification{}, fmt.Errorf("verify receipts: %w", err)
	}

	return out, nil
}

// receiptOriginals returns the receipts by ID with the receipts their
// cancellations cancel, read where they lie in another year.
func receiptOriginals(
	ctx context.Context, tx store.Tx, receipts []domain.Receipt,
) (map[domain.ReceiptID]domain.Receipt, error) {
	out := make(map[domain.ReceiptID]domain.Receipt, len(receipts))
	for _, r := range receipts {
		out[r.ID] = r
	}

	var missing []domain.ReceiptID

	for _, r := range receipts {
		if _, ok := out[r.CancelsReceiptID]; !ok && !r.CancelsReceiptID.IsZero() {
			missing = append(missing, r.CancelsReceiptID)
		}
	}

	others, err := tx.Receipts().ListByIDs(ctx, missing)
	if err != nil {
		return nil, fmt.Errorf("canceled receipts: %w", err)
	}

	for _, o := range others {
		out[o.ID] = o
	}

	return out, nil
}

// verifyReceipts checks receipts, ordered by year and sequence, with
// originals holding the receipts their cancellations cancel.
func verifyReceipts(receipts []domain.Receipt, originals map[domain.ReceiptID]domain.Receipt) ReceiptVerification {
	out := ReceiptVerification{Years: []int{}, Receipts: len(receipts), Findings: []ReceiptFinding{}}
	next := 0 // the sequence number expected next in the year

	for i, r := range receipts {
		if i == 0 || r.Year != receipts[i-1].Year {
			out.Years, next = append(out.Years, r.Year), 1
		}

		if r.Sequence > next {
			out.Findings = append(out.Findings, missingNumbers(r.Year, next, r.Sequence-1))
		}

		next = r.Sequence + 1

		out.Findings = append(out.Findings, verifyReceipt(r, originals)...)
	}

	return out
}

// missingNumbers is the finding of the sequence numbers from to to missing
// in year.
func missingNumbers(year, from, to int) ReceiptFinding {
	msg := fmt.Sprintf("the number with sequence %d is missing", from)
	if to > from {
		msg = fmt.Sprintf("the numbers with sequences %d to %d are missing", from, to)
	}

	return ReceiptFinding{Year: year, Check: CheckReceiptSequence, Message: msg}
}

// verifyReceipt checks the number, the cancellation and the totals of r.
func verifyReceipt(r domain.Receipt, originals map[domain.ReceiptID]domain.Receipt) []ReceiptFinding {
	var out []ReceiptFinding

	finding := func(check ReceiptCheck, format string, args ...any) {
		out = append(out, ReceiptFinding{
			Year: r.Year, Number: r.Number, Check: check, Message: fmt.Sprintf(format, args...),
		})
	}

	if !strings.HasSuffix(r.Number, fmt.Sprintf("-%04d-%04d", r.Year, r.Sequence)) {
		finding(CheckReceiptNumber, "the number does not end in its year %d and sequence %d", r.Year, r.Sequence)
	}

	if r.Kind == domain.ReceiptCancellation {
		// A missing original is refused by the stores; the memory store
		// lets a cancellation name none.
		o, ok := originals[r.CancelsReceiptID]

		switch {
		case !ok || o.Kind != domain.ReceiptSale:
			finding(CheckReceiptCancellation, "the cancellation cancels no sale receipt")
		case o.Net != -r.Net || o.VAT != -r.VAT || o.Gross != -r.Gross || o.Currency != r.Currency:
			finding(CheckReceiptCancellation,
				"the cancellation does not negate %s: net, VAT and gross %d, %d, %d %s against %d, %d, %d %s",
				o.Number, r.Net, r.VAT, r.Gross, r.Currency, o.Net, o.VAT, o.Gross, o.Currency)
		}
	}

	var net, vat, gross domain.Money

	for _, l := range r.Lines {
		net, vat, gross = net+l.Net, vat+l.VAT, gross+l.Gross
	}

	if net != r.Net || vat != r.VAT || gross != r.Gross {
		finding(CheckReceiptTotals, "net, VAT and gross are %d, %d, %d; the lines sum to %d, %d, %d",
			r.Net, r.VAT, r.Gross, net, vat, gross)
	}

	return out
}
