package servicetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Integrity runs the scenarios of the integrity checks (db check, ADR 0020)
// on fresh stores from newStore: a seeded demo school and a school whose
// data only the services wrote have no finding, which is the strongest test
// that the checks agree with the services; every inconsistency, written
// through the store, is found as it is, and nothing beside it.
func Integrity(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	for _, school := range []seed.School{seed.SchoolProgramme, seed.SchoolWerkstatt} {
		t.Run("Demo/"+string(school), func(t *testing.T) {
			t.Parallel()
			integrityDemo(t, newStore(t), school)
		})
	}

	for name, fn := range map[string]func(t *testing.T, w *world){
		"ServicesAgree":   integrityServicesAgree,
		"Passes":          integrityPasses,
		"Capacity":        integrityCapacity,
		"Tombstones":      integrityTombstones,
		"ReceiptCounters": integrityReceiptCounters,
		"Subset":          integritySubset,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, newWorld(t, newStore(t)))
		})
	}
}

// integrityDemo: the demo school, as seed.LoadDemo writes it, has no
// finding.
func integrityDemo(t *testing.T, st store.Store, school seed.School) {
	if err := seed.LoadDemoSchoolAt(t.Context(), st, seed.FixedDemoAnchor, school); err != nil {
		t.Fatal(err)
	}

	got, err := service.NewIntegrityService(st).Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Errorf("the demo school has findings:\n%s", findingsText(got))
	}
}

// check runs the checks on the world's store.
func (w *world) check(checks ...service.IntegrityCheck) []service.IntegrityFinding {
	w.t.Helper()

	got, err := service.NewIntegrityService(w.st).Check(w.t.Context(), checks...)
	if err != nil {
		w.t.Fatalf("check %v: %v", checks, err)
	}

	return got
}

// wantFindings compares the findings with want, in their order.
func wantFindings(t *testing.T, got []service.IntegrityFinding, want ...service.IntegrityFinding) {
	t.Helper()

	if !slices.Equal(got, want) {
		t.Errorf("findings:\n%s\nwant:\n%s", findingsText(got), findingsText(want))
	}
}

func findingsText(fs []service.IntegrityFinding) string {
	if len(fs) == 0 {
		return "  (none)"
	}

	var s string
	for _, f := range fs {
		s += fmt.Sprintf("  %s %s %s: %s\n", f.Check, f.Entity, f.ID, f.Message)
	}

	return s
}

// stubPDF renders every receipt as the same stand-in PDF.
type stubPDF struct{}

func (stubPDF) Render(context.Context, []byte) ([]byte, error) { return []byte("%PDF-stub"), nil }

// agreeServices are the services the ServicesAgree scenario writes
// through.
type agreeServices struct {
	persons      *service.PersonService
	passes       *service.PassService
	participants *service.ParticipantService
	bookings     *service.BookingService
	lessons      *service.LessonService
	waitlist     *service.WaitlistService
	timetable    *service.TimetableService
	catalog      *service.CatalogService
	auth         *service.AuthService
}

func (w *world) agreeServices() agreeServices {
	clk := clock.NewFixed(now)
	mails := service.Mails{Renderer: w.renderer(), AppURL: "https://school.example.org/"}

	return agreeServices{
		persons:      w.persons(),
		passes:       service.NewPassService(w.st, clk, w.seq, service.NewReceiptService(w.st, clk, w.seq, mails, stubPDF{})),
		participants: service.NewParticipantService(w.st, clk, w.seq, mails),
		bookings:     service.NewBookingService(w.st, clk, w.seq, mails),
		lessons:      service.NewLessonService(w.st, clk, w.seq, mails),
		waitlist:     service.NewWaitlistService(w.st, clk, w.seq, mails),
		timetable:    service.NewTimetableService(w.st, clk, w.seq, mails),
		catalog:      service.NewCatalogService(w.st, w.seq),
		auth:         w.auth(),
	}
}

// must fails the test on err.
func must[T any](t *testing.T, what string) func(v T, err error) T {
	return func(v T, err error) T {
		t.Helper()

		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}

		return v
	}
}

