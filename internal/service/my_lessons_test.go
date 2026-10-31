package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The "My lessons" tests run on the booking fixture (Europe/Berlin, booking
// cutoff an hour, cancellation cutoff two hours): Anna's family is Anna, Mia
// and Tom (a managed dependent with his own account); Sam belongs to
// Stella. At bookingNow, Wednesday 2026-10-21, 12:00 in Berlin.

func (f *bookingFixture) myLessons(t *testing.T, now string) *MyLessonsService {
	t.Helper()

	return NewMyLessonsService(f.st, clock.NewFixed(mustInstant(t, now)))
}

// put stores a booking of p in l with status, booked at bookedAt; a
// canceled one is canceled an hour later, with the late flag.
func (f *bookingFixture) put(
	t *testing.T, l domain.Lesson, p domain.Person, status domain.BookingStatus, late bool, bookedAt string,
) domain.Booking {
	t.Helper()

	b := domain.Booking{
		ID: ids.New[domain.BookingID](f.seq), LessonID: l.ID, PersonID: p.ID, Status: status,
		Source: domain.BookingSourceSelf, BookedAt: mustInstant(t, bookedAt),
	}
	if status == domain.BookingCanceled {
		b.CanceledAt, b.Late, b.Released = b.BookedAt.Add(time.Hour), late, true
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

	return b
}

// cancelLesson marks l as canceled by the school.
func (f *bookingFixture) cancelLesson(t *testing.T, l domain.Lesson) domain.Lesson {
	t.Helper()

	l.Status = domain.LessonCanceled
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, l) })

	return l
}

func bookingIDs(items []MyBooking) []domain.BookingID {
	out := make([]domain.BookingID, len(items))
	for i, it := range items {
		out[i] = it.Booking.ID
	}

	return out
}

func wantBookings(t *testing.T, got []MyBooking, want ...domain.Booking) {
	t.Helper()

	ids := make([]domain.BookingID, len(want))
	for i, b := range want {
		ids[i] = b.ID
	}

	if !slices.Equal(bookingIDs(got), ids) {
		t.Fatalf("bookings %v, want %v", bookingIDs(got), ids)
	}
}

