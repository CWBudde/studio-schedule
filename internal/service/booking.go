package service

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Members book lessons for themselves and their dependents, and cancel
// those bookings (ADR 0009). Every capacity check and booking change runs
// under the lesson lock, in one transaction with its booking event and its
// mail to the acting account.

var (
	// ErrAlreadyBooked refuses a second active booking of a person for a
	// lesson.
	ErrAlreadyBooked = fmt.Errorf("%w: the person is booked for the lesson already", domain.ErrConflict)
	// ErrLessonFull refuses a booking when the active bookings fill the
	// lesson's capacity.
	ErrLessonFull = fmt.Errorf("%w: the lesson is full", domain.ErrConflict)
	// ErrBookingCanceled refuses to cancel a booking that is canceled
	// already.
	ErrBookingCanceled = fmt.Errorf("%w: the booking is canceled already", domain.ErrConflict)
	// ErrNotClassBooking refuses to cancel the following weeks of a booking
	// for a one-off lesson, which has no following weeks. Like
	// ErrLessonStarted, the booking itself is the problem, not a field.
	ErrNotClassBooking = fmt.Errorf("%w: the booking is not for a lesson of a class", domain.ErrValidation)
)

// ClosedReason says why a lesson cannot be booked although the caller may
// see it.
type ClosedReason string

// Reasons of a BookingClosedError.
const (
	// ClosedCutoff: the booking cutoff before the start has passed.
	ClosedCutoff ClosedReason = "cutoff"
	// ClosedRelease: members may not book the lesson yet (not released and
	// beyond their early access) or at all (hidden). Only staff, who see
	// every lesson but book under member rules, get it.
	ClosedRelease ClosedReason = "release"
	// ClosedCanceled: the school canceled the lesson.
	ClosedCanceled ClosedReason = "canceled"
	// ClosedStarted: the lesson has started or is over.
	ClosedStarted ClosedReason = "started"
)

// BookingClosedError refuses a booking of a lesson the caller may see but
// not book (ADR 0009).
type BookingClosedError struct {
	Why ClosedReason
}

func (e *BookingClosedError) Error() string { return "the lesson cannot be booked: " + string(e.Why) }

func (e *BookingClosedError) Unwrap() error { return domain.ErrConflict }

// FollowingBooking is a booking that CancelFollowing cancels, with its
// lesson.
type FollowingBooking struct {
	Booking domain.Booking
	Lesson  domain.Lesson
	// Late tells whether the cancellation is late: after the cancellation
	// cutoff of a lesson the school has not canceled.
	Late bool
}

// BookingService books and cancels lessons for the signed-in account's
// family (ADR 0009).
type BookingService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
	mails MailRenderer
	w     waitlister
}

// NewBookingService returns a BookingService that sends its mails, and the
// waiting-list invites of the places it frees, with mails.
func NewBookingService(st store.Store, clk clock.Clock, src ids.Source, mails Mails) *BookingService {
	return &BookingService{store: st, clock: clk, ids: src, mails: mails.Renderer, w: waitlister{ids: src, mails: mails}}
}

