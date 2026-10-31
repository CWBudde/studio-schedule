package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Staff actions on the participants of a lesson (ADR 0007 and ADR 0009,
// amendments): the office on every lesson, an instructor or accompanist on
// their own, add persons and guests, mark trials, remove participants and
// take attendance. Every change runs under the lesson lock, like a
// member's booking.

var (
	// ErrDayOver refuses a change to the participants of a lesson by its
	// instructor or accompanist once the lesson's school day is over
	// (attendance window); the office may still make it. The lesson
	// itself is the problem, not a field.
	ErrDayOver = fmt.Errorf("%w: the lesson's day is over, the office changes it now", domain.ErrValidation)
	// ErrLessonNotStarted refuses to take attendance before the lesson
	// starts.
	ErrLessonNotStarted = fmt.Errorf("%w: the lesson has not started", domain.ErrValidation)
	// errNoGuestName refuses a booking with neither a person nor a guest's
	// name.
	errNoGuestName error = &domain.FieldError{Field: "guest.name", Msg: "guest name must not be empty"}
	// errInvalidAttendance refuses a status attendance cannot set.
	errInvalidAttendance error = &domain.FieldError{Field: "status", Msg: "status must be attended, no_show or booked"}
)

// ParticipantService lets staff manage the participants of lessons: the
// office on every lesson, instructors and accompanists on their own.
type ParticipantService struct {
	store store.Store
	clock clock.Clock
	w     waitlister
}

// NewParticipantService returns a ParticipantService that sends its mails,
// and the waiting-list invites of the places it frees, with mails.
func NewParticipantService(st store.Store, clk clock.Clock, src ids.Source, mails Mails) *ParticipantService {
	return &ParticipantService{store: st, clock: clk, w: waitlister{ids: src, mails: mails}}
}

// manages checks that p may act on the participants of lesson l (ADR 0007,
// amendment): the office and admins on every lesson, anyone else only on a
// lesson whose instructor or accompanist is an active staff member linked
// to p's person. Any other lesson is domain.ErrNotFound, like a stranger's
// family (ADR 0008).
func manages(ctx context.Context, tx store.Tx, p Principal, l domain.Lesson) error {
	if p.Role.AtLeast(domain.RoleOffice) {
		return nil
	}

	if p.Role.Staff() {
		own, err := ownStaff(ctx, tx, p.PersonID)
		if err != nil {
			return err
		}

		if _, role := roleIn(l, own); role != TeachOffice {
			return nil
		}
	}

	return fmt.Errorf("%w: lesson %v", domain.ErrNotFound, l.ID)
}

// ownStaff returns the active staff records linked to person (none for a
// zero person), ordered like StaffRepo.List: whose lessons the person's
// account teaches or accompanies.
func ownStaff(ctx context.Context, tx store.Tx, person domain.PersonID) ([]domain.Staff, error) {
	if person.IsZero() {
		return nil, nil
	}

	staff, err := tx.Staff().ListByPerson(ctx, person)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(staff, func(s domain.Staff) bool { return !s.Active }), nil
}

// roleIn returns the part of the staff records own in lesson l, with the
// record: its instructor, else its accompanist, else TeachOffice (none of
// them; a zero record).
func roleIn(l domain.Lesson, own []domain.Staff) (domain.Staff, TeachRole) {
	for _, s := range own {
		if s.ID == l.InstructorID {
			return s, TeachInstructor
		}
	}

	for _, s := range own {
		if !l.AccompanistID.IsZero() && s.ID == l.AccompanistID {
			return s, TeachAccompanist
		}
	}

	return domain.Staff{}, TeachOffice
}

// dayOverAt is when the attendance window of lesson l ends for its
// instructor and accompanist: at the end of the school day the lesson
// starts on, or at its end if that is later.
func dayOverAt(loc *time.Location, l domain.Lesson) time.Time {
	end := domain.DateOf(l.StartsAt, loc).AddDays(1).At(domain.TimeOfDay{}, loc)
	if l.EndsAt.After(end) {
		return l.EndsAt
	}

	return end
}

// changeable checks that p may still change the participants of lesson l
// at now: the office always, its instructor and accompanist until
// dayOverAt (ErrDayOver).
func changeable(p Principal, loc *time.Location, now time.Time, l domain.Lesson) error {
	if p.Role.AtLeast(domain.RoleOffice) || now.Before(dayOverAt(loc, l)) {
		return nil
	}

	return ErrDayOver
}

// lockManaged locks lesson id and checks that p manages it.
func lockManaged(ctx context.Context, tx store.Tx, p Principal, id domain.LessonID) (domain.Lesson, error) {
	l, err := tx.Lessons().Lock(ctx, id)
	if err != nil {
		return domain.Lesson{}, err
	}

	return l, manages(ctx, tx, p, l)
}

// managedBooking returns booking id, read again under its lesson's lock,
// which it takes, and the lesson, if p manages the lesson;
// domain.ErrNotFound otherwise.
func managedBooking(
	ctx context.Context, tx store.Tx, p Principal, id domain.BookingID,
) (domain.Booking, domain.Lesson, error) {
	b, err := tx.Bookings().Get(ctx, id)
	if err != nil {
		return domain.Booking{}, domain.Lesson{}, err
	}

	l, err := lockManaged(ctx, tx, p, b.LessonID)
	if err != nil {
		return domain.Booking{}, domain.Lesson{}, err
	}

	if b, err = tx.Bookings().Get(ctx, id); err != nil {
		return domain.Booking{}, domain.Lesson{}, err
	}

	return b, l, nil
}

// NewParticipant is what Add books: a person, or a guest by name with an
// optional email.
type NewParticipant struct {
	PersonID   domain.PersonID
	GuestName  string
	GuestEmail string
	Trial      bool
	TrialNote  string
	// ConfirmOverCapacity books even when the lesson is full; the booking
	// event records the override.
	ConfirmOverCapacity bool
}

// Add books a person or a guest into lesson as staff (source staff): the
// office into every lesson, an instructor or accompanist into their own
// (domain.ErrNotFound otherwise). Unlike a member's booking, it ignores the
// release window and the booking cutoff, and the lesson may have started:
// it is allowed until the lesson's day is over (ErrDayOver; the office
// always), but not into a lesson the school canceled or a room rental (a
// *BookingClosedError with ClosedCanceled or ClosedRental). An unknown or merged person is a validation error on
// personId, invalid guest details on guest.name or guest.email, a note
// without a trial on trialNote. A person with an active booking is
// ErrAlreadyBooked. A lesson whose active bookings and the places held by
// others' invites fill its capacity is ErrLessonFull unless the caller
// confirms going over it, which the booking event records as an override;
// a person whose own invite holds a place takes it without confirming, like
// Book (fitOf). The person's open entry on the lesson's list ends (ADR
// 0010), and like Book it settles the list first. Before the lesson starts, the person's
// recipients (or a guest with an email) get the confirmation mail; a
// walk-in after the start gets none. The event records p's real account.
func (s *ParticipantService) Add(
	ctx context.Context, p Principal, lesson domain.LessonID, in NewParticipant,
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

		// Lock order person → lesson, as a merge (ADR 0015) takes them: a
		// merged person is unknown, and no merge makes it one meanwhile.
		// Whether the caller manages the lesson is told first.
		var personErr error
		if !in.PersonID.IsZero() {
			if _, personErr = lockLivePerson(ctx, tx, in.PersonID); errors.Is(personErr, domain.ErrNotFound) {
				personErr = errUnknownPerson
			}
		}

		l, err := lockManaged(ctx, tx, p, lesson)
		if err != nil {
			return err
		}

		if personErr != nil {
			return personErr
		}

		now := s.clock.Now()

		switch {
		case !l.Kind.Bookable():
			return &BookingClosedError{Why: ClosedRental}
		case l.Status == domain.LessonCanceled:
			return &BookingClosedError{Why: ClosedCanceled}
		}

		if err := changeable(p, loc, now, l); err != nil {
			return err
		}

		b = domain.Booking{
			ID: ids.New[domain.BookingID](s.w.ids), LessonID: l.ID, PersonID: in.PersonID,
			GuestName: strings.TrimSpace(in.GuestName), GuestEmail: strings.TrimSpace(in.GuestEmail),
			Status: domain.BookingBooked, Source: domain.BookingSourceStaff, Trial: in.Trial,
			TrialNote: strings.TrimSpace(in.TrialNote), BookedAt: now,
		}
		if b.PersonID.IsZero() && b.GuestName == "" {
			return errNoGuestName
		}

		if err := b.Validate(); err != nil {
			return err
		}

		// The person, if any, was read live under its lock above: no
		// deletion or merge can make it unknown now.
		participant, err := participantName(ctx, tx, b)
		if err != nil {
			return err
		}

		open, err := s.w.settled(ctx, tx, now, l)
		if err != nil {
			return err
		}

		fit, err := fitOf(ctx, tx, now, l, open, b.PersonID)

		switch {
		case err != nil:
			return err
		case fit.Booked:
			refused = ErrAlreadyBooked

			return nil
		case fit.Full() && !in.ConfirmOverCapacity:
			refused = ErrLessonFull

			return nil
		}

		// Redeeming on booking, a person's booking uses a pass now (ADR
		// 0012); a guest's and a trial do not.
		switch err := createBooking(ctx, tx, chargesOf(settings, loc), now, l, b); {
		case errors.Is(err, domain.ErrDuplicate):
			return fmt.Errorf("%w (%w)", ErrAlreadyBooked, err)
		case errors.Is(err, domain.ErrConflict):
			// The lesson is locked: the person was deleted meanwhile.
			return errUnknownPerson
		case err != nil:
			return err
		}

		accepted := false
		if !b.PersonID.IsZero() {
			if accepted, err = s.w.booked(ctx, tx, now, l, open, b.PersonID); err != nil {
				return err
			}
		}

		if accepted {
			err = recordWaitlistEvent(ctx, tx, s.w.ids, actorOf(p), now, b, fit.Over)
		} else {
			err = recordEvent(ctx, tx, s.w.ids, actorOf(p), now, b, fit.Over)
		}

		if err != nil {
			return err
		}

		if !now.Before(l.StartsAt) {
			return nil // a walk-in: nobody needs to be told
		}

		return s.mailParticipant(ctx, tx, settings, loc, now, mail.TemplateBookingConfirmed, b, participant, l)
	})
	if err == nil {
		err = refused
	}

	if err != nil {
		return domain.Booking{}, fmt.Errorf("add participant to lesson %v: %w", lesson, err)
	}

	return b, nil
}

