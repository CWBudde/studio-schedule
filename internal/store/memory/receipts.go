package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// receiptPDF is the stored PDF of a receipt (a row of receipt_pdfs).
type receiptPDF struct {
	ReceiptID domain.ReceiptID
	PDF       []byte
}

// receiptCounter is the last sequence number issued in a year.
type receiptCounter struct {
	Year int
	Last int
}

type receiptRepo struct{ tx *tx }

// clone copies the lines, so neither the caller nor the store can change the
// other's receipt.
func clone(r domain.Receipt) domain.Receipt {
	r.Lines = slices.Clone(r.Lines)

	return r
}

func (r receiptRepo) Create(_ context.Context, rc domain.Receipt) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case s.receipts.has(rc.ID):
		return conflict("receipt %s exists", rc.ID)
	case !rc.CancelsReceiptID.IsZero() && !s.receipts.has(rc.CancelsReceiptID),
		!rc.PayerID.IsZero() && !s.persons.has(rc.PayerID),
		!rc.CreatedBy.IsZero() && !s.accounts.has(rc.CreatedBy):
		return conflict("receipt references a missing record")
	case s.receipts.exists(func(o domain.Receipt) bool {
		return o.Number == rc.Number || (o.Year == rc.Year && o.Sequence == rc.Sequence)
	}):
		return fmt.Errorf("the receipt number is taken: %w", domain.ErrDuplicate)
	case !rc.CancelsReceiptID.IsZero() &&
		s.receipts.exists(func(o domain.Receipt) bool { return o.CancelsReceiptID == rc.CancelsReceiptID }):
		return fmt.Errorf("the receipt is canceled already: %w", domain.ErrDuplicate)
	}

	rc = clone(rc)
	rc.IssuedAt = instant(rc.IssuedAt)
	s.receipts.put(r.tx.gen, rc.ID, rc)

	return nil
}

func (r receiptRepo) Get(_ context.Context, id domain.ReceiptID) (domain.Receipt, error) {
	rc, ok := r.tx.state.receipts.get(id)
	if !ok {
		return domain.Receipt{}, notFound("receipt")
	}

	return clone(rc), nil
}

func (r receiptRepo) ListByIDs(_ context.Context, ids []domain.ReceiptID) ([]domain.Receipt, error) {
	out := sorted(byIDs(r.tx.state.receipts.get, ids), func(a, b domain.Receipt) int { return compareID(a.ID, b.ID) })
	for i := range out {
		out[i] = clone(out[i])
	}

	return out, nil
}

func (r receiptRepo) first(pred func(domain.Receipt) bool) (domain.Receipt, error) {
	found := r.tx.state.receipts.filter(pred)
	if len(found) == 0 {
		return domain.Receipt{}, notFound("receipt")
	}

	return clone(found[0]), nil
}

func (r receiptRepo) GetByNumber(_ context.Context, number string) (domain.Receipt, error) {
	return r.first(func(rc domain.Receipt) bool { return rc.Number == number })
}

func (r receiptRepo) GetCancellation(_ context.Context, id domain.ReceiptID) (domain.Receipt, error) {
	if id.IsZero() {
		return domain.Receipt{}, notFound("receipt")
	}

	return r.first(func(rc domain.Receipt) bool { return rc.CancelsReceiptID == id })
}

// newestFirst orders receipts like ReceiptRepo.List.
func newestFirst(a, b domain.Receipt) int {
	return cmp.Or(cmp.Compare(b.Year, a.Year), cmp.Compare(b.Sequence, a.Sequence), compareID(a.ID, b.ID))
}

func (r receiptRepo) list(pred func(domain.Receipt) bool) []domain.Receipt {
	out := sorted(r.tx.state.receipts.filter(pred), newestFirst)
	for i := range out {
		out[i] = clone(out[i])
	}

	return out
}

func (r receiptRepo) List(_ context.Context, f store.ReceiptFilter) ([]domain.Receipt, int, error) {
	q := foldASCII(f.Query)
	all := r.list(func(rc domain.Receipt) bool {
		return (f.Year <= 0 || rc.Year == f.Year) &&
			(q == "" || strings.Contains(foldASCII(rc.Number), q) || strings.Contains(foldASCII(rc.RecipientName), q))
	})

	from := min(max(f.Offset, 0), len(all))
	to := min(from+max(f.Limit, 0), len(all))

	return all[from:to], len(all), nil
}

func (r receiptRepo) ListByPayers(_ context.Context, persons []domain.PersonID) ([]domain.Receipt, error) {
	set := setOf(persons)

	return r.list(func(rc domain.Receipt) bool { _, ok := set[rc.PayerID]; return ok && !rc.PayerID.IsZero() }), nil
}

func (r receiptRepo) ListCancellations(_ context.Context, ids []domain.ReceiptID) ([]domain.Receipt, error) {
	set := setOf(ids)
	out := r.list(func(rc domain.Receipt) bool {
		_, ok := set[rc.CancelsReceiptID]

		return ok && !rc.CancelsReceiptID.IsZero()
	})
	slices.SortFunc(out, func(a, b domain.Receipt) int { return compareID(a.CancelsReceiptID, b.CancelsReceiptID) })

	return out, nil
}

func (r receiptRepo) PutPDF(_ context.Context, id domain.ReceiptID, pdf []byte) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case len(pdf) == 0:
		return fmt.Errorf("%w: empty PDF", domain.ErrValidation)
	case !s.receipts.has(id):
		return conflict("receipt %s does not exist", id)
	case s.receiptPDFs.has(id):
		return fmt.Errorf("the receipt has its PDF already: %w", domain.ErrDuplicate)
	}

	s.receiptPDFs.put(r.tx.gen, id, receiptPDF{ReceiptID: id, PDF: slices.Clone(pdf)})

	return nil
}

func (r receiptRepo) PDF(_ context.Context, id domain.ReceiptID) ([]byte, error) {
	p, ok := r.tx.state.receiptPDFs.get(id)
	if !ok {
		return nil, notFound("receipt PDF")
	}

	return slices.Clone(p.PDF), nil
}

type receiptCounterRepo struct{ tx *tx }

// Next needs no lock of its own: the store mutex serialises every
// transaction, and a rolled-back transaction drops its copy of the counter.
func (r receiptCounterRepo) Next(_ context.Context, year int) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	c, _ := r.tx.state.receiptCounters.get(year)
	c.Year = year
	c.Last++
	r.tx.state.receiptCounters.put(r.tx.gen, year, c)

	return c.Last, nil
}

func (r receiptCounterRepo) Years(context.Context) ([]int, error) {
	years := make([]int, 0, len(r.tx.state.receiptCounters.rows))
	for y := range r.tx.state.receiptCounters.rows {
		years = append(years, y)
	}

	slices.SortFunc(years, func(a, b int) int { return cmp.Compare(b, a) })

	return years, nil
}