// sell sells a 10-visit pass of the world's type to owner, paid by payer
// if set, valid from the first of October.
func (s agreeServices) sell(t *testing.T, w *world, owner, payer domain.Person) domain.Pass {
	t.Helper()

	v := must[service.PassView](t, "sell")(s.passes.Sell(t.Context(), w.office, owner.ID, service.PassSale{
		PassTypeID: w.passType, PayerID: payer.ID, ValidFrom: domain.Date{Year: 2026, Month: time.October, Day: 1},
	}))

	return v.Pass
}

// add books p into l as the office, beyond the capacity if over.
func (s agreeServices) add(t *testing.T, w *world, l domain.Lesson, p domain.Person, over bool) domain.Booking {
	t.Helper()

	return must[domain.Booking](t, "add participant")(s.participants.Add(t.Context(), w.office, l.ID,
		service.NewParticipant{PersonID: p.ID, ConfirmOverCapacity: over}))
}

// withCapacity stores a lesson of an hour from start with the capacity.
func (w *world) withCapacity(start time.Time, capacity int) domain.Lesson {
	w.t.Helper()

	l := w.lesson(domain.ClassID{}, start)
	l.Capacity = capacity
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, l) })

	return l
}

// integrityServicesAgree: a school whose data only the services wrote has
// no finding: after a merge of a person with everything a merge moves or
// keeps, an anonymization, a lesson the school canceled, cancellations in
// time and late, bookings beyond the capacity with the staff's override,
// and receipts of every sale.
func integrityServicesAgree(t *testing.T, w *world) {
	ctx := t.Context()
	s := w.agreeServices()
	admin := newAdminFixture(w).admin

	// Sven twice: the survivor with an account, the duplicate with an
	// account that the admin disables before the merge.
	survivor := w.person("Sven", "Example", "sven@example.org")
	survivorAcc := w.account(survivor, "sven@example.org")
	merged := w.person("Sven", "Example", "sven.dup@example.org")
	mergedAcc := w.account(merged, "sven.dup@example.org")
	mergedP := w.principal(mergedAcc)

	guardian, kid := w.person("Gerd", "Example", ""), w.person("Kim", "Example", "")
	must[domain.Person](t, "guardian")(s.persons.AddGuardian(ctx, merged.ID, guardian.ID))
	must[domain.Person](t, "dependent")(s.persons.AddGuardian(ctx, kid.ID, merged.ID))
	must[service.ClassMemberEntry](t, "member")(s.timetable.AddClassMember(ctx, w.office,
		domain.ClassMember{ClassID: w.classes[0], PersonID: merged.ID}))
	must[service.StaffEntry](t, "staff")(s.catalog.CreateStaff(ctx, service.StaffData{
		DisplayName: "Sven E.", Kind: domain.StaffAccompanist, PersonID: merged.ID, Active: true,
	}))

	other := w.person("Olli", "Other", "")
	if err := s.persons.DismissDuplicate(ctx, w.office, merged.ID, other.ID); err != nil {
		t.Fatal(err)
	}

	// An earlier duplicate, merged into the duplicate, goes along.
	earlier := w.person("Sven", "Example", "")
	must[service.MergeSummary](t, "earlier merge")(s.persons.Merge(ctx, w.office,
		service.MergeRequest{SurvivorID: merged.ID, MergedID: earlier.ID}, true))

	// The duplicate's pass, and one it paid for its kid.
	mergedPass := s.sell(t, w, merged, domain.Person{})
	s.sell(t, w, kid, merged)

	// Its bookings: one of a lesson both are booked into (the merge cancels
	// it), one of a lesson that started (history), one of a lesson to come,
	// each paid by its pass, and one it canceled in time.
	started := w.lesson(domain.ClassID{}, now.Add(-30*time.Minute))
	s.add(t, w, w.lessons[0], survivor, false)
	s.add(t, w, w.lessons[0], merged, false)
	s.add(t, w, started, survivor, false)
	s.add(t, w, started, merged, false)
	s.add(t, w, w.lessons[1], merged, false)

	canceled := must[domain.Booking](t, "book")(s.bookings.Book(ctx, mergedP, w.lessons[2].ID, merged.ID))
	must[domain.Booking](t, "cancel")(s.bookings.Cancel(ctx, mergedP, canceled.ID))

	// It waits for a full lesson.
	full := w.withCapacity(now.Add(96*time.Hour), 1)
	s.add(t, w, full, other, false)
	must[service.WaitlistPlace](t, "join")(s.waitlist.Join(ctx, mergedP, full.ID, merged.ID, false))

	if p := w.passOf(mergedPass.ID); p.VisitsRemaining != 7 {
		t.Fatalf("the duplicate's pass has %d visits left, want 7 (three bookings paid)", p.VisitsRemaining)
	}

	must[service.AccountEntry](t, "disable")(s.auth.SetAccountDisabled(ctx, admin, mergedAcc.ID, true))

	sum := must[service.MergeSummary](t, "merge")(s.persons.Merge(ctx, w.office,
		service.MergeRequest{SurvivorID: survivor.ID, MergedID: merged.ID}, true))
	if sum.History != 1 || len(sum.Canceled) != 1 || sum.WaitlistMoved != 1 || sum.PassesMoved != 1 ||
		sum.PaidPassesMoved != 1 || sum.StaffMoved != 1 || sum.AccountMoved {
		t.Fatalf("merge summary %+v", sum)
	}

	if acc, err := w.account1(survivorAcc.ID); err != nil || acc.PersonID != survivor.ID {
		t.Fatalf("the survivor's account %+v, %v", acc, err)
	}

	// Paula: a guardian, a dependent who has another guardian, an account,
	// a membership, a past booking and one to come, both paid by her pass.
	paula := w.person("Paula", "Private", "paula@example.org")
	w.account(paula, "paula@example.org")
	grandma, child, otherParent := w.person("Greta", "Private", ""), w.person("Kai", "Private", ""),
		w.person("Piet", "Private", "")
	must[domain.Person](t, "guardian")(s.persons.AddGuardian(ctx, paula.ID, grandma.ID))
	must[domain.Person](t, "dependent")(s.persons.AddGuardian(ctx, child.ID, paula.ID))
	must[domain.Person](t, "dependent")(s.persons.AddGuardian(ctx, child.ID, otherParent.ID))
	must[service.ClassMemberEntry](t, "member")(s.timetable.AddClassMember(ctx, w.office,
		domain.ClassMember{ClassID: w.classes[1], PersonID: paula.ID}))

	paulasPass := s.sell(t, w, paula, domain.Person{})
	past := w.lesson(domain.ClassID{}, now.Add(-48*time.Hour))
	s.add(t, w, past, paula, false)
	s.add(t, w, w.lessons[1], paula, false)

	anon := must[service.AnonymizeSummary](t, "anonymize")(s.persons.Anonymize(ctx, w.office, paula.ID))
	if anon.BookingsCanceled != 1 || anon.LinksRemoved != 2 || anon.MembershipsEnded != 1 || !anon.AccountDeleted {
		t.Fatalf("anonymize summary %+v", anon)
	}

	if p := w.passOf(paulasPass.ID); p.VisitsRemaining != 9 || p.PersonID != paula.ID {
		t.Fatalf("Paula's pass %+v; want 9 visits left, hers", p)
	}

	// Lena's paid booking of a lesson the school cancels.
	lena := w.person("Lena", "Example", "")
	lenasPass := s.sell(t, w, lena, domain.Person{})
	called := w.lesson(domain.ClassID{}, now.Add(120*time.Hour))
	s.add(t, w, called, lena, false)
	must[service.ScheduledLesson](t, "cancel lesson")(s.lessons.CancelLesson(ctx, w.office, called.ID, "Ill."))

	if p := w.passOf(lenasPass.ID); p.VisitsRemaining != 10 {
		t.Fatalf("Lena's pass has %d visits left, want all 10 back", p.VisitsRemaining)
	}

	// Yara cancels one booking in time and one late, both paid by her pass.
	yara := w.person("Yara", "Example", "yara@example.org")
	yaraP := w.principal(w.account(yara, "yara@example.org"))
	yarasPass := s.sell(t, w, yara, domain.Person{})
	soon := w.lesson(domain.ClassID{}, now.Add(time.Hour))

	for _, l := range []domain.Lesson{w.lessons[1], soon} {
		b := must[domain.Booking](t, "book")(s.bookings.Book(ctx, yaraP, l.ID, yara.ID))
		must[domain.Booking](t, "cancel")(s.bookings.Cancel(ctx, yaraP, b.ID))
	}

	if p := w.passOf(yarasPass.ID); p.VisitsRemaining != 9 {
		t.Fatalf("Yara's pass has %d visits left, want 9 (the late cancellation costs one)", p.VisitsRemaining)
	}

	// A lesson of one place, booked by three with the staff's override.
	small := w.withCapacity(now.Add(144*time.Hour), 1)
	s.add(t, w, small, lena, false)

	if _, err := s.participants.Add(ctx, w.office, small.ID, service.NewParticipant{PersonID: yara.ID}); !errors.Is(err, service.ErrLessonFull) {
		t.Fatalf("booking a full lesson without the override: %v", err)
	}

	s.add(t, w, small, yara, true)
	s.add(t, w, small, survivor, true)

	wantFindings(t, w.check())
}

