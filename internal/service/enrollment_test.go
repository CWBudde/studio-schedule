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
)

// The enrollment tests run on the booking fixture at bookingNow (12:00 on
// Wednesday 2026-10-21 in Berlin). The Tuesday 18:00 class of classData
// then has the lessons of 2026-10-27, 11-03, 11-10 and 11-17 (the horizon
// of the demo settings is 28 days).

// addMembers stores memberships directly, without enrollment.
func (f *bookingFixture) addMembers(t *testing.T, c domain.ClassID, ms ...domain.ClassMember) {
	t.Helper()

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		for _, m := range ms {
			m.ClassID = c
			if err := tx.Classes().AddMember(ctx, m); err != nil {
				return err
			}
		}

		return nil
	})
}

// bookingOf returns person's bookings of lesson l, oldest first.
func (f *bookingFixture) bookingOf(t *testing.T, l domain.Lesson, person domain.Person) []domain.Booking {
	t.Helper()

	return slices.DeleteFunc(f.bookingsOf(t, l), func(b domain.Booking) bool { return b.PersonID != person.ID })
}

// wantMemberBooking checks that b is an automatic member booking with one
// booked event by the actor and the override flag.
func (f *bookingFixture) wantMemberBooking(t *testing.T, b domain.Booking, actor domain.AccountID, override bool) {
	t.Helper()

	got, events := f.booking(t, b.ID)
	if got.Source != domain.BookingSourceMember || got.Status != domain.BookingBooked {
		t.Fatalf("booking %+v, want a booked member booking", got)
	}

	if len(events) != 1 || events[0].Status != domain.BookingBooked || events[0].ActorAccountID != actor ||
		events[0].Override != override {
		t.Fatalf("events %+v, want one booked event by %v with override %v", events, actor, override)
	}
}

func TestGenerationBooksMembers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	now := mustInstant(t, bookingNow)
	c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})
	f.addMembers(t, c.ID,
		domain.ClassMember{PersonID: f.mia.ID},
		domain.ClassMember{PersonID: f.tom.ID, Since: mustDate(t, "2026-11-03")})

	gen := NewLessonGenerator(f.st, clock.NewFixed(now), f.seq)

	// A lesson in the past gets no bookings.
	if n, err := gen.Generate(ctx, mustDate(t, "2026-10-20"), mustDate(t, "2026-10-21")); err != nil || n != 1 {
		t.Fatalf("generate the past: %d, %v", n, err)
	}

	if got := f.bookingsOf(t, f.lessonAt(t, c.ID, "2026-10-20")); len(got) != 0 {
		t.Fatalf("past lesson has bookings %+v", got)
	}

	if n, err := gen.GenerateAhead(ctx); err != nil || n != 4 {
		t.Fatalf("generate ahead: %d, %v", n, err)
	}

	for _, slot := range []string{"2026-10-27", "2026-11-03", "2026-11-10", "2026-11-17"} {
		l := f.lessonAt(t, c.ID, slot)

		mia := f.bookingOf(t, l, f.mia)
		if len(mia) != 1 {
			t.Fatalf("%s: Mia's bookings %+v, want one", slot, mia)
		}

		f.wantMemberBooking(t, mia[0], domain.AccountID{}, false)

		// Tom is a member from 2026-11-03 on.
		if tom := f.bookingOf(t, l, f.tom); (len(tom) == 1) != (slot != "2026-10-27") {
			t.Fatalf("%s: Tom's bookings %+v", slot, tom)
		}
	}

	if m := f.mails(t); len(m) != 0 {
		t.Fatalf("generation mailed %+v", m)
	}

	// Mia releases 2026-10-27; a second run creates nothing and keeps the
	// date released.
	svc := f.bookingService(t, bookingNow)
	anna := f.principal(t, f.anna)
	l := f.lessonAt(t, c.ID, "2026-10-27")

	if _, err := svc.Cancel(ctx, anna, f.bookingOf(t, l, f.mia)[0].ID); err != nil {
		t.Fatal(err)
	}

	if n, err := gen.GenerateAhead(ctx); err != nil || n != 0 {
		t.Fatalf("second run: %d, %v", n, err)
	}

	mia := f.bookingOf(t, l, f.mia)
	if len(mia) != 1 || mia[0].Status != domain.BookingCanceled {
		t.Fatalf("released date: %+v, want the canceled booking only", mia)
	}

	// Taking it back is a normal booking: a new row of her own.
	back, err := svc.Book(ctx, anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	if back.Source != domain.BookingSourceSelf || back.ID == mia[0].ID {
		t.Fatalf("taken back: %+v", back)
	}

	if got := f.bookingOf(t, l, f.mia); len(got) != 2 {
		t.Fatalf("Mia's bookings %+v, want the released and the new one", got)
	}
}

