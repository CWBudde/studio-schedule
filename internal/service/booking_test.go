package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The booking tests run on the class fixture (Europe/Berlin) with a booking
// cutoff of one hour and a cancellation cutoff of two hours. bookingNow is
// Wednesday 2026-10-21, 12:00 in Berlin: the schedule is released until
// Monday 2026-10-26, and members see regular lessons until Thursday
// 2026-11-05 (14 visible days).
const bookingNow = "2026-10-21T10:00:00Z"

// bookingFixture is a family on top of the class fixture: Anna (member,
// German mails) with her daughter Mia and her son Tom, who has an account
// of his own (a managed dependent); Olga from the office; Ada the admin;
// and Sam, a stranger's child.
type bookingFixture struct {
	*genFixture

	anna, mia, tom, olga, sam domain.Person
}

func bookingSettings() domain.Settings {
	s := seed.DemoSettings()
	s.SchoolName = "Studio Example"
	s.BookingCutoff, s.CancellationCutoff = time.Hour, 2*time.Hour

	return s
}

func newBookingFixture(t *testing.T) *bookingFixture {
	t.Helper()

	f := &bookingFixture{genFixture: newGenFixture(t, bookingSettings())}
	f.anna = f.person(t, "Anna", "", "anna@example.org", "de")
	f.mia = f.person(t, "Mia", "", "", "")
	f.tom = f.person(t, "Tom", "", "tom@example.org", "")
	f.olga = f.person(t, "Olga", "", "olga@example.net", "")
	f.sam = f.person(t, "Sam", "", "", "")
	stranger := f.person(t, "Stella", "", "stella@example.org", "")

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().GetByPerson(ctx, f.olga.ID)
		if err != nil {
			return err
		}

		acc.Role = domain.RoleOffice

		return errors.Join(
			tx.Accounts().Update(ctx, acc),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: f.anna.ID, DependentID: f.mia.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: f.anna.ID, DependentID: f.tom.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: stranger.ID, DependentID: f.sam.ID}),
		)
	})

	return f
}

func (f *bookingFixture) bookingService(t *testing.T, now string) *BookingService {
	t.Helper()

	return f.bookingServiceAt(t, mustInstant(t, now))
}

func (f *bookingFixture) bookingServiceAt(t *testing.T, now time.Time) *BookingService {
	t.Helper()

	r, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	return NewBookingService(f.st, clock.NewFixed(now), f.seq, testMails(r))
}

// principal signs in as the account of person p.
func (f *bookingFixture) principal(t *testing.T, p domain.Person) Principal {
	t.Helper()

	var acc domain.Account

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		acc, err = tx.Accounts().GetByPerson(ctx, p.ID)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return Principal{AccountID: acc.ID, PersonID: p.ID, Email: acc.Email, Role: acc.Role}
}

