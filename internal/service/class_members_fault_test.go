package service

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The membership fault tests run on the Tuesday class of classData, created
// at bookingNow: its lessons are on 2026-10-27, 11-03, 11-10 and 11-17.

// timetableOn returns a timetable service on fs at bookingNow.
func (f *bookingFixture) timetableOn(t *testing.T, fs *FaultStore) *TimetableService {
	t.Helper()

	return NewTimetableService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(newTestRenderer(t)))
}

// TestAddClassMemberStoreFaults enrolls Mia: she is booked into the four
// lessons, and her waiting entry for the first ends. A store failure
// anywhere leaves her no membership, booking or changed entry.
func TestAddClassMemberStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		olga := f.principal(t, f.olga)

		c, err := f.timetable(t, bookingNow).CreateClass(t.Context(), f.term.ID, f.classData())
		if err != nil {
			t.Fatal(err)
		}

		first := f.lessonAt(t, c.ID, "2026-10-27")
		waiting := f.waitingEntry(t, first, f.mia, "2026-10-20T11:00:00Z")
		fs := NewFaultStore(f.st)
		svc := f.timetableOn(t, fs)

		return fs, func() error {
			_, err := svc.AddClassMember(t.Context(), olga, domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID})
			if err == nil {
				wantStatus(t, f.entry(t, waiting.ID), domain.WaitlistWithdrawn)

				for _, l := range f.classLessons(t, c.ID) {
					if b := f.bookingOf(t, l, f.mia); len(b) != 1 || b[0].Status != domain.BookingBooked {
						t.Errorf("%s: bookings %+v, want one", l.SlotDate, b)
					}
				}
			}

			return err
		}
	}})
}

// TestUpdateClassMemberStoreFaults pauses Mia until 2026-11-03: her
// bookings up to that day are canceled. A store failure anywhere keeps the
// old dates and bookings.
func TestUpdateClassMemberStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		olga := f.principal(t, f.olga)
		c := f.memberClass(t)
		fs := NewFaultStore(f.st)
		faulty := f.timetableOn(t, fs)

		return fs, func() error {
			e, err := faulty.UpdateClassMember(t.Context(), olga, domain.ClassMember{
				ClassID: c.ID, PersonID: f.mia.ID, PausedUntil: mustDate(t, "2026-11-03"),
			})
			if err == nil {
				if e.Person.ID != f.mia.ID || e.PausedUntil != mustDate(t, "2026-11-03") {
					t.Errorf("entry %+v", e)
				}

				for slot, want := range map[string]domain.BookingStatus{
					"2026-10-27": domain.BookingCanceled, "2026-11-03": domain.BookingCanceled, "2026-11-10": domain.BookingBooked,
				} {
					if b := f.bookingOf(t, f.lessonAt(t, c.ID, slot), f.mia); len(b) != 1 || b[0].Status != want {
						t.Errorf("%s: bookings %+v, want one %s", slot, b, want)
					}
				}
			}

			return err
		}
	}})
}

// TestRemoveClassMemberStoreFaults removes Mia, which cancels her bookings
// and invites Olga into the full first lesson. A store failure anywhere
// keeps her membership and bookings, and invites nobody.
func TestRemoveClassMemberStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		c := f.memberClass(t)
		first := f.lessonAt(t, c.ID, "2026-10-27")

		for i := 1; i < first.Capacity; i++ {
			f.bookAs(t, f.person(t, "Filler", "", "", ""), first)
		}

		olga := f.waitingEntry(t, first, f.olga, "2026-10-20T11:00:00Z")
		fs := NewFaultStore(f.st)
		svc := f.timetableOn(t, fs)

		return fs, func() error {
			err := svc.RemoveClassMember(t.Context(), f.principal(t, f.olga), c.ID, f.mia.ID)
			if err == nil {
				wantStatus(t, f.entry(t, olga.ID), domain.WaitlistInvited)
			}

			return err
		}
	}})
}

// TestClassMembersStoreFaults: a store failure while listing the members is
// returned.
func TestClassMembersStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		c := f.memberClass(t)
		fs := NewFaultStore(f.st)
		svc := f.timetableOn(t, fs)

		return fs, func() error {
			ms, err := svc.ClassMembers(t.Context(), c.ID)
			if err == nil && (len(ms) != 1 || ms[0].Person.ID != f.mia.ID) {
				t.Errorf("members %+v, want Mia", ms)
			}

			return err
		}
	}})
}

// TestClassMembersWithoutSchool lists the members of a class before the
// school's settings exist: without a school date, a pause counts as
// lasting.
func TestClassMembersWithoutSchool(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})
	f.addMembers(t, c.ID, domain.ClassMember{PersonID: f.mia.ID, PausedUntil: mustDate(t, "2026-01-01")})
	fs := NewFaultStore(f.st).FailWith("Settings.Get", 1, domain.ErrNotFound)

	ms, err := f.timetableOn(t, fs).ClassMembers(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(ms) != 1 || ms[0].PausedUntil != mustDate(t, "2026-01-01") {
		t.Fatalf("members %+v, want Mia with her pause", ms)
	}
}

// TestDeleteClassLessonStoreFaults: a store failure while deleting a lesson
// of a class with its member bookings is returned, and both stay.
func TestDeleteClassLessonStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		c := f.memberClass(t)
		l := f.lessonAt(t, c.ID, "2026-11-17")
		fs := NewFaultStore(f.st)

		return fs, func() error {
			return fs.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
				return deleteClassLesson(ctx, tx, l.ID)
			})
		}
	}})
}

// TestBookMembersStoreFaults: a store failure while booking the members
// into a lesson generation creates is returned, and neither the lesson nor
// a booking stays.
func TestBookMembersStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})
		f.addMembers(t, c.ID, domain.ClassMember{PersonID: f.mia.ID}, domain.ClassMember{PersonID: f.tom.ID})
		l := weeklyLessons(t, f, c, 1, "2026-10-27")[0]
		f.write(t, func(ctx context.Context, tx store.Tx) error { return deleteClassLesson(ctx, tx, l.ID) })
		fs := NewFaultStore(f.st)
		now := mustInstant(t, bookingNow)

		return fs, func() error {
			err := fs.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
				c, err := chargesIn(ctx, tx)
				if err != nil {
					return err
				}

				members, err := tx.Classes().ListMembers(ctx, l.ClassID)
				if err != nil {
					return err
				}

				if err := tx.Lessons().Create(ctx, l); err != nil {
					return err
				}

				return bookMembers(ctx, tx, f.seq, c, now, l, members)
			})
			if err == nil {
				// One place: the second member is booked beyond it.
				if b := f.bookingsOf(t, l); len(b) != 2 {
					t.Errorf("bookings %+v, want both members", b)
				}
			}

			return err
		}
	}})
}