func TestGenerationOverridesCapacityForMembers(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})
	// More members than places: the service refuses this, older data may
	// have it.
	c.Capacity = 1
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Classes().Update(ctx, c) })
	f.addMembers(t, c.ID, domain.ClassMember{PersonID: f.mia.ID}, domain.ClassMember{PersonID: f.tom.ID})

	if _, err := NewLessonGenerator(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq).GenerateAhead(t.Context()); err != nil {
		t.Fatal(err)
	}

	bookings := f.bookingsOf(t, f.lessonAt(t, c.ID, "2026-10-27"))
	if len(bookings) != 2 {
		t.Fatalf("bookings %+v, want both members", bookings)
	}

	overrides := 0

	for _, b := range bookings {
		_, events := f.booking(t, b.ID)
		if len(events) == 1 && events[0].Override {
			overrides++
		}
	}

	if overrides != 1 {
		t.Fatalf("%d overrides, want the second booking only", overrides)
	}
}

func TestAddClassMemberBooksLessons(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.timetable(t, bookingNow)
	olga := f.principal(t, f.olga)

	c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	// A lesson that has started, one the school canceled (members are
	// booked into it all the same, as its bookings stay; ADR 0009), one Mia
	// has released, and one that is full.
	past := domain.Date{Year: 2026, Month: time.October, Day: 20}
	if _, err := NewLessonGenerator(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq).Generate(ctx, past, past.AddDays(1)); err != nil {
		t.Fatal(err)
	}

	if _, err := f.lessonService(t, bookingNow).CancelLesson(ctx, olga, f.lessonAt(t, c.ID, "2026-11-03").ID, ""); err != nil {
		t.Fatal(err)
	}

	released := f.lessonAt(t, c.ID, "2026-11-10")
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return tx.Bookings().Create(ctx, domain.Booking{
			ID: ids.New[domain.BookingID](f.seq), LessonID: released.ID, PersonID: f.mia.ID,
			Status: domain.BookingCanceled, Source: domain.BookingSourceMember, Released: true,
			BookedAt: mustInstant(t, "2026-10-01T10:00:00Z"), CanceledAt: mustInstant(t, "2026-10-02T10:00:00Z"),
		})
	})

	full := f.lessonAt(t, c.ID, "2026-11-17")
	f.book(t, full, full.Capacity)

	mails := len(f.mails(t))

	m, err := svc.AddClassMember(ctx, olga, domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID})
	if err != nil {
		t.Fatal(err)
	}

	if m.Person.ID != f.mia.ID {
		t.Fatalf("added %+v", m)
	}

	actor := f.accountOf(t, f.olga.ID)

	want := map[string]int{"2026-10-20": 0, "2026-10-27": 1, "2026-11-03": 1, "2026-11-10": 1, "2026-11-17": 1}
	for slot, n := range want {
		if got := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia); len(got) != n {
			t.Errorf("%s: Mia's bookings %+v, want %d", slot, got, n)
		}
	}

	if b := f.bookingOf(t, released, f.mia); len(b) != 1 || b[0].Status != domain.BookingCanceled {
		t.Errorf("released date %+v, want it to stay released", b)
	}

	f.wantMemberBooking(t, f.bookingOf(t, f.lessonAt(t, c.ID, "2026-10-27"), f.mia)[0], actor, false)
	f.wantMemberBooking(t, f.bookingOf(t, full, f.mia)[0], actor, true)

	if got := len(f.mails(t)); got != mails {
		t.Errorf("%d mails queued, want none", got-mails)
	}

	// The school restores the canceled lesson: Mia is booked like everybody
	// who was booked before the cancellation.
	if _, err := f.lessonService(t, bookingNow).RestoreLesson(ctx, f.lessonAt(t, c.ID, "2026-11-03").ID, false); err != nil {
		t.Fatal(err)
	}

	if b := f.bookingOf(t, f.lessonAt(t, c.ID, "2026-11-03"), f.mia); len(b) != 1 || b[0].Status != domain.BookingBooked {
		t.Errorf("restored lesson: Mia's bookings %+v", b)
	}

	// Tom from 2026-11-10 on.
	if _, err := svc.AddClassMember(ctx, olga, domain.ClassMember{
		ClassID: c.ID, PersonID: f.tom.ID, Since: mustDate(t, "2026-11-10"),
	}); err != nil {
		t.Fatal(err)
	}

	for slot, n := range map[string]int{"2026-10-27": 0, "2026-11-10": 1, "2026-11-17": 1} {
		if got := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.tom); len(got) != n {
			t.Errorf("%s: Tom's bookings %+v, want %d", slot, got, n)
		}
	}
}

