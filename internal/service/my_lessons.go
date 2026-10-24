package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// "My lessons" (ADR 0009, amendment): the bookings and waiting-list entries
// of a signed-in account's family, the upcoming ones and the history.

// HistoryMonths is how many months one page of the history goes back.
const HistoryMonths = 3

// MyBooking is a booking of the caller's family with its lesson.
type MyBooking struct {
	// Lesson is the lesson with its references and its BookingClosesAt
	// and LateCancelFrom; FreePlaces and the family fields are not set.
	Lesson  ScheduledLesson
	Person  domain.Person
	Name    string // the person's given and family name
	Booking domain.Booking
	// Cancelable tells that the family may cancel the booking now: it is
	// active and its lesson has not started (cancelBooking).
	Cancelable bool
}

// MyWaitlistEntry is an open waiting-list entry of the caller's family with
// its lesson.
type MyWaitlistEntry struct {
	// Lesson is set like MyBooking.Lesson.
	Lesson ScheduledLesson
	WaitingPerson
}

// MyLessons is what is ahead for the caller's family.
type MyLessons struct {
	// Timezone is the IANA time zone of the school.
	Timezone string
	// Now is the server time the lists were computed at.
	Now time.Time
	// Upcoming are the active bookings of lessons that are not over, by the
	// lessons' start, then by booking time.
	Upcoming []MyBooking
	// Waitlist are the open entries of lessons that have not started, by
	// the lessons' start, then the caller first and the dependents by
	// name.
	Waitlist []MyWaitlistEntry
}

// MyLessonHistory is one page of the bookings of the caller's family in
// lessons that are over.
type MyLessonHistory struct {
	// Timezone is the IANA time zone of the school.
	Timezone string
	// Now is the server time the page was computed at.
	Now time.Time
	// From and Before bound the page: lessons with From <= start < Before.
	// From is HistoryMonths months before Before in the school time zone,
	// and the Before of the next page.
	From, Before time.Time
	// Items are the bookings, every status, the newest lessons first, in
	// one lesson the latest booking first.
	Items []MyBooking
	// HasMore tells that the family has bookings of lessons before From.
	HasMore bool
}

// MyLessonsService lists the lessons of a signed-in account's family: its
// own person and its dependents, managed ones included (ADR 0009).
//
// A booking is listed whatever its lesson's visibility: a hidden or
// unreleased lesson is still the family's booking (the office booked it,
// say), so they see it here although the schedule does not show it.
type MyLessonsService struct {
	store store.Store
	clock clock.Clock
}

// NewMyLessonsService returns a MyLessonsService backed by st.
func NewMyLessonsService(st store.Store, clk clock.Clock) *MyLessonsService {
	return &MyLessonsService{store: st, clock: clk}
}

// Upcoming returns the active bookings of p's family in lessons that are
// not over and their open waiting-list entries of lessons that have not
// started (see MyLessons). A zero p has none. It costs the same few queries
// for any number of bookings.
func (s *MyLessonsService) Upcoming(ctx context.Context, p Principal) (MyLessons, error) {
	out := MyLessons{Now: s.clock.Now()}

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, _, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		out.Timezone, out.Upcoming, out.Waitlist = settings.Timezone, nil, nil // the callback may run more than once

		fam, err := familyOf(ctx, tx, p)
		if err != nil || len(fam.persons) == 0 {
			return err
		}

		bookings, err := tx.Bookings().ListByPersons(ctx, store.BookingQuery{
			Persons: fam.ids(), Active: true, EndsAfter: out.Now,
		})
		if err != nil {
			return fmt.Errorf("bookings: %w", err)
		}

		entries, err := tx.Waitlist().ListOpenByPersons(ctx, fam.ids())
		if err != nil {
			return fmt.Errorf("waiting lists: %w", err)
		}

		lessons, err := lessonsOf(ctx, tx, settings, bookings, entries)
		if err != nil {
			return err
		}

		out.Upcoming = myBookings(out.Now, fam, lessons, bookings)
		out.Waitlist, err = myEntries(ctx, tx, out.Now, fam, lessons, entries)

		return err
	})
	if err != nil {
		return MyLessons{}, fmt.Errorf("my lessons: %w", err)
	}

	return out, nil
}