// ParticipantUpdate is what UpdateTrial sets on a booking.
type ParticipantUpdate struct {
	Trial     bool
	TrialNote string
}

// UpdateTrial sets whether an active booking of a lesson p manages
// (domain.ErrNotFound otherwise) is a trial, and its note, under the same
// window as Add (ErrDayOver), on a lesson the school canceled too. A canceled booking is ErrBookingCanceled; a
// note without a trial is a validation error on trialNote. It records no
// booking event: its status does not change. A trial is free: a booking
// that becomes one gives its visit back, and one that stops being one uses
// a pass if it is due (ADR 0012).
func (s *ParticipantService) UpdateTrial(
	ctx context.Context, p Principal, id domain.BookingID, in ParticipantUpdate,
) (domain.Booking, error) {
	var b domain.Booking

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		var l domain.Lesson
		if b, l, err = managedBooking(ctx, tx, p, id); err != nil {
			return err
		}

		if b.Status == domain.BookingCanceled {
			return ErrBookingCanceled
		}

		now := s.clock.Now()
		if err := changeable(p, loc, now, l); err != nil {
			return err
		}

		old := b
		b.Trial, b.TrialNote = in.Trial, strings.TrimSpace(in.TrialNote)

		if err := b.Validate(); err != nil {
			return err
		}

		return updateBookings(ctx, tx, chargesOf(settings, loc), now, l, bookingChange{before: old, after: b})
	})
	if err != nil {
		return domain.Booking{}, fmt.Errorf("update participant %v: %w", id, err)
	}

	return b, nil
}

