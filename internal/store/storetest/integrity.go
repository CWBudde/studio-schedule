package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// visitPass creates a visit pass of p with total visits of which remaining
// are left, assigned at soldAt.
func (e *env) visitPass(pt domain.PassType, p domain.Person, total, remaining int, soldAt time.Time) domain.Pass {
	e.t.Helper()

	pass := e.newPass(pt, p, remaining, soldAt)
	pass.VisitsTotal = total
	valid(e.t, pass)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, pass) })

	return pass
}

// paidPass creates a visit pass of owner sold at soldAt and paid by payer.
func (e *env) paidPass(pt domain.PassType, owner, payer domain.Person, soldAt time.Time) domain.Pass {
	e.t.Helper()

	pass := e.newPass(pt, owner, 10, soldAt)
	pass.Origin, pass.PayerID, pass.PricePaid = domain.PassSold, payer.ID, 12000
	valid(e.t, pass)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, pass) })

	return pass
}

// canceledBooking creates a booking of p canceled a minute after it was
// made, late or in time.
func (e *env) canceledBooking(l domain.Lesson, p domain.Person, late bool) domain.Booking {
	e.t.Helper()

	b := e.newBooking(l, p, t0)
	b.Status, b.CanceledAt, b.Late = domain.BookingCanceled, t0.Add(time.Minute), late
	valid(e.t, b)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

	return b
}

// canceledLesson creates a lesson the school canceled.
func (e *env) canceledLesson(s setting, start time.Time, capacity int) domain.Lesson {
	e.t.Helper()

	l := e.newLesson(s, start, capacity)
	l.Status, l.CanceledAt = domain.LessonCanceled, t0
	valid(e.t, l)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// bookingEvents adds an event to b for each override flag.
func (e *env) bookingEvents(b domain.Booking, overrides ...bool) {
	e.t.Helper()

	for i, override := range overrides {
		ev := domain.BookingEvent{
			ID: newID[domain.BookingEventID](e), BookingID: b.ID, At: t0.Add(time.Duration(i) * time.Minute),
			Status: domain.BookingBooked, Override: override,
		}
		valid(e.t, ev)
		e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().AddEvent(ctx, ev) })
	}
}

// closedEntry creates a waiting-list entry of p for l that is no longer
// open: withdrawn, expired or accepted (with the invite token).
func (e *env) closedEntry(l domain.Lesson, p domain.Person, status domain.WaitlistStatus, token byte) domain.WaitlistEntry {
	e.t.Helper()

	w := e.newEntry(l, p, t0)
	w.Status = status

	if status == domain.WaitlistAccepted {
		w.InvitedAt, w.ExpiresAt, w.InviteTokenHash = t0.Add(time.Minute), t0.Add(time.Hour), [32]byte{31: token}
	}

	valid(e.t, w)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, w) })

	return w
}

// staffOf creates a staff record naming p.
func (e *env) staffOf(p domain.Person) domain.Staff {
	e.t.Helper()

	st := domain.Staff{
		ID: newID[domain.StaffID](e), PersonID: p.ID, DisplayName: p.GivenName, Kind: domain.StaffInstructor,
		Active: true,
	}
	valid(e.t, st)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Staff().Create(ctx, st) })

	return st
}

// member makes p a member of c.
func (e *env) member(c domain.Class, p domain.Person) {
	e.t.Helper()

	m := domain.ClassMember{ClassID: c.ID, PersonID: p.ID}
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Classes().AddMember(ctx, m) })
}

// guardianOf links guardian to dependent.
func (e *env) guardianOf(guardian, dependent domain.Person) {
	e.t.Helper()

	g := domain.Guardian{GuardianID: guardian.ID, DependentID: dependent.ID}
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().AddGuardian(ctx, g) })
}

// dismissal records that a and b are not the same person.
func (e *env) dismissal(a, b domain.Person, by domain.Account) {
	e.t.Helper()

	d := domain.NewDuplicateDismissal(a.ID, b.ID, by.ID, t0)
	valid(e.t, d)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.DuplicateDismissals().Create(ctx, d) })
}

