package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The pass tests run on the staff fixture (Europe/Berlin, booking cutoff
// 1 h, cancellation cutoff 2 h) from bookingNow, Wednesday 2026-10-21, 12:00
// in Berlin. Passes are sold that day by Olga from the office; their
// lessons are one-offs on the following days.

// passFixture adds the pass catalog to the staff fixture: a 10-visit pass
// valid 180 days, a single visit without expiry and a 30-day pass.
type passFixture struct {
	*staffFixture

	tenVisits, single, month domain.PassType
	office                   Principal
	// pdf renders the receipts of the sales.
	pdf *fakePDF
}

func newPassFixture(t *testing.T, mode domain.RedemptionMode) *passFixture {
	t.Helper()

	f := &passFixture{staffFixture: newStaffFixture(t), pdf: &fakePDF{}}
	f.office = f.principal(t, f.olga)
	f.setMode(t, mode)

	svc := f.passService(t, bookingNow)
	for _, pt := range []struct {
		into *domain.PassType
		data PassTypeData
	}{
		{&f.tenVisits, PassTypeData{Name: "10-visit pass", Kind: domain.PassVisits, Visits: 10, ValidityDays: 180, Price: 12000, VATRate: 1900, Active: true}},
		{&f.single, PassTypeData{Name: "Single visit", Kind: domain.PassVisits, Visits: 1, Price: 1500, Active: true}},
		{&f.month, PassTypeData{Name: "30-day pass", Kind: domain.PassTime, ValidityDays: 30, Price: 9900, VATRate: 1900, Active: true}},
	} {
		var err error
		if *pt.into, err = svc.CreatePassType(t.Context(), pt.data); err != nil {
			t.Fatal(err)
		}
	}

	return f
}

func (f *passFixture) setMode(t *testing.T, mode domain.RedemptionMode) {
	t.Helper()

	if err := f.passService(t, bookingNow).SetRedemptionMode(t.Context(), mode); err != nil {
		t.Fatal(err)
	}
}

func (f *passFixture) passService(t *testing.T, now string) *PassService {
	t.Helper()

	return NewPassService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, f.receiptService(t, now))
}

func (f *passFixture) receiptService(t *testing.T, now string) *ReceiptService {
	t.Helper()

	return NewReceiptService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, testMails(newTestRenderer(t)), f.pdf)
}

// sell sells a pass of pt to p at bookingNow, valid from the school date
// from (today if empty).
func (f *passFixture) sell(t *testing.T, p domain.Person, pt domain.PassType, from string) domain.Pass {
	t.Helper()

	sale := PassSale{PassTypeID: pt.ID}
	if from != "" {
		sale.ValidFrom = mustDate(t, from)
	}

	v, err := f.passService(t, bookingNow).Sell(t.Context(), f.office, p.ID, sale)
	if err != nil {
		t.Fatal(err)
	}

	return v.Pass
}

func (f *passFixture) pass(t *testing.T, id domain.PassID) domain.Pass {
	t.Helper()

	var p domain.Pass

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		p, err = tx.Passes().Get(ctx, id)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return p
}

// paidBy returns the pass that paid for booking b, or a zero ID.
func (f *passFixture) paidBy(t *testing.T, b domain.BookingID) domain.PassID {
	t.Helper()

	var r domain.PassRedemption

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		r, err = tx.Redemptions().GetByBooking(ctx, b)

		return err
	})
	if errors.Is(err, domain.ErrNotFound) {
		return domain.PassID{}
	} else if err != nil {
		t.Fatal(err)
	}

	return r.PassID
}

// wantVisits checks the visits left of a visit pass.
func (f *passFixture) wantVisits(t *testing.T, pass domain.Pass, want int) {
	t.Helper()

	if got := f.pass(t, pass.ID).VisitsRemaining; got != want {
		t.Errorf("%d visits left, want %d", got, want)
	}
}

func (f *passFixture) wantPaidBy(t *testing.T, b domain.BookingID, want domain.PassID) {
	t.Helper()

	if got := f.paidBy(t, b); got != want {
		t.Errorf("booking %v paid by %v, want %v", b, got, want)
	}
}