func TestRejoiningMemberIsBookedAgain(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	c := f.memberClass(t)
	svc := f.timetable(t, bookingNow)
	olga := f.principal(t, f.olga)

	// Mia releases 2026-10-27, then leaves the class and rejoins.
	released := f.lessonAt(t, c.ID, "2026-10-27")
	if _, err := f.bookingService(t, bookingNow).Cancel(ctx, f.principal(t, f.anna), f.bookingOf(t, released, f.mia)[0].ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.RemoveClassMember(ctx, olga, c.ID, f.mia.ID); err != nil {
		t.Fatal(err)
	}

	ended := f.bookingOf(t, f.lessonAt(t, c.ID, "2026-11-03"), f.mia)
	if len(ended) != 1 || ended[0].Released || ended[0].Status != domain.BookingCanceled {
		t.Fatalf("after leaving: %+v, want a booking canceled without releasing the date", ended)
	}

	if b := f.bookingOf(t, released, f.mia); len(b) != 1 || !b[0].Released {
		t.Fatalf("released date %+v, want it released", b)
	}

	if _, err := svc.AddClassMember(ctx, olga, domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID}); err != nil {
		t.Fatal(err)
	}

	// The released date stays released; the others are booked again, as
	// new bookings next to the ended ones.
	if b := f.bookingOf(t, released, f.mia); len(b) != 1 || b[0].Status != domain.BookingCanceled {
		t.Errorf("released date %+v, want it to stay released", b)
	}

	for _, slot := range []string{"2026-11-03", "2026-11-10", "2026-11-17"} {
		b := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia)
		if len(b) != 2 || b[0].Status != domain.BookingCanceled || b[1].Status != domain.BookingBooked ||
			b[1].Source != domain.BookingSourceMember {
			t.Errorf("%s: Mia's bookings %+v, want the ended and a new member booking", slot, b)
		}
	}
}

func TestAddClassMemberRefusesRetiredClass(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.timetable(t, bookingNow)

	d := f.classData()
	d.Retired = true

	c, err := svc.CreateClass(ctx, f.term.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.AddClassMember(ctx, f.principal(t, f.olga), domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID})
	if !errors.Is(err, ErrClassRetired) || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("retired class: %v, want ErrClassRetired", err)
	}

	if ms, err := svc.ClassMembers(ctx, c.ID); err != nil || len(ms) != 0 {
		t.Fatalf("members %+v, %v; want none", ms, err)
	}
}

func TestClassEditKeepsMemberOverrides(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.timetable(t, bookingNow)

	d := f.classData()
	d.Capacity = 3

	c, err := svc.CreateClass(ctx, f.term.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	// The lesson of 2026-10-27 is full when Mia joins: she is booked
	// beyond its capacity.
	full := f.lessonAt(t, c.ID, "2026-10-27")
	f.book(t, full, 3)

	if _, err := svc.AddClassMember(ctx, f.principal(t, f.olga), domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID}); err != nil {
		t.Fatal(err)
	}

	// A new start time with the same capacity moves the lesson along.
	d.StartTime = domain.TimeOfDay{Hour: 18, Minute: 30}
	if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
		t.Fatalf("edit with the same capacity: %v", err)
	}

	if l := f.lesson(t, full.ID); l.StartsAt.Equal(full.StartsAt) || len(f.bookingsOf(t, l)) != 4 {
		t.Fatalf("lesson %+v did not move with its 4 bookings", l)
	}

	// Lowering the capacity below a lesson's bookings is still refused.
	d.Capacity = 2
	_, err = svc.UpdateClass(ctx, c.ID, d)
	wantFieldError(t, err, "capacity")
}