// integrityPasses: a visit pass whose used visits differ from its
// redemptions, and redemptions of bookings that cost no visit, are found;
// a late cancellation's, a balanced pass and a time pass's are not.
func integrityPasses(t *testing.T, w *world) {
	ana, ben := w.person("Ana", "Example", ""), w.person("Ben", "Example", "")

	// Ana's pass shows three visits used for two bookings.
	short := w.pass(ana, domain.Person{}, 7)
	w.redeem(short, w.booking(w.lessons[0], ana, false))
	w.redeem(short, w.booking(w.lessons[1], ana, false))

	// Ben's pass is balanced: four visits for four bookings, three of which
	// cost none.
	called, calledToo := w.canceledLesson(now.Add(96*time.Hour)), w.canceledLesson(now.Add(120*time.Hour))
	balanced := w.pass(ben, domain.Person{}, 6)
	inTime := w.booking(w.lessons[0], ben, true)
	ofCalled := w.booking(called, ben, false)
	both := w.booking(calledToo, ben, true)
	late := w.booking(w.lessons[1], ben, true)
	late.Late = true
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, late) })

	for _, b := range []domain.Booking{inTime, ofCalled, both, late} {
		w.redeem(balanced, b)
	}

	// A pass without redemptions, and a time pass that paid for a booking.
	w.pass(w.person("Cleo", "Example", ""), domain.Person{}, 10)

	dana := w.person("Dana", "Example", "")
	timePass := domain.Pass{
		ID: newID[domain.PassID](w), PassTypeID: w.passType, PersonID: dana.ID, Kind: domain.PassTime,
		ValidFrom:  domain.Date{Year: 2026, Month: time.October, Day: 1},
		ValidUntil: domain.Date{Year: 2026, Month: time.October, Day: 31},
		Status:     domain.PassActive, Origin: domain.PassAssigned, SoldAt: now.Add(-48 * time.Hour),
	}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, timePass) })
	w.redeem(timePass, w.booking(w.lessons[0], dana, false))

	wantFindings(t, w.check(),
		service.IntegrityFinding{
			Check: service.CheckPasses, Entity: service.EntityPass, ID: short.ID.String(),
			Message: "3 of its visits are used, but it paid for 2 bookings",
		},
		service.IntegrityFinding{
			Check: service.CheckPasses, Entity: service.EntityBooking, ID: inTime.ID.String(),
			Message: "pass " + balanced.ID.String() + " paid for it, but it costs no visit: the booking was canceled in time",
		},
		service.IntegrityFinding{
			Check: service.CheckPasses, Entity: service.EntityBooking, ID: ofCalled.ID.String(),
			Message: "pass " + balanced.ID.String() + " paid for it, but it costs no visit: the school canceled lesson " +
				called.ID.String(),
		},
		service.IntegrityFinding{
			Check: service.CheckPasses, Entity: service.EntityBooking, ID: both.ID.String(),
			Message: "pass " + balanced.ID.String() + " paid for it, but it costs no visit: " +
				"the booking was canceled in time, and the school canceled lesson " + calledToo.ID.String(),
		},
	)
}

