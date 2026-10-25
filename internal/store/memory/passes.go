package memory

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ---------------------------------------------------------------- pass types

type passTypeRepo struct{ tx *tx }

func (r passTypeRepo) Get(_ context.Context, id domain.PassTypeID) (domain.PassType, error) {
	t, ok := r.tx.state.passTypes.get(id)
	if !ok {
		return domain.PassType{}, notFound("pass type")
	}

	return t, nil
}

func (r passTypeRepo) Create(_ context.Context, t domain.PassType) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.passTypes.has(t.ID) {
		return conflict("pass type %s exists", t.ID)
	}

	return r.put(t)
}

func (r passTypeRepo) Update(_ context.Context, t domain.PassType) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.passTypes.has(t.ID) {
		return notFound("pass type")
	}

	return r.put(t)
}

func (r passTypeRepo) put(t domain.PassType) error {
	s := &r.tx.state
	if s.passTypes.exists(func(o domain.PassType) bool { return o.ID != t.ID && sameName(o.Name, t.Name) }) {
		return fmt.Errorf("pass type name is taken: %w", domain.ErrDuplicate)
	}

	t.CreatedAt = instant(t.CreatedAt)
	s.passTypes.put(r.tx.gen, t.ID, t)

	return nil
}

func (r passTypeRepo) Delete(_ context.Context, id domain.PassTypeID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.passTypes.has(id):
		return notFound("pass type")
	case s.passes.exists(func(p domain.Pass) bool { return p.PassTypeID == id }):
		return conflict("pass type %s is referenced", id)
	}

	s.passTypes.delete(r.tx.gen, id)

	return nil
}

func (r passTypeRepo) List(context.Context) ([]domain.PassType, error) {
	return sorted(r.tx.state.passTypes.filter(all), func(a, b domain.PassType) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), compareID(a.ID, b.ID))
	}), nil
}

// ---------------------------------------------------------------- passes

type passRepo struct{ tx *tx }

func (r passRepo) Get(_ context.Context, id domain.PassID) (domain.Pass, error) {
	p, ok := r.tx.state.passes.get(id)
	if !ok {
		return domain.Pass{}, notFound("pass")
	}

	return p, nil
}

func (r passRepo) Create(_ context.Context, p domain.Pass) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.passes.has(p.ID) {
		return conflict("pass %s exists", p.ID)
	}

	return r.put(p)
}

func (r passRepo) Update(_ context.Context, p domain.Pass) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.passes.has(p.ID) {
		return notFound("pass")
	}

	return r.put(p)
}

func (r passRepo) put(p domain.Pass) error {
	s := &r.tx.state

	switch {
	case !s.passTypes.has(p.PassTypeID), !s.persons.has(p.PersonID),
		!p.PayerID.IsZero() && !s.persons.has(p.PayerID),
		!p.SoldBy.IsZero() && !s.accounts.has(p.SoldBy),
		!p.ReceiptID.IsZero() && !s.receipts.has(p.ReceiptID):
		return conflict("pass references a missing record")
	case !p.ReceiptID.IsZero() && s.passes.exists(func(o domain.Pass) bool { return o.ID != p.ID && o.ReceiptID == p.ReceiptID }):
		return conflict("another pass has the receipt")
	}

	p.SoldAt, p.VoidedAt = instant(p.SoldAt), instant(p.VoidedAt)
	s.passes.put(r.tx.gen, p.ID, p)

	return nil
}

func (r passRepo) ListByIDs(_ context.Context, ids []domain.PassID) ([]domain.Pass, error) {
	return sorted(byIDs(r.tx.state.passes.get, ids), byPassID), nil
}

func (r passRepo) ListByPersons(_ context.Context, persons []domain.PersonID) ([]domain.Pass, error) {
	set := setOf(persons)
	passes := r.tx.state.passes.filter(func(p domain.Pass) bool { _, ok := set[p.PersonID]; return ok })

	return sorted(passes, func(a, b domain.Pass) int {
		return cmp.Or(a.SoldAt.Compare(b.SoldAt), compareID(a.ID, b.ID))
	}), nil
}