// book books p (Anna's family) into l at now.
func (f *passFixture) book(t *testing.T, now string, l domain.Lesson, p domain.Person) domain.Booking {
	t.Helper()

	b, err := f.bookingService(t, now).Book(t.Context(), f.principal(t, f.anna), l.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func TestPassTypeAdmin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)
	svc := f.passService(t, bookingNow)

	types, err := svc.PassTypes(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(types) != 3 || types[0].Name != "10-visit pass" || !types[0].CreatedAt.Equal(mustInstant(t, bookingNow)) {
		t.Fatalf("pass types %+v", types)
	}

	_, err = svc.CreatePassType(ctx, PassTypeData{Name: "single VISIT", Kind: domain.PassVisits, Visits: 1, Active: true})
	if !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("duplicate name: %v", err)
	}

	_, err = svc.CreatePassType(ctx, PassTypeData{Name: "Odd", Kind: domain.PassTime, Visits: 3, ValidityDays: 7})
	wantFieldError(t, err, "visits")

	_, err = svc.CreatePassType(ctx, PassTypeData{Name: "Odd", Kind: domain.PassVisits, Visits: 3, VATRate: -1})
	wantFieldError(t, err, "vatRate")

	// Retiring a sold type stops its sale; the pass sold keeps its visits.
	sold := f.sell(t, f.mia, f.tenVisits, "")

	retired, err := svc.UpdatePassType(ctx, f.tenVisits.ID, PassTypeData{
		Name: " 10-visit pass (old) ", Kind: domain.PassVisits, Visits: 12, ValidityDays: 90, Price: 13000,
	})
	if err != nil || retired.Active || retired.Name != "10-visit pass (old)" || !retired.CreatedAt.Equal(f.tenVisits.CreatedAt) {
		t.Fatalf("retire: %+v, %v", retired, err)
	}

	if p := f.pass(t, sold.ID); p.VisitsTotal != 10 || p.ValidUntil != mustDate(t, "2027-04-18") {
		t.Errorf("sold pass changed with its type: %+v", p)
	}

	_, err = svc.Sell(ctx, f.office, f.mia.ID, PassSale{PassTypeID: f.tenVisits.ID})
	wantFieldError(t, err, "passTypeId")

	if err := svc.DeletePassType(ctx, f.tenVisits.ID); !errors.Is(err, domain.ErrInUse) {
		t.Errorf("delete a sold type: %v", err)
	}

	if err := svc.DeletePassType(ctx, f.single.ID); err != nil {
		t.Errorf("delete an unsold type: %v", err)
	}

	if _, err := svc.UpdatePassType(ctx, f.single.ID, PassTypeData{Name: "Gone", Kind: domain.PassVisits, Visits: 1}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("update a deleted type: %v", err)
	}
}

func TestSellAndAssignPass(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)
	svc := f.passService(t, bookingNow)

	// Anna pays for her daughter's pass; it starts today in Berlin.
	v, err := svc.Sell(ctx, f.office, f.mia.ID, PassSale{PassTypeID: f.tenVisits.ID, PayerID: f.anna.ID, Note: " cash "})
	if err != nil {
		t.Fatal(err)
	}

	want := domain.Pass{
		ID: v.Pass.ID, PassTypeID: f.tenVisits.ID, PersonID: f.mia.ID, Kind: domain.PassVisits, VisitsTotal: 10,
		VisitsRemaining: 10, ValidFrom: mustDate(t, "2026-10-21"), ValidUntil: mustDate(t, "2027-04-18"),
		Status: domain.PassActive, Origin: domain.PassSold, SoldAt: mustInstant(t, bookingNow), SoldBy: f.office.AccountID,
		PayerID: f.anna.ID, ReceiptID: v.Pass.ReceiptID, PricePaid: 12000, Note: "cash",
	}
	if v.Pass != want || v.Pass.ReceiptID.IsZero() || v.OwnerName != "Mia Example" || v.PayerName != "Anna Example" || v.PassType.ID != f.tenVisits.ID ||
		!v.Usable || len(v.Uses) != 0 {
		t.Fatalf("sold\n%+v\nwant pass %+v", v, want)
	}

	if f.pass(t, v.Pass.ID) != want {
		t.Errorf("stored %+v", f.pass(t, v.Pass.ID))
	}

	// A time pass from a later day, at a discount, paid by its owner.
	price := domain.Money(5000)

	v, err = svc.Sell(ctx, f.office, f.anna.ID, PassSale{
		PassTypeID: f.month.ID, PayerID: f.anna.ID, ValidFrom: mustDate(t, "2026-11-01"), Price: &price,
	})
	if err != nil {
		t.Fatal(err)
	}

	if p := v.Pass; p.Kind != domain.PassTime || p.VisitsTotal != 0 || p.ValidUntil != mustDate(t, "2026-11-30") ||
		p.PricePaid != 5000 || !p.PayerID.IsZero() || v.Usable {
		t.Errorf("time pass %+v (usable %v)", p, v.Usable)
	}

	// A single visit given away, without expiry.
	v, err = svc.Assign(ctx, f.office, f.tom.ID, PassAssignment{PassTypeID: f.single.ID, Note: "Goodwill"})
	if err != nil {
		t.Fatal(err)
	}

	if p := v.Pass; p.Origin != domain.PassAssigned || p.PricePaid != 0 || !p.ValidUntil.IsZero() || p.Note != "Goodwill" {
		t.Errorf("assigned %+v", p)
	}

	_, err = svc.Sell(ctx, f.office, f.mia.ID, PassSale{PassTypeID: ids.New[domain.PassTypeID](f.seq)})
	wantFieldError(t, err, "passTypeId")

	_, err = svc.Sell(ctx, f.office, f.mia.ID, PassSale{PassTypeID: f.single.ID, PayerID: ids.New[domain.PersonID](f.seq)})
	wantFieldError(t, err, "payerId")

	negative := domain.Money(-1)
	_, err = svc.Sell(ctx, f.office, f.mia.ID, PassSale{PassTypeID: f.single.ID, Price: &negative})
	wantFieldError(t, err, "price")

	if _, err = svc.Sell(ctx, f.office, ids.New[domain.PersonID](f.seq), PassSale{PassTypeID: f.single.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sell to nobody: %v", err)
	}

	if _, err = svc.Assign(ctx, f.office, ids.New[domain.PersonID](f.seq), PassAssignment{PassTypeID: f.single.ID}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("assign to nobody: %v", err)
	}
}

