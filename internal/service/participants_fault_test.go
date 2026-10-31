package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// faultParticipants returns a ParticipantService at now on a FaultStore
// over the fixture's store.
func (f *staffFixture) faultParticipants(t *testing.T, now string) (*FaultStore, *ParticipantService) {
	t.Helper()

	fs := NewFaultStore(f.st)

	return fs, NewParticipantService(fs, clock.NewFixed(mustInstant(t, now)), f.seq, testMails(newTestRenderer(t)))
}

// waitingMia puts Mia on the list of the full lesson of capacity 1 that
// Stella booked.
func (f *staffFixture) waitingMia(t *testing.T) domain.Lesson {
	t.Helper()

	l := f.fullLesson(t, 1)
	if _, err := f.waitlistService(t, bookingNow).Join(t.Context(), f.principal(t, f.anna), l.ID, f.mia.ID, false); err != nil {
		t.Fatal(err)
	}

	return l
}

// TestAddParticipantStoreFaults: a store failure anywhere in adding a
// person or a guest is returned, and neither the booking, its event, the
// accepted waiting-list entry nor the confirmation stays.
func TestAddParticipantStoreFaults(t *testing.T) {
	t.Parallel()

	t.Run("person from the waiting list", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newStaffFixture(t)
			ivy := f.principal(t, f.ivy)
			l := f.waitingMia(t)
			fs, svc := f.faultParticipants(t, bookingNow)

			return fs, func() error {
				_, err := svc.Add(t.Context(), ivy, l.ID, NewParticipant{PersonID: f.mia.ID, ConfirmOverCapacity: true})

				return err
			}
		}})
	})

	t.Run("guest", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newStaffFixture(t)
			olga := f.principal(t, f.olga)
			l := f.lessonWithPianist(t, 5)
			fs, svc := f.faultParticipants(t, bookingNow)

			return fs, func() error {
				_, err := svc.Add(t.Context(), olga, l.ID, NewParticipant{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})

				return err
			}
		}})
	})
}

// TestAddParticipantConcurrentWriter: when storing the booking still meets
// another active booking of the person, or a missing reference, which only
// a concurrent writer causes, Add reports what the caller can act on, and
// nothing stays.
func TestAddParticipantConcurrentWriter(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		storeErr, want error
	}{
		"duplicate booking": {domain.ErrDuplicate, ErrAlreadyBooked},
		"person deleted":    {domain.ErrConflict, errUnknownPerson},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newStaffFixture(t)
			ivy := f.principal(t, f.ivy)
			l := f.lessonWithPianist(t, 5)
			fs, svc := f.faultParticipants(t, bookingNow)
			before := fs.Snapshot(t)
			fs.FailWith("Bookings.Create", 1, c.storeErr)

			_, err := svc.Add(t.Context(), ivy, l.ID, NewParticipant{PersonID: f.mia.ID})
			if !errors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}

			if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
				t.Errorf("the store changed: %v", changed)
			}
		})
	}
}

// TestAddParticipantRefusals: a lesson the caller does not manage is a 404
// even for an unknown person, so the answer does not reveal the lesson; a
// room rental takes no participants.
func TestAddParticipantRefusals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	svc := f.participants(t, bookingNow)
	l := f.lessonWithPianist(t, 5)

	if _, err := svc.Add(ctx, f.principal(t, f.otto), l.ID, NewParticipant{PersonID: domain.PersonID{1}}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown person in another's lesson: want ErrNotFound, got %v", err)
	}

	_, err := svc.Add(ctx, f.principal(t, f.olga), f.rental(t, staffLesson).ID, NewParticipant{GuestName: "Gina Guest"})
	wantClosed(t, err, ClosedRental)

	if got := f.bookingsOf(t, l); len(got) != 0 {
		t.Errorf("bookings %+v, want none", got)
	}
}

// TestUpdateTrialStoreFaults: a store failure while marking a trial is
// returned and the booking stays as it was.
func TestUpdateTrialStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		ivy := f.principal(t, f.ivy)
		l := f.lessonWithPianist(t, 5)
		f.bookAs(t, f.mia, l)
		b := f.bookingIn(t, l, f.mia)
		fs, svc := f.faultParticipants(t, bookingNow)

		return fs, func() error {
			_, err := svc.UpdateTrial(t.Context(), ivy, b.ID, ParticipantUpdate{Trial: true, TrialNote: "First class"})

			return err
		}
	}})
}

// TestRemoveParticipantStoreFaults: a store failure while removing a
// participant is returned; the booking stays active, and neither the
// cancellation mail nor the invite of the next on the list is queued.
func TestRemoveParticipantStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		ivy := f.principal(t, f.ivy)
		l := f.waitingMia(t)
		stella := f.bookingIn(t, l, f.stella(t))
		fs, svc := f.faultParticipants(t, bookingNow)

		return fs, func() error {
			_, err := svc.Remove(t.Context(), ivy, stella.ID)

			return err
		}
	}})
}