// Book books lesson for person, who must be p's own person or one of p's
// dependents (domain.ErrNotFound otherwise, like a lesson p may not see).
// Staff book under member rules: a lesson members may not book yet, or
// not at all, is a *BookingClosedError with ClosedRelease. A canceled,
// started or past lesson, or one whose booking cutoff has passed, is a
// *BookingClosedError too. A person with an active booking for the lesson
// is ErrAlreadyBooked; a lesson whose active bookings and the places held
// by others' waiting-list invites fill its capacity is ErrLessonFull, unless
// the person's own invite holds a place: booking then takes that place like
// accepting the invite, even if the office has overbooked the lesson since,
// which the booking event records as an override (fitOf). Any other open
// entry of the person on the lesson's list is withdrawn (ADR 0010). The
// booking event records p's real account (the admin when impersonating),
// and the acting account p gets the confirmation mail. Before it counts
// the places, it settles the lesson's waiting list (an invite that ran out
// goes to the next entry), and that stays even when the booking is refused.
func (s *BookingService) Book(
	ctx context.Context, p Principal, lesson domain.LessonID, person domain.PersonID,
) (domain.Booking, error) {
	var (
		b       domain.Booking
		refused error // a refusal after the list was settled, which commits
	)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		refused = nil // the callback may run more than once

		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		who, err := familyMember(ctx, tx, p, person)
		if err != nil {
			return err
		}

		l, err := tx.Lessons().Lock(ctx, lesson)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if err := bookable(p, settings, loc, now, l); err != nil {
			return err
		}

		open, err := s.w.settled(ctx, tx, now, l)
		if err != nil {
			return err
		}

		fit, err := fitOf(ctx, tx, now, l, open, who.ID)

		switch {
		case err != nil:
			return err
		case fit.Booked:
			refused = ErrAlreadyBooked

			return nil
		case fit.Full():
			refused = ErrLessonFull

			return nil
		}

		b = domain.Booking{
			ID: ids.New[domain.BookingID](s.ids), LessonID: l.ID, PersonID: who.ID, Status: domain.BookingBooked,
			Source: domain.BookingSourceSelf, BookedAt: now,
		}
		if err := b.Validate(); err != nil {
			return err
		}

		switch err := tx.Bookings().Create(ctx, b); {
		case errors.Is(err, domain.ErrDuplicate):
			// Checked above under the lock; the unique index is the fallback.
			return fmt.Errorf("%w (%w)", ErrAlreadyBooked, err)
		case errors.Is(err, domain.ErrConflict):
			// A missing reference: the lesson is locked, so it is the
			// person, deleted after familyMember read it. The person is not
			// locked, as nothing else in a booking depends on it.
			return fmt.Errorf("person %v: %w", who.ID, domain.ErrNotFound)
		case err != nil:
			return err
		}

		accepted, err := s.w.booked(ctx, tx, now, l, open, who.ID)
		if err != nil {
			return err
		}

		if accepted {
			err = recordWaitlistEvent(ctx, tx, s.ids, actorOf(p), now, b, fit.Over)
		} else {
			err = s.addEvent(ctx, tx, p, b, now)
		}

		if err != nil {
			return err
		}

		return s.mail(ctx, tx, p, settings, loc, now, mail.TemplateBookingConfirmed, who,
			[]FollowingBooking{{Booking: b, Lesson: l}})
	})
	if err == nil {
		err = refused
	}

	if err != nil {
		return domain.Booking{}, fmt.Errorf("book lesson %v: %w", lesson, err)
	}

	return b, nil
}

// Cancel cancels a booking of p's own person or one of p's dependents
// (domain.ErrNotFound for any other booking) and frees its place. A
// booking canceled already is ErrBookingCanceled; one whose lesson has
// started is ErrLessonStarted. The cancellation is late after the
// cancellation cutoff, unless the school canceled the lesson. The event
// records p's real account, and the acting account p gets a mail.
func (s *BookingService) Cancel(ctx context.Context, p Principal, id domain.BookingID) (domain.Booking, error) {
	var b domain.Booking

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		who, first, err := ownBooking(ctx, tx, p, id)
		if err != nil {
			return err
		}

		l, err := tx.Lessons().Lock(ctx, first.LessonID)
		if err != nil {
			return err
		}

		// Read again under the lock: a concurrent cancellation may have
		// come first.
		if b, err = tx.Bookings().Get(ctx, id); err != nil {
			return err
		}

		now := s.clock.Now()

		canceled, err := s.cancel(ctx, tx, p, settings, now, l, b, true)
		if err != nil {
			return err
		}

		b = canceled.Booking

		return s.mail(ctx, tx, p, settings, loc, now, mail.TemplateBookingCanceled, who, []FollowingBooking{canceled})
	})
	if err != nil {
		return domain.Booking{}, fmt.Errorf("cancel booking %v: %w", id, err)
	}

	return b, nil
}

// FollowingCancellation is what CancelFollowing cancels, or would cancel.
type FollowingCancellation struct {
	// Bookings are the canceled bookings, the requested one first and the
	// others by the start of their lessons.
	Bookings []FollowingBooking
	// Until is the last slot date canceled: the one asked for, or the last
	// day of the class's term.
	Until domain.Date
	// PausedUntil is the end of the person's pause as a member of the class
	// (Until), and zero for a person who is not a member (ADR 0009).
	PausedUntil domain.Date
}