func TestRedeemOnBooking(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)
	pass := f.sell(t, f.mia, f.tenVisits, "")

	// Booking uses a visit; a timely cancellation gives it back.
	l := f.regular(t, staffLesson)
	b := f.book(t, bookingNow, l, f.mia)
	f.wantPaidBy(t, b.ID, pass.ID)
	f.wantVisits(t, pass, 9)

	if _, err := f.bookingService(t, bookingNow).Cancel(ctx, f.principal(t, f.anna), b.ID); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, domain.PassID{})
	f.wantVisits(t, pass, 10)

	// A late cancellation keeps the visit.
	b = f.book(t, bookingNow, l, f.mia)
	f.wantVisits(t, pass, 9)

	canceled, err := f.bookingService(t, "2026-10-22T15:00:00Z").Cancel(ctx, f.principal(t, f.anna), b.ID)
	if err != nil || !canceled.Late {
		t.Fatalf("late cancellation: %+v, %v", canceled, err)
	}

	f.wantPaidBy(t, b.ID, pass.ID)
	f.wantVisits(t, pass, 9)

	// The school cancels the lesson: free, the late one included; restored,
	// the visits are used again.
	l2 := f.regular(t, "2026-10-23T16:00:00Z")
	b2 := f.book(t, bookingNow, l2, f.mia)
	f.wantVisits(t, pass, 8)

	lessons := f.lessonService(t, bookingNow)
	if _, err := lessons.CancelLesson(ctx, f.office, l.ID, ""); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, domain.PassID{})
	f.wantVisits(t, pass, 9)

	if _, err := lessons.CancelLesson(ctx, f.office, l2.ID, "Sick"); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b2.ID, domain.PassID{})
	f.wantVisits(t, pass, 10)

	if _, err := lessons.RestoreLesson(ctx, l2.ID, false); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b2.ID, pass.ID)
	f.wantVisits(t, pass, 9)

	if _, err := lessons.RestoreLesson(ctx, l.ID, false); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, pass.ID) // the late cancellation stands
	f.wantVisits(t, pass, 8)

	// Staff removing a participant is free.
	if _, err := f.participants(t, bookingNow).Remove(ctx, f.office, b2.ID); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b2.ID, domain.PassID{})
	f.wantVisits(t, pass, 9)

	// Attendance changes nothing in this mode.
	b3 := f.book(t, bookingNow, l2, f.mia)
	staff := f.participants(t, "2026-10-23T16:30:00Z")

	for _, status := range []domain.BookingStatus{domain.BookingAttended, domain.BookingBooked, domain.BookingNoShow} {
		if _, err := staff.SetAttendance(ctx, f.office, b3.ID, status); err != nil {
			t.Fatal(err)
		}

		f.wantPaidBy(t, b3.ID, pass.ID)
		f.wantVisits(t, pass, 8)
	}
}