// TestRemoveParticipantBrokenPerson: a booked person who cannot be read
// for the cancellation mail is broken data, never a 404 for the booking.
func TestRemoveParticipantBrokenPerson(t *testing.T) {
	t.Parallel()

	type run struct {
		f      *staffFixture
		l      domain.Lesson
		fs     *FaultStore
		remove func() error
	}

	setup := func() run {
		f := newStaffFixture(t)
		ivy := f.principal(t, f.ivy)
		l := f.lessonWithPianist(t, 5)
		f.bookAs(t, f.mia, l)
		b := f.bookingIn(t, l, f.mia)
		fs, svc := f.faultParticipants(t, bookingNow)

		return run{f: f, l: l, fs: fs, remove: func() error {
			_, err := svc.Remove(t.Context(), ivy, b.ID)

			return err
		}}
	}

	first := setup()
	if err := first.remove(); err != nil {
		t.Fatal(err)
	}

	// The participant's name for the mail is read after the cancellation.
	read := callAfter(t, first.fs.Calls(), "Bookings.Update", "Persons.Get")

	r := setup()
	r.fs.FailWith(read.Method, read.N, domain.ErrNotFound)

	if err := r.remove(); !errors.Is(err, errBrokenReference) || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want the broken reference, not a 404, got %v", err)
	}

	if got := r.f.bookingIn(t, r.l, r.f.mia); got.Status != domain.BookingBooked {
		t.Errorf("booking %+v, want it still active", got)
	}
}

// callAfter returns the first call of method after the first call of
// after.
func callAfter(t *testing.T, calls []FaultCall, after, method string) FaultCall {
	t.Helper()

	seen := false

	for _, c := range calls {
		switch {
		case c.Method == after:
			seen = true
		case seen && c.Method == method:
			return c
		}
	}

	t.Fatalf("no %s after %s in %v", method, after, calls)

	return FaultCall{}
}

// TestSetAttendanceStoreFaults: a store failure while taking attendance is
// returned and neither the status nor its event stays.
func TestSetAttendanceStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		paul := f.principal(t, f.paul)
		l := f.lessonWithPianist(t, 5)
		f.bookAs(t, f.mia, l)
		b := f.bookingIn(t, l, f.mia)
		fs, svc := f.faultParticipants(t, staffLesson)

		return fs, func() error {
			_, err := svc.SetAttendance(t.Context(), paul, b.ID, domain.BookingAttended)

			return err
		}
	}})
}

// TestMarkAllAttendedStoreFaults: a store failure while marking everyone
// attended is returned and no booking changes: they change together.
func TestMarkAllAttendedStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		ivy := f.principal(t, f.ivy)
		l := f.lessonWithPianist(t, 5)
		f.bookAs(t, f.mia, l)
		f.bookAs(t, f.tom, l)
		fs, svc := f.faultParticipants(t, staffStarted)

		return fs, func() error {
			_, err := svc.MarkAllAttended(t.Context(), ivy, l.ID)

			return err
		}
	}})
}

// TestLessonParticipantsStoreFaults: a store failure while listing the
// participants, their contacts, the waiting list or the payments is
// returned.
func TestLessonParticipantsStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		ivy := f.principal(t, f.ivy)
		l := f.waitingMia(t)
		f.bookAs(t, f.mia, f.lessonWithPianist(t, 5)) // another lesson's booking is not read

		if _, err := f.participants(t, bookingNow).Add(t.Context(), ivy, l.ID, NewParticipant{
			GuestName: "Gina Guest", ConfirmOverCapacity: true,
		}); err != nil {
			t.Fatal(err)
		}

		fs, svc := f.faultParticipants(t, bookingNow)

		return fs, func() error {
			_, err := svc.Participants(t.Context(), ivy, l.ID)

			return err
		}
	}})
}

// TestBookingEventsStoreFaults: a store failure while reading a booking's
// history or its actors is returned.
func TestBookingEventsStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		olga := f.principal(t, f.olga)
		l := f.lessonWithPianist(t, 5)

		b, err := f.participants(t, bookingNow).Add(t.Context(), f.principal(t, f.ivy), l.ID, NewParticipant{PersonID: f.mia.ID})
		if err != nil {
			t.Fatal(err)
		}

		fs, svc := f.faultParticipants(t, bookingNow)

		return fs, func() error {
			_, err := svc.Events(t.Context(), olga, b.ID)

			return err
		}
	}})
}

// TestFindPersonsStoreFaults: a store failure in the staff's person search
// is returned, not an empty result.
func TestFindPersonsStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		fs, svc := f.faultParticipants(t, bookingNow)

		return fs, func() error {
			_, err := svc.FindPersons(t.Context(), "mia")

			return err
		}
	}})
}

// TestStaffWithoutPerson: a staff principal without a person owns no staff
// record, not even one that is linked to nobody, so it manages no lesson
// and teaches none.
func TestStaffWithoutPerson(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	unlinked := domain.Staff{
		ID: ids.New[domain.StaffID](f.seq), DisplayName: "Una Example", Kind: domain.StaffInstructor, Active: true,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Staff().Create(ctx, unlinked) })
	l := f.lessonBy(t, staffLesson, unlinked.ID, domain.StaffID{})
	nobody := Principal{AccountID: f.principal(t, f.nils).AccountID, Role: domain.RoleInstructor}

	if _, err := f.participants(t, bookingNow).Participants(ctx, nobody, l.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("participants: want ErrNotFound, got %v", err)
	}

	got, err := f.teach(t, bookingNow).Lessons(ctx, nobody, TeachQuery{})
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Lessons) != 0 {
		t.Errorf("lessons %+v, want none", got.Lessons)
	}
}
