package service

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestBookStoreFaults: a store failure anywhere in a booking is returned,
// and neither the booking, its event, a redemption nor the mail stays.
func TestBookStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		anna := f.principal(t, f.anna)
		l := f.regular(t, "2026-10-22T16:00:00Z")

		r, err := mail.NewRenderer()
		if err != nil {
			t.Fatal(err)
		}

		fs := NewFaultStore(f.st)
		svc := NewBookingService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(r))

		return fs, func() error {
			_, err := svc.Book(t.Context(), anna, l.ID, f.mia.ID)

			return err
		}
	}})
}

// TestBookSettlesStoreFaults books with everything a booking may touch: an
// invite that ran out is ended and mailed, the places are offered to the
// waiting entries (Mia among them), Mia's booking takes her new invite and
// her pass pays for it. A store failure anywhere rolls all of it back: no
// ended entry, no invite, no visit spent and no mail stays.
func TestBookSettlesStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnBooking)
		anna := f.principal(t, f.anna)
		f.sell(t, f.mia, f.tenVisits, "")

		l := f.oneOff(t, waitlistLesson, 3, domain.VisibilityPublic, domain.LessonWorkshop)
		f.bookAs(t, f.stella(t), l)
		f.invitedEntry(t, l, f.tom, 1, "2026-10-21T09:00:00Z") // ran out an hour ago
		f.waitingEntry(t, l, f.olga, "2026-10-20T11:00:00Z")
		f.waitingEntry(t, l, f.mia, "2026-10-20T12:00:00Z")

		fs := NewFaultStore(f.st)
		svc := NewBookingService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(newTestRenderer(t)))

		return fs, func() error {
			b, err := svc.Book(t.Context(), anna, l.ID, f.mia.ID)
			if err == nil && f.paidBy(t, b.ID).IsZero() {
				t.Error("the pass did not pay for the booking")
			}

			return err
		}
	}})
}

// TestCancelStoreFaults: a store failure anywhere in a cancellation is
// returned, and the booking stays active, its visit spent, the waiting
// entry uninvited and nobody mailed.
func TestCancelStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnBooking)
		anna := f.principal(t, f.anna)
		pass := f.sell(t, f.mia, f.tenVisits, "")

		l := f.oneOff(t, waitlistLesson, 2, domain.VisibilityPublic, domain.LessonWorkshop)
		f.bookAs(t, f.stella(t), l)
		mia := f.book(t, bookingNow, l, f.mia)
		olga := f.waitingEntry(t, l, f.olga, "2026-10-21T10:30:00Z")

		fs := NewFaultStore(f.st)
		svc := NewBookingService(fs, clock.NewFixed(mustInstant(t, "2026-10-21T11:00:00Z")), f.seq,
			testMails(newTestRenderer(t)))

		return fs, func() error {
			_, err := svc.Cancel(t.Context(), anna, mia.ID)
			if err == nil {
				// The timely cancellation gives the visit back, and the
				// free place goes to Olga.
				f.wantVisits(t, pass, 10)
				wantStatus(t, f.entry(t, olga.ID), domain.WaitlistInvited)
			}

			return err
		}
	}})
}

// followingFixture is a weekly class on Tuesdays at 18:00 whose member Mia
// is booked into its lessons of 2026-10-27 and 2026-11-03 (paid by her
// 10-visit pass) and 2026-11-10; Olga waits for the first, which is full.
type followingFixture struct {
	*passFixture

	guardian Principal
	pass     domain.Pass
	lessons  []domain.Lesson
	first    domain.Booking
}

func newFollowingFixture(t *testing.T) *followingFixture {
	t.Helper()

	f := &followingFixture{passFixture: newPassFixture(t, domain.RedeemOnBooking)}
	f.guardian = f.principal(t, f.anna)
	f.pass = f.sell(t, f.mia, f.tenVisits, "")

	c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})
	f.lessons = append(weeklyLessons(t, f.bookingFixture, c, 2, "2026-10-27"),
		weeklyLessons(t, f.bookingFixture, c, c.Capacity, "2026-11-03", "2026-11-10")...)

	f.first = f.book(t, bookingNow, f.lessons[0], f.mia)
	f.book(t, bookingNow, f.lessons[1], f.mia)
	f.bookAs(t, f.mia, f.lessons[2]) // not released for members yet
	f.bookAs(t, f.stella(t), f.lessons[0])
	f.waitingEntry(t, f.lessons[0], f.olga, "2026-10-21T10:30:00Z")
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID})
	})

	return f
}

// TestCancelFollowingStoreFaults: a store failure anywhere in canceling the
// following weeks is returned, and no booking, visit, pause, invite or
// mail changes.
func TestCancelFollowingStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newFollowingFixture(t)
		fs := NewFaultStore(f.st)
		svc := NewBookingService(fs, clock.NewFixed(mustInstant(t, "2026-10-21T11:00:00Z")), f.seq,
			testMails(newTestRenderer(t)))

		return fs, func() error {
			out, err := svc.CancelFollowing(t.Context(), f.guardian, f.first.ID, domain.Date{})
			if err == nil && (len(out.Bookings) != 3 || out.PausedUntil != f.term.EndsOn) {
				t.Errorf("canceled %d bookings, paused until %v; want 3 and the term's end", len(out.Bookings), out.PausedUntil)
			}

			return err
		}
	}})
}

// TestPreviewCancelFollowingStoreFaults: a store failure in the preview is
// returned.
func TestPreviewCancelFollowingStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newFollowingFixture(t)
		fs := NewFaultStore(f.st)
		svc := NewBookingService(fs, clock.NewFixed(mustInstant(t, "2026-10-21T11:00:00Z")), f.seq,
			testMails(newTestRenderer(t)))

		return fs, func() error {
			out, err := svc.PreviewCancelFollowing(t.Context(), f.guardian, f.first.ID, domain.Date{})
			if err == nil && (len(out.Bookings) != 3 || out.PausedUntil != f.term.EndsOn) {
				t.Errorf("preview of %d bookings, paused until %v; want 3 and the term's end", len(out.Bookings), out.PausedUntil)
			}

			return err
		}
	}})
}

// weeklyLessons stores scheduled public lessons of class c with capacity
// places on the slot dates, at its start time in Berlin.
func weeklyLessons(t *testing.T, f *bookingFixture, c domain.Class, capacity int, dates ...string) []domain.Lesson {
	t.Helper()

	out := make([]domain.Lesson, 0, len(dates))

	for _, d := range dates {
		slot := mustDate(t, d)
		start := slot.At(c.StartTime, berlin(t))
		l := domain.Lesson{
			ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: slot, CourseTypeID: f.courseType,
			RoomID: f.room, InstructorID: f.instructor, StartsAt: start, EndsAt: start.Add(c.Duration),
			Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular,
			Capacity: capacity,
		}
		f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })
		out = append(out, l)
	}

	return out
}

// waitingEntry stores a waiting entry of person in l, joined at joined.
func (f *bookingFixture) waitingEntry(t *testing.T, l domain.Lesson, person domain.Person, joined string) domain.WaitlistEntry {
	t.Helper()

	e := domain.WaitlistEntry{
		ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: l.ID, PersonID: person.ID,
		Status: domain.WaitlistWaiting, JoinedAt: mustInstant(t, joined),
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, e) })

	return e
}
