package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// verifyTReceipt returns a fictional receipt of one line of gross, its
// totals the line's.
func verifyTReceipt(src ids.Source, year, seq int, kind domain.ReceiptKind, gross domain.Money) domain.Receipt {
	line := domain.NewReceiptLine(1, "Fictional pass", 1, gross, 1900, domain.TaxStandard)
	day := domain.Date{Year: year, Month: 12, Day: 1}

	return domain.Receipt{
		ID: ids.New[domain.ReceiptID](src), Number: domain.ReceiptNumber("TS", year, seq), Year: year, Sequence: seq,
		Kind: kind, IssuedAt: time.Date(year, 12, 1, 10, 0, 0, 0, time.UTC), IssueDate: day, Locale: "en",
		Issuer:  domain.ReceiptIssuer{Name: "Fictional Studio", Address: "1 Example Road", TaxID: "TEST-0000"},
		TaxMode: domain.TaxStandard, RecipientName: "Ida Example", PaymentMethod: domain.PaymentCash, Currency: "EUR",
		Net: line.Net, VAT: line.VAT, Gross: line.Gross, Lines: []domain.ReceiptLine{line},
	}
}

// verifyTCreate writes receipts into st directly.
func verifyTCreate(t *testing.T, st store.Store, receipts ...domain.Receipt) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for _, r := range receipts {
			if err := tx.Receipts().Create(ctx, r); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// verifyTService returns a receipt service on st for Verify, which reads
// the store alone.
func verifyTService(st store.Store) *ReceiptService {
	return NewReceiptService(st, clock.NewFixed(time.Date(2026, 12, 2, 8, 0, 0, 0, time.UTC)), ids.NewSequence(), Mails{}, nil)
}

// TestReceiptsVerifyCancellationOfNothing: the memory store lets a
// cancellation name no receipt it cancels (PostgreSQL's CHECK forbids
// it); Verify reports it as canceling no sale receipt.
func TestReceiptsVerifyCancellationOfNothing(t *testing.T) {
	t.Parallel()

	st, src := memory.New(), ids.NewSequence()
	verifyTCreate(t, st,
		verifyTReceipt(src, 2026, 1, domain.ReceiptSale, 1500),
		verifyTReceipt(src, 2026, 2, domain.ReceiptCancellation, -1500))

	got, err := verifyTService(st).Verify(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}

	want := []ReceiptFinding{{
		Year: 2026, Number: "TS-2026-0002", Check: CheckReceiptCancellation,
		Message: "the cancellation cancels no sale receipt",
	}}
	if got.Receipts != 2 || !slices.Equal(got.Years, []int{2026}) || !slices.Equal(got.Findings, want) {
		t.Errorf("Verify = %+v, want the finding %+v", got, want)
	}
}

// TestReceiptsVerifyYears: Verify refuses a year outside 0 to 9999 with a
// field error on "year", and on an empty store finds nothing.
func TestReceiptsVerifyYears(t *testing.T) {
	t.Parallel()

	svc := verifyTService(memory.New())

	for _, year := range []int{-1, 10000} {
		var fe *domain.FieldError
		if _, err := svc.Verify(t.Context(), year); !errors.As(err, &fe) || fe.Field != "year" {
			t.Errorf("Verify(%d) = %v, want a field error on year", year, err)
		}
	}

	got, err := svc.Verify(t.Context(), 0)
	if err != nil || got.Receipts != 0 || len(got.Years) != 0 || got.Findings == nil || len(got.Findings) != 0 {
		t.Errorf("Verify of nothing = %+v, %v; want no receipts, no years, empty findings", got, err)
	}
}

// TestReceiptsVerifyStoreFaults: a store failure anywhere in Verify (the
// receipts of the year, the originals of earlier years) is returned, for
// a storno of 2026 whose sale lies in 2025.
func TestReceiptsVerifyStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		st, src := memory.New(), ids.NewSequence()
		sale := verifyTReceipt(src, 2025, 1, domain.ReceiptSale, 1500)
		storno := verifyTReceipt(src, 2026, 1, domain.ReceiptCancellation, -1500)
		storno.CancelsReceiptID = sale.ID
		verifyTCreate(t, st, sale, storno)

		fs := NewFaultStore(st)

		return fs, func() error {
			got, err := verifyTService(fs).Verify(t.Context(), 2026)
			if err == nil && (got.Receipts != 1 || len(got.Findings) != 0) {
				t.Errorf("Verify(2026) = %+v, want one receipt without findings", got)
			}

			return err
		}
	}})
}