// errUntilBeforeLesson refuses an end date before the requested lesson.
var errUntilBeforeLesson error = &domain.FieldError{
	Field: "until", Msg: "the date must not be before the lesson's date",
}

// PreviewCancelFollowing returns what CancelFollowing would cancel now with
// until, without changing anything.
func (s *BookingService) PreviewCancelFollowing(
	ctx context.Context, p Principal, id domain.BookingID, until domain.Date,
) (FollowingCancellation, error) {
	var out FollowingCancellation

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, _, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		_, b, err := ownBooking(ctx, tx, p, id)
		if err != nil {
			return err
		}

		out, err = following(ctx, tx, settings, s.clock.Now(), b, until)

		return err
	})
	if err != nil {
		return FollowingCancellation{}, fmt.Errorf("preview cancel following of booking %v: %w", id, err)
	}

	return out, nil
}

// CancelFollowing cancels a booking of a class lesson (ErrNotClassBooking
// for a one-off lesson) together with the person's other active bookings
// of the same class from that lesson on up to the slot date until, as far
// as their lessons have not started, in one transaction; see Cancel for who
// may and the late flag. A zero until means the last day of the class's
// term; one before the requested lesson's slot date is a validation error on
// until. A person who is a member of the class is paused until then: the
// membership stays, and generation and enrollment book them again only into
// lessons after it (ADR 0009). It returns the canceled bookings, the
// requested one first and the others by lesson start, and the acting account
// gets one mail listing them all by start.
func (s *BookingService) CancelFollowing(
	ctx context.Context, p Principal, id domain.BookingID, until domain.Date,
) (FollowingCancellation, error) {
	var out FollowingCancellation

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		who, b, err := ownBooking(ctx, tx, p, id)
		if err != nil {
			return err
		}

		now := s.clock.Now()

		l, err := tx.Lessons().Get(ctx, b.LessonID)
		if err != nil {
			return err
		}

		// The same checks, in the same order, as the preview's; they are
		// repeated on the rows read under the locks below.
		if err := followable(now, l, b); err != nil {
			return err
		}

		// Lock the class first (term → class → lesson). Every transaction
		// that locks several lessons of a class, or creates, moves to
		// another slot or deletes them, holds the class lock, so the lesson
		// locks below cannot deadlock with them, whatever their order, and
		// the class's lessons listed under it are complete. Single lesson
		// edits may still change a start, so everything is decided from the
		// rows read under the lesson locks. The lock also keeps membership
		// changes out while the pause is set.
		if _, err := tx.Classes().Lock(ctx, l.ClassID); err != nil {
			return err
		}

		if l, err = tx.Lessons().Get(ctx, b.LessonID); err != nil {
			return err
		}

		if out.Until, err = untilOf(ctx, tx, l, until); err != nil {
			return err
		}

		ahead, err := lessonsFrom(ctx, tx, now, l, out.Until)
		if err != nil {
			return err
		}

		// A stable order that no edit changes.
		lessonIDs := slices.SortedFunc(maps.Keys(ahead), func(a, b domain.LessonID) int { return bytes.Compare(a[:], b[:]) })
		for _, lessonID := range lessonIDs {
			if ahead[lessonID], err = tx.Lessons().Lock(ctx, lessonID); err != nil {
				return err
			}
		}

		// Read the bookings again under the locks: a concurrent
		// cancellation or booking may have come first.
		if b, err = tx.Bookings().Get(ctx, id); err != nil {
			return err
		}

		if err := followable(now, ahead[b.LessonID], b); err != nil {
			return err
		}

		active, err := tx.Bookings().ListActiveByLessonsAndPersons(ctx, lessonIDs, []domain.PersonID{who.ID})
		if err != nil {
			return err
		}

		m, member, err := membership(ctx, tx, l.ClassID, who.ID)
		if err != nil {
			return err
		}

		out.Bookings = nil // the callback may run more than once

		// A member pauses rather than releasing the dates, so ending the
		// pause books them again (ADR 0009).
		for _, fb := range pickFollowing(settings, now, id, ahead, active) {
			canceled, err := s.cancel(ctx, tx, p, settings, now, fb.Lesson, fb.Booking, !member)
			if err != nil {
				return err
			}

			out.Bookings = append(out.Bookings, canceled)
		}

		if member {
			m.PausedUntil = pausedUntil(m, out.Until)
			if err := tx.Classes().UpdateMember(ctx, m); err != nil {
				return err
			}

			out.PausedUntil = m.PausedUntil
		}

		// The mail lists the lessons by start; the response keeps the
		// requested booking first.
		mailed := slices.Clone(out.Bookings)
		slices.SortStableFunc(mailed, func(a, b FollowingBooking) int { return byStart(a.Lesson, b.Lesson) })

		return s.mail(ctx, tx, p, settings, loc, now, mail.TemplateBookingCanceled, who, mailed)
	})
	if err != nil {
		return FollowingCancellation{}, fmt.Errorf("cancel following of booking %v: %w", id, err)
	}

	return out, nil
}

