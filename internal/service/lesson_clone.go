package service

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office copies the scheduled lessons of a school day or a Monday-based
// week to a later day or week (ADR 0007, "Cloning"). Copies are one-off
// lessons; a clone only adds lessons, never changes or deletes one, and
// mails nobody (copies have no bookings).

// CloneUnit is what a clone copies: one school day or a Monday-based week.
type CloneUnit string

// The units of a clone.
const (
	CloneDay  CloneUnit = "day"
	CloneWeek CloneUnit = "week"
)

// SkipReason tells why a clone does not copy a source lesson.
type SkipReason string

// The reasons a clone skips a source lesson.
const (
	// SkipClassSlot: the lesson's class has a slot in the target week
	// within its term, so generation makes that week's lesson.
	SkipClassSlot SkipReason = "class-slot"
	// SkipStarted: the copy would not start in the future.
	SkipStarted SkipReason = "started"
	// SkipStaffInactive: the lesson's instructor or accompanist is no
	// longer active.
	SkipStaffInactive SkipReason = "staff-inactive"
	// SkipRoomTaken: another lesson that is not canceled, a lesson that
	// generation will create for a class and that does not exist yet, or
	// an earlier copy of the same clone uses the room at an overlapping
	// time.
	SkipRoomTaken SkipReason = "room-taken"
)

// CloneRequest names the source and the target of a clone: school dates,
// both Mondays for CloneWeek.
type CloneRequest struct {
	Unit   CloneUnit
	Source domain.Date
	Target domain.Date
}

// CloneResult lists, in the order of the source lessons, the copies a clone
// creates (or would create, in a preview) and the source lessons it skips.
// Canceled source lessons are left out of both.
type CloneResult struct {
	Created []LessonCopy
	Skipped []SkippedLesson
}

// LessonCopy is a copy of Source at a new time.
type LessonCopy struct {
	// ID is the new lesson's; zero in a preview.
	ID               domain.LessonID
	Source           ScheduledLesson
	StartsAt, EndsAt time.Time
}

// SkippedLesson is a source lesson a clone does not copy, with the time its
// copy would have had.
type SkippedLesson struct {
	Source           ScheduledLesson
	StartsAt, EndsAt time.Time
	Reason           SkipReason
	// TakenBy names the lesson in the room, only for SkipRoomTaken.
	TakenBy *RoomTaker
}

// RoomTaker is the lesson that uses the room a copy needs.
type RoomTaker struct {
	// Lesson is the lesson in the room; for a copy of the same clone it
	// is that copy, whose ID is zero in a preview. A lesson that
	// generation will create for its class (Lesson.ClassID) and that does
	// not exist yet has a zero ID and a zero CopyOf.
	Lesson     domain.Lesson
	CourseType domain.CourseType
	// CopyOf is the source lesson of that copy; zero for a lesson that
	// exists.
	CopyOf domain.LessonID
}

// PreviewClone returns what Clone would do now, without changing anything.
func (s *LessonService) PreviewClone(ctx context.Context, r CloneRequest) (CloneResult, error) {
	return s.clone(ctx, r, false)
}

// Clone copies the scheduled lessons of r.Source to r.Target in one
// transaction: each becomes a one-off lesson (no class) at the same
// school-local wall-clock time, with the course type, room, staff, length,
// capacity, kind and visibility of the source and no bookings. A source
// lesson is skipped (see SkipReason, in this order) when its class covers
// the target week, when its copy would not start in the future, when its
// instructor or accompanist is no longer active, or when its room is taken.
//
// The unit must be CloneDay or CloneWeek, a week's dates must be Mondays,
// and the target must differ from the source and not lie before today (a
// week: its Monday); each is a validation error on that field.
func (s *LessonService) Clone(ctx context.Context, r CloneRequest) (CloneResult, error) {
	return s.clone(ctx, r, true)
}

