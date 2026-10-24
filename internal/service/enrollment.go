package service

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Class members are booked into the lessons of their class automatically
// (ADR 0009): generation books them into every lesson it creates that has
// not started, adding a member books the class's lessons that have not
// started, and removing one cancels their bookings of those lessons. These
// bookings have the source domain.BookingSourceMember, ignore the booking
// cutoff and send no mail. A member releases a single date by a normal
// cancellation; neither generation nor enrollment books a person into a
// lesson they have any booking for, canceled ones included, so a released
// date stays released.

var (
	// ErrClassFull refuses a class member beyond the class's capacity.
	ErrClassFull = fmt.Errorf("%w: the class is full", domain.ErrConflict)
	// ErrClassRetired refuses a new member of a retired class, which has
	// no lessons to come. The class itself is the problem, not a field.
	ErrClassRetired = fmt.Errorf("%w: the class is retired", domain.ErrValidation)
)

// membersOn returns the persons of the members who are active on the school
// date d (domain.ClassMember.ActiveOn).
func membersOn(members []domain.ClassMember, d domain.Date) []domain.PersonID {
	var out []domain.PersonID

	for _, m := range members {
		if m.ActiveOn(d) {
			out = append(out, m.PersonID)
		}
	}

	return out
}

// bookMember books person into lesson l as an automatic class member
// booking at now. taken is the number of places of l taken before (its
// active bookings, and places held by waiting-list invites); a booking
// beyond l's capacity is made anyway and its event records an override. The actor is the office account, or zero for generation. The
// caller holds the class lock and either the lesson lock or a lesson it has
// just created.
func bookMember(
	ctx context.Context, tx store.Tx, src ids.Source, now time.Time, actor domain.AccountID, l domain.Lesson,
	person domain.PersonID, taken int,
) error {
	b := domain.Booking{
		ID: ids.New[domain.BookingID](src), LessonID: l.ID, PersonID: person, Status: domain.BookingBooked,
		Source: domain.BookingSourceMember, BookedAt: now,
	}
	if err := b.Validate(); err != nil {
		return err
	}

	if err := tx.Bookings().Create(ctx, b); err != nil {
		return fmt.Errorf("book member %v into lesson %v: %w", person, l.ID, err)
	}

	return recordEvent(ctx, tx, src, actor, now, b, taken >= l.Capacity)
}

// bookMembers books the members of l's class who are active on l's slot
// date into l, which the caller has just created under the class lock, if
// l has not started at now. Nobody else sees the new lesson before the
// transaction commits, so it needs no lesson lock, and nobody is booked
// into it yet.
func bookMembers(
	ctx context.Context, tx store.Tx, src ids.Source, now time.Time, l domain.Lesson, members []domain.ClassMember,
) error {
	if !l.StartsAt.After(now) {
		return nil
	}

	for i, person := range membersOn(members, l.SlotDate) {
		if err := bookMember(ctx, tx, src, now, domain.AccountID{}, l, person, i); err != nil {
			return err
		}
	}

	return nil
}

// lockAhead locks the lessons of class c that have not started at now,
// whatever their slot date (a lesson the office moved into the future
// included), in the order of their IDs, and returns them as read under the
// locks, in that order. The caller holds the class lock (term → class →
// lesson), so the lessons listed are complete.
func lockAhead(ctx context.Context, tx store.Tx, c domain.ClassID, now time.Time) ([]domain.Lesson, error) {
	lessons, err := tx.Lessons().ListByClassStartingAfter(ctx, c, now)
	if err != nil {
		return nil, fmt.Errorf("list lessons of class %v: %w", c, err)
	}

	slices.SortFunc(lessons, func(a, b domain.Lesson) int { return bytes.Compare(a.ID[:], b.ID[:]) })

	ahead := lessons[:0]

	for _, l := range lessons {
		// A single lesson edit may have moved it meanwhile: decide from
		// the locked row.
		if l, err = tx.Lessons().Lock(ctx, l.ID); err != nil {
			return nil, err
		}

		if l.StartsAt.After(now) {
			ahead = append(ahead, l)
		}
	}

	return ahead, nil
}

// lessonIDs returns the IDs of lessons, in their order.
func lessonIDs(lessons []domain.Lesson) []domain.LessonID {
	out := make([]domain.LessonID, len(lessons))
	for i, l := range lessons {
		out[i] = l.ID
	}

	return out
}

