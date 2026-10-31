package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office edits, cancels, restores, creates and deletes single lessons
// (ADR 0007). Every write to a lesson of a class detaches it, so class and
// term edits leave it alone from then on. Cancellations, restores and moves
// of booked lessons that lie ahead queue mails in the same transaction
// (lesson_mail.go). The HTTP layer admits only office and admin accounts to
// these methods.

var (
	// ErrLessonStarted refuses to edit, cancel, restore or delete a lesson
	// that has started. It is a validation error without a field: the lesson itself,
	// not a field of the request, is the problem.
	ErrLessonStarted = fmt.Errorf("%w: the lesson has started", domain.ErrValidation)
	// ErrLessonPast refuses to create a lesson or move one to a start that
	// is not in the future unless the office confirms it.
	ErrLessonPast = fmt.Errorf("%w: the lesson would start in the past", domain.ErrConflict)
	// ErrGeneratedLesson refuses to delete a lesson of a class: generation
	// would create it again, so it is canceled instead.
	ErrGeneratedLesson = fmt.Errorf("%w: a lesson of a class is canceled, not deleted", domain.ErrConflict)
)

// RoomTakenError reports another lesson that is not canceled and uses the
// room at an overlapping time. The office may save anyway (ADR 0007).
type RoomTakenError struct {
	Lesson     domain.Lesson
	CourseType domain.CourseType
}

func (e *RoomTakenError) Error() string {
	return fmt.Sprintf("the room is taken by lesson %s at %s", e.Lesson.ID, e.Lesson.StartsAt.Format(time.RFC3339))
}

func (e *RoomTakenError) Unwrap() error { return domain.ErrConflict }

// LessonService is the office's lesson administration.
type LessonService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
	mails MailRenderer
	w     waitlister
}

// NewLessonService returns a LessonService that sends its mails, and the
// waiting-list invites of the places it frees, with mails.
func NewLessonService(st store.Store, clk clock.Clock, src ids.Source, mails Mails) *LessonService {
	return &LessonService{store: st, clock: clk, ids: src, mails: mails.Renderer, w: waitlister{ids: src, mails: mails}}
}

// overCapacity explains a capacity below n active bookings and held places
// that waiting-list invites hold.
func overCapacity(n, held int) string {
	if held == 0 {
		return fmt.Sprintf("has %d bookings, more than the capacity", n)
	}

	return fmt.Sprintf("has %d bookings and %d places reserved for the waiting list, more than the capacity", n, held)
}

// LessonData are the fields the office edits on a lesson. Date and
// StartTime are school-local; AccompanistID is zero for none. A room rental
// has no instructor, accompanist or capacity, is hidden and names its
// Renter (domain.Lesson.Validate).
type LessonData struct {
	Date          domain.Date
	StartTime     domain.TimeOfDay
	Duration      time.Duration
	RoomID        domain.RoomID
	InstructorID  domain.StaffID
	AccompanistID domain.StaffID
	Capacity      int
	Visibility    domain.LessonVisibility
	Kind          domain.LessonKind
	Renter        string
	// ConfirmRoomOverlap saves even if another lesson uses the room at
	// that time.
	ConfirmRoomOverlap bool
	// ConfirmPast saves even if the lesson starts in the past.
	ConfirmPast bool
}

// NewLesson are the fields of a one-off lesson.
type NewLesson struct {
	LessonData

	CourseTypeID domain.CourseTypeID
}

// LessonAdmin is what the office sees of a lesson beyond the schedule.
type LessonAdmin struct {
	// Detached tells that the lesson no longer follows its class.
	Detached bool
	// ActiveBookings counts the bookings that are not canceled.
	ActiveBookings int
	// CancelNote, CanceledAt and CanceledBy record a cancellation; the
	// canceling account is zero if unknown.
	CancelNote string
	CanceledAt time.Time
	CanceledBy CanceledBy
}