// TestMyLessonsUpcoming lists the family's active bookings of lessons not
// over yet, by start, whatever the lessons' visibility, with what the page
// shows of each.
func TestMyLessonsUpcoming(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)

	over := f.regular(t, "2026-10-21T08:00:00Z")    // 08:00–09:00 UTC, over at 10:00
	running := f.regular(t, "2026-10-21T09:30:00Z") // until 10:30
	soon := f.regular(t, "2026-10-21T11:30:00Z")    // within the cancellation cutoff
	later := f.regular(t, "2026-10-22T16:00:00Z")
	hidden := f.oneOff(t, "2026-10-23T16:00:00Z", 5, domain.VisibilityHidden, domain.LessonPrivate)
	canceled := f.cancelLesson(t, f.regular(t, "2026-10-24T16:00:00Z"))

	f.put(t, over, f.anna, domain.BookingAttended, false, "2026-10-14T10:00:00Z")
	inRunning := f.put(t, running, f.mia, domain.BookingBooked, false, "2026-10-14T10:00:00Z")
	inSoon := f.put(t, soon, f.anna, domain.BookingBooked, false, "2026-10-14T10:00:00Z")
	tomsLater := f.put(t, later, f.tom, domain.BookingBooked, false, "2026-10-15T10:00:00Z")
	annasLater := f.put(t, later, f.anna, domain.BookingBooked, false, "2026-10-14T10:00:00Z")
	f.put(t, later, f.mia, domain.BookingCanceled, true, "2026-10-14T09:00:00Z") // canceled: not listed
	f.put(t, later, f.sam, domain.BookingBooked, false, "2026-10-14T08:00:00Z")  // a stranger's
	inHidden := f.put(t, hidden, f.mia, domain.BookingBooked, false, "2026-10-16T10:00:00Z")
	inCanceled := f.put(t, canceled, f.anna, domain.BookingBooked, false, "2026-10-16T10:00:00Z")

	got, err := f.myLessons(t, bookingNow).Upcoming(t.Context(), anna)
	if err != nil {
		t.Fatal(err)
	}

	if got.Timezone != "Europe/Berlin" || !got.Now.Equal(mustInstant(t, bookingNow)) {
		t.Errorf("timezone %q, now %v", got.Timezone, got.Now)
	}

	// By start; in one lesson by booking time.
	wantBookings(t, got.Upcoming, inRunning, inSoon, annasLater, tomsLater, inHidden, inCanceled)

	byID := map[domain.BookingID]MyBooking{}
	for _, it := range got.Upcoming {
		byID[it.Booking.ID] = it
	}

	// A lesson that has started cannot be canceled any more.
	if it := byID[inRunning.ID]; it.Cancelable || it.Person.ID != f.mia.ID || it.Name != "Mia Example" {
		t.Errorf("running: %+v", it)
	}

	it := byID[inSoon.ID]
	sl := it.Lesson
	if !it.Cancelable || it.Booking.Status != domain.BookingBooked || sl.Lesson.ID != soon.ID ||
		sl.CourseType.Name != "Imaginary Ballet" || sl.Room.Name != "Room One" || sl.Location.Name != "Studio Nowhere" ||
		sl.Instructor.DisplayName != "Ada Example" ||
		!sl.BookingClosesAt.Equal(mustInstant(t, "2026-10-21T10:30:00Z")) ||
		!sl.LateCancelFrom.Equal(mustInstant(t, "2026-10-21T09:30:00Z")) {
		t.Errorf("soon: %+v", it)
	}

	// The managed dependent is part of the family.
	if it := byID[tomsLater.ID]; it.Person.ID != f.tom.ID || it.Name != "Tom Example" || !it.Cancelable {
		t.Errorf("tom: %+v", it)
	}

	// A lesson the school canceled: the booking stays, may still be
	// canceled, and never late.
	if it := byID[inCanceled.ID]; it.Lesson.Lesson.Status != domain.LessonCanceled || !it.Cancelable ||
		!it.Lesson.LateCancelFrom.IsZero() {
		t.Errorf("canceled lesson: %+v", it)
	}

	// Tom, signed in himself, sees his own bookings only; Stella none of
	// Anna's family.
	tom, err := f.myLessons(t, bookingNow).Upcoming(t.Context(), f.principal(t, f.tom))
	if err != nil {
		t.Fatal(err)
	}

	wantBookings(t, tom.Upcoming, tomsLater)

	stella, err := f.myLessons(t, bookingNow).Upcoming(t.Context(), f.principal(t, f.stella(t)))
	if err != nil {
		t.Fatal(err)
	}

	if len(stella.Upcoming) != 1 || stella.Upcoming[0].Person.ID != f.sam.ID {
		t.Errorf("stella: %+v", stella.Upcoming)
	}

	// Signed out: nothing.
	none, err := f.myLessons(t, bookingNow).Upcoming(t.Context(), Principal{})
	if err != nil || len(none.Upcoming) != 0 || len(none.Waitlist) != 0 {
		t.Errorf("signed out: %+v, %v", none, err)
	}
}

// TestMyLessonsWaitlist lists the family's open waiting-list entries of
// lessons that have not started, with their place in the list.
func TestMyLessonsWaitlist(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)

	full := f.oneOff(t, "2026-10-23T16:00:00Z", 1, domain.VisibilityPublic, domain.LessonWorkshop)
	earlier := f.oneOff(t, "2026-10-22T16:00:00Z", 1, domain.VisibilityHidden, domain.LessonWorkshop)
	started := f.oneOff(t, "2026-10-21T09:30:00Z", 1, domain.VisibilityPublic, domain.LessonWorkshop)

	entry := func(l domain.Lesson, p domain.Person, status domain.WaitlistStatus, joined string) domain.WaitlistEntry {
		t.Helper()

		e := domain.WaitlistEntry{
			ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: l.ID, PersonID: p.ID, Status: status,
			JoinedAt: mustInstant(t, joined), StayOnList: true,
		}
		f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, e) })

		return e
	}

	// Sam (a stranger) waits first, then Mia; Tom's invite is open, Anna's
	// ran out (the job has not ended it yet).
	entry(full, f.sam, domain.WaitlistWaiting, "2026-10-19T10:00:00Z")
	mia := entry(full, f.mia, domain.WaitlistWaiting, "2026-10-19T11:00:00Z")
	tom := f.invitedEntry(t, full, f.tom, 1, "2026-10-21T20:00:00Z")
	annas := f.invitedEntry(t, earlier, f.anna, 2, "2026-10-21T09:00:00Z")
	entry(earlier, f.mia, domain.WaitlistWithdrawn, "2026-10-18T10:00:00Z") // ended
	entry(started, f.mia, domain.WaitlistWaiting, "2026-10-18T10:00:00Z")   // the lesson has started

	got, err := f.myLessons(t, bookingNow).Upcoming(t.Context(), anna)
	if err != nil {
		t.Fatal(err)
	}

	var entries []domain.WaitlistEntryID
	for _, w := range got.Waitlist {
		entries = append(entries, w.Entry.ID)
	}

	// By the lesson's start, then like the family: the signed-in person
	// first, then the dependents by name.
	if want := []domain.WaitlistEntryID{annas.ID, mia.ID, tom.ID}; !slices.Equal(entries, want) {
		t.Fatalf("entries %v, want %v", entries, want)
	}

	if w := got.Waitlist[0]; w.Status != domain.WaitlistExpired || w.Position != 0 || w.Lesson.Lesson.ID != earlier.ID ||
		w.Name != "Anna Example" || w.Lesson.CourseType.Name != "Imaginary Ballet" {
		t.Errorf("anna's: %+v", w)
	}

	if w := got.Waitlist[2]; w.Status != domain.WaitlistInvited || w.Position != 0 ||
		!w.Entry.ExpiresAt.Equal(mustInstant(t, "2026-10-21T20:00:00Z")) || w.Person.ID != f.tom.ID {
		t.Errorf("tom's: %+v", w)
	}

	if w := got.Waitlist[1]; w.Status != domain.WaitlistWaiting || w.Position != 2 || !w.Entry.StayOnList ||
		!w.Lesson.BookingClosesAt.Equal(mustInstant(t, "2026-10-23T15:00:00Z")) {
		t.Errorf("mia's: %+v", w)
	}

	if len(got.Upcoming) != 0 {
		t.Errorf("upcoming %+v", got.Upcoming)
	}
}

