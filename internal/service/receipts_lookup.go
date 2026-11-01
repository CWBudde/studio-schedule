package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The reads of the command line (ADR 0020): a receipt by its number, the
// receipts of a range of dates and those of a year, without the page limit
// of List.

// receiptsChunk is how many receipts allReceipts reads at once.
const receiptsChunk = 500

// errReceiptRange refuses a range whose start lies after its end.
var errReceiptRange error = &domain.FieldError{Field: "to", Msg: "the end lies before the start"}

// WithRenderer returns a copy of s that renders PDFs with r, for a caller
// that starts the renderer only when a receipt needs one (the command line:
// a receipt issued without a PDF, the seed's, is rendered on first read).
func (s *ReceiptService) WithRenderer(r ReceiptRenderer) *ReceiptService {
	c := *s
	c.renderer = r

	return &c
}

// ByNumber returns the receipt with the printed number, e.g. SD-2026-0001,
// ignoring surrounding spaces and ASCII case; an unknown number is
// domain.ErrNotFound.
func (s *ReceiptService) ByNumber(ctx context.Context, number string) (ReceiptView, error) {
	number = strings.ToUpper(strings.TrimSpace(number))

	var out ReceiptView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		r, err := tx.Receipts().GetByNumber(ctx, number)
		if err != nil {
			return err
		}

		out, err = s.view(ctx, tx, r)

		return err
	})
	if err != nil {
		return ReceiptView{}, fmt.Errorf("receipt %s: %w", number, err)
	}

	return out, nil
}

// Issued returns the receipts issued on the school dates from to to, both
// inclusive, newest first, with Total their number and Years every year
// with receipts. A zero to is the school's today, a zero from the first day
// of to's year; from after to is a validation error on "to".
func (s *ReceiptService) Issued(ctx context.Context, from, to domain.Date) (ReceiptPage, error) {
	var out ReceiptPage

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		if to.IsZero() {
			to = domain.DateOf(s.clock.Now(), loc)
		}

		from = cmp.Or(from, domain.Date{Year: to.Year, Month: 1, Day: 1})
		if from.Compare(to) > 0 {
			return errReceiptRange
		}

		if out.Years, err = tx.ReceiptCounters().Years(ctx); err != nil {
			return err
		}

		var receipts []domain.Receipt

		for _, year := range out.Years { // the latest first, like List
			if year < from.Year || year > to.Year {
				continue
			}

			all, err := allReceipts(ctx, tx, year)
			if err != nil {
				return err
			}

			for _, r := range all {
				if r.IssueDate.Compare(from) >= 0 && r.IssueDate.Compare(to) <= 0 {
					receipts = append(receipts, r)
				}
			}
		}

		out.Total = len(receipts)
		out.Items, err = s.views(ctx, tx, receipts)

		return err
	})
	if err != nil {
		return ReceiptPage{}, fmt.Errorf("receipts from %v to %v: %w", from, to, err)
	}

	return out, nil
}

// OfYear returns every receipt of a year in the order of their numbers,
// each with the receipt it cancels. A year outside 1 to 9999 is a
// validation error on "year".
func (s *ReceiptService) OfYear(ctx context.Context, year int) ([]ReceiptView, error) {
	if year < 1 || year > 9999 {
		return nil, &domain.FieldError{Field: "year", Msg: "the year must lie between 1 and 9999"}
	}

	var out []ReceiptView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		receipts, err := allReceipts(ctx, tx, year)
		if err != nil {
			return err
		}

		slices.Reverse(receipts)

		out, err = s.views(ctx, tx, receipts)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("receipts of %d: %w", year, err)
	}

	return out, nil
}

// allReceipts returns every receipt of a year (zero: of all years), newest
// first like ReceiptRepo.List, read in chunks.
func allReceipts(ctx context.Context, tx store.Tx, year int) ([]domain.Receipt, error) {
	var out []domain.Receipt

	for {
		page, total, err := tx.Receipts().List(ctx, store.ReceiptFilter{
			Year: year, Offset: len(out), Limit: receiptsChunk,
		})
		if err != nil {
			return nil, err
		}

		out = append(out, page...)
		if len(page) == 0 || len(out) >= total {
			return out, nil
		}
	}
}