func TestClassCapacityLimitsMembers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.timetable(t, bookingNow)
	olga := f.principal(t, f.olga)

	d := f.classData()
	d.Capacity = 2

	c, err := svc.CreateClass(ctx, f.term.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range []domain.Person{f.mia, f.tom} {
		if _, err := svc.AddClassMember(ctx, olga, domain.ClassMember{ClassID: c.ID, PersonID: p.ID}); err != nil {
			t.Fatal(err)
		}
	}

	_, err = svc.AddClassMember(ctx, olga, domain.ClassMember{ClassID: c.ID, PersonID: f.sam.ID})
	if !errors.Is(err, ErrClassFull) || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("beyond capacity: %v, want ErrClassFull", err)
	}

	// A member twice is a duplicate, full or not.
	_, err = svc.AddClassMember(ctx, olga, domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID})
	if !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("twice: %v, want ErrDuplicate", err)
	}

	d.Capacity = 1
	_, err = svc.UpdateClass(ctx, c.ID, d)
	wantFieldError(t, err, "capacity")

	if got := f.lessonAt(t, c.ID, "2026-10-27"); got.Capacity != 2 {
		t.Fatalf("lesson capacity %d after the refused edit", got.Capacity)
	}

	d.Capacity = 3
	if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddClassMember(ctx, olga, domain.ClassMember{ClassID: c.ID, PersonID: f.sam.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveClassMemberCancelsBookings(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	olga := f.principal(t, f.olga)

	c, err := f.timetable(t, bookingNow).CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range []domain.Person{f.mia, f.tom} {
		if _, err := f.timetable(t, bookingNow).AddClassMember(ctx, olga, domain.ClassMember{ClassID: c.ID, PersonID: p.ID}); err != nil {
			t.Fatal(err)
		}
	}

	// Mia released 2026-11-03 and took it back with a booking of her own.
	anna := f.principal(t, f.anna)
	bookings := f.bookingService(t, bookingNow)
	retaken := f.lessonAt(t, c.ID, "2026-11-03")

	released := f.bookingOf(t, retaken, f.mia)[0]
	if _, err := bookings.Cancel(ctx, anna, released.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := bookings.Book(ctx, anna, retaken.ID, f.mia.ID); err != nil {
		t.Fatal(err)
	}

	mails := len(f.mails(t))

	// Half an hour before the lesson of 2026-10-27: a member's own
	// cancellation would be late, the office's is not. The office removes
	// Mia once that lesson has started: it keeps her booking.
	if err := f.timetable(t, "2026-10-27T17:30:00Z").RemoveClassMember(ctx, olga, c.ID, f.mia.ID); err != nil {
		t.Fatal(err)
	}

	actor := f.accountOf(t, f.olga.ID)

	if b := f.bookingOf(t, f.lessonAt(t, c.ID, "2026-10-27"), f.mia); len(b) != 1 || b[0].Status != domain.BookingBooked {
		t.Errorf("started lesson: %+v, want the booking kept", b)
	}

	for _, slot := range []string{"2026-11-03", "2026-11-10", "2026-11-17"} {
		for _, b := range f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia) {
			got, events := f.booking(t, b.ID)
			if got.Status != domain.BookingCanceled || got.Late {
				t.Errorf("%s: %+v, want canceled, not late", slot, got)
			}

			if b.ID == released.ID {
				continue // released by Mia before
			}

			last := events[len(events)-1]
			if last.Status != domain.BookingCanceled || last.ActorAccountID != actor || last.Late {
				t.Errorf("%s: last event %+v, want the office's cancellation", slot, last)
			}
		}

		if tom := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.tom); len(tom) != 1 || tom[0].Status != domain.BookingBooked {
			t.Errorf("%s: Tom's bookings %+v, want them kept", slot, tom)
		}
	}

	if got := len(f.mails(t)); got != mails {
		t.Errorf("%d mails queued, want none", got-mails)
	}

	if err := f.timetable(t, bookingNow).RemoveClassMember(ctx, olga, c.ID, f.mia.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("remove twice: %v, want ErrNotFound", err)
	}
}

// memberClass creates the Tuesday class of classData at bookingNow with Mia
// as a member, so its four lessons have her member bookings.
func (f *bookingFixture) memberClass(t *testing.T) TimetableClass {
	t.Helper()

	svc := f.timetable(t, bookingNow)

	c, err := svc.CreateClass(t.Context(), f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddClassMember(t.Context(), f.principal(t, f.olga), domain.ClassMember{
		ClassID: c.ID, PersonID: f.mia.ID,
	}); err != nil {
		t.Fatal(err)
	}

	return c
}

func (f *bookingFixture) slots(t *testing.T, c domain.ClassID) []string {
	t.Helper()

	var out []string
	for _, l := range f.classLessons(t, c) {
		out = append(out, l.SlotDate.String())
	}

	return out
}

func TestTimetableEditsIgnoreMemberBookings(t *testing.T) {
	t.Parallel()

	t.Run("retire", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newBookingFixture(t)
		c := f.memberClass(t)

		// Mia released 2026-11-10; Tom booked 2026-11-03 himself.
		released := f.lessonAt(t, c.ID, "2026-11-10")
		if _, err := f.bookingService(t, bookingNow).Cancel(ctx, f.principal(t, f.anna), f.bookingOf(t, released, f.mia)[0].ID); err != nil {
			t.Fatal(err)
		}

		kept := f.lessonAt(t, c.ID, "2026-11-03")
		f.bookFor(t, kept, f.tom)

		d := f.classData()
		d.Retired = true

		if _, err := f.timetable(t, bookingNow).UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		if got := f.slots(t, c.ID); !slices.Equal(got, []string{"2026-11-03"}) {
			t.Fatalf("lessons %v, want only the one Tom booked", got)
		}

		if got := f.bookingsOf(t, kept); len(got) != 2 {
			t.Fatalf("kept lesson bookings %+v, want Mia's and Tom's", got)
		}
	})

	t.Run("weekday move", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newBookingFixture(t)
		moved, stays := f.memberClass(t), f.memberClass(t)

		// Tom booked the lesson of 2026-10-27 of the second class himself.
		f.bookFor(t, f.lessonAt(t, stays.ID, "2026-10-27"), f.tom)

		// At 19:00 on Monday 2026-10-26, both classes move to Mondays at
		// 18:00: the slot of their lesson of 2026-10-27 has passed.
		svc := f.timetable(t, "2026-10-26T18:00:00Z")
		d := f.classData()
		d.Weekday = time.Monday

		for _, c := range []TimetableClass{moved, stays} {
			if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
				t.Fatal(err)
			}
		}

		want := []string{"2026-11-02", "2026-11-09", "2026-11-16", "2026-11-23"}
		if got := f.slots(t, moved.ID); !slices.Equal(got, want) {
			t.Fatalf("moved class lessons %v, want %v", got, want)
		}

		for _, l := range f.classLessons(t, moved.ID) {
			if b := f.bookingOf(t, l, f.mia); len(b) != 1 || b[0].Status != domain.BookingBooked {
				t.Errorf("%s: Mia's bookings %+v", l.SlotDate, b)
			}
		}

		if got := f.slots(t, stays.ID); !slices.Equal(got, append([]string{"2026-10-27"}, want...)) {
			t.Fatalf("class with a booking of its own: lessons %v", got)
		}
	})

	t.Run("term shortening", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newBookingFixture(t)
		c := f.memberClass(t)
		svc := f.timetable(t, bookingNow)

		if _, err := svc.UpdateTerm(ctx, f.term.ID, f.termData(t, "2026-09-01", "2026-11-12")); err != nil {
			t.Fatal(err)
		}

		if got := f.slots(t, c.ID); !slices.Equal(got, []string{"2026-10-27", "2026-11-03", "2026-11-10"}) {
			t.Fatalf("lessons %v after shortening the term", got)
		}

		// A booking of its own still refuses the shorter term.
		f.bookFor(t, f.lessonAt(t, c.ID, "2026-11-10"), f.tom)

		_, err := svc.UpdateTerm(ctx, f.term.ID, f.termData(t, "2026-09-01", "2026-11-05"))
		wantFieldError(t, err, "endsOn")

		if got := f.slots(t, c.ID); !slices.Equal(got, []string{"2026-10-27", "2026-11-03", "2026-11-10"}) {
			t.Fatalf("lessons %v after the refused edit", got)
		}
	})

	t.Run("delete after the last member left", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newBookingFixture(t)
		c := f.memberClass(t)
		svc := f.timetable(t, bookingNow)

		if err := svc.DeleteClass(ctx, c.ID); !errors.Is(err, domain.ErrInUse) {
			t.Fatalf("with a member: %v, want ErrInUse", err)
		}

		if err := svc.RemoveClassMember(ctx, f.principal(t, f.olga), c.ID, f.mia.ID); err != nil {
			t.Fatal(err)
		}

		if err := svc.DeleteClass(ctx, c.ID); err != nil {
			t.Fatal(err)
		}

		if got := f.classLessons(t, c.ID); len(got) != 0 {
			t.Fatalf("lessons %+v left", got)
		}
	})
}