// CanceledBy names the account that canceled a lesson.
type CanceledBy struct {
	AccountID domain.AccountID
	Name      string
}

// CreateLesson adds a one-off lesson (no class). An unknown course type,
// room or staff member is a validation error on the field that names it. A
// start that is not in the future is ErrLessonPast unless d.ConfirmPast is
// set; another lesson in the room at that time is a *RoomTakenError unless
// d.ConfirmRoomOverlap is set.
func (s *LessonService) CreateLesson(ctx context.Context, d NewLesson) (ScheduledLesson, error) {
	var out ScheduledLesson

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		l := domain.Lesson{
			ID: ids.New[domain.LessonID](s.ids), CourseTypeID: d.CourseTypeID, Status: domain.LessonScheduled,
		}

		if err := s.apply(&l, d.LessonData, loc); err != nil {
			return err
		}

		if err := lessonReferences(ctx, tx, l); err != nil {
			return err
		}

		if err := s.confirmPast(l, d.LessonData); err != nil {
			return err
		}

		if !d.ConfirmRoomOverlap {
			if err := roomFree(ctx, tx, l); err != nil {
				return err
			}
		}

		if err := tx.Lessons().Create(ctx, l); err != nil {
			return err
		}

		// A new lesson has no bookings and no waiting list.
		out, err = adminLesson(ctx, tx, settings, l, 0, 0)

		return err
	})
	if err != nil {
		return ScheduledLesson{}, fmt.Errorf("create lesson: %w", err)
	}

	return out, nil
}

// UpdateLesson replaces the editable fields of a lesson that has not
// started (ErrLessonStarted otherwise) and detaches a lesson of a class.
// Its course type, class and slot stay. Lowering the capacity below the
// active bookings and the places held by waiting-list invites is a
// validation error on capacity; free places (a higher capacity, a lesson
// that takes invites again) go to the waiting list (ADR 0010). A new start in the past needs
// d.ConfirmPast (ErrLessonPast otherwise). A new time or room of a scheduled
// lesson is checked for another lesson in the room (see CreateLesson), and
// with active bookings mails them the change unless the new start is in the
// past. A canceled lesson moves without a mail; its restore names the new
// time and room.
func (s *LessonService) UpdateLesson(ctx context.Context, id domain.LessonID, d LessonData) (ScheduledLesson, error) {
	var out ScheduledLesson

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		old, err := tx.Lessons().Lock(ctx, id)
		if err != nil {
			return err
		}

		if !old.StartsAt.After(s.clock.Now()) {
			return ErrLessonStarted
		}

		l := old
		if err := s.apply(&l, d, loc); err != nil {
			return err
		}

		if err := lessonReferences(ctx, tx, l); err != nil {
			return err
		}

		now := s.clock.Now()

		active, held, err := places(ctx, tx, now, l.ID)
		if err != nil {
			return err
		}

		if err := becomesRental(ctx, tx, old, l); err != nil {
			return err
		}

		// Only a lower capacity can conflict with the bookings and held
		// places: a lesson booked beyond its capacity already (a member's
		// override, ADR 0009) stays editable otherwise.
		if l.Capacity < old.Capacity && active+held > l.Capacity {
			return &domain.FieldError{Field: "capacity", Msg: "the lesson " + overCapacity(active, held)}
		}

		if err := s.confirmPast(l, d); err != nil {
			return err
		}

		moved := !l.StartsAt.Equal(old.StartsAt) || !l.EndsAt.Equal(old.EndsAt) || l.RoomID != old.RoomID
		if moved && l.Status == domain.LessonScheduled && !d.ConfirmRoomOverlap {
			if err := roomFree(ctx, tx, l); err != nil {
				return err
			}
		}

		// A lesson moved to another date keeps its bookings paid only by
		// passes valid on it (ADR 0012).
		if err := updateLesson(ctx, tx, chargesOf(settings, loc), now, old, l); err != nil {
			return err
		}

		changed := !l.StartsAt.Equal(old.StartsAt) || l.RoomID != old.RoomID
		if changed && l.Status == domain.LessonScheduled && active > 0 {
			if err := s.mailChange(ctx, tx, settings, loc, old, l); err != nil {
				return err
			}
		}

		// A higher capacity, a lesson visible again or moved so that booking
		// is open again: free places go to the waiting list. One query when
		// nobody waits.
		if err := s.w.offer(ctx, tx, now, l, nil); err != nil {
			return err
		}

		if active, held, err = places(ctx, tx, now, l.ID); err != nil {
			return err
		}

		out, err = adminLesson(ctx, tx, settings, l, active, held)

		return err
	})
	if err != nil {
		return ScheduledLesson{}, fmt.Errorf("update lesson %s: %w", id, err)
	}

	return out, nil
}