// canceledLesson stores a lesson of an hour from start that the school
// canceled.
func (w *world) canceledLesson(start time.Time) domain.Lesson {
	w.t.Helper()

	l := w.lesson(domain.ClassID{}, start)
	l.Status, l.CanceledAt, l.CanceledBy = domain.LessonCanceled, now.Add(-time.Hour), w.office.AccountID
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, l) })

	return l
}

// integrityCapacity: a lesson with more active bookings than places is
// found unless enough of them were booked beyond the capacity on purpose;
// canceled bookings do not count, and a booking with two override events
// counts once.
func integrityCapacity(t *testing.T, w *world) {
	people := make([]domain.Person, 4)
	for i := range people {
		people[i] = w.person("Pat"+strconv.Itoa(i), "Example", "")
	}

	// book books the first n people, active, and the rest canceled; the
	// bookings at the indexes of over get override events (one per index
	// given).
	book := func(l domain.Lesson, n int, over ...int) {
		bookings := make([]domain.Booking, len(people))
		for i, p := range people {
			bookings[i] = w.booking(l, p, i >= n)
		}

		w.write(func(ctx context.Context, tx store.Tx) error {
			var errs []error

			// Every booking has its booked event; only those named are
			// overrides.
			for _, b := range bookings {
				errs = append(errs, tx.Bookings().AddEvent(ctx, domain.BookingEvent{
					ID: newID[domain.BookingEventID](w), BookingID: b.ID, At: b.BookedAt, Status: domain.BookingBooked,
				}))
			}

			for _, i := range over {
				errs = append(errs, tx.Bookings().AddEvent(ctx, domain.BookingEvent{
					ID: newID[domain.BookingEventID](w), BookingID: bookings[i].ID, At: bookings[i].BookedAt,
					Status: domain.BookingBooked, Override: true,
				}))
			}

			return errors.Join(errs...)
		})
	}

	// Three active bookings for two places, none of them an override.
	overbooked := w.withCapacity(now.Add(96*time.Hour), 2)
	book(overbooked, 3)

	// Three for one place, two of them overrides: explained.
	explained := w.withCapacity(now.Add(120*time.Hour), 1)
	book(explained, 3, 1, 2)

	// Three for one place, one override recorded twice, and the canceled
	// booking's override does not count.
	partly := w.withCapacity(now.Add(144*time.Hour), 1)
	book(partly, 3, 2, 2, 3)

	// Two active bookings for two places, and two canceled.
	booked := w.withCapacity(now.Add(168*time.Hour), 2)
	book(booked, 2)

	wantFindings(t, w.check(),
		service.IntegrityFinding{
			Check: service.CheckCapacity, Entity: service.EntityLesson, ID: overbooked.ID.String(),
			Message: "3 active bookings for 2 places, and only 0 of them were booked beyond the capacity on purpose",
		},
		service.IntegrityFinding{
			Check: service.CheckCapacity, Entity: service.EntityLesson, ID: partly.ID.String(),
			Message: "3 active bookings for 1 places, and only 1 of them were booked beyond the capacity on purpose",
		},
	)
}