// TestMyLessonsHistory pages back through the family's bookings of lessons
// that are over, three months a page, newest first, with every status.
func TestMyLessonsHistory(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)

	// bookingNow is 2026-10-21 12:00 in Berlin; three months back is
	// 2026-07-21 12:00 (10:00 UTC, summer time), then 2026-04-21 12:00
	// (10:00 UTC) and 2026-01-21 12:00 (11:00 UTC, winter time).
	running := f.regular(t, "2026-10-21T09:30:00Z")
	yesterday := f.regular(t, "2026-10-20T16:00:00Z")
	lastWeek := f.regular(t, "2026-10-14T16:00:00Z")
	hidden := f.oneOff(t, "2026-09-30T16:00:00Z", 5, domain.VisibilityHidden, domain.LessonPrivate)
	september := f.regular(t, "2026-09-01T16:00:00Z")
	canceled := f.cancelLesson(t, f.regular(t, "2026-08-01T16:00:00Z"))
	atEdge := f.regular(t, "2026-07-21T10:00:00Z")
	justBefore := f.regular(t, "2026-07-21T09:59:00Z")
	january := f.regular(t, "2026-01-10T16:00:00Z")

	f.put(t, running, f.mia, domain.BookingBooked, false, "2026-10-14T10:00:00Z") // not over yet
	attended := f.put(t, yesterday, f.mia, domain.BookingAttended, false, "2026-10-14T10:00:00Z")
	noShow := f.put(t, yesterday, f.tom, domain.BookingNoShow, false, "2026-10-14T11:00:00Z")
	f.put(t, yesterday, f.sam, domain.BookingAttended, false, "2026-10-14T09:00:00Z") // a stranger's
	notTaken := f.put(t, lastWeek, f.mia, domain.BookingBooked, false, "2026-10-07T10:00:00Z")
	inHidden := f.put(t, hidden, f.anna, domain.BookingAttended, false, "2026-09-20T10:00:00Z")
	lateCancel := f.put(t, september, f.anna, domain.BookingCanceled, true, "2026-08-20T10:00:00Z")
	byTheSchool := f.put(t, canceled, f.tom, domain.BookingBooked, false, "2026-07-25T10:00:00Z")
	edge := f.put(t, atEdge, f.anna, domain.BookingAttended, false, "2026-07-14T10:00:00Z")
	beforeEdge := f.put(t, justBefore, f.anna, domain.BookingAttended, false, "2026-07-14T10:00:00Z")
	old := f.put(t, january, f.mia, domain.BookingCanceled, false, "2026-01-02T10:00:00Z")

	svc := f.myLessons(t, bookingNow)

	page := func(before time.Time) MyLessonHistory {
		t.Helper()

		h, err := svc.History(t.Context(), anna, before)
		if err != nil {
			t.Fatal(err)
		}

		return h
	}

	first := page(time.Time{})
	wantBookings(t, first.Items, noShow, attended, notTaken, inHidden, lateCancel, byTheSchool, edge)

	if !first.Before.Equal(mustInstant(t, bookingNow)) || !first.From.Equal(mustInstant(t, "2026-07-21T10:00:00Z")) ||
		!first.HasMore || first.Timezone != "Europe/Berlin" {
		t.Errorf("first page: from %v, before %v, more %v", first.From, first.Before, first.HasMore)
	}

	byID := map[domain.BookingID]MyBooking{}
	for _, it := range first.Items {
		byID[it.Booking.ID] = it
	}

	if it := byID[lateCancel.ID]; it.Booking.Status != domain.BookingCanceled || !it.Booking.Late || it.Cancelable {
		t.Errorf("late cancellation: %+v", it)
	}

	if it := byID[byTheSchool.ID]; it.Booking.Status != domain.BookingBooked ||
		it.Lesson.Lesson.Status != domain.LessonCanceled || it.Cancelable {
		t.Errorf("canceled by the school: %+v", it)
	}

	if it := byID[notTaken.ID]; it.Booking.Status != domain.BookingBooked || it.Cancelable || it.Name != "Mia Example" {
		t.Errorf("attendance not taken: %+v", it)
	}

	// The next page starts where this one ended.
	second := page(first.From)
	wantBookings(t, second.Items, beforeEdge)

	if !second.From.Equal(mustInstant(t, "2026-04-21T10:00:00Z")) || !second.HasMore {
		t.Errorf("second page: from %v, more %v", second.From, second.HasMore)
	}

	// An empty page in between still tells there is more.
	third := page(second.From)
	if len(third.Items) != 0 || !third.HasMore || !third.From.Equal(mustInstant(t, "2026-01-21T11:00:00Z")) {
		t.Errorf("third page: %v items from %v, more %v", len(third.Items), third.From, third.HasMore)
	}

	fourth := page(third.From)
	wantBookings(t, fourth.Items, old)

	if fourth.HasMore {
		t.Error("fourth page: more")
	}

	// A before in the future is now.
	if h := page(mustInstant(t, "2027-01-01T00:00:00Z")); !h.Before.Equal(mustInstant(t, bookingNow)) ||
		len(h.Items) != len(first.Items) {
		t.Errorf("future before: %v, %d items", h.Before, len(h.Items))
	}

	// Tom sees his own only.
	tom, err := svc.History(t.Context(), f.principal(t, f.tom), time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	wantBookings(t, tom.Items, noShow, byTheSchool)

	if tom.HasMore {
		t.Error("tom: more")
	}
}