// untilOf returns the last slot date to cancel from lesson l of a class on:
// until, or the last day of the class's term for a zero until. One before
// l's slot date is errUntilBeforeLesson.
func untilOf(ctx context.Context, tx store.Tx, l domain.Lesson, until domain.Date) (domain.Date, error) {
	if until.IsZero() {
		c, err := tx.Classes().Get(ctx, l.ClassID)
		if err != nil {
			return domain.Date{}, err
		}

		t, err := tx.Terms().Get(ctx, c.TermID)
		if err != nil {
			return domain.Date{}, err
		}

		until = t.EndsOn
	}

	if until.Compare(l.SlotDate) < 0 {
		return domain.Date{}, errUntilBeforeLesson
	}

	return until, nil
}

// pausedUntil is the end of m's pause when it pauses until the school date
// until: the later of until and a pause it has already.
func pausedUntil(m domain.ClassMember, until domain.Date) domain.Date {
	if m.PausedUntil.Compare(until) > 0 {
		return m.PausedUntil
	}

	return until
}

// membership returns the membership of person in class, if any.
func membership(
	ctx context.Context, tx store.Tx, class domain.ClassID, person domain.PersonID,
) (domain.ClassMember, bool, error) {
	members, err := tx.Classes().ListMembers(ctx, class)
	if err != nil {
		return domain.ClassMember{}, false, err
	}

	i := slices.IndexFunc(members, func(m domain.ClassMember) bool { return m.PersonID == person })
	if i < 0 {
		return domain.ClassMember{}, false, nil
	}

	return members[i], true, nil
}

// bookable checks that p may book lesson l now (ADR 0009): a lesson p may
// not see is domain.ErrNotFound, like in the schedule. Then member rules
// apply to everyone, staff included, and the lesson must be scheduled and
// before its booking cutoff.
func bookable(p Principal, settings domain.Settings, loc *time.Location, now time.Time, l domain.Lesson) error {
	w := ComputeReleaseWindow(settings, loc, now)

	switch {
	case !viewerOf(p, settings, loc, now).sees(l, w):
		return fmt.Errorf("%w: lesson %v", domain.ErrNotFound, l.ID)
	case !memberViewer(p, settings, loc, now).sees(l, w):
		return &BookingClosedError{Why: ClosedRelease}
	case l.Status == domain.LessonCanceled:
		return &BookingClosedError{Why: ClosedCanceled}
	case !now.Before(l.StartsAt):
		return &BookingClosedError{Why: ClosedStarted}
	case !now.Before(bookingClosesAt(settings, l)):
		return &BookingClosedError{Why: ClosedCutoff}
	}

	return nil
}

// bookingClosesAt is when booking l closes: the booking cutoff before its
// start (ADR 0009).
func bookingClosesAt(settings domain.Settings, l domain.Lesson) time.Time {
	return l.StartsAt.Add(-settings.BookingCutoff)
}