// integrityTombstones: whatever still refers to a merged person, and what
// anonymizing ends or the services refuse afterwards of an anonymized one,
// is found; what both keep is not.
func integrityTombstones(t *testing.T, w *world) {
	survivor := w.person("Sara", "Example", "")
	merged := w.person("Sara", "Example", "")
	guardian, kid, other := w.person("Gerd", "Example", ""), w.person("Kim", "Example", ""), w.person("Olli", "Other", "")
	earlier := w.person("Sara", "Example", "")

	// Everything that refers to the merged person, of every kind.
	booking := w.booking(w.lessons[0], merged, true) // canceled, and still the merged person's
	entry := w.waits(w.lessons[1], merged)
	w.member(w.classes[0], merged)
	w.link(merged, kid)
	w.link(guardian, merged)
	pass := w.pass(merged, domain.Person{}, 10)
	paid := w.pass(kid, merged, 10)
	staff := domain.Staff{
		ID: newID[domain.StaffID](w), PersonID: merged.ID, DisplayName: "Sara E.", Kind: domain.StaffAccompanist,
		Active: true,
	}
	account := w.account(merged, "sara@example.org")
	w.write(func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Staff().Create(ctx, staff),
			tx.DuplicateDismissals().Create(ctx,
				domain.NewDuplicateDismissal(merged.ID, other.ID, w.office.AccountID, now.Add(-time.Hour))),
		)
	})

	earlier.MergedInto = merged.ID
	w.update(earlier)

	merged.MergedInto = survivor.ID
	w.update(merged)

	// Another tombstone keeps what a merge leaves with it: a disabled
	// account, a closed waiting-list entry and its receipts.
	kept := w.person("Sara", "Example", "")
	keptAcc := w.account(kept, "sara.kept@example.org")
	keptAcc.DisabledAt = now.Add(-time.Hour)
	closed := w.waits(w.lessons[2], kept)
	closed.Status = domain.WaitlistWithdrawn
	w.write(func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Accounts().Update(ctx, keptAcc), tx.Waitlist().Update(ctx, closed))
	})
	w.receipt(kept)

	kept.MergedInto = survivor.ID
	w.update(kept)

	// The anonymized person: an active booking of a lesson to come and
	// passes sold after it was anonymized are found; a canceled booking of
	// a lesson to come, an active one of a past lesson and a pass sold
	// before are not.
	anon := w.person("Paula", "Private", "")
	ahead := w.booking(w.lessons[2], anon, false)
	w.booking(w.lessons[1], anon, true)
	w.bookingAs(w.lesson(domain.ClassID{}, now.Add(-48*time.Hour)), anon, domain.BookingSourceSelf, domain.BookingAttended)
	w.pass(anon, domain.Person{}, 10)
	soldAfter := w.passSoldAt(anon, domain.Person{}, now.Add(time.Hour))
	paidAfter := w.passSoldAt(kid, anon, now.Add(time.Hour))

	w.update(anon.Anonymize(now))

	ref := func(p domain.Person, what, format string, id fmt.Stringer) service.IntegrityFinding {
		return service.IntegrityFinding{
			Check: service.CheckTombstones, Entity: service.EntityPerson, ID: p.ID.String(),
			Message: "the person is " + what + ", but " + fmt.Sprintf(format, id) + " still refers to it",
		}
	}

	wantFindings(t, w.check(),
		ref(merged, "merged", "booking %s", booking.ID),
		ref(merged, "merged", "open waiting-list entry %s", entry.ID),
		ref(merged, "merged", "membership of class %s", w.classes[0]),
		ref(merged, "merged", "guardian link to dependent %s", kid.ID),
		ref(merged, "merged", "guardian link from guardian %s", guardian.ID),
		ref(merged, "merged", "pass %s it owns", pass.ID),
		ref(merged, "merged", "pass %s it paid for", paid.ID),
		ref(merged, "merged", "staff record %s", staff.ID),
		ref(merged, "merged", "account %s", account.ID),
		ref(merged, "merged", "duplicate dismissal with person %s", other.ID),
		ref(merged, "merged", "person %s merged into it", earlier.ID),
		ref(anon, "anonymized", "booking %s", ahead.ID),
		ref(anon, "anonymized", "pass %s it owns", soldAfter.ID),
		ref(anon, "anonymized", "pass %s it paid for", paidAfter.ID),
	)
}

