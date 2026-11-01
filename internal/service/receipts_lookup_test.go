package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// lookupReceipt stores a receipt of Anna's issued on the school date day
// without a PDF, as the seed does, with the next number of day's year, and
// returns it.
func (f *passFixture) lookupReceipt(t *testing.T, day string) domain.Receipt {
	t.Helper()

	return f.lookupReceipts(t, day, 1)[0]
}

// lookupReceipts stores n receipts like lookupReceipt in one transaction.
func (f *passFixture) lookupReceipts(t *testing.T, day string, n int) []domain.Receipt {
	t.Helper()

	date := mustDate(t, day)
	line := domain.NewReceiptLine(1, "Single visit for Anna Example", 1, 1500, 0, domain.TaxStandard)
	out := make([]domain.Receipt, 0, n)

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		for range n {
			seq, err := tx.ReceiptCounters().Next(ctx, date.Year)
			if err != nil {
				return err
			}

			r := domain.Receipt{
				ID: ids.New[domain.ReceiptID](f.seq), Number: domain.ReceiptNumber("SD", date.Year, seq), Year: date.Year,
				Sequence: seq, Kind: domain.ReceiptSale, IssuedAt: date.At(domain.TimeOfDay{Hour: 10}, time.UTC),
				IssueDate: date, Locale: "en",
				Issuer:  domain.ReceiptIssuer{Name: "Studio Demo Dance School", Address: "Pirouette Lane 7", TaxID: "99"},
				TaxMode: domain.TaxStandard, PayerID: f.anna.ID, RecipientName: "Anna Example",
				PaymentMethod: domain.PaymentCash, Currency: "EUR", Net: line.Net, VAT: line.VAT, Gross: line.Gross,
				Lines: []domain.ReceiptLine{line},
			}
			if err := tx.Receipts().Create(ctx, r); err != nil {
				return err
			}

			out = append(out, r)
		}

		return nil
	})

	return out
}

// newLookupFixture issues on bookingNow (2026-10-21) Anna's sale
// SD-2026-0001, Mia's SD-2026-0002 and its Storno SD-2026-0003, and stores
// SD-2026-0004 of 2026-03-02, SD-2026-0005 of 2026-11-05 (after
// bookingNow) and SD-2025-0001 of 2025-12-30.
func newLookupFixture(t *testing.T) *passFixture {
	t.Helper()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	f.sell(t, f.anna, f.single, "")
	mia := f.sell(t, f.mia, f.single, "")

	if _, err := f.receiptService(t, bookingNow).Cancel(t.Context(), f.office, mia.ReceiptID, ""); err != nil {
		t.Fatal(err)
	}

	f.lookupReceipt(t, "2026-03-02")
	f.lookupReceipt(t, "2026-11-05")
	f.lookupReceipt(t, "2025-12-30")

	return f
}

// lookupNumbers returns the numbers of the receipts of views, in order.
func lookupNumbers(views []ReceiptView) []string {
	out := []string{}
	for _, v := range views {
		out = append(out, v.Receipt.Number)
	}

	return out
}

// TestReceiptByNumber finds a receipt by its number, ignoring surrounding
// spaces and case, with the receipt that canceled it; an unknown number is
// not found and named in the error.
func TestReceiptByNumber(t *testing.T) {
	t.Parallel()

	f := newLookupFixture(t)
	rs := f.receiptService(t, bookingNow)

	v, err := rs.ByNumber(t.Context(), " sd-2026-0002\n")
	if err != nil {
		t.Fatal(err)
	}

	if v.Receipt.Number != "SD-2026-0002" || v.Receipt.PayerID != f.mia.ID || v.CanceledBy == nil ||
		v.CanceledBy.Number != "SD-2026-0003" || v.Pass.ID.IsZero() {
		t.Errorf("ByNumber = %+v, want Mia's sale with its Storno and pass", v)
	}

	_, err = rs.ByNumber(t.Context(), "SD-2026-0099")
	if !errors.Is(err, domain.ErrNotFound) || !strings.Contains(err.Error(), "SD-2026-0099") {
		t.Errorf("ByNumber of an unknown number = %v, want not found naming it", err)
	}
}

// TestReceiptsIssued selects the receipts by issue date: by default from
// January 1 to today in the school's time zone, else the range, both ends
// inclusive, across years, newest first; a range that ends before it
// starts is a field error on "to".
func TestReceiptsIssued(t *testing.T) {
	t.Parallel()

	f := newLookupFixture(t)
	rs := f.receiptService(t, bookingNow)

	for _, c := range []struct {
		from, to string
		want     []string
	}{
		{"", "", []string{"SD-2026-0004", "SD-2026-0003", "SD-2026-0002", "SD-2026-0001"}},
		{"2025-12-01", "2026-03-31", []string{"SD-2026-0004", "SD-2025-0001"}},
		{"2026-10-21", "2026-10-21", []string{"SD-2026-0003", "SD-2026-0002", "SD-2026-0001"}},
		{"2026-03-03", "2026-10-20", []string{}},
		{"", "2025-12-31", []string{"SD-2025-0001"}},
		{"2026-10-22", "2026-12-31", []string{"SD-2026-0005"}},
		{"2026-10-21", "", []string{"SD-2026-0003", "SD-2026-0002", "SD-2026-0001"}},
		{"2024-01-01", "2024-12-31", []string{}},
	} {
		var from, to domain.Date
		if c.from != "" {
			from = mustDate(t, c.from)
		}

		if c.to != "" {
			to = mustDate(t, c.to)
		}

		page, err := rs.Issued(t.Context(), from, to)
		if err != nil {
			t.Fatalf("Issued(%q, %q): %v", c.from, c.to, err)
		}

		if got := lookupNumbers(page.Items); !slices.Equal(got, c.want) || page.Total != len(c.want) ||
			!slices.Equal(page.Years, []int{2026, 2025}) {
			t.Errorf("Issued(%q, %q) = %v (total %d, years %v), want %v", c.from, c.to, got, page.Total, page.Years, c.want)
		}
	}

	// The Storno names what it cancels.
	page, err := rs.Issued(t.Context(), mustDate(t, "2026-10-21"), mustDate(t, "2026-10-21"))
	if err != nil || page.Items[0].Cancels == nil || page.Items[0].Cancels.Number != "SD-2026-0002" {
		t.Errorf("the Storno's view %+v (%v), want it to cancel SD-2026-0002", page.Items, err)
	}

	_, err = rs.Issued(t.Context(), mustDate(t, "2026-10-22"), mustDate(t, "2026-10-21"))

	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "to" || !errors.Is(err, domain.ErrValidation) {
		t.Errorf("Issued from after to = %v, want a field error on to", err)
	}

	// Without a range after a start after today: the default end is today.
	if _, err := rs.Issued(t.Context(), mustDate(t, "2026-10-22"), domain.Date{}); !errors.As(err, &fe) {
		t.Errorf("Issued from tomorrow = %v, want a field error on to", err)
	}
}