// Remove takes a participant off a lesson p manages (domain.ErrNotFound
// otherwise): it cancels the booking, never late, with p's real account as
// the actor, and frees its place for the waiting list (cancelBooking). A
// person's booking is released (domain.Booking.Released): staff took them
// off that date, so a later change of a class membership does not book
// them into it again (ADR 0009, amendment). It works after the start too (a
// walk-in booked by mistake), until the lesson's day is over (ErrDayOver;
// the office always), and on a lesson the school canceled, to tidy it up.
// A canceled booking is ErrBookingCanceled. Before the start of a lesson
// that takes place, the person's recipients (or a guest with an email) get
// the cancellation mail; for a canceled lesson nobody does, and nobody is
// invited (the lesson takes no invites).
func (s *ParticipantService) Remove(ctx context.Context, p Principal, id domain.BookingID) (domain.Booking, error) {
	var b domain.Booking

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		var l domain.Lesson
		if b, l, err = managedBooking(ctx, tx, p, id); err != nil {
			return err
		}

		now := s.clock.Now()

		if b.Status == domain.BookingCanceled {
			return ErrBookingCanceled
		}

		if err := changeable(p, loc, now, l); err != nil {
			return err
		}

		b.Late, b.Released = false, !b.PersonID.IsZero()
		if b, err = cancelBooking(ctx, tx, s.w, chargesOf(settings, loc), actorOf(p), now, l, b); err != nil {
			return err
		}

		if !now.Before(l.StartsAt) || l.Status == domain.LessonCanceled {
			return nil
		}

		participant, err := participantName(ctx, tx, b)
		if err != nil {
			return refErr(err)
		}

		return s.mailParticipant(ctx, tx, settings, loc, now, mail.TemplateBookingCanceled, b, participant, l)
	})
	if err != nil {
		return domain.Booking{}, fmt.Errorf("remove participant %v: %w", id, err)
	}

	return b, nil
}