// enroll books member m of the class, which the caller has locked, into the
// class's lessons that have not started and that m is active on
// (domain.ClassMember.ActiveOn), those the school canceled included (their
// bookings stay; ADR 0009). A lesson the person has an active booking for,
// or a date they released (domain.Booking.Released), is skipped; a booking
// the office canceled or a pause canceled does not keep them out. A full
// lesson gets the booking anyway, recorded as an override. The member's
// open entry on a lesson's waiting list ends with the booking (ADR 0010).
// The actor is the office account.
func (s *TimetableService) enroll(ctx context.Context, tx store.Tx, actor domain.AccountID, m domain.ClassMember) error {
	now := s.clock.Now()

	lessons, err := lockAhead(ctx, tx, m.ClassID, now)
	if err != nil {
		return err
	}

	aheadIDs := lessonIDs(lessons)

	theirs, err := tx.Bookings().ListByLessonsAndPersons(ctx, aheadIDs, []domain.PersonID{m.PersonID})
	if err != nil {
		return err
	}

	keepOut := map[domain.LessonID]bool{}

	for _, b := range theirs {
		if b.Status != domain.BookingCanceled || b.Released {
			keepOut[b.LessonID] = true // booked already, or released
		}
	}

	active, err := tx.Bookings().CountActiveByLessons(ctx, aheadIDs)
	if err != nil {
		return err
	}

	open, err := tx.Waitlist().ListOpenByLessons(ctx, aheadIDs)
	if err != nil {
		return err
	}

	waiting := map[domain.LessonID][]domain.WaitlistEntry{}
	for _, e := range open {
		waiting[e.LessonID] = append(waiting[e.LessonID], e)
	}

	for _, l := range lessons {
		if keepOut[l.ID] || !m.ActiveOn(l.SlotDate) {
			continue
		}

		// Places that others' invites hold are taken too (ADR 0010).
		others := slices.DeleteFunc(slices.Clone(waiting[l.ID]), func(e domain.WaitlistEntry) bool {
			return e.PersonID == m.PersonID
		})

		if err := bookMember(ctx, tx, s.ids, now, actor, l, m.PersonID, active[l.ID]+heldPlaces(others, now)[l.ID]); err != nil {
			return err
		}

		if _, err := s.w.booked(ctx, tx, now, l, waiting[l.ID], m.PersonID); err != nil {
			return err
		}
	}

	return nil
}

// unenroll cancels the active bookings of person, whatever their source, in
// the lessons of class c that have not started, which frees their places;
// see cancelAhead.
func (s *TimetableService) unenroll(
	ctx context.Context, tx store.Tx, actor domain.AccountID, c domain.ClassID, person domain.PersonID,
) error {
	return s.cancelAhead(ctx, tx, actor, c, person, func(domain.Lesson, domain.Booking) bool { return true })
}

// withdraw cancels the active automatic bookings of member m in the
// lessons of the class that have not started and that m is no longer
// active on: a new pause, or a later since date; see cancelAhead.
func (s *TimetableService) withdraw(ctx context.Context, tx store.Tx, actor domain.AccountID, m domain.ClassMember) error {
	return s.cancelAhead(ctx, tx, actor, m.ClassID, m.PersonID, func(l domain.Lesson, b domain.Booking) bool {
		return b.Source == domain.BookingSourceMember && !m.ActiveOn(l.SlotDate)
	})
}

// cancelAhead cancels the active bookings of person in the lessons of class
// c that have not started and for which drop holds. The caller has locked
// the class. The office's cancellations are never late, do not release the
// date (domain.Booking.Released), so the person is booked again when they
// are active again, and send no mail; the actor is the office account.
func (s *TimetableService) cancelAhead(
	ctx context.Context, tx store.Tx, actor domain.AccountID, c domain.ClassID, person domain.PersonID,
	drop func(domain.Lesson, domain.Booking) bool,
) error {
	now := s.clock.Now()

	lessons, err := lockAhead(ctx, tx, c, now)
	if err != nil {
		return err
	}

	byID := make(map[domain.LessonID]domain.Lesson, len(lessons))
	for _, l := range lessons {
		byID[l.ID] = l
	}

	active, err := tx.Bookings().ListActiveByLessonsAndPersons(ctx, lessonIDs(lessons), []domain.PersonID{person})
	if err != nil {
		return err
	}

	for _, b := range active {
		if !drop(byID[b.LessonID], b) {
			continue
		}

		b.Late, b.Released = false, false
		if _, err := cancelBooking(ctx, tx, s.w, actor, now, byID[b.LessonID], b); err != nil {
			return err
		}
	}

	return nil
}

// hasOwnBookings reports whether bookings other than automatic class
// member bookings refer to the lesson, canceled ones included. They keep
// the timetable edits of ADR 0007 from deleting a lesson of a class; member
// bookings go with the lesson (deleteClassLesson).
func hasOwnBookings(ctx context.Context, tx store.Tx, id domain.LessonID) (bool, error) {
	bookings, err := tx.Bookings().ListByLesson(ctx, id)

	return slices.ContainsFunc(bookings, func(b domain.Booking) bool { return b.Source != domain.BookingSourceMember }), err
}

// deleteClassLesson deletes a lesson of a class together with its
// automatic member bookings and their events (ADR 0007, ADR 0009). Any
// other booking makes the delete fail with domain.ErrConflict.
func deleteClassLesson(ctx context.Context, tx store.Tx, id domain.LessonID) error {
	if _, err := tx.Bookings().DeleteByLessonAndSource(ctx, id, domain.BookingSourceMember); err != nil {
		return err
	}

	return tx.Lessons().Delete(ctx, id)
}