// oneOff stores a scheduled one-off lesson of an hour from start.
func (f *bookingFixture) oneOff(
	t *testing.T, start string, capacity int, vis domain.LessonVisibility, kind domain.LessonKind,
) domain.Lesson {
	t.Helper()

	at := mustInstant(t, start)
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor,
		StartsAt: at, EndsAt: at.Add(time.Hour), Status: domain.LessonScheduled, Visibility: vis, Kind: kind,
		Capacity: capacity,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// regular stores a public regular one-off lesson with 5 places.
func (f *bookingFixture) regular(t *testing.T, start string) domain.Lesson {
	t.Helper()

	return f.oneOff(t, start, 5, domain.VisibilityPublic, domain.LessonRegular)
}

func (f *bookingFixture) booking(t *testing.T, id domain.BookingID) (domain.Booking, []domain.BookingEvent) {
	t.Helper()

	var (
		b      domain.Booking
		events []domain.BookingEvent
	)

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		if b, err = tx.Bookings().Get(ctx, id); err != nil {
			return err
		}

		events, err = tx.Bookings().ListEvents(ctx, id)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return b, events
}

func (f *bookingFixture) bookingsOf(t *testing.T, l domain.Lesson) []domain.Booking {
	t.Helper()

	var out []domain.Booking

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.Bookings().ListByLesson(ctx, l.ID)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func wantClosed(t *testing.T, err error, why ClosedReason) {
	t.Helper()

	var closed *BookingClosedError
	if !errors.As(err, &closed) || closed.Why != why || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("want BookingClosedError %s, got %v", why, err)
	}
}

func TestBook(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.bookingService(t, bookingNow)
	anna := f.principal(t, f.anna)
	l := f.regular(t, "2026-10-22T16:00:00Z") // Thursday 18:00

	b, err := svc.Book(ctx, anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	got, events := f.booking(t, b.ID)
	if got != b || b.LessonID != l.ID || b.PersonID != f.mia.ID || b.Status != domain.BookingBooked ||
		b.Source != domain.BookingSourceSelf || !b.BookedAt.Equal(mustInstant(t, bookingNow)) || b.Late {
		t.Fatalf("booking %+v, stored %+v", b, got)
	}

	if len(events) != 1 || events[0].Status != domain.BookingBooked || events[0].ActorAccountID != anna.AccountID ||
		events[0].Late || events[0].Override {
		t.Fatalf("events %+v", events)
	}

	// The acting account gets the confirmation in its language.
	mails := f.mails(t)
	if len(mails) != 1 || mails[0].To != "anna@example.org" || mails[0].Template != mail.TemplateBookingConfirmed ||
		!strings.Contains(mails[0].Subject, "ist gebucht") || !strings.Contains(mails[0].Text, "Gebucht für: Mia Example") ||
		!strings.Contains(mails[0].Text, "Donnerstag, 22. Oktober 2026, 18:00–19:00 Uhr") {
		t.Fatalf("mails %+v", mails)
	}

	t.Run("already booked", func(t *testing.T) {
		if _, err := svc.Book(ctx, anna, l.ID, f.mia.ID); !errors.Is(err, ErrAlreadyBooked) {
			t.Fatalf("want ErrAlreadyBooked, got %v", err)
		}
	})

	t.Run("self and managed dependent", func(t *testing.T) {
		for _, p := range []domain.Person{f.anna, f.tom} {
			if _, err := svc.Book(ctx, anna, l.ID, p.ID); err != nil {
				t.Fatalf("book %s: %v", p.GivenName, err)
			}
		}
	})

	t.Run("not the family", func(t *testing.T) {
		for _, p := range []domain.PersonID{f.sam.ID, f.olga.ID, ids.New[domain.PersonID](f.seq), {}} {
			if _, err := svc.Book(ctx, anna, l.ID, p); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("book %v: want ErrNotFound, got %v", p, err)
			}
		}

		// A dependent does not book for its guardian.
		if _, err := svc.Book(ctx, f.principal(t, f.tom), l.ID, f.anna.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("book the guardian: want ErrNotFound, got %v", err)
		}
	})

	t.Run("unknown lesson", func(t *testing.T) {
		if _, err := svc.Book(ctx, anna, ids.New[domain.LessonID](f.seq), f.mia.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	if n := len(f.mails(t)); n != 3 {
		t.Errorf("%d mails, want one per booking (3)", n)
	}
}

// TestBookCreateFails maps what Bookings().Create reports when a concurrent
// transaction wins after the checks: a second active booking is
// already-booked, a person deleted meanwhile (a missing reference) is not
// found, never already-booked.
func TestBookCreateFails(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.regular(t, "2026-10-22T16:00:00Z")

	for _, tt := range []struct {
		name    string
		err     error
		want    error
		wantNot error
	}{
		{"duplicate", fmt.Errorf("person already has an active booking: %w", domain.ErrDuplicate), ErrAlreadyBooked, nil},
		{"person deleted", fmt.Errorf("foreign key violated: %w", domain.ErrConflict), domain.ErrNotFound, ErrAlreadyBooked},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := mail.NewRenderer()
			if err != nil {
				t.Fatal(err)
			}

			svc := NewBookingService(failingBookingStore{Store: f.st, createErr: tt.err},
				clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(r))

			_, err = svc.Book(t.Context(), anna, l.ID, f.mia.ID)
			if !errors.Is(err, tt.want) || (tt.wantNot != nil && errors.Is(err, tt.wantNot)) {
				t.Fatalf("want %v, got %v", tt.want, err)
			}
		})
	}
}

// failingBookingStore is a store whose Bookings().Create fails with
// createErr, as when a concurrent transaction commits after the service
// checked.
type failingBookingStore struct {
	store.Store

	createErr error
}

func (s failingBookingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, failingBookingTx{Tx: tx, createErr: s.createErr})
	})
}

type failingBookingTx struct {
	store.Tx

	createErr error
}

func (t failingBookingTx) Bookings() store.BookingRepo {
	return failingBookings{BookingRepo: t.Tx.Bookings(), createErr: t.createErr}
}

type failingBookings struct {
	store.BookingRepo

	createErr error
}

func (r failingBookings) Create(context.Context, domain.Booking) error { return r.createErr }