// attendable checks that p may take attendance of lesson l at now: the
// school has not canceled it (a *BookingClosedError with ClosedCanceled),
// it has started (ErrLessonNotStarted), and its day is not over for its
// instructor and accompanist (ErrDayOver; the office any time after).
func attendable(p Principal, loc *time.Location, now time.Time, l domain.Lesson) error {
	switch {
	case l.Status == domain.LessonCanceled:
		return &BookingClosedError{Why: ClosedCanceled}
	case now.Before(l.StartsAt):
		return ErrLessonNotStarted
	}

	return changeable(p, loc, now, l)
}

// SetAttendance sets the status of an active booking of a lesson p manages
// (domain.ErrNotFound otherwise) to attended, no_show or booked (back to
// unknown); any other status is a validation error on status. It is
// allowed from the lesson's start (attendable). A canceled booking is
// ErrBookingCanceled. A change records a booking event with p's real
// account; setting the status a booking has changes nothing. Redeeming on
// attendance, attended and no-show use a pass, and back to booked gives
// the visit back (ADR 0012).
func (s *ParticipantService) SetAttendance(
	ctx context.Context, p Principal, id domain.BookingID, status domain.BookingStatus,
) (domain.Booking, error) {
	var b domain.Booking

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if status == domain.BookingCanceled || !status.Valid() {
			return errInvalidAttendance
		}

		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		var l domain.Lesson
		if b, l, err = managedBooking(ctx, tx, p, id); err != nil {
			return err
		}

		now := s.clock.Now()

		if b.Status == domain.BookingCanceled {
			return ErrBookingCanceled
		}

		if err := attendable(p, loc, now, l); err != nil {
			return err
		}

		changed, err := s.attend(ctx, tx, p, chargesOf(settings, loc), now, l, []domain.Booking{b}, status)
		if err != nil {
			return err
		}

		if len(changed) > 0 {
			b = changed[0]
		}

		return nil
	})
	if err != nil {
		return domain.Booking{}, fmt.Errorf("set attendance of booking %v: %w", id, err)
	}

	return b, nil
}

// MarkAllAttended marks every booking of a lesson p manages
// (domain.ErrNotFound otherwise) whose attendance is unknown (status
// booked) as attended, under the rules of SetAttendance, and returns the
// bookings it changed.
func (s *ParticipantService) MarkAllAttended(
	ctx context.Context, p Principal, lesson domain.LessonID,
) ([]domain.Booking, error) {
	var out []domain.Booking

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		out = nil // the callback may run more than once

		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		l, err := lockManaged(ctx, tx, p, lesson)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if err := attendable(p, loc, now, l); err != nil {
			return err
		}

		bookings, err := tx.Bookings().ListByLesson(ctx, l.ID)
		if err != nil {
			return err
		}

		unknown := slices.DeleteFunc(bookings, func(b domain.Booking) bool { return b.Status != domain.BookingBooked })

		out, err = s.attend(ctx, tx, p, chargesOf(settings, loc), now, l, unknown, domain.BookingAttended)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("mark all attended in lesson %v: %w", lesson, err)
	}

	return out, nil
}

