package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// LessonGenerator creates the lessons of the weekly classes (ADR 0003).
//
// Each class has one lesson per matching weekday within its term's
// inclusive date range: one per slot, identified by the slot's school date.
// A lesson starts at the class's wall-clock start time in the school time
// zone, so it keeps its local time across DST changes. Generation is
// idempotent on (class, slot date): it reads the lessons that exist and
// creates only the missing ones, never relying on a failed insert. Lessons
// that exist are left untouched, whatever their state, and a lesson that
// moved keeps its slot, so the slot is not filled again. Retired classes get
// no lessons. Each new lesson that has not started gets the class members
// booked (ADR 0009).
type LessonGenerator struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
}

// NewLessonGenerator returns a LessonGenerator.
func NewLessonGenerator(st store.Store, clk clock.Clock, src ids.Source) *LessonGenerator {
	return &LessonGenerator{store: st, clock: clk, ids: src}
}

// Generate creates the missing lessons for the school dates in [from, to),
// past ones included, and returns how many it created.
func (g *LessonGenerator) Generate(ctx context.Context, from, to domain.Date) (int, error) {
	return g.generate(ctx, func(context.Context, store.Tx, domain.Settings, *time.Location) (domain.Date, domain.Date, time.Time, error) {
		return from, to, time.Time{}, nil
	})
}

// GenerateAhead creates the missing lessons of the rolling horizon (see
// horizon) that have not started yet. A lesson that was never generated
// before its start did not take place, so generation does not create the
// past; a class created today has no history (ADR 0003).
func (g *LessonGenerator) GenerateAhead(ctx context.Context) (int, error) {
	return g.generate(ctx, func(
		ctx context.Context, tx store.Tx, s domain.Settings, loc *time.Location,
	) (domain.Date, domain.Date, time.Time, error) {
		now := g.clock.Now()
		from, to, err := horizon(ctx, tx, s, domain.DateOf(now, loc))

		return from, to, now, err
	})
}

// horizon returns the school dates [from, to) that generation keeps filled:
// from today to max(7·(ReleaseWeeksAhead+2), visible days) + 7 days after
// it (inclusive), visible days being the longest early access of any
// account (DefaultVisibleDays or an account's override), and at least to
// 7 days after a manual ReleasedThrough date. The horizon covers
// everything a release or a member's view can reach before the next hourly
// run.
func horizon(ctx context.Context, tx store.Tx, s domain.Settings, today domain.Date) (from, to domain.Date, err error) {
	longest, err := tx.Accounts().MaxVisibleDays(ctx)
	if err != nil {
		return domain.Date{}, domain.Date{}, fmt.Errorf("longest early access: %w", err)
	}

	visible := max(s.DefaultVisibleDays, longest)
	ahead := max(7*(s.ReleaseWeeksAhead+2), visible) + 7

	// The manual released-through date releases through a fixed day; the
	// horizon reaches a week past it, as it reaches a week past early access.
	if !s.ReleasedThrough.IsZero() {
		ahead = max(ahead, today.DaysUntil(s.ReleasedThrough)+7)
	}

	return today, today.AddDays(ahead + 1), nil
}

// generate creates the missing lessons for the date range returned by
// dates that start after the returned instant (any start for the zero
// instant), in one transaction.
func (g *LessonGenerator) generate(
	ctx context.Context,
	dates func(context.Context, store.Tx, domain.Settings, *time.Location) (from, to domain.Date, after time.Time, err error),
) (int, error) {
	var created int

	err := g.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		created = 0 // the callback may be retried

		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		from, to, after, err := dates(ctx, tx, settings, loc)
		if err != nil {
			return err
		}

		if from.Compare(to) >= 0 {
			return nil
		}

		terms, err := tx.Terms().List(ctx)
		if err != nil {
			return fmt.Errorf("list terms: %w", err)
		}

		for _, term := range terms {
			if term.EndsOn.Compare(from) < 0 || term.StartsOn.Compare(to) >= 0 {
				continue // no overlap with [from, to)
			}

			// Work with the locked term: a change of its dates that ran
			// meanwhile is complete (TermRepo.Lock).
			if term, err = tx.Terms().Lock(ctx, term.ID); err != nil {
				return fmt.Errorf("lock term %s: %w", term.ID, err)
			}

			classes, err := tx.Classes().ListByTerm(ctx, term.ID)
			if err != nil {
				return fmt.Errorf("list classes of term %s: %w", term.ID, err)
			}

			for _, c := range classes {
				n, err := g.fill(ctx, tx, c, term, from, to, chargesOf(settings, loc), after)
				if err != nil {
					return err
				}

				created += n
			}
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("generate lessons: %w", err)
	}

	return created, nil
}