// disabled disables a.
func (e *env) disabled(a domain.Account) domain.Account {
	e.t.Helper()

	a.DisabledAt = t0
	valid(e.t, a)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, a) })

	return a
}

// anonymized anonymizes p at the instant and returns it.
func (e *env) anonymized(p domain.Person, at time.Time) domain.Person {
	e.t.Helper()

	p = p.Anonymize(at)
	valid(e.t, p)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Update(ctx, p) })

	return p
}

func testIntegrity(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	unbalanced := func(e *env) []store.PassBalance {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]store.PassBalance, error) {
			return tx.Passes().ListUnbalanced(ctx)
		})
	}
	stray := func(e *env) []store.StrayRedemption {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]store.StrayRedemption, error) {
			return tx.Redemptions().ListStray(ctx)
		})
	}
	overCapacity := func(e *env) []store.LessonOccupancy {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]store.LessonOccupancy, error) {
			return tx.Bookings().ListOverCapacity(ctx)
		})
	}
	counterStates := func(e *env) []store.ReceiptCounterState {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]store.ReceiptCounterState, error) {
			return tx.ReceiptCounters().ListStates(ctx)
		})
	}
	tombstoneRefs := func(e *env) []store.TombstoneReference {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]store.TombstoneReference, error) {
			return tx.Persons().ListTombstoneReferences(ctx)
		})
	}

	cases(t, newStore, map[string]func(e *env){
		"Empty": func(e *env) {
			wantSlice(e.t, unbalanced(e), nil)
			wantSlice(e.t, stray(e), nil)
			wantSlice(e.t, overCapacity(e), nil)
			wantSlice(e.t, counterStates(e), nil)
			wantSlice(e.t, tombstoneRefs(e), nil)
		},
		"UnbalancedPasses": func(e *env) {
			s := e.setting()
			l := e.lesson(s, t0, 10)
			pt := e.passType("10-visit pass")
			timeType := domain.PassType{
				ID: newID[domain.PassTypeID](e), Name: "30-day pass", Kind: domain.PassTime, ValidityDays: 30, CreatedAt: t0,
			}
			valid(e.t, timeType)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.PassTypes().Create(ctx, timeType) })

			// redeemFor redeems pass for a new booking of l.
			redeemFor := func(pass domain.Pass) {
				e.t.Helper()
				e.redeem(pass, e.booking(l, e.person("Rita", "Redeemer"), t0), t0)
			}

			owner := e.person("Ada", "Owner")

			over := e.visitPass(pt, owner, 10, 9, t0)
			redeemFor(over)
			redeemFor(over)

			balanced := e.visitPass(pt, owner, 10, 8, t0)
			redeemFor(balanced)
			redeemFor(balanced)

			// Used without any redemption.
			unredeemed := e.visitPass(pt, owner, 5, 3, t0)

			// Unused and without redemptions.
			e.visitPass(pt, owner, 10, 10, t0)

			under := e.visitPass(pt, owner, 10, 7, t0)
			redeemFor(under)

			// A time pass uses no visits, whatever pays with it.
			timePass := e.newPass(pt, owner, 0, t0)
			timePass.PassTypeID, timePass.Kind, timePass.VisitsTotal = timeType.ID, domain.PassTime, 0
			timePass.ValidUntil = oct1.AddDays(29)
			valid(e.t, timePass)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, timePass) })
			redeemFor(timePass)

			wantSlice(e.t, unbalanced(e), []store.PassBalance{
				{Pass: over.ID, Used: 1, Redeemed: 2},
				{Pass: unredeemed.ID, Used: 2, Redeemed: 0},
				{Pass: under.ID, Used: 3, Redeemed: 1},
			})
		},
		"StrayRedemptions": func(e *env) {
			s := e.setting()
			scheduled := e.lesson(s, t0, 10)
			canceled := e.canceledLesson(s, t0.Add(time.Hour), 10)
			pass := e.visitPass(e.passType("10-visit pass"), e.person("Ada", "Owner"), 10, 4, t0)
			booker := func() domain.Person { return e.person("Bo", "Booker") }

			inTime := e.canceledBooking(scheduled, booker(), false)
			late := e.canceledBooking(scheduled, booker(), true)
			active := e.booking(scheduled, booker(), t0)
			ofCanceled := e.booking(canceled, booker(), t0)
			bothInTime := e.canceledBooking(canceled, booker(), false)
			lateOfCanceled := e.canceledBooking(canceled, booker(), true)
			// Canceled in time but never redeemed.
			e.canceledBooking(scheduled, booker(), false)

			// Redeemed in the reverse order of the bookings.
			rLateOfCanceled := e.redeem(pass, lateOfCanceled, t0.Add(6*time.Minute))
			rBoth := e.redeem(pass, bothInTime, t0.Add(5*time.Minute))
			rOfCanceled := e.redeem(pass, ofCanceled, t0.Add(4*time.Minute))
			e.redeem(pass, active, t0.Add(3*time.Minute))
			e.redeem(pass, late, t0.Add(2*time.Minute))
			rInTime := e.redeem(pass, inTime, t0.Add(time.Minute))

			wantSlice(e.t, stray(e), []store.StrayRedemption{
				{Redemption: rInTime, Lesson: scheduled.ID, CanceledInTime: true},
				{Redemption: rOfCanceled, Lesson: canceled.ID, LessonCanceled: true},
				{Redemption: rBoth, Lesson: canceled.ID, CanceledInTime: true, LessonCanceled: true},
				{Redemption: rLateOfCanceled, Lesson: canceled.ID, LessonCanceled: true},
			})
		},
		"LessonsOverCapacity": func(e *env) {
			s := e.setting()
			booker := func() domain.Person { return e.person("Bo", "Booker") }

			// Full but not over: canceled bookings don't count.
			full := e.lesson(s, t0, 2)
			e.booking(full, booker(), t0)
			e.booking(full, booker(), t0)
			e.bookingEvents(e.canceledBooking(full, booker(), false), true)

			over := e.lesson(s, t0.Add(time.Hour), 2)
			// Two override events count once.
			e.bookingEvents(e.booking(over, booker(), t0), true, true)
			e.bookingEvents(e.booking(over, booker(), t0), false)
			e.booking(over, booker(), t0)
			// An override on a canceled booking doesn't count.
			e.bookingEvents(e.canceledBooking(over, booker(), true), true)

			e.lesson(s, t0.Add(2*time.Hour), 1)

			// Every booking that is not canceled is active.
			overAll := e.lesson(s, t0.Add(3*time.Hour), 1)
			attended := e.newBooking(overAll, booker(), t0)
			attended.Status = domain.BookingAttended
			valid(e.t, attended)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, attended) })
			e.bookingEvents(attended, false, true)
			e.bookingEvents(e.booking(overAll, booker(), t0), true)

			wantSlice(e.t, overCapacity(e), []store.LessonOccupancy{
				{Lesson: over.ID, Capacity: 2, Active: 3, Overrides: 1},
				{Lesson: overAll.ID, Capacity: 1, Active: 2, Overrides: 2},
			})
		},
		"ReceiptCounterStates": func(e *env) {
			payer := e.person("Paul", "Payer")
			next := func(year int) {
				e.t.Helper()
				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					_, err := tx.ReceiptCounters().Next(ctx, year)

					return err
				})
			}

			// 2027: a counter without receipts.
			next(2027)

			// 2026: a counter at 2 and receipts up to 4.
			next(2026)
			next(2026)
			e.receipt(e.newReceipt(2026, 4, payer))
			e.receipt(e.newReceipt(2026, 1, payer))

			// 2025: receipts without a counter.
			e.receipt(e.newReceipt(2025, 3, payer))
			e.receipt(e.newReceipt(2025, 2, payer))

			wantSlice(e.t, counterStates(e), []store.ReceiptCounterState{
				{Year: 2025, Last: 0, Highest: 3},
				{Year: 2026, Last: 2, Highest: 4},
				{Year: 2027, Last: 1, Highest: 0},
			})
		},
		"TombstoneReferences": func(e *env) { testTombstoneReferences(e, tombstoneRefs) },
	})
}