// attend sets the active bookings of lesson l to status, through the
// ledger under c, which settles their passes together, and records the
// events, by p's real account; a booking that has the status already
// changes nothing. It returns the bookings it changed.
func (s *ParticipantService) attend(
	ctx context.Context, tx store.Tx, p Principal, c charges, now time.Time, l domain.Lesson,
	bookings []domain.Booking, status domain.BookingStatus,
) ([]domain.Booking, error) {
	var (
		changes []bookingChange
		out     []domain.Booking
	)

	for _, b := range bookings {
		if b.Status == status {
			continue
		}

		old := b
		b.Status = status

		if err := b.Validate(); err != nil {
			return nil, err
		}

		changes = append(changes, bookingChange{before: old, after: b})
		out = append(out, b)
	}

	if err := updateBookings(ctx, tx, c, now, l, changes...); err != nil {
		return nil, err
	}

	for _, b := range out {
		if err := recordEvent(ctx, tx, s.w.ids, actorOf(p), now, b, false); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// participantName is the name of the participant of booking b: the
// person's full name, or the guest's name. A missing person is
// domain.ErrNotFound.
func participantName(ctx context.Context, tx store.Tx, b domain.Booking) (string, error) {
	if b.PersonID.IsZero() {
		return b.GuestName, nil
	}

	who, err := tx.Persons().Get(ctx, b.PersonID)
	if err != nil {
		return "", fmt.Errorf("person %v: %w", b.PersonID, err)
	}

	return fullName(who), nil
}

// mailParticipant queues template about booking b of lesson l to the
// recipients of its participant: a person's recipient chain (ADR 0010), or
// a guest's own address in the school's default language, if any.
func (s *ParticipantService) mailParticipant(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, now time.Time, template string,
	b domain.Booking, participant string, l domain.Lesson,
) error {
	var recipients []recipient

	if b.PersonID.IsZero() {
		if r, ok := guestRecipient(b); ok {
			recipients = append(recipients, r)
		}
	} else {
		var err error
		if recipients, err = personRecipients(ctx, tx, b.PersonID); err != nil {
			return err
		}
	}

	return mailBookings(ctx, tx, s.w.ids, s.w.mails.Renderer, settings, loc, now, template, recipients, participant,
		[]FollowingBooking{{Booking: b, Lesson: l}})
}

// Participant is a booking of a lesson as staff see it: the participant's
// name and how to reach them.
type Participant struct {
	Booking domain.Booking
	// Name is the person's full name or the guest's name; Anonymized tells
	// that the person is anonymized (ADR 0016), so that Name is the fixed one.
	Name       string
	Anonymized bool
	// ContactEmail is where the lesson mails about a person go first (the
	// head of their recipient chain: their account, their own address, or
	// a guardian's account), or a guest's address; empty if none.
	ContactEmail string
	// ContactPhone is the person's phone, else the phone of the first of
	// their guardians who has one; empty for guests and if nobody has one.
	ContactPhone string
	// Payment is how the booking is paid (ADR 0012).
	Payment BookingPayment
}

// WaitingParticipant is an entry of a lesson's waiting list as staff see
// it.
type WaitingParticipant struct {
	WaitlistPlace

	// Name is the person's full name; Anonymized as in Participant.
	Name       string
	Anonymized bool
	// Status is the entry's status, except that an invite whose expiry has
	// passed shows as expired, like in the schedule.
	Status domain.WaitlistStatus
}

// LessonParticipants is everyone booked for a lesson or on its list, for
// its staff.
type LessonParticipants struct {
	Lesson domain.Lesson
	// Bookings are all the lesson's bookings, canceled ones included,
	// ordered by BookedAt and ID.
	Bookings []Participant
	// Waitlist is the lesson's list, ended entries included, ordered by
	// JoinedAt and ID.
	Waitlist []WaitingParticipant
	// Active counts the active bookings, Held the places held by invites,
	// Waiting the entries waiting for an invite.
	Active, Held, Waiting int
	// DayOverAt is when the instructor's attendance window closes
	// (attendance opens at the lesson's start).
	DayOverAt time.Time
}

// FreePlaces is the capacity minus the active bookings and the held places,
// at least 0.
func (lp LessonParticipants) FreePlaces() int { return max(lp.Lesson.Capacity-lp.Active-lp.Held, 0) }

// Participants returns the bookings and the waiting list of a lesson p
// manages (domain.ErrNotFound otherwise) with the participants' names and
// contact data (ADR 0009, amendment).
func (s *ParticipantService) Participants(
	ctx context.Context, p Principal, lesson domain.LessonID,
) (LessonParticipants, error) {
	var out LessonParticipants

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		l, err := tx.Lessons().Get(ctx, lesson)
		if err != nil {
			return err
		}

		if err := manages(ctx, tx, p, l); err != nil {
			return err
		}

		bookings, err := tx.Bookings().ListByLesson(ctx, l.ID)
		if err != nil {
			return err
		}

		entries, err := tx.Waitlist().ListByLesson(ctx, l.ID)
		if err != nil {
			return err
		}

		if out, err = participantsOf(ctx, tx, s.clock.Now(), l, bookings, entries); err != nil {
			return err
		}

		out.DayOverAt = dayOverAt(loc, l)

		pays, err := chargesOf(settings, loc).payments(ctx, tx, map[domain.LessonID]domain.Lesson{l.ID: l}, bookings)
		if err != nil {
			return err
		}

		for i := range out.Bookings {
			out.Bookings[i].Payment = pays[out.Bookings[i].Booking.ID]
		}

		return nil
	})
	if err != nil {
		return LessonParticipants{}, fmt.Errorf("participants of lesson %v: %w", lesson, err)
	}

	return out, nil
}