// passSoldAt stores a visit pass of owner sold to payer (the owner if
// zero) at the instant.
func (w *world) passSoldAt(owner, payer domain.Person, at time.Time) domain.Pass {
	w.t.Helper()

	p := domain.Pass{
		ID: newID[domain.PassID](w), PassTypeID: w.passType, PersonID: owner.ID, Kind: domain.PassVisits,
		VisitsTotal: 10, VisitsRemaining: 10, ValidFrom: domain.Date{Year: 2026, Month: time.October, Day: 1},
		Status: domain.PassActive, Origin: domain.PassSold, PayerID: payer.ID, PricePaid: 12000, SoldAt: at,
	}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, p) })

	return p
}

// receiptNumbered stores a receipt of the year with the sequence without
// taking a number from the year's counter.
func (w *world) receiptNumbered(payer domain.Person, year, seq int) domain.Receipt {
	w.t.Helper()

	line := domain.NewReceiptLine(1, "10-visit pass", 1, 12000, 1900, domain.TaxStandard)
	r := domain.Receipt{
		ID: newID[domain.ReceiptID](w), Number: domain.ReceiptNumber("R", year, seq), Year: year, Sequence: seq,
		Kind: domain.ReceiptSale, IssuedAt: time.Date(year, time.October, 19, 10, 0, 0, 0, time.UTC),
		IssueDate: domain.Date{Year: year, Month: time.October, Day: 19}, Locale: "en",
		Issuer: domain.ReceiptIssuer{
			Name: "Studio Example", Address: "Example Street 1", TaxID: "00/000/00000", Email: "office@example.net",
		},
		TaxMode: domain.TaxStandard, PayerID: payer.ID, RecipientName: payer.GivenName + " " + payer.FamilyName,
		PaymentMethod: domain.PaymentCash, Currency: "EUR", Net: line.Net, VAT: line.VAT, Gross: line.Gross,
		CreatedBy: w.office.AccountID, Lines: []domain.ReceiptLine{line},
	}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Receipts().Create(ctx, r) })

	return r
}

