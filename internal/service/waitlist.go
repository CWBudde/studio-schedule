package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The waiting list (ADR 0010). A person joins the list of a full lesson;
// whenever a place frees, the oldest waiting entry is invited by mail, and
// the invite reserves the place until it expires. Everything that frees a
// place, or ends an invite, offers the place again in the same transaction
// and under the same lesson lock (waitlister.offer).

var (
	// ErrNotFull refuses to put a person on the waiting list of a lesson
	// that has a free place: they book it instead.
	ErrNotFull = fmt.Errorf("%w: the lesson has a free place", domain.ErrConflict)
	// ErrAlreadyWaiting refuses a second open entry of a person for a
	// lesson.
	ErrAlreadyWaiting = fmt.Errorf("%w: the person is on the waiting list already", domain.ErrConflict)
	// ErrNotInvited refuses to accept or decline an entry that is waiting
	// for an invite.
	ErrNotInvited = fmt.Errorf("%w: the entry has no invite", domain.ErrConflict)
	// ErrInviteExpired refuses an invite that ran out, was declined or was
	// voided, or an entry that ended because its invite ran out.
	ErrInviteExpired = fmt.Errorf("%w: the invite has expired", domain.ErrConflict)
	// ErrInviteUsed refuses an invite that was accepted, or whose entry
	// left the list (withdrawn).
	ErrInviteUsed = fmt.Errorf("%w: the invite has been used", domain.ErrConflict)
	// ErrNotWaiting refuses to leave a list the entry is no longer on.
	ErrNotWaiting = fmt.Errorf("%w: the entry is no longer on the waiting list", domain.ErrConflict)
)

// waitlistNote is the note of the booking event of a place taken from the
// waiting list.
const waitlistNote = "waitlist"

// acceptRoute is the app route the invite link opens; the token follows as
// the query parameter token, like the account links (ADR 0004).
const acceptRoute = "waitlist/accept"

// Mails is what services need to send mails: the renderer, and the
// absolute URL of the web app that links in mails start with, ending in "/"
// (or "#/" with hash routing).
type Mails struct {
	Renderer MailRenderer
	AppURL   string
	// Push queues the web push notifications of some mails (ADR 0017);
	// the zero value queues none.
	Push PushQueue
}

// WaitlistPlace is an entry with its place in the list: Position counts
// the waiting entries up to and including it (1 for the next to be
// invited), and is 0 for an entry that is not waiting.
type WaitlistPlace struct {
	Entry    domain.WaitlistEntry
	Position int
}

// position returns the place of entry id among the open entries of its
// lesson, ordered like the list (WaitlistRepo.ListOpenByLessons).
func position(open []domain.WaitlistEntry, id domain.WaitlistEntryID) int {
	n := 0

	for _, e := range open {
		if e.Status != domain.WaitlistWaiting {
			continue
		}

		n++

		if e.ID == id {
			return n
		}
	}

	return 0
}

// InviteState is what an invite link stands for now.
type InviteState string

// States of an invite.
const (
	// InviteOpen: the place is reserved and may be taken.
	InviteOpen InviteState = "invited"
	// InviteAccepted: the place was taken.
	InviteAccepted InviteState = "accepted"
	// InviteExpired: the invite ran out, was declined or voided, or the
	// lesson started.
	InviteExpired InviteState = "expired"
	// InviteWithdrawn: the person left the list or got booked otherwise.
	InviteWithdrawn InviteState = "withdrawn"
	// InviteWaiting: the invite ended (ran out, declined, voided), but the
	// person is back on the list and waits for another invite, which brings
	// a new link.
	InviteWaiting InviteState = "waiting"
)

// stateOf returns the state of entry e's invite at now.
func stateOf(e domain.WaitlistEntry, now time.Time) InviteState {
	switch e.Status {
	case domain.WaitlistWaiting:
		return InviteWaiting
	case domain.WaitlistInvited:
		if holdsPlace(e, now) {
			return InviteOpen
		}

		return InviteExpired
	case domain.WaitlistAccepted:
		return InviteAccepted
	case domain.WaitlistWithdrawn:
		return InviteWithdrawn
	default:
		return InviteExpired
	}
}