// participantsOf builds the LessonParticipants of lesson l at now from its
// bookings and its waiting-list entries.
func participantsOf(
	ctx context.Context, tx store.Tx, now time.Time, l domain.Lesson, bookings []domain.Booking,
	entries []domain.WaitlistEntry,
) (LessonParticipants, error) {
	var personIDs []domain.PersonID

	for _, b := range bookings {
		if !b.PersonID.IsZero() && !slices.Contains(personIDs, b.PersonID) {
			personIDs = append(personIDs, b.PersonID)
		}
	}

	contacts, err := contactsOf(ctx, tx, personIDs)
	if err != nil {
		return LessonParticipants{}, err
	}

	waiting := make([]domain.PersonID, 0, len(entries))
	for _, e := range entries {
		waiting = append(waiting, e.PersonID)
	}

	waitingPersons, err := tx.Persons().ListByIDs(ctx, waiting)
	if err != nil {
		return LessonParticipants{}, err
	}

	names := byID(waitingPersons)
	out := LessonParticipants{Lesson: l, Bookings: make([]Participant, 0, len(bookings))}

	for _, b := range bookings {
		pt := Participant{Booking: b, Name: b.GuestName, ContactEmail: b.GuestEmail}
		if c, ok := contacts[b.PersonID]; ok && !b.PersonID.IsZero() {
			pt.Name, pt.ContactEmail, pt.ContactPhone, pt.Anonymized = c.name, c.email, c.phone, c.anonymized
		}

		if b.Status != domain.BookingCanceled {
			out.Active++
		}

		out.Bookings = append(out.Bookings, pt)
	}

	open := slices.DeleteFunc(slices.Clone(entries), func(e domain.WaitlistEntry) bool { return !e.Open() })
	out.Held = heldPlaces(open, now)[l.ID]
	out.Waitlist = make([]WaitingParticipant, 0, len(entries))

	for _, e := range entries {
		status := e.Status
		if status == domain.WaitlistInvited && !holdsPlace(e, now) {
			status = domain.WaitlistExpired
		}

		if status == domain.WaitlistWaiting {
			out.Waiting++
		}

		out.Waitlist = append(out.Waitlist, WaitingParticipant{
			WaitlistPlace: WaitlistPlace{Entry: e, Position: position(open, e.ID)},
			Name:          fullName(names[e.PersonID]), Anonymized: names[e.PersonID].Anonymized(), Status: status,
		})
	}

	return out, nil
}

// contact is how staff reach a booked person.
type contact struct {
	name, email, phone string
	anonymized         bool
}

// contactsOf returns, by person, the full name and the contact data of the
// persons (Participant): the head of their mail chain (chainOf) and their
// phone, else their first guardian's with one (ordered like
// PersonRepo.List). It costs the same few queries for any number of
// persons.
func contactsOf(ctx context.Context, tx store.Tx, ids []domain.PersonID) (map[domain.PersonID]contact, error) {
	persons, accounts, err := personsWithAccounts(ctx, tx, ids)
	if err != nil {
		return nil, err
	}

	var viaGuardians []domain.PersonID

	for _, id := range ids {
		p, ok := persons[id]
		if !ok {
			return nil, fmt.Errorf("person %v: %w", id, errBrokenReference)
		}

		if p.Phone == "" || mailsGuardians(p, accounts) {
			viaGuardians = append(viaGuardians, id)
		}
	}

	guardiansOf, guardians, guardianAccounts, err := guardiansWithAccounts(ctx, tx, viaGuardians)
	if err != nil {
		return nil, err
	}

	out := make(map[domain.PersonID]contact, len(ids))

	for _, id := range ids {
		p := persons[id]
		c := contact{name: fullName(p), phone: p.Phone, anonymized: p.Anonymized()}

		if chain := chainOf(p, accounts, guardiansOf[id], guardians, guardianAccounts); len(chain) > 0 {
			c.email = chain[0].email
		}

		for _, g := range guardiansOf[id] {
			c.phone = cmp.Or(c.phone, guardians[g].Phone)
		}

		out[id] = c
	}

	return out, nil
}