func TestRedeemOnAttendance(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnAttendance)
	pass := f.sell(t, f.mia, f.tenVisits, "")
	l := f.regular(t, staffLesson)

	// Booking uses nothing yet.
	b := f.book(t, bookingNow, l, f.mia)
	f.wantPaidBy(t, b.ID, domain.PassID{})
	f.wantVisits(t, pass, 10)

	// Attended uses a visit, a no-show keeps it, back to booked gives it
	// back, all at once uses it again.
	staff := f.participants(t, staffStarted)
	for _, step := range []struct {
		status domain.BookingStatus
		paid   bool
	}{
		{domain.BookingAttended, true},
		{domain.BookingNoShow, true},
		{domain.BookingNoShow, true},
		{domain.BookingBooked, false},
		{domain.BookingNoShow, true},
		{domain.BookingBooked, false},
	} {
		if _, err := staff.SetAttendance(ctx, f.office, b.ID, step.status); err != nil {
			t.Fatal(err)
		}

		if step.paid {
			f.wantPaidBy(t, b.ID, pass.ID)
			f.wantVisits(t, pass, 9)
		} else {
			f.wantPaidBy(t, b.ID, domain.PassID{})
			f.wantVisits(t, pass, 10)
		}
	}

	if _, err := staff.MarkAllAttended(ctx, f.office, l.ID); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, pass.ID)
	f.wantVisits(t, pass, 9)

	// A late cancellation costs a visit, a timely one nothing.
	l2, l3 := f.regular(t, "2026-10-23T16:00:00Z"), f.regular(t, "2026-10-24T16:00:00Z")
	late, timely := f.book(t, bookingNow, l2, f.mia), f.book(t, bookingNow, l3, f.mia)
	members := f.bookingService(t, "2026-10-23T15:00:00Z")

	for _, id := range []domain.BookingID{late.ID, timely.ID} {
		if _, err := members.Cancel(ctx, f.principal(t, f.anna), id); err != nil {
			t.Fatal(err)
		}
	}

	f.wantPaidBy(t, late.ID, pass.ID)
	f.wantPaidBy(t, timely.ID, domain.PassID{})
	f.wantVisits(t, pass, 8)

	// Removing a participant who attended gives the visit back.
	if _, err := f.participants(t, staffStarted).Remove(ctx, f.office, b.ID); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, domain.PassID{})
	f.wantVisits(t, pass, 9)
}

func TestRedemptionExemptions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnAttendance)
	pass := f.sell(t, f.mia, f.tenVisits, "")
	guestPass := f.sell(t, f.anna, f.tenVisits, "")
	l := f.regular(t, staffLesson)

	// A class member's automatic booking never uses a pass.
	member := domain.Booking{
		ID: ids.New[domain.BookingID](f.seq), LessonID: l.ID, PersonID: f.mia.ID, Status: domain.BookingBooked,
		Source: domain.BookingSourceMember, BookedAt: mustInstant(t, bookingNow),
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, member) })

	staff := f.participants(t, staffStarted)

	// A trial is free; a guest owns no pass.
	trial, err := staff.Add(ctx, f.office, l.ID, NewParticipant{PersonID: f.tom.ID, Trial: true})
	if err != nil {
		t.Fatal(err)
	}

	guest, err := staff.Add(ctx, f.office, l.ID, NewParticipant{GuestName: "Gina Guest"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := staff.MarkAllAttended(ctx, f.office, l.ID); err != nil {
		t.Fatal(err)
	}

	for _, b := range []domain.Booking{member, trial, guest} {
		f.wantPaidBy(t, b.ID, domain.PassID{})
	}

	f.wantVisits(t, pass, 10)
	f.wantVisits(t, guestPass, 10)

	lp, err := staff.Participants(ctx, f.office, l.ID)
	if err != nil {
		t.Fatal(err)
	}

	got := map[domain.BookingID]PaymentStatus{}
	for _, pt := range lp.Bookings {
		got[pt.Booking.ID] = pt.Payment.Status
	}

	// The guest pays at the desk.
	if got[member.ID] != PaymentFree || got[trial.ID] != PaymentFree || got[guest.ID] != PaymentUnpaid {
		t.Errorf("payments %v", got)
	}

	// A trial that turns out not to be one uses a visit; one again gives
	// it back.
	if _, err := staff.UpdateTrial(ctx, f.office, trial.ID, ParticipantUpdate{}); err != nil {
		t.Fatal(err)
	}

	tomsPass := f.sell(t, f.tom, f.single, "")
	f.wantPaidBy(t, trial.ID, domain.PassID{}) // sold after: pays nothing booked before

	if _, err := staff.UpdateTrial(ctx, f.office, trial.ID, ParticipantUpdate{Trial: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := staff.UpdateTrial(ctx, f.office, trial.ID, ParticipantUpdate{}); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, trial.ID, tomsPass.ID)
	f.wantVisits(t, tomsPass, 0)

	if _, err := staff.UpdateTrial(ctx, f.office, trial.ID, ParticipantUpdate{Trial: true}); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, trial.ID, domain.PassID{})
	f.wantVisits(t, tomsPass, 1)
}

func TestPassChoice(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnBooking)

	// Mia has a single visit without expiry, a 10-visit pass expiring in
	// April, an older one expiring in April too, and a 30-day pass from
	// November.
	single := f.sell(t, f.mia, f.single, "")
	newer := f.sell(t, f.mia, f.tenVisits, "2026-10-21")
	older := f.sell(t, f.mia, f.tenVisits, "2026-10-20")
	month := f.sell(t, f.mia, f.month, "2026-11-01")
	// Her brother's pass is his.
	f.sell(t, f.tom, f.month, "2026-10-01")

	// The 10-visit passes expire before the single visit: the one that
	// expires first.
	b := f.book(t, bookingNow, f.regular(t, staffLesson), f.mia)
	f.wantPaidBy(t, b.ID, older.ID)

	// From November the time pass covers the day: it pays, visits stay.
	for _, start := range []string{"2026-11-02T16:00:00Z", "2026-11-03T16:00:00Z"} {
		b := f.book(t, "2026-11-01T10:00:00Z", f.regular(t, start), f.mia)
		f.wantPaidBy(t, b.ID, month.ID)
	}

	f.wantVisits(t, older, 9)
	f.wantVisits(t, newer, 10)
	f.wantVisits(t, single, 1)

	// Of passes that expire the same day, the older sale first.
	sameDay := f.sell(t, f.anna, f.tenVisits, "")
	sameDayLater := f.sell(t, f.anna, f.tenVisits, "")

	b = f.book(t, bookingNow, f.regular(t, "2026-10-23T16:00:00Z"), f.anna)
	if got := f.paidBy(t, b.ID); got != sameDay.ID || sameDay.ID == sameDayLater.ID {
		t.Errorf("paid by %v, want the first of the same day %v", got, sameDay.ID)
	}
}