// TestBookImpersonated records the admin as the actor; the impersonated
// account gets the mail.
func TestBookImpersonated(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	admin := f.principal(t, f.person(t, "Ada", "", "ada@example.net", ""))
	anna := f.principal(t, f.anna)
	anna.ImpersonatorID = admin.AccountID
	l := f.regular(t, "2026-10-22T16:00:00Z")

	b, err := f.bookingService(t, bookingNow).Book(t.Context(), anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, events := f.booking(t, b.ID); len(events) != 1 || events[0].ActorAccountID != admin.AccountID {
		t.Fatalf("events %+v, want the admin as actor", events)
	}

	if mails := f.mails(t); len(mails) != 1 || mails[0].To != "anna@example.org" {
		t.Fatalf("mails %+v", mails)
	}

	c, err := f.bookingService(t, bookingNow).Cancel(t.Context(), anna, b.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, events := f.booking(t, c.ID); len(events) != 2 || events[1].ActorAccountID != admin.AccountID {
		t.Fatalf("events %+v, want the admin as actor", events)
	}
}

// TestBookCutoff closes booking exactly BookingCutoff before the start.
func TestBookCutoff(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.regular(t, "2026-10-22T16:00:00Z")
	edge := l.StartsAt.Add(-time.Hour)

	_, err := f.bookingServiceAt(t, edge).Book(t.Context(), anna, l.ID, f.mia.ID)
	wantClosed(t, err, ClosedCutoff)

	if _, err := f.bookingServiceAt(t, edge.Add(-time.Nanosecond)).Book(t.Context(), anna, l.ID, f.mia.ID); err != nil {
		t.Fatalf("one nanosecond before the cutoff: %v", err)
	}

	_, err = f.bookingServiceAt(t, l.StartsAt).Book(t.Context(), anna, l.ID, f.anna.ID)
	wantClosed(t, err, ClosedStarted)

	_, err = f.bookingServiceAt(t, l.EndsAt.Add(time.Hour)).Book(t.Context(), anna, l.ID, f.anna.ID)
	wantClosed(t, err, ClosedStarted)
}

// TestBookVisibility books what members may see, early access and early
// published lessons included; staff book under member rules.
func TestBookVisibility(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.bookingService(t, bookingNow)
	anna, olga := f.principal(t, f.anna), f.principal(t, f.olga)

	early := f.regular(t, "2026-10-28T17:00:00Z")  // after the release, within early access
	beyond := f.regular(t, "2026-11-10T17:00:00Z") // beyond early access
	workshop := f.oneOff(t, "2026-12-01T17:00:00Z", 5, domain.VisibilityPublic, domain.LessonWorkshop)
	members := f.oneOff(t, "2026-10-22T17:00:00Z", 5, domain.VisibilityMembers, domain.LessonRegular)
	hidden := f.oneOff(t, "2026-10-22T18:00:00Z", 5, domain.VisibilityHidden, domain.LessonRegular)

	for _, l := range []domain.Lesson{early, workshop, members} {
		if _, err := svc.Book(ctx, anna, l.ID, f.mia.ID); err != nil {
			t.Errorf("member books %s: %v", l.StartsAt, err)
		}

		if _, err := svc.Book(ctx, olga, l.ID, f.olga.ID); err != nil {
			t.Errorf("office books %s: %v", l.StartsAt, err)
		}
	}

	// Members get a 404 for what they may not see, like in the schedule;
	// staff see it, but may not book it under member rules.
	for _, l := range []domain.Lesson{beyond, hidden} {
		if _, err := svc.Book(ctx, anna, l.ID, f.mia.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("member books %s: want ErrNotFound, got %v", l.StartsAt, err)
		}

		_, err := svc.Book(ctx, olga, l.ID, f.olga.ID)
		wantClosed(t, err, ClosedRelease)
	}

	// An office account with more visible days books further ahead.
	olga.VisibleDays = 30
	if _, err := svc.Book(ctx, olga, beyond.ID, f.olga.ID); err != nil {
		t.Errorf("with 30 visible days: %v", err)
	}
}

// TestBookReleasedThrough books up to the manual released-through date,
// beyond the weekly release and early access (ADR 0003, amendment
// 2026-10-31), and not after it.
func TestBookReleasedThrough(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		s.ReleasedThrough = domain.Date{Year: 2026, Month: time.November, Day: 20}

		return errors.Join(err, tx.Settings().Put(ctx, s))
	})

	svc := f.bookingService(t, bookingNow)
	anna, olga := f.principal(t, f.anna), f.principal(t, f.olga)

	last := f.regular(t, "2026-11-20T22:00:00Z")  // Friday 23:00 in Berlin, the last day released
	after := f.regular(t, "2026-11-20T23:00:00Z") // Saturday 00:00 in Berlin

	if _, err := svc.Book(ctx, anna, last.ID, f.mia.ID); err != nil {
		t.Errorf("member books the last day released: %v", err)
	}

	if _, err := svc.Book(ctx, olga, last.ID, f.olga.ID); err != nil {
		t.Errorf("office books the last day released: %v", err)
	}

	if _, err := svc.Book(ctx, anna, after.ID, f.mia.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("member books the day after: want ErrNotFound, got %v", err)
	}

	_, err := svc.Book(ctx, olga, after.ID, f.olga.ID)
	wantClosed(t, err, ClosedRelease)
}