// BookingEventView is an entry of a booking's history with the name of its
// actor.
type BookingEventView struct {
	Event domain.BookingEvent
	// ActorName is the full name of the actor account's person; empty for
	// the system.
	ActorName  string
	ActorEmail string
}

// Events returns the history of a booking with the names of its actors,
// ordered by time, for the office (domain.ErrNotFound for anyone else). The
// actor of a change made while an admin impersonated an account is the
// admin.
func (s *ParticipantService) Events(
	ctx context.Context, p Principal, id domain.BookingID,
) ([]BookingEventView, error) {
	var out []BookingEventView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		if !p.Role.AtLeast(domain.RoleOffice) {
			return fmt.Errorf("%w: booking %v", domain.ErrNotFound, id)
		}

		if _, err := tx.Bookings().Get(ctx, id); err != nil {
			return err
		}

		events, err := tx.Bookings().ListEvents(ctx, id)
		if err != nil {
			return err
		}

		var actorIDs []domain.AccountID

		for _, e := range events {
			if !e.ActorAccountID.IsZero() && !slices.Contains(actorIDs, e.ActorAccountID) {
				actorIDs = append(actorIDs, e.ActorAccountID)
			}
		}

		accounts, err := tx.Accounts().ListByIDs(ctx, actorIDs)
		if err != nil {
			return err
		}

		personIDs := make([]domain.PersonID, 0, len(accounts))
		for _, a := range accounts {
			personIDs = append(personIDs, a.PersonID)
		}

		persons, err := tx.Persons().ListByIDs(ctx, personIDs)
		if err != nil {
			return err
		}

		names := byID(persons)
		actors := make(map[domain.AccountID]domain.Account, len(accounts))

		for _, a := range accounts {
			actors[a.ID] = a
		}

		out = make([]BookingEventView, 0, len(events))

		for _, e := range events {
			v := BookingEventView{Event: e}
			if a, ok := actors[e.ActorAccountID]; ok {
				v.ActorName, v.ActorEmail = fullName(names[a.PersonID]), a.Email
			}

			out = append(out, v)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("events of booking %v: %w", id, err)
	}

	return out, nil
}

// MaxStaffPersonMatches is the most persons FindPersons returns.
const MaxStaffPersonMatches = 20

// minPersonQuery is the shortest query, in characters, FindPersons answers.
const minPersonQuery = 2

// PersonHit is a person as staff find them to add to a lesson: the name and
// the birth year to tell persons of the same name apart, no contact data.
type PersonHit struct {
	ID   domain.PersonID
	Name string
	// BirthYear is the year of the birth date, 0 if unknown.
	BirthYear int
}

// FindPersons returns the persons whose given or family name contains
// every word of query, ignoring case, ordered by family name and given
// name; at most MaxStaffPersonMatches. Unlike the office's
// PersonService.SearchPersons it matches names only and returns no contact
// data, as every staff member may search (ADR 0009, amendment). A query
// shorter than two characters, spaces aside, finds nobody.
func (s *ParticipantService) FindPersons(ctx context.Context, query string) ([]PersonHit, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < minPersonQuery {
		return []PersonHit{}, nil
	}

	var persons []domain.Person

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		persons, err = tx.Persons().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("find persons: %w", err)
	}

	words := strings.Fields(strings.ToLower(query))
	out := make([]PersonHit, 0, min(len(persons), MaxStaffPersonMatches))

	for _, p := range persons {
		if len(out) == MaxStaffPersonMatches {
			break
		}

		if containsAll(strings.ToLower(p.GivenName+" "+p.FamilyName), words) {
			out = append(out, PersonHit{ID: p.ID, Name: fullName(p), BirthYear: p.BirthDate.Year})
		}
	}

	return out, nil
}