func TestPassValidityInTheSchoolZone(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnBooking)

	// The single visit of a time pass of one day: Thursday 2026-10-22 in
	// Berlin, until midnight there (22:00 UTC).
	oneDay, err := f.passService(t, bookingNow).CreatePassType(t.Context(), PassTypeData{
		Name: "Day pass", Kind: domain.PassTime, ValidityDays: 1, Price: 2000, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	pass := f.sell(t, f.mia, oneDay, "2026-10-22")

	before := f.book(t, bookingNow, f.regular(t, "2026-10-21T21:30:00Z"), f.mia) // Wed 23:30
	first := f.book(t, bookingNow, f.regular(t, "2026-10-21T22:30:00Z"), f.mia)  // Thu 00:30
	last := f.book(t, bookingNow, f.regular(t, "2026-10-22T21:30:00Z"), f.mia)   // Thu 23:30
	after := f.book(t, bookingNow, f.regular(t, "2026-10-22T22:30:00Z"), f.mia)  // Fri 00:30, still Thursday in UTC

	f.wantPaidBy(t, before.ID, domain.PassID{})
	f.wantPaidBy(t, first.ID, pass.ID)
	f.wantPaidBy(t, last.ID, pass.ID)
	f.wantPaidBy(t, after.ID, domain.PassID{})
}

func TestVoidPass(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)
	pass := f.sell(t, f.mia, f.tenVisits, "")
	single := f.sell(t, f.mia, f.single, "")

	// The 10-visit pass expires first: it pays for both lessons.
	started := f.book(t, bookingNow, f.regular(t, staffLesson), f.mia)
	ahead := f.book(t, bookingNow, f.regular(t, "2026-10-23T16:00:00Z"), f.mia)
	f.wantPaidBy(t, started.ID, pass.ID)
	f.wantPaidBy(t, ahead.ID, pass.ID)

	// Voided while the first lesson runs: that one stays paid, the one ahead
	// moves to the single visit.
	svc := f.passService(t, staffStarted)

	v, err := svc.Void(ctx, pass.ID)
	if err != nil {
		t.Fatal(err)
	}

	if v.Pass.Status != domain.PassVoided || !v.Pass.VoidedAt.Equal(mustInstant(t, staffStarted)) || v.Usable ||
		v.UseCount != 1 || len(v.Uses) != 1 || v.Uses[0].Booking.ID != started.ID || v.Pass.VisitsRemaining != 9 {
		t.Fatalf("voided %+v", v)
	}

	f.wantPaidBy(t, started.ID, pass.ID)
	f.wantPaidBy(t, ahead.ID, single.ID)
	f.wantVisits(t, single, 0)

	if _, err := svc.Void(ctx, pass.ID); !errors.Is(err, ErrPassVoided) {
		t.Errorf("void twice: %v", err)
	}

	if _, err := svc.Void(ctx, ids.New[domain.PassID](f.seq)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("void nothing: %v", err)
	}

	// Voiding the single visit too leaves the lesson ahead unpaid, and the
	// voided passes pay for nothing more.
	if _, err := svc.Void(ctx, single.ID); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, ahead.ID, domain.PassID{})

	next := f.book(t, "2026-10-22T17:00:00Z", f.regular(t, "2026-10-24T16:00:00Z"), f.mia)
	f.wantPaidBy(t, next.ID, domain.PassID{})
}