func (s *LessonService) clone(ctx context.Context, r CloneRequest, apply bool) (CloneResult, error) {
	mode := store.ReadOnly
	if apply {
		mode = store.ReadWrite
	}

	var out CloneResult

	err := s.store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		_, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if err := r.validate(domain.DateOf(now, loc)); err != nil {
			return err
		}

		c := &cloner{tx: tx, loc: loc, now: now, lock: apply, terms: map[domain.TermID]domain.Term{}}
		if apply {
			c.ids = s.ids
		}

		out, err = c.run(ctx, r)

		return err
	})
	if err != nil {
		return CloneResult{}, fmt.Errorf("clone %s %s to %s: %w", r.Unit, r.Source, r.Target, err)
	}

	return out, nil
}

// validate checks r against the school date today.
func (r CloneRequest) validate(today domain.Date) error {
	switch {
	case r.Unit != CloneDay && r.Unit != CloneWeek:
		return &domain.FieldError{Field: "unit", Msg: "the unit must be day or week"}
	case !r.Source.Valid():
		return &domain.FieldError{Field: "source", Msg: "invalid date"}
	case !r.Target.Valid():
		return &domain.FieldError{Field: "target", Msg: "invalid date"}
	case r.Unit == CloneWeek && r.Source.Weekday() != time.Monday:
		return &domain.FieldError{Field: "source", Msg: "a week starts on a Monday"}
	case r.Unit == CloneWeek && r.Target.Weekday() != time.Monday:
		return &domain.FieldError{Field: "target", Msg: "a week starts on a Monday"}
	case r.Target == r.Source:
		return &domain.FieldError{Field: "target", Msg: "the target must differ from the source"}
	case r.Target.Compare(today) < 0:
		return &domain.FieldError{Field: "target", Msg: "the target must not lie in the past"}
	}

	return nil
}

// days is the number of school dates r copies.
func (r CloneRequest) days() int {
	if r.Unit == CloneWeek {
		return 7
	}

	return 1
}

// cloner plans, and with ids set creates, the copies of one clone.
type cloner struct {
	tx  store.Tx
	loc *time.Location
	now time.Time
	// lock locks the terms and classes it reads, as generation does.
	lock bool
	// ids creates the copies; nil for a preview.
	ids   ids.Source
	refs  lessonRefs
	terms map[domain.TermID]domain.Term
	// pending are the lessons that generation will create in the target
	// dates and that do not exist yet (see missingLessons), without IDs.
	pending []domain.Lesson
	// copies are the planned copies, with their sources.
	copies []plannedCopy
}

type plannedCopy struct {
	lesson domain.Lesson
	source ScheduledLesson
}

func (c *cloner) run(ctx context.Context, r CloneRequest) (CloneResult, error) {
	until := r.Source.AddDays(r.days())
	midnight := domain.TimeOfDay{}

	lessons, err := c.tx.Lessons().ListRange(ctx, r.Source.At(midnight, c.loc), until.At(midnight, c.loc))
	if err != nil {
		return CloneResult{}, fmt.Errorf("lessons of %s: %w", r.Source, err)
	}

	if c.refs, err = loadRangeRefs(ctx, c.tx, r.Source, until); err != nil {
		return CloneResult{}, err
	}

	lessons = slices.DeleteFunc(lessons, func(l domain.Lesson) bool { return l.Status != domain.LessonScheduled })
	for _, l := range lessons {
		if err := c.refs.loadClass(ctx, c.tx, l.ClassID); err != nil {
			return CloneResult{}, err
		}
	}

	if err := c.loadTimetable(ctx, lessons, r.Target, r.Target.AddDays(r.days())); err != nil {
		return CloneResult{}, err
	}

	shift := r.Source.DaysUntil(r.Target)
	out := CloneResult{Created: []LessonCopy{}, Skipped: []SkippedLesson{}}

	for _, l := range lessons {
		src, err := c.refs.resolve(l)
		if err != nil {
			return CloneResult{}, err
		}

		cp := c.copyOf(l, shift)

		skip, err := c.skip(ctx, src, cp)
		if err != nil {
			return CloneResult{}, err
		}

		if skip != nil {
			skip.Source = src
			out.Skipped = append(out.Skipped, *skip)

			continue
		}

		if c.ids != nil {
			cp.ID = ids.New[domain.LessonID](c.ids)
		}

		c.copies = append(c.copies, plannedCopy{lesson: cp, source: src})
		out.Created = append(out.Created, LessonCopy{ID: cp.ID, Source: src, StartsAt: cp.StartsAt, EndsAt: cp.EndsAt})
	}

	if c.ids == nil {
		return out, nil
	}

	for _, p := range c.copies {
		if err := p.lesson.Validate(); err != nil {
			return CloneResult{}, fmt.Errorf("copy of lesson %s: %w", p.source.Lesson.ID, err)
		}

		if err := c.tx.Lessons().Create(ctx, p.lesson); err != nil {
			return CloneResult{}, fmt.Errorf("copy of lesson %s: %w", p.source.Lesson.ID, err)
		}
	}

	return out, nil
}