// memberOf returns person's membership of class c, or fails.
func (f *bookingFixture) memberOf(t *testing.T, c domain.ClassID, person domain.Person) domain.ClassMember {
	t.Helper()

	ms, err := f.timetable(t, bookingNow).ClassMembers(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range ms {
		if m.PersonID == person.ID {
			return m.ClassMember
		}
	}

	t.Fatalf("%s is no member of class %s", person.GivenName, c)

	return domain.ClassMember{}
}

func TestCancelFollowingPausesMember(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	c := f.memberClass(t)
	anna := f.principal(t, f.anna)
	svc := f.bookingService(t, bookingNow)
	first := f.bookingOf(t, f.lessonAt(t, c.ID, "2026-10-27"), f.mia)[0]

	slots := func(fc FollowingCancellation) []string {
		var out []string
		for _, fb := range fc.Bookings {
			out = append(out, fb.Lesson.SlotDate.String())
		}

		return out
	}

	// Without an end date: the end of the term.
	preview, err := svc.PreviewCancelFollowing(ctx, anna, first.ID, domain.Date{})
	if err != nil {
		t.Fatal(err)
	}

	if end := f.term.EndsOn; preview.Until != end || preview.PausedUntil != end || len(preview.Bookings) != 4 {
		t.Fatalf("default preview %+v, want all four until the end of the term", preview)
	}

	preview, err = svc.PreviewCancelFollowing(ctx, anna, first.ID, mustDate(t, "2026-11-03"))
	if err != nil {
		t.Fatal(err)
	}

	if got := slots(preview); !slices.Equal(got, []string{"2026-10-27", "2026-11-03"}) || preview.PausedUntil != mustDate(t, "2026-11-03") {
		t.Fatalf("preview until 2026-11-03: %v, paused until %s", got, preview.PausedUntil)
	}

	_, err = svc.PreviewCancelFollowing(ctx, anna, first.ID, mustDate(t, "2026-10-26"))
	wantFieldError(t, err, "until")

	_, err = svc.CancelFollowing(ctx, anna, first.ID, mustDate(t, "2026-10-26"))
	wantFieldError(t, err, "until")

	if m := f.memberOf(t, c.ID, f.mia); !m.PausedUntil.IsZero() {
		t.Fatalf("paused by a refused request: %+v", m)
	}

	// Mia pauses until 2026-12-01: her bookings up to then are canceled
	// (the next one too), the membership stays.
	done, err := svc.CancelFollowing(ctx, anna, first.ID, mustDate(t, "2026-12-01"))
	if err != nil {
		t.Fatal(err)
	}

	if got := slots(done); !slices.Equal(got, []string{"2026-10-27", "2026-11-03", "2026-11-10", "2026-11-17"}) ||
		done.PausedUntil != mustDate(t, "2026-12-01") {
		t.Fatalf("canceled %v, paused until %s", got, done.PausedUntil)
	}

	if m := f.memberOf(t, c.ID, f.mia); m.PausedUntil != mustDate(t, "2026-12-01") {
		t.Fatalf("membership %+v, want paused until 2026-12-01", m)
	}

	// Generation books her again only after the pause.
	for _, now := range []string{"2026-11-04T10:00:00Z", "2026-11-11T10:00:00Z"} {
		if _, err := NewLessonGenerator(f.st, clock.NewFixed(mustInstant(t, now)), f.seq).GenerateAhead(ctx); err != nil {
			t.Fatal(err)
		}
	}

	for slot, n := range map[string]int{"2026-11-24": 0, "2026-12-01": 0, "2026-12-08": 1} {
		if got := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia); len(got) != n {
			t.Errorf("%s: Mia's bookings %+v, want %d", slot, got, n)
		}
	}

	// The office ends the pause on 2026-11-11: Mia is booked into the
	// lessons generated meanwhile; the dates she released stay released.
	m := f.memberOf(t, c.ID, f.mia)
	m.PausedUntil = domain.Date{}

	got, err := f.timetable(t, "2026-11-11T10:00:00Z").UpdateClassMember(ctx, f.principal(t, f.olga), m)
	if err != nil {
		t.Fatal(err)
	}

	if !got.PausedUntil.IsZero() || f.memberOf(t, c.ID, f.mia).PausedUntil != (domain.Date{}) {
		t.Fatalf("pause not ended: %+v", got)
	}

	for slot, want := range map[string][]domain.BookingStatus{
		// Canceled by the pause, not released: booked again.
		"2026-11-17": {domain.BookingCanceled, domain.BookingBooked},
		"2026-11-24": {domain.BookingBooked},
		"2026-12-01": {domain.BookingBooked},
		"2026-12-08": {domain.BookingBooked},
	} {
		var statuses []domain.BookingStatus
		for _, b := range f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia) {
			statuses = append(statuses, b.Status)
		}

		if !slices.Equal(statuses, want) {
			t.Errorf("%s: Mia's bookings %v, want %v", slot, statuses, want)
		}
	}

	stranger := domain.ClassMember{ClassID: c.ID, PersonID: f.sam.ID}
	if _, err := f.timetable(t, bookingNow).UpdateClassMember(ctx, f.principal(t, f.olga), stranger); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("not a member: %v, want ErrNotFound", err)
	}
}