// TestRedeemLastVisitConcurrently books many lessons for a person with one
// visit left from many goroutines: exactly one booking uses it.
func TestRedeemLastVisitConcurrently(t *testing.T) {
	t.Parallel()

	const n = 8

	f := newPassFixture(t, domain.RedeemOnBooking)
	pass := f.sell(t, f.mia, f.single, "")
	svc := f.bookingService(t, bookingNow)
	anna := f.principal(t, f.anna)

	lessons := make([]domain.Lesson, n)
	for i := range lessons {
		lessons[i] = f.regular(t, mustInstant(t, staffLesson).Add(time.Duration(i)*time.Hour).Format(time.RFC3339))
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		bookings []domain.Booking
	)

	for _, l := range lessons {
		wg.Go(func() {
			b, err := svc.Book(context.Background(), anna, l.ID, f.mia.ID)
			if err != nil {
				t.Error(err)

				return
			}

			mu.Lock()
			defer mu.Unlock()

			bookings = append(bookings, b)
		})
	}

	wg.Wait()

	paid := 0

	for _, b := range bookings {
		if f.paidBy(t, b.ID) == pass.ID {
			paid++
		}
	}

	if len(bookings) != n || paid != 1 {
		t.Errorf("%d bookings, %d paid by the pass; want %d and 1", len(bookings), paid, n)
	}

	f.wantVisits(t, pass, 0)
}

// TestRedemptionUnderRetry runs every transaction twice, like the
// PostgreSQL store after a serialization failure: the booking uses one
// visit.
func TestRedemptionUnderRetry(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnBooking)
	pass := f.sell(t, f.mia, f.tenVisits, "")

	svc := NewBookingService(retryingStore{f.st}, clock.NewFixed(mustInstant(t, bookingNow)), f.seq,
		testMails(newTestRenderer(t)))

	b, err := svc.Book(t.Context(), f.principal(t, f.anna), f.regular(t, staffLesson).ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, pass.ID)
	f.wantVisits(t, pass, 9)
}

func TestNoPassLeavesTheBookingUnpaid(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)
	b := f.book(t, bookingNow, f.regular(t, staffLesson), f.mia)
	f.wantPaidBy(t, b.ID, domain.PassID{})

	my, err := NewMyLessonsService(f.st, clock.NewFixed(mustInstant(t, bookingNow))).Upcoming(ctx, f.principal(t, f.anna))
	if err != nil || len(my.Upcoming) != 1 {
		t.Fatalf("my lessons %+v, %v", my, err)
	}

	if got := my.Upcoming[0].Payment.Status; got != PaymentUnpaid {
		t.Errorf("payment %v, want unpaid", got)
	}
}

func TestBookingPayments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnAttendance)
	pass := f.sell(t, f.mia, f.tenVisits, "")
	l := f.regular(t, staffLesson)
	mias, toms := f.book(t, bookingNow, l, f.mia), f.book(t, bookingNow, l, f.tom)

	my := func(now string) map[domain.BookingID]BookingPayment {
		t.Helper()

		out, err := NewMyLessonsService(f.st, clock.NewFixed(mustInstant(t, now))).Upcoming(ctx, f.principal(t, f.anna))
		if err != nil {
			t.Fatal(err)
		}

		got := map[domain.BookingID]BookingPayment{}
		for _, b := range out.Upcoming {
			got[b.Booking.ID] = b.Payment
		}

		return got
	}

	// Before attendance both are pending.
	got := my(bookingNow)
	if got[mias.ID].Status != PaymentPending || got[toms.ID].Status != PaymentPending {
		t.Errorf("before attendance: %+v", got)
	}

	if _, err := f.participants(t, staffStarted).MarkAllAttended(ctx, f.office, l.ID); err != nil {
		t.Fatal(err)
	}

	got = my(staffStarted)
	if p := got[mias.ID]; p.Status != PaymentPaid || p.Pass.ID != pass.ID || p.Pass.VisitsRemaining != 9 ||
		p.PassType.Name != "10-visit pass" {
		t.Errorf("Mia's payment %+v", p)
	}

	if p := got[toms.ID]; p.Status != PaymentUnpaid || !p.Pass.ID.IsZero() {
		t.Errorf("Tom's payment %+v", p)
	}
}