// inviteErr is the error for using the invite of entry e at now, nil if it
// holds a place.
func inviteErr(e domain.WaitlistEntry, now time.Time) error {
	switch stateOf(e, now) {
	case InviteOpen:
		return nil
	case InviteAccepted, InviteWithdrawn:
		return ErrInviteUsed
	default:
		if e.Status == domain.WaitlistWaiting {
			return ErrNotInvited
		}

		return ErrInviteExpired
	}
}

// InviteView is what an invite link shows: the lesson, whose place it is,
// and the invite's state (ADR 0010).
type InviteView struct {
	Entry  domain.WaitlistEntry
	State  InviteState
	Person domain.Person
	Lesson ScheduledLesson
	// Closed says why the lesson cannot be booked now, if it cannot: the
	// invite is then of no use even while it is open.
	Closed ClosedReason
	// Position is the entry's place in the list while it waits (see
	// WaitlistPlace), and 0 otherwise.
	Position int
}

// WaitlistService puts persons on the waiting lists of full lessons, lets
// them leave, accept and decline invites, and ends what is due (ADR 0010).
type WaitlistService struct {
	store store.Store
	clock clock.Clock
	w     waitlister
}

// NewWaitlistService returns a WaitlistService that sends its mails with
// mails.
func NewWaitlistService(st store.Store, clk clock.Clock, src ids.Source, mails Mails) *WaitlistService {
	return &WaitlistService{store: st, clock: clk, w: waitlister{ids: src, mails: mails}}
}

// Join puts person, who must be p's own person or one of p's dependents
// (domain.ErrNotFound otherwise), on the waiting list of lesson, at its
// end. The lesson must be one p may book under member rules (see Book):
// domain.ErrNotFound, ErrEventSession or a *BookingClosedError otherwise. A person booked
// for the lesson is ErrAlreadyBooked, one on its list already
// ErrAlreadyWaiting, and a lesson with a free place (the capacity minus
// the active bookings and the places held by invites) ErrNotFull. With
// stayOnList, an invite the person lets expire or declines puts them back
// at the end instead of ending the entry. Like Book, it settles the list
// before it counts the places, and that stays even when joining is refused.
func (s *WaitlistService) Join(
	ctx context.Context, p Principal, lesson domain.LessonID, person domain.PersonID, stayOnList bool,
) (WaitlistPlace, error) {
	var (
		out     WaitlistPlace
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
		if err := notSession(ctx, tx, p, settings, loc, now, l); err != nil {
			return err
		}

		if err := bookable(p, settings, loc, now, l); err != nil {
			return err
		}

		open, err := s.w.settled(ctx, tx, now, l)
		if err != nil {
			return err
		}

		booked, err := tx.Bookings().ListActiveByLessonsAndPersons(ctx, []domain.LessonID{l.ID}, []domain.PersonID{who.ID})
		if err != nil {
			return err
		}

		active, err := tx.Bookings().CountActive(ctx, l.ID)
		if err != nil {
			return err
		}

		switch {
		case len(booked) > 0:
			refused = ErrAlreadyBooked
		case slices.ContainsFunc(open, func(e domain.WaitlistEntry) bool { return e.PersonID == who.ID }):
			refused = ErrAlreadyWaiting
		case active+heldPlaces(open, now)[l.ID] < l.Capacity:
			refused = ErrNotFull
		}

		if refused != nil {
			return nil
		}

		e := domain.WaitlistEntry{
			ID: ids.New[domain.WaitlistEntryID](s.w.ids), LessonID: l.ID, PersonID: who.ID,
			Status: domain.WaitlistWaiting, JoinedAt: now, StayOnList: stayOnList, JoinedBy: actorOf(p),
		}
		if err := e.Validate(); err != nil {
			return err
		}

		switch err := tx.Waitlist().Create(ctx, e); {
		case errors.Is(err, domain.ErrDuplicate):
			return fmt.Errorf("%w (%w)", ErrAlreadyWaiting, err)
		case errors.Is(err, domain.ErrConflict):
			// The lesson is locked: the person was deleted meanwhile.
			return fmt.Errorf("person %v: %w", who.ID, domain.ErrNotFound)
		case err != nil:
			return err
		}

		out = WaitlistPlace{Entry: e, Position: position(append(open, e), e.ID)}

		return nil
	})
	if err == nil {
		err = refused
	}

	if err != nil {
		return WaitlistPlace{}, fmt.Errorf("join the waiting list of lesson %v: %w", lesson, err)
	}

	return out, nil
}