func TestCancelFollowingOfNonMember(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	c := f.memberClass(t)

	for _, slot := range []string{"2026-10-27", "2026-11-03", "2026-11-10"} {
		f.bookFor(t, f.lessonAt(t, c.ID, slot), f.tom)
	}

	first := f.bookingOf(t, f.lessonAt(t, c.ID, "2026-10-27"), f.tom)[0]

	done, err := f.bookingService(t, bookingNow).CancelFollowing(ctx, f.principal(t, f.anna), first.ID, mustDate(t, "2026-11-03"))
	if err != nil {
		t.Fatal(err)
	}

	if len(done.Bookings) != 2 || !done.PausedUntil.IsZero() || done.Until != mustDate(t, "2026-11-03") {
		t.Fatalf("canceled %+v, want two bookings and no pause", done)
	}

	// Without a membership to pause, the cancellations release the dates.
	for _, fb := range done.Bookings {
		if !fb.Booking.Released {
			t.Errorf("not released: %+v", fb.Booking)
		}
	}

	if b := f.bookingOf(t, f.lessonAt(t, c.ID, "2026-11-10"), f.tom); b[0].Status != domain.BookingBooked {
		t.Fatalf("booking after until: %+v", b)
	}
}

func TestEndingPauseBooksPausedLessonsAgain(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	c := f.memberClass(t)
	anna, olga := f.principal(t, f.anna), f.principal(t, f.olga)
	svc := f.bookingService(t, bookingNow)

	// Mia releases 2026-11-10 on its own, then pauses from 2026-10-27 on
	// until 2026-11-17.
	if _, err := svc.Cancel(ctx, anna, f.bookingOf(t, f.lessonAt(t, c.ID, "2026-11-10"), f.mia)[0].ID); err != nil {
		t.Fatal(err)
	}

	done, err := svc.CancelFollowing(ctx, anna, f.bookingOf(t, f.lessonAt(t, c.ID, "2026-10-27"), f.mia)[0].ID,
		mustDate(t, "2026-11-17"))
	if err != nil {
		t.Fatal(err)
	}

	for _, fb := range done.Bookings {
		if fb.Booking.Released {
			t.Errorf("a pause released %+v", fb.Booking)
		}
	}

	// Taking back 2026-11-03 and canceling the following weeks up to it
	// keeps the longer pause.
	back, err := svc.Book(ctx, anna, f.lessonAt(t, c.ID, "2026-11-03").ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewCancelFollowing(ctx, anna, back.ID, mustDate(t, "2026-11-03"))
	if err != nil || preview.PausedUntil != mustDate(t, "2026-11-17") {
		t.Fatalf("preview %+v, %v; want the pause until 2026-11-17 kept", preview, err)
	}

	again, err := svc.CancelFollowing(ctx, anna, back.ID, mustDate(t, "2026-11-03"))
	if err != nil || again.PausedUntil != mustDate(t, "2026-11-17") {
		t.Fatalf("canceled %+v, %v; want the pause until 2026-11-17 kept", again, err)
	}

	if m := f.memberOf(t, c.ID, f.mia); m.PausedUntil != mustDate(t, "2026-11-17") {
		t.Fatalf("membership %+v, want paused until 2026-11-17", m)
	}

	// The office ends the pause: the paused lessons are booked again, the
	// released date stays released.
	m := f.memberOf(t, c.ID, f.mia)
	m.PausedUntil = domain.Date{}

	if _, err := f.timetable(t, bookingNow).UpdateClassMember(ctx, olga, m); err != nil {
		t.Fatal(err)
	}

	for slot, want := range map[string]domain.BookingStatus{
		"2026-10-27": domain.BookingBooked,
		"2026-11-03": domain.BookingBooked,
		"2026-11-10": domain.BookingCanceled,
		"2026-11-17": domain.BookingBooked,
	} {
		bs := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia)
		if last := bs[len(bs)-1]; last.Status != want {
			t.Errorf("%s: Mia's bookings %+v, want the last %s", slot, bs, want)
		}
	}
}