// CancelLesson cancels a lesson that has not started (ErrLessonStarted
// otherwise) with an optional note, records when and by whom, and mails
// everyone with an active booking. The bookings stay as they are, but cost
// nothing: the passes they used get their visits back (ADR 0012); the
// invites of its waiting list are void and go back to waiting, without a
// mail (ADR 0010). An admin
// signed in as another account is recorded as the canceling account. A
// lesson canceled already stays as it is, started or not: its note stays
// too, and nobody is mailed again.
func (s *LessonService) CancelLesson(
	ctx context.Context, p Principal, id domain.LessonID, note string,
) (ScheduledLesson, error) {
	by := cmp.Or(p.ImpersonatorID, p.AccountID)

	return s.setStatus(ctx, id, domain.LessonCanceled, func(ctx context.Context, tx store.Tx, l *domain.Lesson, now time.Time) error {
		l.CancelNote, l.CanceledAt, l.CanceledBy = strings.TrimSpace(note), now, by

		return s.w.void(ctx, tx, l.ID)
	}, mail.TemplateLessonCanceled)
}

// RestoreLesson lets a canceled lesson that has not started take place
// after all (ErrLessonStarted otherwise): it clears the cancellation, keeps
// the lesson detached and mails everyone with an active booking its current
// time and room. Its bookings that are due use passes again (ADR 0012). Its free places go to the waiting list. Another lesson in the room at that time is a
// *RoomTakenError unless confirmRoomOverlap is set. A scheduled lesson stays
// as it is, started or not.
func (s *LessonService) RestoreLesson(
	ctx context.Context, id domain.LessonID, confirmRoomOverlap bool,
) (ScheduledLesson, error) {
	return s.setStatus(ctx, id, domain.LessonScheduled, func(ctx context.Context, tx store.Tx, l *domain.Lesson, _ time.Time) error {
		l.CancelNote, l.CanceledAt, l.CanceledBy = "", time.Time{}, domain.AccountID{}

		if confirmRoomOverlap {
			return nil
		}

		return roomFree(ctx, tx, *l)
	}, mail.TemplateLessonRestored)
}

// setStatus locks lesson id and, unless it has that status already, refuses
// it once started, sets status, lets change record the rest, and mails the
// bookings with template.
func (s *LessonService) setStatus(
	ctx context.Context, id domain.LessonID, status domain.LessonStatus,
	change func(ctx context.Context, tx store.Tx, l *domain.Lesson, now time.Time) error, template string,
) (ScheduledLesson, error) {
	var out ScheduledLesson

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		l, err := tx.Lessons().Lock(ctx, id)
		if err != nil {
			return err
		}

		if l.Status != status {
			now := s.clock.Now()
			if !l.StartsAt.After(now) {
				return ErrLessonStarted
			}

			old := l
			l.Status, l.Detached = status, !l.ClassID.IsZero()
			if err := change(ctx, tx, &l, now); err != nil {
				return err
			}

			if err := l.Validate(); err != nil {
				return err
			}

			// The ledger settles the passes of its bookings: a canceled
			// lesson costs nothing (ADR 0012).
			if err := updateLesson(ctx, tx, chargesOf(settings, loc), now, old, l); err != nil {
				return err
			}

			if err := s.mailLesson(ctx, tx, settings, loc, l, template); err != nil {
				return err
			}

			if status == domain.LessonScheduled {
				if err := s.w.offer(ctx, tx, now, l, nil); err != nil {
					return err
				}
			}
		}

		active, held, err := places(ctx, tx, s.clock.Now(), l.ID)
		if err != nil {
			return err
		}

		out, err = adminLesson(ctx, tx, settings, l, active, held)

		return err
	})
	if err != nil {
		return ScheduledLesson{}, fmt.Errorf("lesson %s: %w", id, err)
	}

	return out, nil
}