func TestMyPasses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)

	mias := f.sell(t, f.mia, f.tenVisits, "")
	used := f.sell(t, f.anna, f.single, "")
	future := f.sell(t, f.anna, f.month, "2026-12-01")
	f.sell(t, f.sam, f.single, "") // a stranger's child

	first := f.book(t, bookingNow, f.regular(t, staffLesson), f.mia)
	second := f.book(t, bookingNow, f.regular(t, "2026-10-23T16:00:00Z"), f.mia)
	f.book(t, bookingNow, f.regular(t, "2026-10-24T16:00:00Z"), f.anna)

	svc := f.passService(t, bookingNow)

	got, err := svc.Mine(ctx, f.principal(t, f.anna))
	if err != nil {
		t.Fatal(err)
	}

	if got.Timezone != "Europe/Berlin" || got.Today != mustDate(t, "2026-10-21") || len(got.Passes) != 3 {
		t.Fatalf("my passes %+v", got)
	}

	// Usable first; then the others, newest sale first (the later ID
	// breaks the tie of the same instant).
	order := []domain.PassID{got.Passes[0].Pass.ID, got.Passes[1].Pass.ID, got.Passes[2].Pass.ID}
	if order[0] != mias.ID || !got.Passes[0].Usable || got.Passes[0].OwnerName != "Mia Example" {
		t.Errorf("first %+v", got.Passes[0])
	}

	if order[1] != future.ID || order[2] != used.ID {
		t.Errorf("order %v, want %v then %v", order, future.ID, used.ID)
	}

	// The latest lesson first.
	uses := got.Passes[0].Uses
	if len(uses) != 2 || uses[0].Booking.ID != second.ID || uses[1].Booking.ID != first.ID ||
		uses[0].Lesson.CourseType.Name == "" {
		t.Errorf("uses %+v", uses)
	}

	// Tom has an account of his own: his family is himself.
	tom, err := svc.Mine(ctx, f.principal(t, f.tom))
	if err != nil || len(tom.Passes) != 0 {
		t.Errorf("Tom's passes %+v, %v", tom, err)
	}

	office, err := svc.PersonPasses(ctx, f.mia.ID)
	if err != nil || len(office) != 1 || office[0].Pass.ID != mias.ID {
		t.Errorf("office view %+v, %v", office, err)
	}

	if _, err := svc.PersonPasses(ctx, ids.New[domain.PersonID](f.seq)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("passes of nobody: %v", err)
	}
}

func TestRedemptionMode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)
	svc := f.passService(t, bookingNow)

	if mode, err := svc.RedemptionMode(ctx); err != nil || mode != domain.RedeemOnBooking {
		t.Fatalf("mode %v, %v", mode, err)
	}

	wantFieldError(t, svc.SetRedemptionMode(ctx, "never"), "redemptionMode")

	// What was redeemed stays when the mode changes.
	pass := f.sell(t, f.mia, f.tenVisits, "")
	b := f.book(t, bookingNow, f.regular(t, staffLesson), f.mia)

	if err := svc.SetRedemptionMode(ctx, domain.RedeemOnAttendance); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, pass.ID)

	if mode, err := svc.RedemptionMode(ctx); err != nil || mode != domain.RedeemOnAttendance {
		t.Errorf("mode %v, %v", mode, err)
	}
}

// TestRedemptionOnlyOnTransitions checks that visits move only when a write
// changes whether a booking is due: writes that change nothing, a later
// sale and a change of the mode leave earlier bookings as they are.
func TestRedemptionOnlyOnTransitions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnAttendance)
	l := f.regular(t, staffLesson)
	staff := f.participants(t, staffStarted)

	// Attended without a pass: unpaid, and it stays so after a sale while
	// nothing changes whether it is due.
	b := f.book(t, bookingNow, l, f.mia)
	if _, err := staff.SetAttendance(ctx, f.office, b.ID, domain.BookingAttended); err != nil {
		t.Fatal(err)
	}

	pass := f.sell(t, f.mia, f.tenVisits, "")

	if _, err := staff.SetAttendance(ctx, f.office, b.ID, domain.BookingAttended); err != nil {
		t.Fatal(err)
	}

	if _, err := staff.SetAttendance(ctx, f.office, b.ID, domain.BookingNoShow); err != nil {
		t.Fatal(err)
	}

	if _, err := staff.UpdateTrial(ctx, f.office, b.ID, ParticipantUpdate{}); err != nil {
		t.Fatal(err)
	}

	if _, err := staff.MarkAllAttended(ctx, f.office, l.ID); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, domain.PassID{})
	f.wantVisits(t, pass, 10)

	// A real transition charges: back to unknown, then attended.
	for _, status := range []domain.BookingStatus{domain.BookingBooked, domain.BookingAttended} {
		if _, err := staff.SetAttendance(ctx, f.office, b.ID, status); err != nil {
			t.Fatal(err)
		}
	}

	f.wantPaidBy(t, b.ID, pass.ID)
	f.wantVisits(t, pass, 9)

	// Redeemed on booking, a booking keeps its visit when the school
	// switches to attendance, also through writes that change nothing.
	f.setMode(t, domain.RedeemOnBooking)

	booked := f.book(t, bookingNow, f.regular(t, "2026-10-23T16:00:00Z"), f.mia)
	f.wantPaidBy(t, booked.ID, pass.ID)
	f.setMode(t, domain.RedeemOnAttendance)

	if _, err := f.participants(t, bookingNow).UpdateTrial(ctx, f.office, booked.ID, ParticipantUpdate{}); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, booked.ID, pass.ID)
	f.wantVisits(t, pass, 8)

	// Booked without a pass on booking, a late cancellation stays unpaid
	// after a sale: it was due before and after.
	f.setMode(t, domain.RedeemOnBooking)

	tom := f.book(t, bookingNow, f.regular(t, "2026-10-24T16:00:00Z"), f.tom)
	f.sell(t, f.tom, f.single, "")

	if _, err := f.bookingService(t, "2026-10-24T15:00:00Z").Cancel(ctx, f.principal(t, f.anna), tom.ID); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, tom.ID, domain.PassID{})
}