// ownEntry returns entry id, read again under its lesson's lock, which it
// takes, and the lesson, if the entry's person is p's own or one of p's
// dependents; domain.ErrNotFound otherwise.
func ownEntry(
	ctx context.Context, tx store.Tx, p Principal, id domain.WaitlistEntryID,
) (domain.Person, domain.WaitlistEntry, domain.Lesson, error) {
	e, err := tx.Waitlist().Get(ctx, id)
	if err != nil {
		return domain.Person{}, domain.WaitlistEntry{}, domain.Lesson{}, err
	}

	who, err := familyMember(ctx, tx, p, e.PersonID)
	if err != nil {
		return domain.Person{}, domain.WaitlistEntry{}, domain.Lesson{}, err
	}

	l, err := tx.Lessons().Lock(ctx, e.LessonID)
	if err != nil {
		return domain.Person{}, domain.WaitlistEntry{}, domain.Lesson{}, err
	}

	if e, err = tx.Waitlist().Get(ctx, id); err != nil {
		return domain.Person{}, domain.WaitlistEntry{}, domain.Lesson{}, err
	}

	return who, e, l, nil
}

// Leave takes an entry of p's own person or one of p's dependents
// (domain.ErrNotFound for any other) off the list: it is withdrawn, and a
// place its invite held goes to the next entry. An entry that has ended is
// ErrNotWaiting.
func (s *WaitlistService) Leave(ctx context.Context, p Principal, id domain.WaitlistEntryID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		_, e, l, err := ownEntry(ctx, tx, p, id)
		if err != nil {
			return err
		}

		if !e.Open() {
			return ErrNotWaiting
		}

		return s.w.end(ctx, tx, s.clock.Now(), l, e, domain.WaitlistWithdrawn)
	})
	if err != nil {
		return fmt.Errorf("leave waiting list entry %v: %w", id, err)
	}

	return nil
}

// acceptable checks that the place of lesson l may be taken at now: it is
// no room rental, the school has not canceled it, it has not started,
// booking has not closed, and members may still see it.
func acceptable(settings domain.Settings, now time.Time, l domain.Lesson) error {
	switch {
	case !l.Kind.Bookable():
		return &BookingClosedError{Why: ClosedRental}
	case l.Status == domain.LessonCanceled:
		return &BookingClosedError{Why: ClosedCanceled}
	case !now.Before(l.StartsAt):
		return &BookingClosedError{Why: ClosedStarted}
	case !now.Before(bookingClosesAt(settings, l)):
		return &BookingClosedError{Why: ClosedCutoff}
	case l.Visibility == domain.VisibilityHidden:
		return &BookingClosedError{Why: ClosedRelease}
	}

	return nil
}

// accept takes the place that entry e's invite holds in lesson l, which the
// caller has locked: the invite must hold it (inviteErr) and the lesson be
// acceptable. It books the person (source self, the event noting the
// waiting list, by actor or the system), settles the booking's pass like
// Book (ADR 0012) and marks the entry accepted.
func (s *WaitlistService) accept(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, now time.Time, l domain.Lesson,
	e domain.WaitlistEntry, actor domain.AccountID,
) (domain.Booking, error) {
	if err := inviteErr(e, now); err != nil {
		return domain.Booking{}, err
	}

	if err := acceptable(settings, now, l); err != nil {
		return domain.Booking{}, err
	}

	open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID})
	if err != nil {
		return domain.Booking{}, err
	}

	fit, err := fitOf(ctx, tx, now, l, open, e.PersonID)
	if err != nil {
		return domain.Booking{}, err
	}

	b := domain.Booking{
		ID: ids.New[domain.BookingID](s.w.ids), LessonID: l.ID, PersonID: e.PersonID, Status: domain.BookingBooked,
		Source: domain.BookingSourceSelf, BookedAt: now,
	}
	if err := b.Validate(); err != nil {
		return domain.Booking{}, err
	}

	switch err := createBooking(ctx, tx, chargesOf(settings, loc), now, l, b); {
	case errors.Is(err, domain.ErrDuplicate):
		return domain.Booking{}, fmt.Errorf("%w (%w)", ErrAlreadyBooked, err)
	case err != nil:
		return domain.Booking{}, err
	}

	if err := recordWaitlistEvent(ctx, tx, s.w.ids, actor, now, b, fit.Over); err != nil {
		return domain.Booking{}, err
	}

	return b, s.w.end(ctx, tx, now, l, e, domain.WaitlistAccepted)
}