// LockByPersons needs no lock of its own: the store mutex already serialises
// every transaction.
func (r passRepo) LockByPersons(_ context.Context, persons ...domain.PersonID) ([]domain.Pass, error) {
	if err := r.tx.checkWritable(); err != nil {
		return nil, err
	}

	set := setOf(persons)
	passes := r.tx.state.passes.filter(func(p domain.Pass) bool { _, ok := set[p.PersonID]; return ok })

	return sorted(passes, byPassID), nil
}

func (r passRepo) GetByReceipt(_ context.Context, receipt domain.ReceiptID) (domain.Pass, error) {
	found := r.tx.state.passes.filter(func(p domain.Pass) bool { return p.ReceiptID == receipt })
	if receipt.IsZero() || len(found) == 0 {
		return domain.Pass{}, notFound("pass")
	}

	return found[0], nil
}

func (r passRepo) ListByReceipts(_ context.Context, receipts []domain.ReceiptID) ([]domain.Pass, error) {
	set := setOf(receipts)
	passes := r.tx.state.passes.filter(func(p domain.Pass) bool {
		_, ok := set[p.ReceiptID]

		return ok && !p.ReceiptID.IsZero()
	})

	return sorted(passes, byPassID), nil
}

func byPassID(a, b domain.Pass) int { return compareID(a.ID, b.ID) }

// ---------------------------------------------------------------- redemptions

type redemptionRepo struct{ tx *tx }

func (r redemptionRepo) Create(_ context.Context, red domain.PassRedemption) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case s.redemptions.has(red.BookingID):
		return fmt.Errorf("a pass paid for the booking already: %w", domain.ErrDuplicate)
	case !s.passes.has(red.PassID), !s.bookings.has(red.BookingID):
		return conflict("redemption references a missing record")
	}

	red.At = instant(red.At)
	s.redemptions.put(r.tx.gen, red.BookingID, red)

	return nil
}

func (r redemptionRepo) GetByBooking(_ context.Context, booking domain.BookingID) (domain.PassRedemption, error) {
	red, ok := r.tx.state.redemptions.get(booking)
	if !ok {
		return domain.PassRedemption{}, notFound("redemption")
	}

	return red, nil
}

func (r redemptionRepo) Delete(_ context.Context, booking domain.BookingID) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.redemptions.has(booking) {
		return notFound("redemption")
	}

	r.tx.state.redemptions.delete(r.tx.gen, booking)

	return nil
}

func (r redemptionRepo) ListByPasses(
	_ context.Context, passes []domain.PassID, latest int,
) ([]domain.PassRedemption, error) {
	set := setOf(passes)
	all := sorted(r.tx.state.redemptions.filter(func(red domain.PassRedemption) bool { _, ok := set[red.PassID]; return ok }),
		func(a, b domain.PassRedemption) int {
			return cmp.Or(compareID(a.PassID, b.PassID), a.At.Compare(b.At), compareID(a.BookingID, b.BookingID))
		})
	if latest <= 0 {
		return all, nil
	}

	// Each pass's redemptions are together and ascending: keep their tail.
	out := all[:0:0]

	for start := 0; start < len(all); {
		end := start
		for end < len(all) && all[end].PassID == all[start].PassID {
			end++
		}

		out = append(out, all[max(start, end-latest):end]...)
		start = end
	}

	return out, nil
}

func (r redemptionRepo) CountByPasses(_ context.Context, passes []domain.PassID) (map[domain.PassID]int, error) {
	set := setOf(passes)
	out := map[domain.PassID]int{}

	for _, red := range r.tx.state.redemptions.filter(func(red domain.PassRedemption) bool { _, ok := set[red.PassID]; return ok }) {
		out[red.PassID]++
	}

	return out, nil
}

func (r redemptionRepo) ListByBookings(
	_ context.Context, bookings []domain.BookingID,
) ([]domain.PassRedemption, error) {
	out := byIDs(r.tx.state.redemptions.get, bookings)

	return sorted(out, func(a, b domain.PassRedemption) int { return compareID(a.BookingID, b.BookingID) }), nil
}