// lateCancelFrom is when canceling a booking of l becomes late: the
// cancellation cutoff before its start, and zero (never) if the school
// canceled the lesson (ADR 0009).
func lateCancelFrom(settings domain.Settings, l domain.Lesson) time.Time {
	if l.Status == domain.LessonCanceled {
		return time.Time{}
	}

	return l.StartsAt.Add(-settings.CancellationCutoff)
}

// lateCancellation reports whether canceling a booking of l now is late:
// at or after lateCancelFrom.
func lateCancellation(settings domain.Settings, now time.Time, l domain.Lesson) bool {
	from := lateCancelFrom(settings, l)

	return !from.IsZero() && !now.Before(from)
}

// cancel cancels booking b of the locked lesson l and records the event.
// released says whether the cancellation gives up the date
// (domain.Booking.Released): every cancellation of a member's family but a
// class member's pause.
func (s *BookingService) cancel(
	ctx context.Context, tx store.Tx, p Principal, settings domain.Settings, now time.Time, l domain.Lesson,
	b domain.Booking, released bool,
) (FollowingBooking, error) {
	switch {
	case b.Status == domain.BookingCanceled:
		return FollowingBooking{}, ErrBookingCanceled
	case !now.Before(l.StartsAt):
		return FollowingBooking{}, ErrLessonStarted
	}

	b.Late, b.Released = lateCancellation(settings, now, l), released

	b, err := cancelBooking(ctx, tx, s.w, actorOf(p), now, l, b)
	if err != nil {
		return FollowingBooking{}, err
	}

	return FollowingBooking{Booking: b, Lesson: l, Late: b.Late}, nil
}

// cancelBooking cancels the active booking b of lesson l, which the caller
// has locked, at now, keeping the Late and Released flags the caller set,
// and records the event with the actor (zero for the system). It frees the
// place in the caller's transaction and offers it to the lesson's waiting
// list (ADR 0010). Every cancellation goes through here: a member's
// (BookingService.cancel), the office's when a membership ends (unenroll)
// and staff removing a participant (ParticipantService.Remove).
func cancelBooking(
	ctx context.Context, tx store.Tx, w waitlister, actor domain.AccountID, now time.Time, l domain.Lesson,
	b domain.Booking,
) (domain.Booking, error) {
	b.Status, b.CanceledAt = domain.BookingCanceled, now
	if err := b.Validate(); err != nil {
		return domain.Booking{}, err
	}

	if err := tx.Bookings().Update(ctx, b); err != nil {
		return domain.Booking{}, err
	}

	if err := recordEvent(ctx, tx, w.ids, actor, now, b, false); err != nil {
		return domain.Booking{}, err
	}

	// The place is free now: the next entry of the waiting list gets it,
	// under the same lesson lock.
	return b, w.offer(ctx, tx, now, l, nil)
}

// addEvent records b's current status, with p's real account as the actor:
// the admin when impersonating.
func (s *BookingService) addEvent(ctx context.Context, tx store.Tx, p Principal, b domain.Booking, now time.Time) error {
	return recordEvent(ctx, tx, s.ids, actorOf(p), now, b, false)
}

// recordEvent adds b's current status and late flag to its history, at now
// by the actor (zero for the system), with the override flag of a booking
// beyond the lesson's capacity.
func recordEvent(
	ctx context.Context, tx store.Tx, src ids.Source, actor domain.AccountID, now time.Time, b domain.Booking,
	override bool,
) error {
	e := domain.BookingEvent{
		ID: ids.New[domain.BookingEventID](src), BookingID: b.ID, At: now, Status: b.Status, ActorAccountID: actor,
		Late: b.Late, Override: override,
	}
	if err := e.Validate(); err != nil {
		return err
	}

	return tx.Bookings().AddEvent(ctx, e)
}

// familyMember returns person if it is p's own person or one of p's
// dependents, and domain.ErrNotFound otherwise (ADR 0008).
func familyMember(ctx context.Context, tx store.Tx, p Principal, person domain.PersonID) (domain.Person, error) {
	switch {
	case person.IsZero():
		return domain.Person{}, fmt.Errorf("%w: no person", domain.ErrNotFound)
	case person == p.PersonID:
		return tx.Persons().Get(ctx, person)
	default:
		return dependentOf(ctx, tx, p.PersonID, person)
	}
}

