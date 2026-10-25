package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// syncLessons brings the future lessons of a class in term t in line with
// its edit from old to c (ADR 0007), then generates the missing lessons of
// the rolling horizon that have not started yet. old is the zero class for a
// new one.
//
// A future lesson follows the class while it is not detached and still is
// what old generated: scheduled, public and regular, in a slot on old's weekday at its start,
// with old's course type, room, staff and capacity. Anything else was
// edited one by one and stays, like every lesson that has started. A
// follower moves to the new weekday within its Monday-based week and takes
// the new time, room, staff and capacity; its bookings move with it. A
// follower whose new slot lies in the past or outside the term, or whose
// week has a lesson that stays, and every follower of a retired class, is
// deleted with its automatic member bookings, or stays as it is while it
// has other bookings (ADR 0009). The class has at most one lesson a week,
// so generation skips a week with a lesson that stays.
func (s *TimetableService) syncLessons(ctx context.Context, tx store.Tx, t domain.Term, old, c domain.Class) error {
	settings, loc, err := loadSchool(ctx, tx)
	if errors.Is(err, ErrNoSchool) {
		return nil // without a school there are no lessons
	} else if err != nil {
		return err
	}

	now := s.clock.Now()
	today := domain.DateOf(now, loc)

	ch := chargesOf(settings, loc)

	if !old.ID.IsZero() {
		if err := s.moveFollowers(ctx, tx, t, old, c, now, ch); err != nil {
			return err
		}
	}

	from, to, err := horizon(ctx, tx, settings, today)
	if err != nil {
		return err
	}

	_, err = s.gen.fill(ctx, tx, c, t, from, to, ch, now)

	return err
}

// moveFollowers applies the edit from old to c to the future lessons that
// follow the class; see syncLessons.
func (s *TimetableService) moveFollowers(
	ctx context.Context, tx store.Tx, t domain.Term, old, c domain.Class, now time.Time, ch charges,
) error {
	loc := ch.loc

	lessons, err := tx.Lessons().ListByClass(ctx, c.ID, domain.DateOf(now, loc))
	if err != nil {
		return fmt.Errorf("list lessons of class %s: %w", c.ID, err)
	}

	// The weeks (by their Monday) of the lessons that stay where they are.
	taken := map[domain.Date]bool{}

	var followers []domain.Lesson

	for _, l := range lessons {
		if l.StartsAt.After(now) && follows(l, old, loc) {
			followers = append(followers, l)
		} else {
			taken[weekOf(l.SlotDate)] = true
		}
	}

	shift := mondayIndex(c.Weekday) - mondayIndex(old.Weekday)

	for _, l := range followers {
		slot := l.SlotDate.AddDays(shift)
		start := slot.At(c.StartTime, loc)
		moves := !c.Retired && !taken[weekOf(slot)] && start.After(now) &&
			slot.Compare(t.StartsOn) >= 0 && slot.Compare(t.EndsOn) <= 0

		if err := s.follow(ctx, tx, ch, l.ID, c, slot, start, moves); err != nil {
			return err
		}

		if moves {
			taken[weekOf(slot)] = true
		}
	}

	return nil
}

// follow moves lesson id of class c to slot, starting at start, or, unless
// it moves, deletes it with its member bookings while it has no other
// bookings. It locks the lesson first, as every capacity check must. A
// moved lesson's bookings keep their passes only if valid on the new date
// (updateLesson).
func (s *TimetableService) follow(
	ctx context.Context, tx store.Tx, ch charges, id domain.LessonID, c domain.Class, slot domain.Date,
	start time.Time, moves bool,
) error {
	l, err := tx.Lessons().Lock(ctx, id)
	if err != nil {
		return err
	}

	if !moves {
		kept, err := hasOwnBookings(ctx, tx, l.ID)
		if err != nil || kept {
			return err
		}

		return deleteClassLesson(ctx, tx, l.ID)
	}

	now := s.clock.Now()

	// Only a lower capacity can conflict with the bookings and the places
	// that waiting-list invites hold (ADR 0010): a lesson booked beyond its
	// capacity already (a member's override, ADR 0009) moves along when the
	// capacity stays.
	if c.Capacity < l.Capacity {
		n, held, err := places(ctx, tx, now, l.ID)
		if err != nil {
			return err
		}

		if n+held > c.Capacity {
			return &domain.FieldError{Field: "capacity", Msg: fmt.Sprintf("the lesson of %s %s", l.SlotDate, overCapacity(n, held))}
		}
	}

	raised := c.Capacity > l.Capacity
	old := l

	l.SlotDate, l.StartsAt, l.EndsAt = slot, start, start.Add(c.Duration)
	l.CourseTypeID, l.RoomID, l.Capacity = c.CourseTypeID, c.RoomID, c.Capacity
	l.InstructorID, l.AccompanistID = c.InstructorID, c.AccompanistID

	if err := updateLesson(ctx, tx, ch, now, old, l); err != nil || !raised {
		return err
	}

	// The new places go to the waiting list.
	return s.w.offer(ctx, tx, now, l, nil)
}

// follows reports whether lesson l still is what class c generated for its
// slot: it is not detached (ADR 0007), and its fields are what c generates.
// The second part covers lessons edited before the detached flag existed.
func follows(l domain.Lesson, c domain.Class, loc *time.Location) bool {
	start := l.SlotDate.At(c.StartTime, loc)

	return !l.Detached && l.SlotDate.Weekday() == c.Weekday &&
		l.Status == domain.LessonScheduled && l.Visibility == domain.VisibilityPublic &&
		l.Kind == domain.LessonRegular && l.StartsAt.Equal(start) && l.EndsAt.Equal(start.Add(c.Duration)) &&
		l.CourseTypeID == c.CourseTypeID && l.RoomID == c.RoomID && l.InstructorID == c.InstructorID &&
		l.AccompanistID == c.AccompanistID && l.Capacity == c.Capacity
}