func TestBookCanceledLesson(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	l := f.regular(t, "2026-10-22T16:00:00Z")
	_, err := f.lessonService(t, bookingNow).CancelLesson(t.Context(), f.principal(t, f.olga), l.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.bookingService(t, bookingNow).Book(t.Context(), f.principal(t, f.anna), l.ID, f.mia.ID)
	wantClosed(t, err, ClosedCanceled)
}

// TestBookFull refuses the place beyond capacity; a cancellation frees it,
// and booking again makes a new booking.
func TestBookFull(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.bookingService(t, bookingNow)
	anna := f.principal(t, f.anna)
	l := f.oneOff(t, "2026-10-22T16:00:00Z", 2, domain.VisibilityPublic, domain.LessonRegular)

	first, err := svc.Book(ctx, anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Book(ctx, anna, l.ID, f.tom.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Book(ctx, anna, l.ID, f.anna.ID); !errors.Is(err, ErrLessonFull) {
		t.Fatalf("want ErrLessonFull, got %v", err)
	}

	if _, err := svc.Cancel(ctx, anna, first.ID); err != nil {
		t.Fatal(err)
	}

	again, err := svc.Book(ctx, anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	if again.ID == first.ID || len(f.bookingsOf(t, l)) != 3 {
		t.Fatalf("booking again: %+v, bookings %+v", again, f.bookingsOf(t, l))
	}
}

// TestBookLastPlaceConcurrently books the last place from many goroutines:
// exactly one gets it.
func TestBookLastPlaceConcurrently(t *testing.T) {
	t.Parallel()

	const n = 8

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.bookingService(t, bookingNow)
	anna := f.principal(t, f.anna)
	l := f.oneOff(t, "2026-10-22T16:00:00Z", 2, domain.VisibilityPublic, domain.LessonRegular)

	if _, err := svc.Book(ctx, anna, l.ID, f.anna.ID); err != nil {
		t.Fatal(err)
	}

	children := make([]domain.Person, n)
	for i := range children {
		children[i] = f.person(t, fmt.Sprint("Child ", i), "", "", "")
		f.write(t, func(ctx context.Context, tx store.Tx) error {
			return tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: f.anna.ID, DependentID: children[i].ID})
		})
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		ok   int
		errs []error
	)

	for _, c := range children {
		wg.Go(func() {
			_, err := svc.Book(ctx, anna, l.ID, c.ID)

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				ok++
			} else if !errors.Is(err, ErrLessonFull) {
				errs = append(errs, err)
			}
		})
	}

	wg.Wait()

	if ok != 1 || len(errs) > 0 {
		t.Fatalf("%d bookings succeeded, unexpected errors %v; want exactly one", ok, errs)
	}

	if active := len(slices.DeleteFunc(f.bookingsOf(t, l), func(b domain.Booking) bool {
		return b.Status == domain.BookingCanceled
	})); active != 2 {
		t.Fatalf("%d active bookings, want 2", active)
	}
}

// TestCancel cancels in time, late exactly from the cancellation cutoff on,
// and never after the start.
func TestCancel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.regular(t, "2026-10-22T16:00:00Z")
	edge := l.StartsAt.Add(-2 * time.Hour)
	book := f.bookingService(t, bookingNow)

	inTime, err := book.Book(ctx, anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	late, err := book.Book(ctx, anna, l.ID, f.anna.ID)
	if err != nil {
		t.Fatal(err)
	}

	tooLate, err := book.Book(ctx, anna, l.ID, f.tom.ID)
	if err != nil {
		t.Fatal(err)
	}

	before := f.bookingServiceAt(t, edge.Add(-time.Nanosecond))

	c, err := before.Cancel(ctx, anna, inTime.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The store keeps microseconds.
	got, events := f.booking(t, inTime.ID)
	if got.Status != c.Status || c.Status != domain.BookingCanceled || c.Late || got.Late ||
		!c.CanceledAt.Equal(edge.Add(-time.Nanosecond)) || !got.CanceledAt.Equal(edge.Add(-time.Microsecond)) ||
		len(events) != 2 || events[1].Status != domain.BookingCanceled || events[1].Late ||
		events[1].ActorAccountID != anna.AccountID {
		t.Fatalf("in time: %+v, stored %+v, events %+v", c, got, events)
	}

	c, err = f.bookingServiceAt(t, edge).Cancel(ctx, anna, late.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got, events := f.booking(t, late.ID); !c.Late || !got.Late || !events[1].Late {
		t.Fatalf("at the cutoff: %+v, stored %+v, events %+v", c, got, events)
	}

	// Mails go to the acting account, the late one says so.
	mails := slices.DeleteFunc(f.mails(t), func(m domain.OutboxMail) bool {
		return m.Template != mail.TemplateBookingCanceled
	})
	if len(mails) != 2 || mails[0].To != "anna@example.org" || strings.Contains(mails[0].Text, "Späte Absage") ||
		!strings.Contains(mails[1].Text, "Späte Absage") || !strings.Contains(mails[1].Text, "Gebucht für: Anna Example") {
		t.Fatalf("cancellation mails %+v", mails)
	}

	t.Run("canceled already", func(t *testing.T) {
		if _, err := before.Cancel(ctx, anna, inTime.ID); !errors.Is(err, ErrBookingCanceled) {
			t.Fatalf("want ErrBookingCanceled, got %v", err)
		}
	})

	t.Run("started", func(t *testing.T) {
		if _, err := f.bookingServiceAt(t, l.StartsAt).Cancel(ctx, anna, tooLate.ID); !errors.Is(err, ErrLessonStarted) {
			t.Fatalf("want ErrLessonStarted, got %v", err)
		}
	})

	t.Run("not the family", func(t *testing.T) {
		olga := f.principal(t, f.olga)
		for _, id := range []domain.BookingID{tooLate.ID, ids.New[domain.BookingID](f.seq)} {
			if _, err := before.Cancel(ctx, olga, id); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("cancel %v: want ErrNotFound, got %v", id, err)
			}
		}

		// Tom's own account may cancel his booking.
		if _, err := before.Cancel(ctx, f.principal(t, f.tom), tooLate.ID); err != nil {
			t.Fatal(err)
		}
	})
}

// TestCancelCanceledLesson keeps the bookings of a lesson the school
// canceled; canceling one is never late.
func TestCancelCanceledLesson(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.regular(t, "2026-10-22T16:00:00Z")

	b, err := f.bookingService(t, bookingNow).Book(ctx, anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.lessonService(t, bookingNow).CancelLesson(ctx, f.principal(t, f.olga), l.ID, ""); err != nil {
		t.Fatal(err)
	}

	if got, _ := f.booking(t, b.ID); got.Status != domain.BookingBooked {
		t.Fatalf("booking of the canceled lesson: %+v", got)
	}

	c, err := f.bookingServiceAt(t, l.StartsAt.Add(-time.Minute)).Cancel(ctx, anna, b.ID)
	if err != nil || c.Late {
		t.Fatalf("cancel: %+v, %v; want in time", c, err)
	}
}

// TestCancelFollowingMovedBooking: a booking that a concurrent merge moved
// to another person before the lesson lock (here Mia's, merged into Tom)
// is no longer among the person's bookings read under the locks, so
// nothing is canceled and nothing mailed, and the call succeeds with no
// bookings. This pins the current behavior.
func TestCancelFollowingMovedBooking(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})
	l := weeklyLessons(t, f, c, 9, "2026-10-27")[0]
	f.bookAs(t, f.mia, l)
	b := f.bookingIn(t, l, f.mia)

	moved := editingStore{Store: f.st, onLock: func(ctx context.Context, tx store.Tx, locked domain.Lesson) domain.Lesson {
		got, err := tx.Bookings().Get(ctx, b.ID)
		if err == nil && got.PersonID == f.mia.ID {
			got.PersonID = f.tom.ID
			err = tx.Bookings().Update(ctx, got)
		}

		if err != nil {
			t.Error(err)
		}

		return locked
	}}

	r, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	svc := NewBookingService(moved, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(r))

	out, err := svc.CancelFollowing(ctx, f.principal(t, f.anna), b.ID, domain.Date{})
	if err != nil || len(out.Bookings) != 0 || !out.PausedUntil.IsZero() {
		t.Fatalf("CancelFollowing = %+v, %v; want nothing canceled", out, err)
	}

	if got, _ := f.booking(t, b.ID); got.Status != domain.BookingBooked || got.PersonID != f.tom.ID {
		t.Errorf("booking %+v, want Tom's, active", got)
	}

	if mails := f.mails(t); len(mails) != 0 {
		t.Errorf("mails %+v", mails)
	}
}

// TestCancelBeforeBooked refuses a cancellation at a time before the
// booking was made (a clock behind the one that booked) as a validation
// error, and keeps the booking.
func TestCancelBeforeBooked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.regular(t, "2026-10-22T16:00:00Z")

	b, err := f.bookingService(t, bookingNow).Book(ctx, anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.bookingServiceAt(t, b.BookedAt.Add(-time.Second)).Cancel(ctx, anna, b.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("cancel before the booking: %v, want a validation error", err)
	}

	if got, events := f.booking(t, b.ID); got.Status != domain.BookingBooked || len(events) != 1 {
		t.Fatalf("booking %+v, events %+v; want it kept", got, events)
	}
}

// TestCancelFollowing previews and cancels a person's bookings of a class
// from one lesson on.
func TestCancelFollowing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})

	// Tuesdays at 18:00 in Berlin (17:00 UTC in winter time).
	var lessons []domain.Lesson

	for _, d := range []string{"2026-10-20", "2026-10-27", "2026-11-03", "2026-11-10", "2026-11-17"} {
		slot := mustDate(t, d)
		start := slot.At(c.StartTime, berlin(t))
		l := domain.Lesson{
			ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: slot, CourseTypeID: f.courseType,
			RoomID: f.room, InstructorID: f.instructor, StartsAt: start, EndsAt: start.Add(c.Duration),
			Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 9,
		}
		f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })
		lessons = append(lessons, l)
	}

	// Mia is booked into every lesson but 2026-11-10, where Anna is; the
	// first lesson is over at "now".
	f.bookAs(t, f.mia, lessons[0], lessons[1], lessons[2], lessons[4])
	f.bookAs(t, f.anna, lessons[3])

	oneOff := f.regular(t, "2026-11-04T17:00:00Z")
	f.bookAs(t, f.mia, oneOff)

	// "Now" is 2026-10-27, 17:30 in Berlin: the lesson of that day is within
	// the cancellation cutoff.
	now := "2026-10-27T16:30:00Z"
	svc := f.bookingService(t, now)
	start := f.bookingIn(t, lessons[1], f.mia)

	preview, err := svc.PreviewCancelFollowing(ctx, anna, start.ID, domain.Date{})
	if err != nil {
		t.Fatal(err)
	}

	type item struct {
		lesson domain.LessonID
		late   bool
	}

	summarize := func(fbs []FollowingBooking) []item {
		out := make([]item, len(fbs))
		for i, fb := range fbs {
			out[i] = item{fb.Lesson.ID, fb.Late}
		}

		return out
	}

	want := []item{{lessons[1].ID, true}, {lessons[2].ID, false}, {lessons[4].ID, false}}
	if got := summarize(preview.Bookings); !slices.Equal(got, want) {
		t.Fatalf("preview %+v, want %+v", got, want)
	}

	if n := len(f.mails(t)); n != 0 {
		t.Fatalf("the preview mailed %d times", n)
	}

	done, err := svc.CancelFollowing(ctx, anna, start.ID, domain.Date{})
	if err != nil {
		t.Fatal(err)
	}

	if got := summarize(done.Bookings); !slices.Equal(got, want) {
		t.Fatalf("canceled %+v, want %+v", got, want)
	}

	for _, fb := range done.Bookings {
		got, events := f.booking(t, fb.Booking.ID)
		if got.Status != domain.BookingCanceled || got.Late != fb.Late || len(events) != 1 || events[0].Late != fb.Late {
			t.Errorf("stored %+v, events %+v", got, events)
		}
	}

	for _, l := range []domain.Lesson{lessons[0], lessons[3], oneOff} {
		for _, b := range f.bookingsOf(t, l) {
			if b.Status != domain.BookingBooked {
				t.Errorf("booking of %s changed: %+v", l.StartsAt, b)
			}
		}
	}

	// One mail lists them all.
	mails := f.mails(t)
	if len(mails) != 1 || mails[0].Template != mail.TemplateBookingCanceled ||
		!strings.Contains(mails[0].Subject, "3 Buchungen") || strings.Count(mails[0].Text, "Späte Absage") != 1 {
		t.Fatalf("mails %+v", mails)
	}

	t.Run("errors", func(t *testing.T) {
		if _, err := svc.CancelFollowing(ctx, anna, start.ID, domain.Date{}); !errors.Is(err, ErrBookingCanceled) {
			t.Errorf("again: want ErrBookingCanceled, got %v", err)
		}

		if _, err := svc.PreviewCancelFollowing(ctx, anna, f.bookingIn(t, oneOff, f.mia).ID, domain.Date{}); !errors.Is(err, ErrNotClassBooking) {
			t.Errorf("one-off: want ErrNotClassBooking, got %v", err)
		}

		past := f.bookingIn(t, lessons[0], f.mia)
		if _, err := svc.CancelFollowing(ctx, anna, past.ID, domain.Date{}); !errors.Is(err, ErrLessonStarted) {
			t.Errorf("started: want ErrLessonStarted, got %v", err)
		}

		annas := f.bookingIn(t, lessons[3], f.anna)
		if _, err := svc.PreviewCancelFollowing(ctx, f.principal(t, f.olga), annas.ID, domain.Date{}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("stranger: want ErrNotFound, got %v", err)
		}
	})
}