// TestMyLessonsLessonDeletedMeanwhile: a class edit that deletes a lesson
// with its member bookings may commit between the read of the bookings and
// the read of their lessons (PostgreSQL reads each query's committed
// data). The booking whose lesson is gone is left out; the others stay.
func TestMyLessonsLessonDeletedMeanwhile(t *testing.T) {
	t.Parallel()

	f := newStaffFixture(t)
	s := f.newScene(t)
	st := lessonsGoneStore{Store: f.st, gone: s.early.ID}
	svc := NewMyLessonsService(st, clock.NewFixed(mustInstant(t, bookingNow)))

	got, err := svc.Upcoming(t.Context(), f.principal(t, f.anna))
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Upcoming) != 4 || len(got.Waitlist) != 1 {
		t.Fatalf("%d bookings, %d entries; want 4 and 1", len(got.Upcoming), len(got.Waitlist))
	}

	for _, b := range got.Upcoming {
		if b.Booking.LessonID == s.early.ID || b.Lesson.Lesson.ID != b.Booking.LessonID {
			t.Errorf("booking %+v of lesson %v", b.Booking, b.Lesson.Lesson.ID)
		}
	}
}

// lessonsGoneStore reads lesson gone as deleted in Lessons().ListByIDs,
// as if deleted by a transaction that committed just before that read.
type lessonsGoneStore struct {
	store.Store

	gone domain.LessonID
}

func (s lessonsGoneStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, lessonsGoneTx{Tx: tx, gone: s.gone})
	})
}

type lessonsGoneTx struct {
	store.Tx

	gone domain.LessonID
}

func (t lessonsGoneTx) Lessons() store.LessonRepo { return lessonsGoneRepo{t.Tx.Lessons(), t.gone} }

type lessonsGoneRepo struct {
	store.LessonRepo

	gone domain.LessonID
}

func (r lessonsGoneRepo) ListByIDs(ctx context.Context, ids []domain.LessonID) ([]domain.Lesson, error) {
	lessons, err := r.LessonRepo.ListByIDs(ctx, ids)

	return slices.DeleteFunc(lessons, func(l domain.Lesson) bool { return l.ID == r.gone }), err
}