// ownBooking returns booking id and its person if the person is p's own or
// one of p's dependents, and domain.ErrNotFound otherwise, guest bookings
// included.
func ownBooking(
	ctx context.Context, tx store.Tx, p Principal, id domain.BookingID,
) (domain.Person, domain.Booking, error) {
	b, err := tx.Bookings().Get(ctx, id)
	if err != nil {
		return domain.Person{}, domain.Booking{}, err
	}

	who, err := familyMember(ctx, tx, p, b.PersonID)
	if err != nil {
		return domain.Person{}, domain.Booking{}, err
	}

	return who, b, nil
}

// following returns what CancelFollowing would cancel now with until: the
// active booking b of a class lesson that has not started, followed by the
// person's other active bookings of the same class whose lessons have the
// same or a later slot date up to the effective until and have not started,
// by lesson start, each with whether canceling it now is late, and whether
// the person's membership would be paused. It reads without locks, for the
// preview.
func following(
	ctx context.Context, tx store.Tx, settings domain.Settings, now time.Time, b domain.Booking, until domain.Date,
) (FollowingCancellation, error) {
	l, err := tx.Lessons().Get(ctx, b.LessonID)
	if err != nil {
		return FollowingCancellation{}, err
	}

	if err := followable(now, l, b); err != nil {
		return FollowingCancellation{}, err
	}

	var out FollowingCancellation

	if out.Until, err = untilOf(ctx, tx, l, until); err != nil {
		return FollowingCancellation{}, err
	}

	ahead, err := lessonsFrom(ctx, tx, now, l, out.Until)
	if err != nil {
		return FollowingCancellation{}, err
	}

	active, err := tx.Bookings().ListActiveByLessonsAndPersons(
		ctx, slices.Collect(maps.Keys(ahead)), []domain.PersonID{b.PersonID})
	if err != nil {
		return FollowingCancellation{}, err
	}

	out.Bookings = pickFollowing(settings, now, b.ID, ahead, active)

	if m, member, err := membership(ctx, tx, l.ClassID, b.PersonID); err != nil {
		return FollowingCancellation{}, err
	} else if member {
		out.PausedUntil = pausedUntil(m, out.Until)
	}

	return out, nil
}

// followable checks that the following weeks of booking b of lesson l can
// be canceled now: l is a lesson of a class that has not started, and b is
// not canceled.
func followable(now time.Time, l domain.Lesson, b domain.Booking) error {
	switch {
	case l.ClassID.IsZero():
		return ErrNotClassBooking
	case b.Status == domain.BookingCanceled:
		return ErrBookingCanceled
	case !now.Before(l.StartsAt):
		return ErrLessonStarted
	}

	return nil
}

// lessonsFrom returns, by ID, l and the lessons of its class with the same
// or a later slot date up to until that have not started now.
func lessonsFrom(
	ctx context.Context, tx store.Tx, now time.Time, l domain.Lesson, until domain.Date,
) (map[domain.LessonID]domain.Lesson, error) {
	lessons, err := tx.Lessons().ListByClass(ctx, l.ClassID, l.SlotDate)
	if err != nil {
		return nil, err
	}

	ahead := map[domain.LessonID]domain.Lesson{l.ID: l}

	for _, c := range lessons {
		if now.Before(c.StartsAt) && c.SlotDate.Compare(until) <= 0 {
			ahead[c.ID] = c
		}
	}

	return ahead, nil
}

// pickFollowing returns the active bookings whose lessons have not started
// now, the requested one first and the others by lesson start, each with
// whether canceling it now is late.
func pickFollowing(
	settings domain.Settings, now time.Time, requested domain.BookingID, lessons map[domain.LessonID]domain.Lesson,
	active []domain.Booking,
) []FollowingBooking {
	active = slices.DeleteFunc(active, func(b domain.Booking) bool { return !now.Before(lessons[b.LessonID].StartsAt) })
	sortFollowing(active, lessons, requested)

	out := make([]FollowingBooking, len(active))
	for i, b := range active {
		l := lessons[b.LessonID]
		out[i] = FollowingBooking{Booking: b, Lesson: l, Late: lateCancellation(settings, now, l)}
	}

	return out
}