// loadTimetable loads the terms and classes the clone checks, locked when
// locking, and the pending lessons of the target dates [from, to): the
// terms of the classes of the source lessons and the terms that overlap the
// target dates, the classes of the source lessons and all classes of the
// terms that overlap the target dates. The classes come from c.refs.
//
// Lock order: all terms first, in the order of TermRepo.List, then the
// classes, term by term in that order and within a term in the order of
// ClassRepo.ListByTerm. Generation (LessonGenerator.generate) and term
// edits lock in these orders too, and lock a class only while holding its
// term, so they cannot deadlock with a clone.
func (c *cloner) loadTimetable(ctx context.Context, sources []domain.Lesson, from, to domain.Date) error {
	sourceClasses := map[domain.ClassID]bool{}
	sourceTerms := map[domain.TermID]bool{}

	for _, l := range sources {
		if !l.ClassID.IsZero() {
			sourceClasses[l.ClassID] = true
			sourceTerms[c.refs.classes[l.ClassID].TermID] = true
		}
	}

	all, err := c.tx.Terms().List(ctx)
	if err != nil {
		return fmt.Errorf("terms: %w", err)
	}

	var terms []domain.Term

	inTarget := map[domain.TermID]bool{}

	for _, t := range all {
		inTarget[t.ID] = t.EndsOn.Compare(from) >= 0 && t.StartsOn.Compare(to) < 0
		if !inTarget[t.ID] && !sourceTerms[t.ID] {
			continue
		}

		if c.lock {
			if t, err = c.tx.Terms().Lock(ctx, t.ID); err != nil {
				return fmt.Errorf("lock term %s: %w", t.ID, err)
			}
		}

		c.terms[t.ID] = t
		terms = append(terms, t)
	}

	for _, t := range terms {
		if !inTarget[t.ID] && !c.lock {
			continue // c.refs holds the classes of the source lessons
		}

		classes, err := c.tx.Classes().ListByTerm(ctx, t.ID)
		if err != nil {
			return fmt.Errorf("classes of term %s: %w", t.ID, err)
		}

		for _, class := range classes {
			if !inTarget[t.ID] && !sourceClasses[class.ID] {
				continue
			}

			if c.lock {
				if class, err = c.tx.Classes().Lock(ctx, class.ID); err != nil {
					return fmt.Errorf("lock class %s: %w", class.ID, err)
				}
			}

			c.refs.classes[class.ID] = class

			if !inTarget[t.ID] {
				continue
			}

			pending, err := missingLessons(ctx, c.tx, class, t, from, to, c.loc, c.now)
			if err != nil {
				return err
			}

			c.pending = append(c.pending, pending...)
		}
	}

	return nil
}