// TestReceiptsIssuedSchoolToday: the default range ends on the school's
// today, which may be another day than UTC's: at 23:30 UTC on December 31
// it is January 1 in Berlin, so the default range is that day alone.
func TestReceiptsIssuedSchoolToday(t *testing.T) {
	t.Parallel()

	f := newLookupFixture(t)
	f.lookupReceipt(t, "2027-01-01")

	page, err := f.receiptService(t, "2026-12-31T23:30:00Z").Issued(t.Context(), domain.Date{}, domain.Date{})
	if err != nil {
		t.Fatal(err)
	}

	if got := lookupNumbers(page.Items); !slices.Equal(got, []string{"SD-2027-0001"}) {
		t.Errorf("Issued by default on New Year's Eve, 23:30 UTC = %v, want SD-2027-0001", got)
	}
}

// TestReceiptsOfYear returns the receipts of a year in the order of their
// numbers, each Storno with what it cancels; a year outside 1 to 9999 is a
// field error on "year".
func TestReceiptsOfYear(t *testing.T) {
	t.Parallel()

	f := newLookupFixture(t)
	rs := f.receiptService(t, bookingNow)

	views, err := rs.OfYear(t.Context(), 2026)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"SD-2026-0001", "SD-2026-0002", "SD-2026-0003", "SD-2026-0004", "SD-2026-0005"}
	if got := lookupNumbers(views); !slices.Equal(got, want) {
		t.Errorf("OfYear(2026) = %v, want %v", got, want)
	}

	if c := views[2]; c.Cancels == nil || c.Cancels.Number != "SD-2026-0002" {
		t.Errorf("the Storno %+v, want it to cancel SD-2026-0002", c)
	}

	for year, want := range map[int][]string{2025: {"SD-2025-0001"}, 2024: {}, 1: {}, 9999: {}} {
		views, err := rs.OfYear(t.Context(), year)
		if got := lookupNumbers(views); err != nil || !slices.Equal(got, want) {
			t.Errorf("OfYear(%d) = %v, %v; want %v", year, got, err, want)
		}
	}

	for _, year := range []int{0, -1, 10000} {
		_, err := rs.OfYear(t.Context(), year)

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != "year" {
			t.Errorf("OfYear(%d) = %v, want a field error on year", year, err)
		}
	}
}

// TestReceiptsInChunks reads more receipts than one chunk: OfYear and
// Issued return them all, in three reads of the store for 1001.
func TestReceiptsInChunks(t *testing.T) {
	t.Parallel()

	const n = 2*receiptsChunk + 1

	f := newPassFixture(t, domain.RedeemOnAttendance)
	f.lookupReceipts(t, "2026-05-04", n)

	fs := NewFaultStore(f.st)
	rs := NewReceiptService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, Mails{}, nil)

	views, err := rs.OfYear(t.Context(), 2026)
	if err != nil {
		t.Fatal(err)
	}

	if len(views) != n || views[0].Receipt.Sequence != 1 || views[n-1].Receipt.Sequence != n {
		t.Fatalf("OfYear: %d receipts, want %d from 1 to %d", len(views), n, n)
	}

	if reads := fs.Count("Receipts.List"); reads != 3 {
		t.Errorf("OfYear read the receipts %d times, want 3", reads)
	}

	page, err := rs.Issued(t.Context(), domain.Date{}, domain.Date{})
	if err != nil {
		t.Fatal(err)
	}

	if page.Total != n || len(page.Items) != n || page.Items[0].Receipt.Sequence != n {
		t.Errorf("Issued: total %d, %d receipts, want %d, newest first", page.Total, len(page.Items), n)
	}
}

// TestReceiptLookupStoreFaults: the reads of the command line return a
// store failure.
func TestReceiptLookupStoreFaults(t *testing.T) {
	t.Parallel()

	for name, read := range map[string]func(t *testing.T, rs *ReceiptService) error{
		"ByNumber": func(t *testing.T, rs *ReceiptService) error {
			_, err := rs.ByNumber(t.Context(), "SD-2026-0002")

			return err
		},
		"Issued": func(t *testing.T, rs *ReceiptService) error {
			_, err := rs.Issued(t.Context(), mustDate(t, "2025-12-01"), domain.Date{})

			return err
		},
		"OfYear": func(t *testing.T, rs *ReceiptService) error {
			_, err := rs.OfYear(t.Context(), 2026)

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newLookupFixture(t)
				fs, _, rs := f.faulty(t)

				return fs, func() error { return read(t, rs) }
			}})
		})
	}
}
