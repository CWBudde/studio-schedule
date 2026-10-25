package service

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Redemption (ADR 0012): whether a booking costs a visit follows from the
// booking, its lesson and the school's redemption mode (charges.due). Visits
// move only on transitions: when a write makes a booking due, it uses a
// pass; when a write makes it stop being due, it gives its visit back; a
// write that leaves it as it was changes nothing, so a booking unpaid when
// it became due stays unpaid, whatever passes are bought later. Every write
// of a booking or of a lesson with bookings goes through the ledger
// (ledger.go), which settles it here under the lesson lock.

// charges are the redemption rules of one transaction: the school's mode
// and its time zone, which turns a lesson's start into the school date a
// pass must cover.
type charges struct {
	mode domain.RedemptionMode
	loc  *time.Location
}

// chargesOf returns the redemption rules of the school's settings.
func chargesOf(settings domain.Settings, loc *time.Location) charges {
	return charges{mode: settings.RedemptionMode, loc: loc}
}

// free reports whether a booking of lesson l never costs a visit, whatever
// its status (ADR 0012): a class member's automatic booking (members pay
// their fee outside the app), a trial lesson, or a lesson the school
// canceled.
func free(l domain.Lesson, b domain.Booking) bool {
	return b.Source == domain.BookingSourceMember || b.Trial || l.Status == domain.LessonCanceled
}

// due reports whether booking b of lesson l costs a visit now (ADR 0012):
// unless it is free, a late cancellation does in either mode and a timely
// one does not; otherwise, when the school redeems on booking, every active
// booking does, and when it redeems on attendance, one marked attended or
// no-show. A guest's booking may be due too; it is paid at the desk.
func (c charges) due(l domain.Lesson, b domain.Booking) bool {
	switch {
	case free(l, b):
		return false
	case b.Status == domain.BookingCanceled:
		return b.Late
	case c.mode == domain.RedeemOnBooking:
		return true
	default:
		return b.Status == domain.BookingAttended || b.Status == domain.BookingNoShow
	}
}

// bookingChange is a write of a booking: before is the booking as it was
// (zero for a new one), after as it is written.
type bookingChange struct {
	before, after domain.Booking
}

// settle settles the passes of changes to bookings of one lesson, which
// the caller has locked and which was old before the write and l after it
// (the same lesson unless its status changed). A person's booking that
// becomes due uses a pass of the person (pickPass) that covers l's school
// date, if any; without one it stays unpaid. One that stops being due gives
// its redemption back: the redemption goes, and a visit pass gets its visit
// back. Guests own no passes. Before any change it locks the passes of the
// persons concerned, together and in ID order (term → class → lesson →
// passes).
func (c charges) settle(
	ctx context.Context, tx store.Tx, now time.Time, old, l domain.Lesson, changes ...bookingChange,
) error {
	var redeem, refund []domain.Booking

	for _, ch := range changes {
		if ch.after.PersonID.IsZero() {
			continue
		}

		was := !ch.before.ID.IsZero() && c.due(old, ch.before)

		switch is := c.due(l, ch.after); {
		case is && !was:
			redeem = append(redeem, ch.after)
		case was && !is:
			refund = append(refund, ch.after)
		}
	}

	if len(redeem)+len(refund) == 0 {
		return nil
	}

	paid, passes, err := lockPayments(ctx, tx, slices.Concat(redeem, refund))
	if err != nil {
		return err
	}

	// Refunds first: a visit given back may pay for another booking.
	for _, b := range refund {
		if r, ok := paid[b.ID]; ok {
			if err := giveBack(ctx, tx, passes, r); err != nil {
				return err
			}
		}
	}

	day := domain.DateOf(l.StartsAt, c.loc)

	for _, b := range redeem {
		if _, ok := paid[b.ID]; ok {
			continue // paid already
		}

		if err := redeemFrom(ctx, tx, now, passes, b, day); err != nil {
			return err
		}
	}

	return nil
}