// testTombstoneReferences gives a merged, an anonymized and a merged and
// anonymized person references of every kind, beside references that do
// not count, and checks which ListTombstoneReferences returns.
func testTombstoneReferences(e *env, list func(*env) []store.TombstoneReference) {
	e.t.Helper()

	anonymizedAt := t0.Add(24 * time.Hour)

	s := e.setting()
	started := e.lesson(s, t0, 10)                      // started before the anonymization
	later := e.lesson(s, t0.Add(48*time.Hour), 10)      // starts after it
	laterStill := e.lesson(s, t0.Add(72*time.Hour), 10) // starts after it too
	c := e.class(s, time.Monday, domain.TimeOfDay{Hour: 17})
	pt := e.passType("10-visit pass")
	office := e.account(e.person("Olga", "Office"), "olga@example.org")

	survivor := e.person("Sven", "Survivor")
	m := e.person("Mia", "Merged")
	a := e.person("Anna", "Anonymized")
	both := e.person("Bea", "Both")
	live := e.person("Lou", "Live")

	ref := func(p domain.Person, anonymized bool, kind store.TombstoneRefKind, id interface{ String() string }) store.TombstoneReference {
		return store.TombstoneReference{Person: p.ID, Anonymized: anonymized, Kind: kind, Ref: id.String()}
	}

	var want []store.TombstoneReference

	// ---- The merged person: every kind counts.
	mBooking1 := e.booking(started, m, t0)
	mBooking2 := e.canceledBooking(later, m, false) // a merged person's canceled booking counts too
	mWaiting := e.entry(later, m, t0)
	mInvited := e.invite(e.entry(laterStill, m, t0), 1, t0.Add(time.Hour))
	e.closedEntry(started, m, domain.WaitlistWithdrawn, 0)
	e.closedEntry(later, m, domain.WaitlistExpired, 0)
	e.closedEntry(laterStill, m, domain.WaitlistAccepted, 2)
	e.member(c, m)

	mKid := e.person("Kim", "Kid")
	e.guardianOf(m, mKid)

	mParent := e.person("Pia", "Parent")
	e.guardianOf(mParent, m)

	mPass := e.pass(pt, m, 5, t0)
	mPaid := e.paidPass(pt, e.person("Pat", "Owner"), m, t0)
	mStaff := e.staffOf(m)
	mAccount := e.account(m, "mia@example.org")

	mOther := e.person("Mia", "Other")
	e.dismissal(survivor, m, office) // m is the pair's second person
	e.dismissal(m, mOther, office)   // and the first

	m = e.merged(m, survivor)
	intoM := e.merged(e.person("Mia", "Earlier"), m)

	// Not of the merged person: a booking it was merged from, now the
	// survivor's.
	moved := e.newBooking(started, survivor, t0)
	moved.MergedFrom = m.ID
	valid(e.t, moved)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, moved) })

	// A merged person's disabled account does not count.
	e.disabled(e.account(e.merged(e.person("Dora", "Disabled"), survivor), "dora@example.org"))

	want = append(want,
		ref(m, false, store.TombstoneBooking, mBooking1.ID),
		ref(m, false, store.TombstoneBooking, mBooking2.ID),
		ref(m, false, store.TombstoneWaitlist, mWaiting.ID),
		ref(m, false, store.TombstoneWaitlist, mInvited.ID),
		ref(m, false, store.TombstoneMember, c.ID),
		ref(m, false, store.TombstoneGuardian, mKid.ID),
		ref(m, false, store.TombstoneDependent, mParent.ID),
		ref(m, false, store.TombstonePass, mPass.ID),
		ref(m, false, store.TombstonePaidPass, mPaid.ID),
		ref(m, false, store.TombstoneStaff, mStaff.ID),
		ref(m, false, store.TombstoneAccount, mAccount.ID),
		ref(m, false, store.TombstoneDismissal, survivor.ID),
		ref(m, false, store.TombstoneDismissal, mOther.ID),
		ref(m, false, store.TombstoneMerged, intoM.ID),
	)

	// ---- The anonymized person: only what anonymizing would have ended.
	aBooking := e.booking(later, a, t0)
	e.canceledBooking(laterStill, a, false) // canceled
	e.booking(started, a, t0)               // of a lesson that had started
	aWaiting := e.entry(later, a, t0)
	e.closedEntry(started, a, domain.WaitlistWithdrawn, 0)
	e.member(c, a)

	aKid := e.person("Kai", "Kid")
	e.guardianOf(a, aKid)

	aParent := e.person("Pam", "Parent")
	e.guardianOf(aParent, a)

	e.pass(pt, a, 5, t0) // sold before the anonymization
	aPass := e.pass(pt, a, 5, t0.Add(48*time.Hour))
	e.paidPass(pt, e.person("Pat", "Owner"), a, t0) // sold before the anonymization
	aPaid := e.paidPass(pt, e.person("Pat", "Owner"), a, t0.Add(48*time.Hour))
	aStaff := e.staffOf(a)
	aAccount := e.disabled(e.account(a, "anna@example.org")) // any account of an anonymized person

	aOther := e.person("Anna", "Other")
	e.dismissal(a, aOther, office)

	a = e.anonymized(a, anonymizedAt)
	// A person merged into an anonymized person is not a merge to move.
	e.merged(e.person("Anna", "Earlier"), a)

	want = append(want,
		ref(a, true, store.TombstoneBooking, aBooking.ID),
		ref(a, true, store.TombstoneWaitlist, aWaiting.ID),
		ref(a, true, store.TombstoneMember, c.ID),
		ref(a, true, store.TombstoneGuardian, aKid.ID),
		ref(a, true, store.TombstoneDependent, aParent.ID),
		ref(a, true, store.TombstonePass, aPass.ID),
		ref(a, true, store.TombstonePaidPass, aPaid.ID),
		ref(a, true, store.TombstoneStaff, aStaff.ID),
		ref(a, true, store.TombstoneAccount, aAccount.ID),
		ref(a, true, store.TombstoneDismissal, aOther.ID),
	)

	// ---- Merged and anonymized: the merged person's rules.
	bothCanceled := e.canceledBooking(later, both, false)
	bothStarted := e.booking(started, both, t0)
	bothPass := e.pass(pt, both, 5, t0) // sold before the anonymization
	e.disabled(e.account(both, "bea@example.org"))

	both = e.merged(both, survivor)
	both = e.anonymized(both, anonymizedAt)
	intoBoth := e.merged(e.person("Bea", "Earlier"), both)

	want = append(want,
		ref(both, false, store.TombstoneBooking, bothCanceled.ID),
		ref(both, false, store.TombstoneBooking, bothStarted.ID),
		ref(both, false, store.TombstonePass, bothPass.ID),
		ref(both, false, store.TombstoneMerged, intoBoth.ID),
	)

	// ---- A live person's references never count.
	e.booking(later, live, t0)
	e.entry(laterStill, live, t0)
	e.member(c, live)
	e.guardianOf(live, e.person("Liv", "Kid"))
	e.guardianOf(e.person("Leo", "Parent"), live)
	e.pass(pt, live, 5, t0)
	e.paidPass(pt, e.person("Pat", "Owner"), live, t0)
	e.staffOf(live)
	e.account(live, "lou@example.org")
	e.dismissal(live, e.person("Lou", "Other"), office)
	e.merged(e.person("Lou", "Earlier"), live)

	wantSlice(e.t, list(e), want)
}