// DeleteLesson deletes a one-off lesson that has not started
// (ErrLessonStarted otherwise) and that no booking refers to, canceled ones
// included (domain.ErrInUse otherwise). A lesson of a class is
// ErrGeneratedLesson: it is canceled instead.
func (s *LessonService) DeleteLesson(ctx context.Context, id domain.LessonID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		l, err := tx.Lessons().Lock(ctx, id)
		if err != nil {
			return err
		}

		if !l.ClassID.IsZero() {
			return ErrGeneratedLesson
		}

		if !l.StartsAt.After(s.clock.Now()) {
			return ErrLessonStarted
		}

		booked, err := hasBookings(ctx, tx, l.ID)
		if err != nil {
			return err
		}

		if booked {
			return fmt.Errorf("%w: the lesson has bookings", domain.ErrInUse)
		}

		return tx.Lessons().Delete(ctx, id)
	})
	// A booking added in the meantime makes the delete fail on its
	// reference.
	if errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrInUse) && !errors.Is(err, ErrGeneratedLesson) {
		err = fmt.Errorf("%w (%w)", domain.ErrInUse, err)
	}

	if err != nil {
		return fmt.Errorf("delete lesson %s: %w", id, err)
	}

	return nil
}

// apply sets the fields of d on l, detaches a lesson of a class, and
// validates the result. The lesson must end on its day.
func (s *LessonService) apply(l *domain.Lesson, d LessonData, loc *time.Location) error {
	switch {
	case !d.Date.Valid():
		return &domain.FieldError{Field: "date", Msg: "invalid date"}
	case !d.StartTime.Valid():
		return &domain.FieldError{Field: "startTime", Msg: "invalid start time"}
	case d.Duration <= 0 || d.Duration%time.Minute != 0:
		return &domain.FieldError{Field: "durationMinutes", Msg: "the duration must be a positive number of minutes"}
	case d.Duration > 24*time.Hour-d.StartTime.SinceMidnight():
		return &domain.FieldError{Field: "durationMinutes", Msg: "the lesson must not run past midnight"}
	}

	start := d.Date.At(d.StartTime, loc)
	l.StartsAt, l.EndsAt = start, start.Add(d.Duration)
	l.RoomID, l.InstructorID, l.AccompanistID = d.RoomID, d.InstructorID, d.AccompanistID
	l.Capacity, l.Visibility, l.Kind, l.Renter = d.Capacity, d.Visibility, d.Kind, strings.TrimSpace(d.Renter)
	l.Detached = !l.ClassID.IsZero()

	return l.Validate()
}

// becomesRental refuses to make old, which is no room rental, the rental
// l while bookings (canceled ones included) or its waiting list refer to
// it: a rental takes neither (ADR 0007, amendment). A validation error on
// kind.
func becomesRental(ctx context.Context, tx store.Tx, old, l domain.Lesson) error {
	if l.Kind != domain.LessonRental || old.Kind == domain.LessonRental {
		return nil
	}

	booked, err := hasBookings(ctx, tx, l.ID)
	if err != nil {
		return err
	}

	open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID})
	if err != nil {
		return err
	}

	if booked || len(open) > 0 {
		return &domain.FieldError{Field: "kind", Msg: "a lesson with bookings or a waiting list cannot become a room rental"}
	}

	return nil
}