// TestCancelFollowingMovedLesson keeps the requested booking first when the
// office moved a later lesson of the class before it.
func TestCancelFollowingMovedLesson(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})

	var lessons []domain.Lesson

	for _, d := range []string{"2026-10-27", "2026-11-03", "2026-11-10"} {
		slot := mustDate(t, d)
		start := slot.At(c.StartTime, berlin(t))
		l := domain.Lesson{
			ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: slot, CourseTypeID: f.courseType,
			RoomID: f.room, InstructorID: f.instructor, StartsAt: start, EndsAt: start.Add(c.Duration),
			Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 9,
		}
		f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })
		lessons = append(lessons, l)
	}

	// The office moved the lesson of 2026-11-03 to Monday 2026-10-26, before
	// the lesson of 2026-10-27.
	moved := lessons[1]
	moved.Detached = true
	moved.StartsAt = mustInstant(t, "2026-10-26T17:00:00Z")
	moved.EndsAt = moved.StartsAt.Add(c.Duration)
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, moved) })

	f.bookAs(t, f.mia, lessons...)

	svc := f.bookingService(t, bookingNow)
	start := f.bookingIn(t, lessons[0], f.mia)
	want := []domain.LessonID{lessons[0].ID, moved.ID, lessons[2].ID}

	lessonsOf := func(fbs []FollowingBooking) []domain.LessonID {
		out := make([]domain.LessonID, len(fbs))
		for i, fb := range fbs {
			out[i] = fb.Lesson.ID
		}

		return out
	}

	preview, err := svc.PreviewCancelFollowing(ctx, anna, start.ID, domain.Date{})
	if err != nil {
		t.Fatal(err)
	}

	if got := lessonsOf(preview.Bookings); !slices.Equal(got, want) {
		t.Fatalf("preview %v, want %v: the requested booking first, then by start", got, want)
	}

	// The mail about the three bookings in one room reads it, and its
	// location, once.
	r, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	counting := &countingStore{Store: f.st}

	done, err := NewBookingService(counting, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(r)).
		CancelFollowing(ctx, anna, start.ID, domain.Date{})
	if err != nil {
		t.Fatal(err)
	}

	if got := lessonsOf(done.Bookings); !slices.Equal(got, want) {
		t.Fatalf("canceled %v, want %v", got, want)
	}

	if counting.rooms != 1 || counting.locations != 1 {
		t.Errorf("read the room %d and the location %d times, want once each", counting.rooms, counting.locations)
	}

	// The mail lists the lessons by start, the moved one first.
	mails := f.mails(t)
	if len(mails) != 1 {
		t.Fatalf("mails %+v", mails)
	}

	text := mails[0].Text
	atMoved, atRequested, atLast := strings.Index(text, "26. Oktober"), strings.Index(text, "27. Oktober"),
		strings.Index(text, "10. November")

	if atMoved < 0 || atMoved > atRequested || atRequested > atLast {
		t.Errorf("mail lessons not by start:\n%s", text)
	}
}

