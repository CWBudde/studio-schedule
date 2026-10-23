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
// inclusive date range. A lesson starts at the class's wall-clock start time
// in the school time zone, so it keeps its local time across DST changes.
// Generation is idempotent on (class, start): it reads the lessons that
// exist and creates only the missing ones, never relying on a failed insert.
// Lessons that exist are left untouched, whatever their state.
type LessonGenerator struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
}

// NewLessonGenerator returns a LessonGenerator.
func NewLessonGenerator(st store.Store, clk clock.Clock, src ids.Source) *LessonGenerator {
	return &LessonGenerator{store: st, clock: clk, ids: src}
}

// Generate creates the missing lessons for the school dates in [from, to)
// and returns how many it created.
func (g *LessonGenerator) Generate(ctx context.Context, from, to domain.Date) (int, error) {
	return g.generate(ctx, func(context.Context, store.Tx, domain.Settings, *time.Location) (domain.Date, domain.Date, error) {
		return from, to, nil
	})
}

// GenerateAhead creates the missing lessons of the rolling horizon: from 7
// days before today to max(7·(ReleaseWeeksAhead+2), visible days) + 7 days
// after it (inclusive), today being the current school date and visible
// days the longest early access of any account (DefaultVisibleDays or an
// account's override). The horizon covers everything a release or a
// member's view can reach before the next hourly run.
func (g *LessonGenerator) GenerateAhead(ctx context.Context) (int, error) {
	return g.generate(ctx, func(
		ctx context.Context, tx store.Tx, s domain.Settings, loc *time.Location,
	) (domain.Date, domain.Date, error) {
		longest, err := tx.Accounts().MaxVisibleDays(ctx)
		if err != nil {
			return domain.Date{}, domain.Date{}, fmt.Errorf("longest early access: %w", err)
		}

		visible := max(s.DefaultVisibleDays, longest)

		today := domain.DateOf(g.clock.Now(), loc)
		ahead := max(7*(s.ReleaseWeeksAhead+2), visible) + 7

		return today.AddDays(-7), today.AddDays(ahead + 1), nil
	})
}

// lessonKey identifies a generated lesson. The start is in UTC so that equal
// instants compare equal regardless of their location.
type lessonKey struct {
	class domain.ClassID
	start time.Time
}

// generate creates the missing lessons for the date range returned by
// dates, in one transaction.
func (g *LessonGenerator) generate(
	ctx context.Context,
	dates func(context.Context, store.Tx, domain.Settings, *time.Location) (from, to domain.Date, err error),
) (int, error) {
	var created int

	err := g.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		created = 0 // the callback may be retried

		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		from, to, err := dates(ctx, tx, settings, loc)
		if err != nil {
			return err
		}

		if from.Compare(to) >= 0 {
			return nil
		}

		existing, err := tx.Lessons().ListRange(ctx, from.At(domain.TimeOfDay{}, loc), to.At(domain.TimeOfDay{}, loc))
		if err != nil {
			return fmt.Errorf("list lessons: %w", err)
		}

		have := make(map[lessonKey]bool, len(existing))
		for _, l := range existing {
			if !l.ClassID.IsZero() {
				have[lessonKey{l.ClassID, l.StartsAt.UTC()}] = true
			}
		}

		terms, err := tx.Terms().List(ctx)
		if err != nil {
			return fmt.Errorf("list terms: %w", err)
		}

		for _, term := range terms {
			if term.EndsOn.Compare(from) < 0 || term.StartsOn.Compare(to) >= 0 {
				continue // no overlap with [from, to)
			}

			classes, err := tx.Classes().ListByTerm(ctx, term.ID)
			if err != nil {
				return fmt.Errorf("list classes of term %s: %w", term.ID, err)
			}

			for _, c := range classes {
				for _, s := range classSlots(c, term, from, to, loc) {
					key := lessonKey{c.ID, s.Start.UTC()}
					if have[key] {
						continue
					}

					if err := g.create(ctx, tx, c, s); err != nil {
						return err
					}

					have[key] = true
					created++
				}
			}
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("generate lessons: %w", err)
	}

	return created, nil
}

// create stores the lesson of class c in slot s.
func (g *LessonGenerator) create(ctx context.Context, tx store.Tx, c domain.Class, s slot) error {
	l := domain.Lesson{
		ID:            ids.New[domain.LessonID](g.ids),
		ClassID:       c.ID,
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
	}

	if err := l.Validate(); err != nil {
		return fmt.Errorf("lesson of class %s at %s: %w", c.ID, s.Start, err)
	}

	if err := tx.Lessons().Create(ctx, l); err != nil {
		return fmt.Errorf("create lesson of class %s at %s: %w", c.ID, s.Start, err)
	}

	return nil
}

// slot is the time of one lesson of a class.
type slot struct {
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
		slots = append(slots, slot{Start: start, End: start.Add(c.Duration)})
	}

	return slots
}