// confirmPast returns ErrLessonPast if l does not start in the future and
// d does not confirm it.
func (s *LessonService) confirmPast(l domain.Lesson, d LessonData) error {
	if d.ConfirmPast || l.StartsAt.After(s.clock.Now()) {
		return nil
	}

	return ErrLessonPast
}

// lessonReferences checks that the course type, room and staff of l exist,
// reporting a missing one on the field that names it.
func lessonReferences(ctx context.Context, tx store.Tx, l domain.Lesson) error {
	_, _, err := checkReferences(ctx, tx, references{
		CourseTypeID: l.CourseTypeID, RoomID: l.RoomID, InstructorID: l.InstructorID, AccompanistID: l.AccompanistID,
	})

	return err
}

// roomFree returns a *RoomTakenError for the first other lesson that is not
// canceled and uses the room of l at an overlapping time.
func roomFree(ctx context.Context, tx store.Tx, l domain.Lesson) error {
	other, taken, err := roomTaker(ctx, tx, l)
	if err != nil || !taken {
		return err
	}

	ct, err := tx.CourseTypes().Get(ctx, other.CourseTypeID)
	if err != nil {
		return fmt.Errorf("course type %v: %w", other.CourseTypeID, refErr(err))
	}

	return &RoomTakenError{Lesson: other, CourseType: ct}
}

// roomTaker returns the first other lesson that is not canceled and uses
// the room of l at an overlapping time, if there is one.
func roomTaker(ctx context.Context, tx store.Tx, l domain.Lesson) (domain.Lesson, bool, error) {
	others, err := tx.Lessons().ListRoomOverlaps(ctx, l.RoomID, l.StartsAt, l.EndsAt, l.ID)
	if err != nil || len(others) == 0 {
		return domain.Lesson{}, false, err
	}

	return others[0], true, nil
}

// adminLesson returns l with its references, cutoffs and what the office
// sees of it, given its number of active bookings and the places that
// waiting-list invites hold.
func adminLesson(
	ctx context.Context, tx store.Tx, settings domain.Settings, l domain.Lesson, active, held int,
) (ScheduledLesson, error) {
	refs, err := loadRefsOf(ctx, tx, l)
	if err != nil {
		return ScheduledLesson{}, err
	}

	sl, err := refs.resolve(l)
	if err != nil {
		return ScheduledLesson{}, err
	}

	a, err := lessonAdmin(ctx, tx, l, active)
	if err != nil {
		return ScheduledLesson{}, err
	}

	sl.Admin = &a
	sl.setAvailability(settings, active, held)

	return sl, nil
}

// lessonAdmin returns what the office sees of l beyond the schedule, given
// its number of active bookings.
func lessonAdmin(ctx context.Context, tx store.Tx, l domain.Lesson, active int) (LessonAdmin, error) {
	a := LessonAdmin{
		Detached: l.Detached, ActiveBookings: active, CancelNote: l.CancelNote, CanceledAt: l.CanceledAt,
	}

	if l.CanceledBy.IsZero() {
		return a, nil
	}

	acc, err := tx.Accounts().Get(ctx, l.CanceledBy)
	if err != nil {
		return LessonAdmin{}, fmt.Errorf("account %v: %w", l.CanceledBy, refErr(err))
	}

	person, err := tx.Persons().Get(ctx, acc.PersonID)
	if err != nil {
		return LessonAdmin{}, fmt.Errorf("person %v: %w", acc.PersonID, refErr(err))
	}

	a.CanceledBy = CanceledBy{AccountID: acc.ID, Name: fullName(person)}

	return a, nil
}

// fullName is the given and the family name of p.
func fullName(p domain.Person) string { return p.GivenName + " " + p.FamilyName }