// TestCancelFollowingConcurrentEdits cancels what holds under the locks: a
// lesson moved into the past meanwhile is skipped, not a failure, and a
// booking made meanwhile is canceled too. Only the requested booking's own
// lesson having started fails the request.
func TestCancelFollowingConcurrentEdits(t *testing.T) {
	t.Parallel()

	// setup builds Tuesday lessons from 2026-10-27 on, with Mia booked into
	// all but the last one.
	setup := func(t *testing.T) (*bookingFixture, []domain.Lesson) {
		t.Helper()

		f := newBookingFixture(t)
		c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 18})

		var lessons []domain.Lesson

		for _, d := range []string{"2026-10-27", "2026-11-03", "2026-11-10", "2026-11-17"} {
			slot := mustDate(t, d)
			start := slot.At(c.StartTime, berlin(t))
			l := domain.Lesson{
				ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: slot, CourseTypeID: f.courseType,
				RoomID: f.room, InstructorID: f.instructor, StartsAt: start, EndsAt: start.Add(c.Duration),
				Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 9,
			}
			f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })
			lessons = append(lessons, l)
		}

		f.bookAs(t, f.mia, lessons[:3]...)

		return f, lessons
	}

	service := func(t *testing.T, f *bookingFixture, onLock func(context.Context, store.Tx, domain.Lesson) domain.Lesson) *BookingService {
		t.Helper()

		r, err := mail.NewRenderer()
		if err != nil {
			t.Fatal(err)
		}

		return NewBookingService(editingStore{Store: f.st, onLock: onLock}, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(r))
	}

	past := func(l domain.Lesson) domain.Lesson {
		l.Detached, l.StartsAt, l.EndsAt = true, mustInstant(t, "2026-10-21T09:00:00Z"), mustInstant(t, "2026-10-21T10:00:00Z")

		return l
	}

	t.Run("moved and booked meanwhile", func(t *testing.T) {
		t.Parallel()

		f, lessons := setup(t)
		booked := false

		svc := service(t, f, func(ctx context.Context, tx store.Tx, l domain.Lesson) domain.Lesson {
			if !booked {
				// Another transaction booked Mia into the last lesson
				// before the first lock.
				booked = true
				err := tx.Bookings().Create(ctx, domain.Booking{
					ID: ids.New[domain.BookingID](f.seq), LessonID: lessons[3].ID, PersonID: f.mia.ID,
					Status: domain.BookingBooked, Source: domain.BookingSourceSelf, BookedAt: mustInstant(t, bookingNow),
				})
				if err != nil {
					t.Fatal(err)
				}
			}

			if l.ID == lessons[1].ID {
				return past(l) // the office moved it into the past
			}

			return l
		})

		done, err := svc.CancelFollowing(t.Context(), f.principal(t, f.anna), f.bookingIn(t, lessons[0], f.mia).ID, domain.Date{})
		if err != nil {
			t.Fatal(err)
		}

		got := make([]domain.LessonID, len(done.Bookings))
		for i, fb := range done.Bookings {
			got[i] = fb.Lesson.ID
		}

		if want := []domain.LessonID{lessons[0].ID, lessons[2].ID, lessons[3].ID}; !slices.Equal(got, want) {
			t.Fatalf("canceled %v, want %v", got, want)
		}

		if b := f.bookingIn(t, lessons[1], f.mia); b.Status != domain.BookingBooked {
			t.Errorf("the moved lesson's booking changed: %+v", b)
		}
	})

	t.Run("requested lesson started meanwhile", func(t *testing.T) {
		t.Parallel()

		f, lessons := setup(t)
		svc := service(t, f, func(_ context.Context, _ store.Tx, l domain.Lesson) domain.Lesson {
			if l.ID == lessons[0].ID {
				return past(l)
			}

			return l
		})

		_, err := svc.CancelFollowing(t.Context(), f.principal(t, f.anna), f.bookingIn(t, lessons[0], f.mia).ID, domain.Date{})
		if !errors.Is(err, ErrLessonStarted) {
			t.Fatalf("want ErrLessonStarted, got %v", err)
		}
	})
}