// fill creates the missing lessons of class c in term t for the slots on
// the school dates in [from, to) that start after the given instant (any
// start for the zero instant; see missingSlots), and returns how many it
// created. It books the class members into each new lesson that has not
// started (bookMembers; ADR 0009). It locks the class first and works with
// the locked version, so a concurrent class edit (UpdateClass, which locks
// it too) is either complete or not begun. The caller has locked t
// (TermRepo.Lock).
func (g *LessonGenerator) fill(
	ctx context.Context, tx store.Tx, c domain.Class, t domain.Term, from, to domain.Date, ch charges,
	after time.Time,
) (int, error) {
	loc := ch.loc

	c, err := tx.Classes().Lock(ctx, c.ID)
	if err != nil {
		return 0, fmt.Errorf("lock class %s: %w", c.ID, err)
	}

	missing, err := missingLessons(ctx, tx, c, t, from, to, loc, after)
	if err != nil {
		return 0, err
	}

	if len(missing) == 0 {
		return 0, nil
	}

	members, err := tx.Classes().ListMembers(ctx, c.ID)
	if err != nil {
		return 0, fmt.Errorf("list members of class %s: %w", c.ID, err)
	}

	now := g.clock.Now()

	for _, l := range missing {
		l.ID = ids.New[domain.LessonID](g.ids)

		if err := l.Validate(); err != nil {
			return 0, fmt.Errorf("lesson of class %s at %s: %w", c.ID, l.StartsAt, err)
		}

		if err := tx.Lessons().Create(ctx, l); err != nil {
			return 0, fmt.Errorf("create lesson of class %s at %s: %w", c.ID, l.StartsAt, err)
		}

		if err := bookMembers(ctx, tx, g.ids, ch, now, l, members); err != nil {
			return 0, err
		}
	}

	return len(missing), nil
}

// missingLessons returns, without IDs, the lessons that generation creates
// for class c in term t: one for each slot on the school dates in [from, to)
// that starts after the given instant (any start for the zero instant) and
// is missing. A weekly class has one lesson a week: a slot is missing while
// no lesson of the class, canceled ones included, has a slot date in its
// Monday-based week, so a lesson that stayed on an earlier weekday of the
// class still counts. A retired class has none.
func missingLessons(
	ctx context.Context, tx store.Tx, c domain.Class, t domain.Term, from, to domain.Date, loc *time.Location,
	after time.Time,
) ([]domain.Lesson, error) {
	slots := classSlots(c, t, from, to, loc)
	if c.Retired || len(slots) == 0 {
		return nil, nil
	}

	existing, err := tx.Lessons().ListByClass(ctx, c.ID, weekOf(slots[0].Date))
	if err != nil {
		return nil, fmt.Errorf("list lessons of class %s: %w", c.ID, err)
	}

	have := make(map[domain.Date]bool, len(existing))
	for _, l := range existing {
		have[weekOf(l.SlotDate)] = true
	}

	var missing []domain.Lesson

	for _, s := range slots {
		if have[weekOf(s.Date)] || !s.Start.After(after) {
			continue
		}

		missing = append(missing, domain.Lesson{
			ClassID:       c.ID,
			SlotDate:      s.Date,
			CourseTypeID:  c.CourseTypeID,
			RoomID:        c.RoomID,
			InstructorID:  c.InstructorID,
			AccompanistID: c.AccompanistID,
			StartsAt:      s.Start,
			EndsAt:        s.End,
			Status:        domain.LessonScheduled,
			Visibility:    domain.VisibilityPublic,
			Kind:          domain.LessonRegular,
			Capacity:      c.Capacity,
		})
	}

	return missing, nil
}

// slot is the school date and time of one lesson of a class.
type slot struct {
	Date       domain.Date
	Start, End time.Time
}

// classSlots returns the lesson times of class c on the school dates in
// [from, to) that lie within the inclusive range of term t, in date order.
// A lesson starts at the class's wall-clock start time in loc (see
// domain.Date.At for DST gaps and overlaps) and lasts c.Duration.
func classSlots(c domain.Class, t domain.Term, from, to domain.Date, loc *time.Location) []slot {
	if from.Compare(t.StartsOn) < 0 {
		from = t.StartsOn
	}

	if end := t.EndsOn.AddDays(1); to.Compare(end) > 0 {
		to = end
	}

	if from.Compare(to) >= 0 {
		return nil
	}

	// Advance to the first matching weekday, then step by weeks.
	d := from.AddDays((int(c.Weekday) - int(from.Weekday()) + 7) % 7)

	var slots []slot

	for ; d.Compare(to) < 0; d = d.AddDays(7) {
		start := d.At(c.StartTime, loc)
		slots = append(slots, slot{Date: d, Start: start, End: start.Add(c.Duration)})
	}

	return slots
}

// weekOf returns the Monday of the week of d.
func weekOf(d domain.Date) domain.Date { return d.AddDays(-mondayIndex(d.Weekday())) }

// mondayIndex numbers the weekdays from Monday (0) to Sunday (6).
func mondayIndex(d time.Weekday) int { return (int(d) + 6) % 7 }