// recheck keeps the bookings of lesson l, which the caller has locked,
// paid by passes that are valid on its school date (domain.Pass.ValidOn),
// after the lesson moved to another date or a pass was voided: a booking
// whose pass is no longer valid on the date gives its redemption back and
// uses another pass of the person that covers the date, if any; without
// one it stays unpaid. Unpaid bookings stay as they are. It locks like
// settle.
func (c charges) recheck(ctx context.Context, tx store.Tx, now time.Time, l domain.Lesson, bookings []domain.Booking) error {
	var ids []domain.BookingID

	for _, b := range bookings {
		if !b.PersonID.IsZero() {
			ids = append(ids, b.ID)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	redemptions, err := tx.Redemptions().ListByBookings(ctx, ids)
	if err != nil || len(redemptions) == 0 {
		return err
	}

	paidIDs := make([]domain.BookingID, len(redemptions))
	for i, r := range redemptions {
		paidIDs[i] = r.BookingID
	}

	paidBookings := slices.DeleteFunc(slices.Clone(bookings), func(b domain.Booking) bool {
		return !slices.Contains(paidIDs, b.ID)
	})

	paid, passes, err := lockPayments(ctx, tx, paidBookings)
	if err != nil {
		return err
	}

	day := domain.DateOf(l.StartsAt, c.loc)

	for _, b := range paidBookings {
		r := paid[b.ID]

		i := slices.IndexFunc(passes, func(p domain.Pass) bool { return p.ID == r.PassID })
		if i >= 0 && passes[i].ValidOn(day) {
			continue
		}

		if err := giveBack(ctx, tx, passes, r); err != nil {
			return err
		}

		if err := redeemFrom(ctx, tx, now, passes, b, day); err != nil {
			return err
		}
	}

	return nil
}

// lockPayments locks the passes of the persons of bookings, together and
// in ID order, and returns them with the redemptions of the bookings by
// booking.
func lockPayments(
	ctx context.Context, tx store.Tx, bookings []domain.Booking,
) (map[domain.BookingID]domain.PassRedemption, []domain.Pass, error) {
	var persons []domain.PersonID

	ids := make([]domain.BookingID, 0, len(bookings))

	for _, b := range bookings {
		ids = append(ids, b.ID)
		if !slices.Contains(persons, b.PersonID) {
			persons = append(persons, b.PersonID)
		}
	}

	passes, err := tx.Passes().LockByPersons(ctx, persons...)
	if err != nil {
		return nil, nil, fmt.Errorf("lock passes: %w", err)
	}

	// Read under the lock: a concurrent redemption of the same booking
	// cannot come in between.
	redemptions, err := tx.Redemptions().ListByBookings(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("redemptions: %w", err)
	}

	paid := make(map[domain.BookingID]domain.PassRedemption, len(redemptions))
	for _, r := range redemptions {
		paid[r.BookingID] = r
	}

	return paid, passes, nil
}

// redeemFrom lets the best pass of b's person among passes, which the
// caller locked, that covers the school date day pay for booking b
// (pickPass); without one, b stays unpaid and the office settles it at the
// desk.
func redeemFrom(
	ctx context.Context, tx store.Tx, now time.Time, passes []domain.Pass, b domain.Booking, day domain.Date,
) error {
	i := pickPass(passes, b.PersonID, day)
	if i < 0 {
		return nil
	}

	if passes[i].Kind == domain.PassVisits {
		passes[i].VisitsRemaining--
		if err := tx.Passes().Update(ctx, passes[i]); err != nil {
			return fmt.Errorf("use pass %v: %w", passes[i].ID, err)
		}
	}

	r := domain.PassRedemption{PassID: passes[i].ID, BookingID: b.ID, At: now}
	if err := r.Validate(); err != nil {
		return err
	}

	if err := tx.Redemptions().Create(ctx, r); err != nil {
		return fmt.Errorf("redeem pass %v for booking %v: %w", r.PassID, b.ID, err)
	}

	return nil
}

// giveBack deletes redemption r and gives a visit pass its visit back. The
// pass is one of passes, which the caller locked, unless it is no longer the
// person's (it always is); a voided or expired pass gets the visit back too,
// as a record, without being usable again.
func giveBack(ctx context.Context, tx store.Tx, passes []domain.Pass, r domain.PassRedemption) error {
	if err := tx.Redemptions().Delete(ctx, r.BookingID); err != nil {
		return fmt.Errorf("refund booking %v: %w", r.BookingID, err)
	}

	i := slices.IndexFunc(passes, func(p domain.Pass) bool { return p.ID == r.PassID })

	var p domain.Pass

	if i >= 0 {
		p = passes[i]
	} else {
		var err error
		if p, err = tx.Passes().Get(ctx, r.PassID); err != nil {
			return fmt.Errorf("pass %v: %w", r.PassID, refErr(err))
		}
	}

	if p.Kind != domain.PassVisits || p.VisitsRemaining >= p.VisitsTotal {
		return nil
	}

	p.VisitsRemaining++
	if err := tx.Passes().Update(ctx, p); err != nil {
		return fmt.Errorf("refund pass %v: %w", p.ID, err)
	}

	if i >= 0 {
		passes[i] = p
	}

	return nil
}

// pickPass returns the index of the pass of person among passes that pays
// for a visit on the school date day, or -1 if none covers it: time passes
// first, so that no visit is spent while one covers the day, then visit
// passes; among them the one that expires first (none last), then the
// oldest sale, then the ID.
func pickPass(passes []domain.Pass, person domain.PersonID, day domain.Date) int {
	best := -1

	for i, p := range passes {
		if p.PersonID != person || !p.Covers(day) {
			continue
		}

		if best < 0 || comparePasses(p, passes[best]) < 0 {
			best = i
		}
	}

	return best
}

// comparePasses orders passes by the preference of pickPass.
func comparePasses(a, b domain.Pass) int {
	rank := func(p domain.Pass) int {
		if p.Kind == domain.PassTime {
			return 0
		}

		return 1
	}
	until := func(p domain.Pass) domain.Date {
		if p.ValidUntil.IsZero() {
			return domain.Date{Year: 10000} // no expiry: after every date
		}

		return p.ValidUntil
	}

	return cmp.Or(
		cmp.Compare(rank(a), rank(b)),
		until(a).Compare(until(b)),
		a.SoldAt.Compare(b.SoldAt),
		bytes.Compare(a.ID[:], b.ID[:]),
	)
}

// PaymentStatus says how a booking is paid (ADR 0012).
type PaymentStatus string

// Payment statuses.
const (
	// PaymentPaid: a pass paid for the booking.
	PaymentPaid PaymentStatus = "paid"
	// PaymentUnpaid: the booking costs a visit, but no pass paid for it;
	// the office settles it at the desk.
	PaymentUnpaid PaymentStatus = "unpaid"
	// PaymentFree: the booking costs nothing: a class member's booking, a
	// trial, a lesson the school canceled, a timely cancellation or a
	// removal by staff.
	PaymentFree PaymentStatus = "free"
	// PaymentPending: the school redeems on attendance, and attendance has
	// not been taken yet.
	PaymentPending PaymentStatus = "pending"
)

// BookingPayment is how a booking is paid.
type BookingPayment struct {
	Status PaymentStatus
	// Pass and PassType are the pass that paid and its type, only with
	// PaymentPaid.
	Pass     domain.Pass
	PassType domain.PassType
}

// status returns how booking b of lesson l is paid, given whether a pass
// paid for it.
func (c charges) status(l domain.Lesson, b domain.Booking, paid bool) PaymentStatus {
	switch {
	case paid:
		return PaymentPaid
	case c.due(l, b):
		return PaymentUnpaid
	case c.mode == domain.RedeemOnAttendance && b.Status == domain.BookingBooked && !free(l, b):
		return PaymentPending
	default:
		return PaymentFree
	}
}

// payments returns, by booking, how the bookings are paid; lessons has
// their lessons. It costs at most three queries for any number of bookings:
// their redemptions, the passes that paid and the pass types.
func (c charges) payments(
	ctx context.Context, tx store.Tx, lessons map[domain.LessonID]domain.Lesson, bookings []domain.Booking,
) (map[domain.BookingID]BookingPayment, error) {
	ids := make([]domain.BookingID, 0, len(bookings))

	for _, b := range bookings {
		if !b.PersonID.IsZero() {
			ids = append(ids, b.ID)
		}
	}

	var redemptions []domain.PassRedemption

	if len(ids) > 0 {
		var err error
		if redemptions, err = tx.Redemptions().ListByBookings(ctx, ids); err != nil {
			return nil, fmt.Errorf("redemptions: %w", err)
		}
	}

	paid := make(map[domain.BookingID]domain.PassID, len(redemptions))
	passIDs := make([]domain.PassID, 0, len(redemptions))

	for _, r := range redemptions {
		paid[r.BookingID] = r.PassID
		passIDs = append(passIDs, r.PassID)
	}

	passes := map[domain.PassID]domain.Pass{}
	types := map[domain.PassTypeID]domain.PassType{}

	if len(passIDs) > 0 {
		list, err := tx.Passes().ListByIDs(ctx, passIDs)
		if err != nil {
			return nil, fmt.Errorf("passes: %w", err)
		}

		for _, p := range list {
			passes[p.ID] = p
		}

		if types, err = passTypesByID(ctx, tx); err != nil {
			return nil, err
		}
	}

	out := make(map[domain.BookingID]BookingPayment, len(bookings))

	for _, b := range bookings {
		passID, ok := paid[b.ID]
		pay := BookingPayment{Status: c.status(lessons[b.LessonID], b, ok)}

		if ok {
			pay.Pass = passes[passID]
			pay.PassType = types[pay.Pass.PassTypeID]
		}

		out[b.ID] = pay
	}

	return out, nil
}

// passTypesByID returns all pass types by ID.
func passTypesByID(ctx context.Context, tx store.Tx) (map[domain.PassTypeID]domain.PassType, error) {
	list, err := tx.PassTypes().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("pass types: %w", err)
	}

	out := make(map[domain.PassTypeID]domain.PassType, len(list))
	for _, t := range list {
		out[t.ID] = t
	}

	return out, nil
}