// editingStore passes every lesson it locks through onLock, which may
// change the store or the locked row, as a concurrent transaction that
// committed just before the lock would.
type editingStore struct {
	store.Store

	onLock func(context.Context, store.Tx, domain.Lesson) domain.Lesson
}

func (s editingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, editingTx{Tx: tx, onLock: s.onLock})
	})
}

type editingTx struct {
	store.Tx

	onLock func(context.Context, store.Tx, domain.Lesson) domain.Lesson
}

func (t editingTx) Lessons() store.LessonRepo { return editingLessons{t.Tx.Lessons(), t} }

type editingLessons struct {
	store.LessonRepo

	tx editingTx
}

func (r editingLessons) Lock(ctx context.Context, id domain.LessonID) (domain.Lesson, error) {
	l, err := r.LessonRepo.Lock(ctx, id)
	if err != nil {
		return l, err
	}

	return r.tx.onLock(ctx, r.tx.Tx, l), nil
}

// bookAs stores active bookings of p, made a week before bookingNow.
func (f *bookingFixture) bookAs(t *testing.T, p domain.Person, lessons ...domain.Lesson) {
	t.Helper()

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		for _, l := range lessons {
			err := tx.Bookings().Create(ctx, domain.Booking{
				ID: ids.New[domain.BookingID](f.seq), LessonID: l.ID, PersonID: p.ID, Status: domain.BookingBooked,
				Source: domain.BookingSourceSelf, BookedAt: mustInstant(t, "2026-10-14T10:00:00Z"),
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// bookingIn returns the booking of p in l.
func (f *bookingFixture) bookingIn(t *testing.T, l domain.Lesson, p domain.Person) domain.Booking {
	t.Helper()

	for _, b := range f.bookingsOf(t, l) {
		if b.PersonID == p.ID {
			return b
		}
	}

	t.Fatalf("%s has no booking in %s", p.GivenName, l.StartsAt)

	return domain.Booking{}
}

// TestScheduleAvailability shows free places to everyone and the family's
// bookings to the signed-in account.
func TestScheduleAvailability(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	full := f.oneOff(t, "2026-10-22T16:00:00Z", 2, domain.VisibilityPublic, domain.LessonRegular)
	open := f.regular(t, "2026-10-23T16:00:00Z")

	f.bookAs(t, f.sam, full)
	f.bookAs(t, f.mia, full)
	f.bookAs(t, f.anna, open)
	f.bookAs(t, f.tom, open)

	// A canceled booking frees its place and is not shown.
	if _, err := f.bookingService(t, bookingNow).Cancel(ctx, anna, f.bookingIn(t, open, f.tom).ID); err != nil {
		t.Fatal(err)
	}

	svc := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, bookingNow)))
	q := ScheduleQuery{From: mustDate(t, "2026-10-22"), To: mustDate(t, "2026-10-24")}

	type avail struct {
		free   int
		booked string
	}

	summarize := func(sl ScheduledLesson) avail {
		names := make([]string, len(sl.BookedFor))
		for i, b := range sl.BookedFor {
			names[i] = b.Person.GivenName
		}

		return avail{sl.FreePlaces, strings.Join(names, ",")}
	}

	for _, tt := range []struct {
		name string
		p    Principal
		want []avail
	}{
		{"guest", Principal{}, []avail{{0, ""}, {4, ""}}},
		{"anna", anna, []avail{{0, "Mia"}, {4, "Anna"}}},
		{"office", f.principal(t, f.olga), []avail{{0, ""}, {4, ""}}},
	} {
		s, err := svc.Schedule(ctx, tt.p, q)
		if err != nil {
			t.Fatal(err)
		}

		got := make([]avail, len(s.Lessons))
		for i, sl := range s.Lessons {
			got[i] = summarize(sl)
		}

		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: schedule %+v, want %+v", tt.name, got, tt.want)
		}

		for i, l := range []domain.Lesson{full, open} {
			sl, err := svc.Lesson(ctx, tt.p, l.ID)
			if err != nil {
				t.Fatal(err)
			}

			if got := summarize(sl); got != tt.want[i] {
				t.Errorf("%s: lesson %s %+v, want %+v", tt.name, l.StartsAt, got, tt.want[i])
			}
		}
	}

	// The booking IDs let the member cancel from the schedule.
	s, err := svc.Schedule(ctx, anna, q)
	if err != nil {
		t.Fatal(err)
	}

	if b := s.Lessons[0].BookedFor; len(b) != 1 || b[0].BookingID != f.bookingIn(t, full, f.mia).ID ||
		b[0].Person.ID != f.mia.ID || b[0].Name != "Mia Example" {
		t.Errorf("booked for %+v", b)
	}

	// Several of the family in family order, even beyond the capacity
	// (free places stay at 0).
	f.bookAs(t, f.tom, full)

	s, err = svc.Schedule(ctx, anna, q)
	if err != nil {
		t.Fatal(err)
	}

	if got := summarize(s.Lessons[0]); got != (avail{0, "Mia,Tom"}) {
		t.Errorf("over capacity: %+v", got)
	}

	// Everyone, guests included, gets the cutoffs: booking closes an hour
	// and a cancellation is late two hours before the start, except for a
	// lesson the school canceled, which is never late.
	canceled := open
	canceled.Status, canceled.CanceledAt = domain.LessonCanceled, mustInstant(t, bookingNow)
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, canceled) })

	type cutoffs struct{ closes, late time.Time }

	want := []cutoffs{
		{mustInstant(t, "2026-10-22T15:00:00Z"), mustInstant(t, "2026-10-22T14:00:00Z")},
		{mustInstant(t, "2026-10-23T15:00:00Z"), time.Time{}},
	}

	for _, p := range []Principal{{}, anna, f.principal(t, f.olga)} {
		s, err := svc.Schedule(ctx, p, q)
		if err != nil {
			t.Fatal(err)
		}

		for i, l := range []domain.Lesson{full, open} {
			sl, err := svc.Lesson(ctx, p, l.ID)
			if err != nil {
				t.Fatal(err)
			}

			for _, got := range []ScheduledLesson{s.Lessons[i], sl} {
				if !got.BookingClosesAt.Equal(want[i].closes) || !got.LateCancelFrom.Equal(want[i].late) {
					t.Errorf("%v: cutoffs of %s: closes %s, late %s, want %+v", p.Role, l.StartsAt,
						got.BookingClosesAt, got.LateCancelFrom, want[i])
				}
			}
		}
	}
}