func TestUpdateClassMemberWithdrawsBookings(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	c := f.memberClass(t)
	olga := f.principal(t, f.olga)
	svc := f.timetable(t, bookingNow)
	actor := f.accountOf(t, f.olga.ID)

	// The office pauses Mia until 2026-11-03 and lets her start on
	// 2026-11-10 at the earliest: her automatic bookings before are
	// canceled without releasing the dates.
	m := f.memberOf(t, c.ID, f.mia)
	m.PausedUntil, m.Since = mustDate(t, "2026-11-03"), mustDate(t, "2026-11-10")

	got, err := svc.UpdateClassMember(ctx, olga, m)
	if err != nil {
		t.Fatal(err)
	}

	if got.PausedUntil != m.PausedUntil || got.Since != m.Since {
		t.Fatalf("updated %+v", got)
	}

	for slot, active := range map[string]bool{"2026-10-27": false, "2026-11-03": false, "2026-11-10": true, "2026-11-17": true} {
		bs := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia)
		if len(bs) != 1 || (bs[0].Status == domain.BookingBooked) != active {
			t.Fatalf("%s: Mia's bookings %+v, want active %v", slot, bs, active)
		}

		if active {
			continue
		}

		b, events := f.booking(t, bs[0].ID)
		if b.Released || b.Late || events[len(events)-1].ActorAccountID != actor {
			t.Errorf("%s: %+v, events %+v; want the office's cancellation, not released, not late", slot, b, events)
		}
	}

	// Back to the start: she is booked again.
	m.PausedUntil, m.Since = domain.Date{}, domain.Date{}
	if _, err := svc.UpdateClassMember(ctx, olga, m); err != nil {
		t.Fatal(err)
	}

	for _, slot := range []string{"2026-10-27", "2026-11-03"} {
		bs := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia)
		if len(bs) != 2 || bs[1].Status != domain.BookingBooked {
			t.Errorf("%s: Mia's bookings %+v, want booked again", slot, bs)
		}
	}

	// A retired class takes no changes of its members.
	d := f.classData()
	d.Retired = true

	if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateClassMember(ctx, olga, m); !errors.Is(err, ErrClassRetired) {
		t.Fatalf("retired class: %v, want ErrClassRetired", err)
	}
}