// History returns the page of the bookings of p's family, canceled ones
// included, in lessons that are over and that started in the
// HistoryMonths months before before (see MyLessonHistory). A zero before,
// or one after now, is now. A zero p has none.
func (s *MyLessonsService) History(ctx context.Context, p Principal, before time.Time) (MyLessonHistory, error) {
	out := MyLessonHistory{Now: s.clock.Now()}
	if before.IsZero() || before.After(out.Now) {
		before = out.Now
	}

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		out.Timezone, out.Items, out.HasMore = settings.Timezone, nil, false // the callback may run more than once
		out.Before = before.UTC()
		out.From = before.In(loc).AddDate(0, -HistoryMonths, 0).UTC()

		fam, err := familyOf(ctx, tx, p)
		if err != nil || len(fam.persons) == 0 {
			return err
		}

		bookings, err := tx.Bookings().ListByPersons(ctx, store.BookingQuery{
			Persons: fam.ids(), StartsFrom: out.From, StartsBefore: out.Before, EndsBy: out.Now, Desc: true,
		})
		if err != nil {
			return fmt.Errorf("bookings: %w", err)
		}

		earlier, err := tx.Bookings().ListByPersons(ctx, store.BookingQuery{
			Persons: fam.ids(), StartsBefore: out.From, EndsBy: out.Now, Desc: true, Limit: 1,
		})
		if err != nil {
			return fmt.Errorf("earlier bookings: %w", err)
		}

		out.HasMore = len(earlier) > 0

		lessons, err := lessonsOf(ctx, tx, settings, bookings, nil)
		if err != nil {
			return err
		}

		out.Items = myBookings(out.Now, fam, lessons, bookings)

		return nil
	})
	if err != nil {
		return MyLessonHistory{}, fmt.Errorf("my lesson history: %w", err)
	}

	return out, nil
}

// lessonsOf reads the lessons of the bookings and the entries and the
// records they reference with a fixed number of queries (the catalog, then
// the lessons and their classes by ID), and returns them by ID, with when
// booking them closes and canceling becomes late.
func lessonsOf(
	ctx context.Context, tx store.Tx, settings domain.Settings, bookings []domain.Booking,
	entries []domain.WaitlistEntry,
) (map[domain.LessonID]ScheduledLesson, error) {
	ids := make([]domain.LessonID, 0, len(bookings)+len(entries))
	for _, b := range bookings {
		ids = append(ids, b.LessonID)
	}

	for _, e := range entries {
		ids = append(ids, e.LessonID)
	}

	out := map[domain.LessonID]ScheduledLesson{}
	if len(ids) == 0 {
		return out, nil
	}

	lessons, err := tx.Lessons().ListByIDs(ctx, ids)
	if err != nil || len(lessons) == 0 {
		return out, err
	}

	refs, err := loadCatalogRefs(ctx, tx)
	if err != nil {
		return nil, err
	}

	var classIDs []domain.ClassID

	for _, l := range lessons {
		if !l.ClassID.IsZero() {
			classIDs = append(classIDs, l.ClassID)
		}
	}

	if len(classIDs) > 0 {
		classes, err := tx.Classes().ListByIDs(ctx, classIDs)
		if err != nil {
			return nil, fmt.Errorf("classes: %w", err)
		}

		for _, c := range classes {
			refs.classes[c.ID] = c
		}
	}

	for _, l := range lessons {
		sl, err := refs.resolve(l)
		if err != nil {
			return nil, err
		}

		sl.BookingClosesAt, sl.LateCancelFrom = bookingClosesAt(settings, l), lateCancelFrom(settings, l)
		out[l.ID] = sl
	}

	return out, nil
}

// myBookings combines the bookings of fam, in their order, with their
// lessons and persons as of now.
func myBookings(
	now time.Time, fam family, lessons map[domain.LessonID]ScheduledLesson, bookings []domain.Booking,
) []MyBooking {
	out := make([]MyBooking, 0, len(bookings))

	for _, b := range bookings {
		sl, ok := lessons[b.LessonID]
		if !ok {
			continue // the store lists only bookings with their lesson
		}

		person := fam.person(b.PersonID)
		out = append(out, MyBooking{
			Lesson: sl, Person: person, Name: fullName(person), Booking: b,
			Cancelable: b.Status != domain.BookingCanceled && now.Before(sl.Lesson.StartsAt),
		})
	}

	return out
}

// myEntries combines the open entries of fam whose lessons have not
// started at now with their lessons, persons and places in the list (one
// query for all lessons), ordered by the lessons' start and then like fam.
func myEntries(
	ctx context.Context, tx store.Tx, now time.Time, fam family, lessons map[domain.LessonID]ScheduledLesson,
	entries []domain.WaitlistEntry,
) ([]MyWaitlistEntry, error) {
	entries = slices.DeleteFunc(entries, func(e domain.WaitlistEntry) bool {
		sl, ok := lessons[e.LessonID]

		return !ok || !now.Before(sl.Lesson.StartsAt) // the job ends them at the start
	})
	if len(entries) == 0 {
		return nil, nil
	}

	ids := make([]domain.LessonID, len(entries))
	for i, e := range entries {
		ids[i] = e.LessonID
	}

	open, err := tx.Waitlist().ListOpenByLessons(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("waiting lists: %w", err)
	}

	byLesson := map[domain.LessonID][]domain.WaitlistEntry{}
	for _, e := range open {
		byLesson[e.LessonID] = append(byLesson[e.LessonID], e)
	}

	out := make([]MyWaitlistEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, MyWaitlistEntry{
			Lesson: lessons[e.LessonID], WaitingPerson: waitingPerson(fam.person(e.PersonID), e, byLesson[e.LessonID], now),
		})
	}

	slices.SortStableFunc(out, func(a, b MyWaitlistEntry) int {
		return cmp.Or(byStart(a.Lesson.Lesson, b.Lesson.Lesson), cmp.Compare(fam.rank[a.Person.ID], fam.rank[b.Person.ID]))
	})

	return out, nil
}