// sortFollowing puts the requested booking first and orders the others by
// the start and ID of their lessons. The requested booking need not start
// first: the office may have moved a later lesson of the class before it.
func sortFollowing(bookings []domain.Booking, lessons map[domain.LessonID]domain.Lesson, requested domain.BookingID) {
	rank := func(b domain.Booking) int {
		if b.ID == requested {
			return 0
		}

		return 1
	}

	slices.SortStableFunc(bookings, func(a, b domain.Booking) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), byStart(lessons[a.LessonID], lessons[b.LessonID]))
	})
}

// byStart orders lessons by their start and ID.
func byStart(a, b domain.Lesson) int {
	return cmp.Or(a.StartsAt.Compare(b.StartsAt), bytes.Compare(a.ID[:], b.ID[:]))
}

// mail renders template about the bookings of person and queues it to the
// acting account p, in its language (else the school's default; ADR 0008).
func (s *BookingService) mail(
	ctx context.Context, tx store.Tx, p Principal, settings domain.Settings, loc *time.Location, now time.Time,
	template string, person domain.Person, bookings []FollowingBooking,
) error {
	if len(bookings) == 0 {
		return nil
	}

	r, err := actingRecipient(ctx, tx, p)
	if err != nil {
		return err
	}

	return mailBookings(ctx, tx, s.ids, s.mails, settings, loc, now, template, []recipient{r}, fullName(person), bookings)
}

// actingRecipient returns the acting account p as the recipient of the
// mails about what it did, greeted by its person's given name.
func actingRecipient(ctx context.Context, tx store.Tx, p Principal) (recipient, error) {
	acc, err := tx.Accounts().Get(ctx, p.AccountID)
	if err != nil {
		return recipient{}, fmt.Errorf("account %v: %w", p.AccountID, refErr(err))
	}

	me, err := tx.Persons().Get(ctx, acc.PersonID)
	if err != nil {
		return recipient{}, fmt.Errorf("person %v: %w", acc.PersonID, refErr(err))
	}

	return recipient{email: acc.Email, locale: acc.Locale, givenName: me.GivenName, account: true}, nil
}

// mailBookings renders template about the bookings of the participant (a
// person's full name, or a guest's name) with mails for each recipient, in
// its language (else the school's default; ADR 0008), and queues it.
func mailBookings(
	ctx context.Context, tx store.Tx, src ids.Source, mails MailRenderer, settings domain.Settings,
	loc *time.Location, now time.Time, template string, recipients []recipient, participant string,
	bookings []FollowingBooking,
) error {
	if len(bookings) == 0 || len(recipients) == 0 {
		return nil
	}

	// The bookings of a class share their course type and mostly their
	// room: read each once.
	type roomAt struct {
		room     domain.Room
		location domain.Location
	}

	courseTypes := map[domain.CourseTypeID]domain.CourseType{}
	places := map[domain.RoomID]roomAt{}
	lessons := make([]mail.BookedLesson, 0, len(bookings))

	for _, fb := range bookings {
		l := fb.Lesson

		ct, ok := courseTypes[l.CourseTypeID]
		if !ok {
			var err error
			if ct, err = tx.CourseTypes().Get(ctx, l.CourseTypeID); err != nil {
				return fmt.Errorf("course type %v: %w", l.CourseTypeID, refErr(err))
			}

			courseTypes[ct.ID] = ct
		}

		at, ok := places[l.RoomID]
		if !ok {
			var err error
			if at.room, at.location, err = place(ctx, tx, l.RoomID); err != nil {
				return err
			}

			places[l.RoomID] = at
		}

		lessons = append(lessons, mail.BookedLesson{
			CourseType: ct.Name, StartsAt: l.StartsAt.In(loc), EndsAt: l.EndsAt.In(loc), Room: at.room.Name,
			Location: at.location.Name, Late: fb.Late,
		})
	}

	return sendMails(ctx, tx, src, mails, settings, now, recipients, template, func(r recipient) any {
		return mail.BookingData{
			SchoolName: settings.SchoolName, GivenName: r.givenName, Participant: participant, Guest: r.guest,
			Lessons: lessons,
		}
	})
}