// next takes the next number of the year's receipt counter.
func (w *world) next(year int) {
	w.t.Helper()

	w.write(func(ctx context.Context, tx store.Tx) error {
		_, err := tx.ReceiptCounters().Next(ctx, year)

		return err
	})
}

// integrityReceiptCounters: a year whose counter is behind the highest
// number issued, or that has receipts but no counter, is found; a counter
// at or ahead of its receipts is not.
func integrityReceiptCounters(t *testing.T, w *world) {
	payer := w.person("Rita", "Example", "")

	// 2026: numbers 1 and 2 from the counter, and number 5 beside it.
	w.receipt(payer)
	w.receipt(payer)
	w.receiptNumbered(payer, 2026, 5)

	// 2025: a receipt, and no counter.
	w.receiptNumbered(payer, 2025, 1)

	// 2024: the counter at the receipt's number; 2027: a counter ahead,
	// without receipts.
	w.next(2024)
	w.receiptNumbered(payer, 2024, 1)
	w.next(2027)
	w.next(2027)

	wantFindings(t, w.check(),
		service.IntegrityFinding{
			Check: service.CheckReceiptCounters, Entity: service.EntityReceiptYear, ID: "2025",
			Message: "the counter is at 0, but receipt number 1 was issued: the next receipt would repeat a number",
		},
		service.IntegrityFinding{
			Check: service.CheckReceiptCounters, Entity: service.EntityReceiptYear, ID: "2026",
			Message: "the counter is at 2, but receipt number 5 was issued: the next receipt would repeat a number",
		},
	)
}

// integritySubset: the checks given run, in their order, each once; the
// others find nothing even where they would.
func integritySubset(t *testing.T, w *world) {
	pia := w.person("Pia", "Example", "")
	pass := w.pass(pia, domain.Person{}, 9) // one visit used, no redemption
	w.receiptNumbered(pia, 2025, 1)

	small := w.withCapacity(now.Add(96*time.Hour), 1)
	w.booking(small, pia, false)
	w.booking(small, w.person("Max", "Example", ""), false)

	passes := service.IntegrityFinding{
		Check: service.CheckPasses, Entity: service.EntityPass, ID: pass.ID.String(),
		Message: "1 of its visits are used, but it paid for 0 bookings",
	}
	capacity := service.IntegrityFinding{
		Check: service.CheckCapacity, Entity: service.EntityLesson, ID: small.ID.String(),
		Message: "2 active bookings for 1 places, and only 0 of them were booked beyond the capacity on purpose",
	}
	counters := service.IntegrityFinding{
		Check: service.CheckReceiptCounters, Entity: service.EntityReceiptYear, ID: "2025",
		Message: "the counter is at 0, but receipt number 1 was issued: the next receipt would repeat a number",
	}

	wantFindings(t, w.check(service.CheckCapacity), capacity)
	wantFindings(t, w.check(service.CheckReceiptCounters, service.CheckCapacity, service.CheckReceiptCounters),
		capacity, counters)
	wantFindings(t, w.check(service.CheckTombstones))
	wantFindings(t, w.check(), passes, capacity, counters)
	wantFindings(t, w.check(service.IntegrityChecks()...), passes, capacity, counters)
}