// recordWaitlistEvent records the booking b of a place from the waiting
// list, with the override flag of a place beyond the lesson's capacity (the
// office overbooked the lesson while the invite held the place; fitOf).
func recordWaitlistEvent(
	ctx context.Context, tx store.Tx, src ids.Source, actor domain.AccountID, now time.Time, b domain.Booking,
	override bool,
) error {
	e := domain.BookingEvent{
		ID: ids.New[domain.BookingEventID](src), BookingID: b.ID, At: now, Status: b.Status, ActorAccountID: actor,
		Note: waitlistNote, Override: override,
	}
	if err := e.Validate(); err != nil {
		return err
	}

	return tx.Bookings().AddEvent(ctx, e)
}

// Accept takes the place an invite of p's own person or one of p's
// dependents holds (domain.ErrNotFound for any other entry), in the app. A
// waiting entry is ErrNotInvited, an invite that ran out ErrInviteExpired
// and one used already ErrInviteUsed; a lesson that cannot be booked any
// more a *BookingClosedError. The booking event records p's real account,
// and the acting account gets the confirmation mail, like Book.
func (s *WaitlistService) Accept(ctx context.Context, p Principal, id domain.WaitlistEntryID) (domain.Booking, error) {
	var b domain.Booking

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		who, e, l, err := ownEntry(ctx, tx, p, id)
		if err != nil {
			return err
		}

		now := s.clock.Now()

		if b, err = s.accept(ctx, tx, settings, loc, now, l, e, actorOf(p)); err != nil {
			return err
		}

		me, err := actingRecipient(ctx, tx, p)
		if err != nil {
			return err
		}

		return mailBookings(ctx, tx, s.w.ids, s.w.mails.Renderer, settings, loc, now, mail.TemplateBookingConfirmed,
			[]recipient{me}, fullName(who), []FollowingBooking{{Booking: b, Lesson: l}})
	})
	if err != nil {
		return domain.Booking{}, fmt.Errorf("accept waiting list entry %v: %w", id, err)
	}

	return b, nil
}

// Decline gives up the place an invite of p's own person or one of p's
// dependents holds (domain.ErrNotFound for any other entry), failing like
// Accept. Like an expiry, the entry goes back to the end of the list if it
// stays on it (and booking has not closed), and expires otherwise, but
// without a mail; the place goes to the next entry. It returns the entry
// with its new place.
func (s *WaitlistService) Decline(ctx context.Context, p Principal, id domain.WaitlistEntryID) (WaitlistPlace, error) {
	var out WaitlistPlace

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, _, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		_, e, l, err := ownEntry(ctx, tx, p, id)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if err := inviteErr(e, now); err != nil {
			return err
		}

		endInvite(&e, settings, now, l)

		if err := s.w.update(ctx, tx, e); err != nil {
			return err
		}

		if err := s.w.offer(ctx, tx, now, l, map[domain.WaitlistEntryID]bool{e.ID: true}); err != nil {
			return err
		}

		open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID})
		out = WaitlistPlace{Entry: e, Position: position(open, e.ID)}

		return err
	})
	if err != nil {
		return WaitlistPlace{}, fmt.Errorf("decline waiting list entry %v: %w", id, err)
	}

	return out, nil
}

// Invite returns what the invite link with token stands for, without
// changing anything; an unknown token is domain.ErrNotFound. No sign-in is
// needed: the token is the proof (ADR 0010).
func (s *WaitlistService) Invite(ctx context.Context, token string) (InviteView, error) {
	var out InviteView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, _, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		e, err := tx.Waitlist().GetByTokenHash(ctx, domain.HashToken(token))
		if err != nil {
			return err
		}

		l, err := tx.Lessons().Get(ctx, e.LessonID)
		if err != nil {
			return err
		}

		out, err = s.view(ctx, tx, settings, s.clock.Now(), e, l)

		return err
	})
	if err != nil {
		return InviteView{}, fmt.Errorf("waiting list invite: %w", err)
	}

	return out, nil
}