func TestClassMembersHidePastPause(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	c := f.memberClass(t)

	m := f.memberOf(t, c.ID, f.mia)
	m.PausedUntil = mustDate(t, "2026-10-20") // the day before bookingNow
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Classes().UpdateMember(ctx, m) })

	if got := f.memberOf(t, c.ID, f.mia); !got.PausedUntil.IsZero() {
		t.Fatalf("past pause shown: %+v", got)
	}

	m.PausedUntil = mustDate(t, "2026-10-21") // today: still paused
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Classes().UpdateMember(ctx, m) })

	if got := f.memberOf(t, c.ID, f.mia); got.PausedUntil != m.PausedUntil {
		t.Fatalf("pause of today not shown: %+v", got)
	}
}

func TestMovedLessonGetsMembers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.timetable(t, bookingNow)
	olga := f.principal(t, f.olga)

	c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	// The lesson of the past slot 2026-10-20, moved by the office to
	// Thursday 2026-10-29.
	past := domain.Date{Year: 2026, Month: time.October, Day: 20}
	if _, err := NewLessonGenerator(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq).Generate(ctx, past, past.AddDays(1)); err != nil {
		t.Fatal(err)
	}

	moved := f.lessonAt(t, c.ID, "2026-10-20")
	moved.StartsAt, moved.EndsAt, moved.Detached = mustInstant(t, "2026-10-29T17:00:00Z"), mustInstant(t, "2026-10-29T18:00:00Z"), true
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, moved) })

	if _, err := svc.AddClassMember(ctx, olga, domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID}); err != nil {
		t.Fatal(err)
	}

	if b := f.bookingOf(t, moved, f.mia); len(b) != 1 || b[0].Status != domain.BookingBooked {
		t.Fatalf("moved lesson: %+v, want Mia booked", b)
	}

	if err := svc.RemoveClassMember(ctx, olga, c.ID, f.mia.ID); err != nil {
		t.Fatal(err)
	}

	if b := f.bookingOf(t, moved, f.mia); len(b) != 1 || b[0].Status != domain.BookingCanceled {
		t.Fatalf("moved lesson after leaving: %+v, want it canceled", b)
	}
}

func TestClassEditWithMoreMembersThanPlaces(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.timetable(t, bookingNow)

	d := f.classData()
	d.Capacity = 1

	c, err := svc.CreateClass(ctx, f.term.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	// Older data: two members, one place.
	f.addMembers(t, c.ID, domain.ClassMember{PersonID: f.mia.ID}, domain.ClassMember{PersonID: f.tom.ID})

	d.StartTime = domain.TimeOfDay{Hour: 18, Minute: 30}
	if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
		t.Fatalf("edit with the same capacity: %v", err)
	}

	d.Retired = true
	if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
		t.Fatalf("retire: %v", err)
	}

	d.Retired, d.Capacity = false, 2
	if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
		t.Fatalf("raise the capacity: %v", err)
	}

	d.Capacity = 1
	_, err = svc.UpdateClass(ctx, c.ID, d)
	wantFieldError(t, err, "capacity")
}

func TestCancelFollowingPreviewAndApplyFailAlike(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	c := f.memberClass(t)
	anna := f.principal(t, f.anna)
	svc := f.bookingService(t, bookingNow)
	early := mustDate(t, "2026-01-01")

	b := f.bookingOf(t, f.lessonAt(t, c.ID, "2026-10-27"), f.mia)[0]
	if _, err := svc.Cancel(ctx, anna, b.ID); err != nil {
		t.Fatal(err)
	}

	oneOff := f.regular(t, "2026-10-29T16:00:00Z")

	single, err := svc.Book(ctx, anna, oneOff.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name    string
		booking domain.BookingID
		want    error
	}{
		{"canceled booking", b.ID, ErrBookingCanceled},
		{"one-off lesson", single.ID, ErrNotClassBooking},
	} {
		_, previewErr := svc.PreviewCancelFollowing(ctx, anna, tt.booking, early)
		_, applyErr := svc.CancelFollowing(ctx, anna, tt.booking, early)

		if !errors.Is(previewErr, tt.want) || !errors.Is(applyErr, tt.want) {
			t.Errorf("%s: preview %v, apply %v; want %v from both", tt.name, previewErr, applyErr, tt.want)
		}
	}
}