// copyOf returns the copy of l shift days later, without an ID: a
// scheduled one-off lesson at the same school-local wall-clock time and of
// the same length.
func (c *cloner) copyOf(l domain.Lesson, shift int) domain.Lesson {
	local := l.StartsAt.In(c.loc)
	start := domain.DateOf(l.StartsAt, c.loc).AddDays(shift).
		At(domain.TimeOfDay{Hour: local.Hour(), Minute: local.Minute()}, c.loc)

	return domain.Lesson{
		CourseTypeID: l.CourseTypeID, RoomID: l.RoomID, InstructorID: l.InstructorID, AccompanistID: l.AccompanistID,
		StartsAt: start, EndsAt: start.Add(l.EndsAt.Sub(l.StartsAt)), Status: domain.LessonScheduled,
		Visibility: l.Visibility, Kind: l.Kind, Capacity: l.Capacity, Renter: l.Renter,
	}
}

// skip returns why source lesson src is not copied as cp, or nil.
func (c *cloner) skip(ctx context.Context, src ScheduledLesson, cp domain.Lesson) (*SkippedLesson, error) {
	skipped := func(reason SkipReason) *SkippedLesson {
		return &SkippedLesson{StartsAt: cp.StartsAt, EndsAt: cp.EndsAt, Reason: reason}
	}

	switch {
	case c.covered(src.Lesson.ClassID, domain.DateOf(cp.StartsAt, c.loc)):
		return skipped(SkipClassSlot), nil
	case !cp.StartsAt.After(c.now):
		return skipped(SkipStarted), nil
	// A room rental has no staff to be inactive.
	case !src.Lesson.InstructorID.IsZero() && !src.Instructor.Active ||
		!src.Lesson.AccompanistID.IsZero() && !src.Accompanist.Active:
		return skipped(SkipStaffInactive), nil
	}

	by, err := c.roomTaker(ctx, cp)
	if by == nil || err != nil {
		return nil, err
	}

	s := skipped(SkipRoomTaken)
	s.TakenBy = by

	return s, nil
}

// roomTaker returns the lesson that uses the room of cp at an overlapping
// time, or nil: a lesson that exists and is not canceled, a pending lesson,
// or an earlier copy of this clone, in this order.
func (c *cloner) roomTaker(ctx context.Context, cp domain.Lesson) (*RoomTaker, error) {
	other, taken, err := roomTaker(ctx, c.tx, cp)
	if err != nil {
		return nil, err
	}

	if taken {
		return c.taker(other, domain.LessonID{})
	}

	overlaps := func(l domain.Lesson) bool {
		return l.RoomID == cp.RoomID && l.StartsAt.Before(cp.EndsAt) && l.EndsAt.After(cp.StartsAt)
	}

	for _, l := range c.pending {
		if overlaps(l) {
			return c.taker(l, domain.LessonID{})
		}
	}

	for _, p := range c.copies {
		if overlaps(p.lesson) {
			return c.taker(p.lesson, p.source.Lesson.ID)
		}
	}

	return nil, nil
}

// taker returns l, a copy of copyOf unless that is zero, as a RoomTaker.
func (c *cloner) taker(l domain.Lesson, copyOf domain.LessonID) (*RoomTaker, error) {
	// A room rental may have no course type; CourseType stays zero then.
	ct, ok := c.refs.courseTypes[l.CourseTypeID]
	if !ok && !l.CourseTypeID.IsZero() {
		return nil, fmt.Errorf("lesson %v: course type %v: %w", l.ID, l.CourseTypeID, errBrokenReference)
	}

	return &RoomTaker{Lesson: l, CourseType: ct, CopyOf: copyOf}, nil
}

// covered reports whether class id (none if zero) has a slot in the
// Monday-based week of d within its term, so generation makes that week's
// lesson. A retired class makes none.
func (c *cloner) covered(id domain.ClassID, d domain.Date) bool {
	if id.IsZero() {
		return false
	}

	class := c.refs.classes[id]
	if class.Retired {
		return false
	}

	week := weekOf(d)

	return len(classSlots(class, c.terms[class.TermID], week, week.AddDays(7), c.loc)) > 0
}