// AcceptInvite takes the place the invite with token holds, without a
// sign-in, and returns the invite's new view. An unknown token is
// domain.ErrNotFound (so is the link of an earlier invite of the entry), the
// link of an entry that went back to waiting, or was invited again while
// this accept waited for the lesson, ErrInviteExpired; otherwise it fails
// like Accept. The booking event has
// no actor (nobody signed in), and the confirmation goes to the person's
// recipients, as the invite did.
func (s *WaitlistService) AcceptInvite(ctx context.Context, token string) (InviteView, error) {
	var out InviteView

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		e, err := tx.Waitlist().GetByTokenHash(ctx, domain.HashToken(token))
		if err != nil {
			return err
		}

		l, err := tx.Lessons().Lock(ctx, e.LessonID)
		if err != nil {
			return err
		}

		// Read again under the lock: a concurrent use may have come first.
		if e, err = tx.Waitlist().Get(ctx, e.ID); err != nil {
			return err
		}

		now := s.clock.Now()

		// The re-read must still be the invite the link was sent for: the
		// entry may have been re-invited with a new token while this accept
		// waited for the lock, and the old link died with the old invite.
		if e.InviteTokenHash != domain.HashToken(token) {
			return ErrInviteExpired
		}

		// The link of an invite that ended while the person went back to
		// the list (ADR 0010): a new invite brings a new link.
		if e.Status == domain.WaitlistWaiting {
			return ErrInviteExpired
		}

		b, err := s.accept(ctx, tx, settings, loc, now, l, e, domain.AccountID{})
		if err != nil {
			return err
		}

		if e, err = tx.Waitlist().Get(ctx, e.ID); err != nil {
			return err
		}

		if out, err = s.view(ctx, tx, settings, now, e, l); err != nil {
			return err
		}

		// Nobody signed in: the confirmation goes where the invite went.
		recipients, err := personRecipients(ctx, tx, out.Person.ID)
		if err != nil {
			return err
		}

		return mailBookings(ctx, tx, s.w.ids, s.w.mails.Renderer, settings, loc, now, mail.TemplateBookingConfirmed,
			recipients, fullName(out.Person), []FollowingBooking{{Booking: b, Lesson: l}})
	})
	if err != nil {
		return InviteView{}, fmt.Errorf("accept waiting list invite: %w", err)
	}

	return out, nil
}

// view returns the InviteView of entry e of lesson l at now.
func (s *WaitlistService) view(
	ctx context.Context, tx store.Tx, settings domain.Settings, now time.Time, e domain.WaitlistEntry, l domain.Lesson,
) (InviteView, error) {
	person, err := tx.Persons().Get(ctx, e.PersonID)
	if err != nil {
		return InviteView{}, fmt.Errorf("person %v: %w", e.PersonID, refErr(err))
	}

	refs, err := loadRefsOf(ctx, tx, l)
	if err != nil {
		return InviteView{}, err
	}

	sl, err := refs.resolve(l)
	if err != nil {
		return InviteView{}, err
	}

	out := InviteView{Entry: e, State: stateOf(e, now), Person: person, Lesson: sl}

	if e.Status == domain.WaitlistWaiting {
		open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID})
		if err != nil {
			return InviteView{}, err
		}

		out.Position = position(open, e.ID)
	}

	var closed *BookingClosedError
	if errors.As(acceptable(settings, now, l), &closed) {
		out.Closed = closed.Why
	}

	return out, nil
}

// ExpireDue ends what is due on every waiting list (waitlister.settle): the
// invites whose expiry has passed and the open entries of lessons that
// have started, offering the places that frees. Each lesson is settled in
// a transaction of its own, so one that fails neither undoes nor holds up
// the others: their errors come back joined, with how many entries it
// changed in all. It is idempotent: a second run at the same time changes
// nothing.
func (s *WaitlistService) ExpireDue(ctx context.Context) (int, error) {
	now := s.clock.Now()

	var due []domain.LessonID

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		due, err = tx.Waitlist().ListDueLessons(ctx, now)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("expire waiting lists: %w", err)
	}

	var (
		n    int
		errs []error
	)

	for _, id := range due {
		var changed int

		err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			l, err := tx.Lessons().Lock(ctx, id)
			if errors.Is(err, domain.ErrNotFound) {
				changed = 0

				return nil // deleted meanwhile, with its entries
			} else if err != nil {
				return err
			}

			open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{id})
			if err != nil {
				return err
			}

			changed, err = s.w.settle(ctx, tx, now, l, open)

			return err
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("lesson %v: %w", id, err))

			continue
		}

		n += changed
	}

	if len(errs) > 0 {
		return n, fmt.Errorf("expire waiting lists: %w", errors.Join(errs...))
	}

	return n, nil
}