// TestLessonCountsBookingsOnce counts a lesson's active bookings once, also
// for the office, whose details carry the count too.
func TestLessonCountsBookingsOnce(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	l := f.regular(t, "2026-10-22T16:00:00Z")
	f.bookAs(t, f.mia, l)

	counting := &countingStore{Store: f.st}
	svc := NewScheduleService(counting, clock.NewFixed(mustInstant(t, bookingNow)))

	for _, p := range []domain.Person{f.anna, f.olga} {
		counting.counts = 0

		sl, err := svc.Lesson(t.Context(), f.principal(t, p), l.ID)
		if err != nil {
			t.Fatal(err)
		}

		if sl.FreePlaces != 4 || (sl.Admin != nil && sl.Admin.ActiveBookings != 1) {
			t.Errorf("%s: free places %d, admin %+v", p.GivenName, sl.FreePlaces, sl.Admin)
		}

		if counting.counts != 1 {
			t.Errorf("%s: active bookings counted %d times, want once", p.GivenName, counting.counts)
		}
	}
}

// countingStore counts the queries for active booking counts and the reads
// of single rooms and locations.
type countingStore struct {
	store.Store

	counts, rooms, locations int
}

func (s *countingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, countingTx{Tx: tx, s: s})
	})
}

type countingTx struct {
	store.Tx

	s *countingStore
}

func (t countingTx) Bookings() store.BookingRepo { return countingBookings{t.Tx.Bookings(), t.s} }

func (t countingTx) Rooms() store.RoomRepo { return countingRooms{t.Tx.Rooms(), t.s} }

func (t countingTx) Locations() store.LocationRepo { return countingLocations{t.Tx.Locations(), t.s} }

type countingRooms struct {
	store.RoomRepo

	s *countingStore
}

func (r countingRooms) Get(ctx context.Context, id domain.RoomID) (domain.Room, error) {
	r.s.rooms++

	return r.RoomRepo.Get(ctx, id)
}

type countingLocations struct {
	store.LocationRepo

	s *countingStore
}

func (r countingLocations) Get(ctx context.Context, id domain.LocationID) (domain.Location, error) {
	r.s.locations++

	return r.LocationRepo.Get(ctx, id)
}

type countingBookings struct {
	store.BookingRepo

	s *countingStore
}

func (r countingBookings) CountActive(ctx context.Context, lesson domain.LessonID) (int, error) {
	r.s.counts++

	return r.BookingRepo.CountActive(ctx, lesson)
}

func (r countingBookings) CountActiveByLessons(
	ctx context.Context, lessons []domain.LessonID,
) (map[domain.LessonID]int, error) {
	r.s.counts++

	return r.BookingRepo.CountActiveByLessons(ctx, lessons)
}