// TestLessonMoveRechecksPasses moves lessons to another date, one by one
// and with their class: a booking keeps its pass only if it is valid on the
// new date, else it uses another one that covers it, or none.
func TestLessonMoveRechecksPasses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)

	week, err := f.passService(t, bookingNow).CreatePassType(ctx, PassTypeData{
		Name: "Week pass", Kind: domain.PassTime, ValidityDays: 7, Price: 3000, Active: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mia's week pass ends on Tuesday 2026-10-27; she has a 10-visit pass
	// too. Anna has only a week pass.
	miasWeek := f.sell(t, f.mia, week, "")
	tenVisits := f.sell(t, f.mia, f.tenVisits, "")
	annasWeek := f.sell(t, f.anna, week, "")

	// A one-off lesson on Tuesday moves to Thursday.
	l := f.regular(t, "2026-10-27T17:00:00Z")
	mia, anna := f.book(t, bookingNow, l, f.mia), f.book(t, bookingNow, l, f.anna)
	f.wantPaidBy(t, mia.ID, miasWeek.ID)
	f.wantPaidBy(t, anna.ID, annasWeek.ID)

	d := dataOf(t, l)
	d.Date = mustDate(t, "2026-10-29")

	if _, err := f.lessonService(t, bookingNow).UpdateLesson(ctx, l.ID, d); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, mia.ID, tenVisits.ID)
	f.wantPaidBy(t, anna.ID, domain.PassID{})
	f.wantVisits(t, tenVisits, 9)

	// Back to Tuesday: the 10-visit pass is valid then, so it keeps paying.
	d.Date = mustDate(t, "2026-10-27")

	if _, err := f.lessonService(t, bookingNow).UpdateLesson(ctx, l.ID, d); err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, mia.ID, tenVisits.ID)

	// The Tuesday class moves to Wednesday; its lesson of 2026-10-27 follows
	// to 2026-10-28, after the week pass.
	tt := f.timetable(t, bookingNow)

	c, err := tt.CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	follower := f.lessonAt(t, c.ID, "2026-10-27")
	booked := f.book(t, bookingNow, follower, f.anna)
	f.wantPaidBy(t, booked.ID, annasWeek.ID)

	moved := f.classData()
	moved.Weekday = time.Wednesday

	if _, err := tt.UpdateClass(ctx, c.ID, moved); err != nil {
		t.Fatal(err)
	}

	if got := f.lesson(t, follower.ID); got.SlotDate != mustDate(t, "2026-10-28") {
		t.Fatalf("the lesson did not follow: %+v", got)
	}

	f.wantPaidBy(t, booked.ID, domain.PassID{})
}

// TestWaitlistAcceptRedeems accepts an invite: the booking uses a pass
// through the ledger like any other booking.
func TestWaitlistAcceptRedeems(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)
	pass := f.sell(t, f.mia, f.tenVisits, "")
	l := f.fullLesson(t, 1)
	anna := f.principal(t, f.anna)

	place, err := f.waitlistService(t, bookingNow).Join(ctx, anna, l.ID, f.mia.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	stella := f.stella(t)
	if _, err := f.bookingService(t, bookingNow).Cancel(ctx, f.principal(t, stella), f.bookingIn(t, l, stella).ID); err != nil {
		t.Fatal(err)
	}

	b, err := f.waitlistService(t, "2026-10-21T11:00:00Z").Accept(ctx, anna, place.Entry.ID)
	if err != nil {
		t.Fatal(err)
	}

	f.wantPaidBy(t, b.ID, pass.ID)
	f.wantVisits(t, pass, 9)
}
