package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// scene is the staff fixture's week from bookingNow with every kind of
// lesson the read paths resolve: the Tuesday member class of Mia from
// 2026-10-27 (in the fixture term), the accompanied one-off of
// 2026-10-22 that Tom is booked for, the full workshop of 2026-10-23 with
// Mia on its waiting list, and a lesson on 2026-10-24 of a class of another
// term (moved out of its term's dates).
type scene struct {
	class                           domain.ClassID
	accompanied, full, stray, early domain.Lesson
}

// newScene builds the scene on f.
func (f *staffFixture) newScene(t *testing.T) scene {
	t.Helper()

	s := scene{class: f.memberClass(t).ID}
	s.early = f.lessonAt(t, s.class, "2026-10-27")
	s.accompanied = f.lessonWithPianist(t, 5)
	f.bookAs(t, f.tom, s.accompanied)
	s.full = f.waitingMia(t)
	s.stray = f.strayClassLesson(t, "2026-10-24T09:00:00Z")

	return s
}

// strayClassLesson stores a term of 2027/28 with a class of the fixture's
// catalog and a lesson of that class at start, outside the term's dates,
// as if moved there.
func (f *staffFixture) strayClassLesson(t *testing.T, start string) domain.Lesson {
	t.Helper()

	term := domain.Term{
		ID: ids.New[domain.TermID](f.seq), Name: "Fictional Next Year",
		StartsOn: mustDate(t, "2027-09-01"), EndsOn: mustDate(t, "2028-07-31"),
	}
	c := domain.Class{
		ID: ids.New[domain.ClassID](f.seq), TermID: term.ID, CourseTypeID: f.courseType, RoomID: f.room,
		InstructorID: f.instructor, AccompanistID: f.pianist, Level: domain.LevelAdvanced, Weekday: time.Saturday,
		StartTime: domain.TimeOfDay{Hour: 11}, Duration: time.Hour, Capacity: 8,
	}
	at := mustInstant(t, start)
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: mustDate(t, "2027-09-04"),
		CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor, AccompanistID: f.pianist,
		StartsAt: at, EndsAt: at.Add(time.Hour), Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic,
		Kind: domain.LessonRegular, Capacity: 8,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Terms().Create(ctx, term), tx.Classes().Create(ctx, c), tx.Lessons().Create(ctx, l))
	})

	return l
}

// earlyAnna is Anna with early access for a fortnight, so she sees the
// member class's first lessons.
func (f *staffFixture) earlyAnna(t *testing.T) Principal {
	t.Helper()

	p := f.principal(t, f.anna)
	p.VisibleDays = 14

	return p
}

func (f *staffFixture) faultSchedule(t *testing.T) (*FaultStore, *ScheduleService) {
	t.Helper()

	fs := NewFaultStore(f.st)

	return fs, NewScheduleService(fs, clock.NewFixed(mustInstant(t, bookingNow)))
}

// TestScheduleStoreFaults: a store failure while reading the timetable,
// its catalog, classes, availability or the viewer's family is returned,
// never a schedule with lessons or family missing.
func TestScheduleStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newStaffFixture(t)
		s := f.newScene(t)
		anna := f.earlyAnna(t)
		fs, svc := f.faultSchedule(t)
		q := ScheduleQuery{From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-28")}

		return fs, func() error {
			got, err := svc.Schedule(t.Context(), anna, q)
			if err != nil {
				return err
			}

			// Without a fault, every path is taken.
			byID := map[domain.LessonID]ScheduledLesson{}
			for _, sl := range got.Lessons {
				byID[sl.Lesson.ID] = sl
			}

			if len(byID[s.accompanied.ID].BookedFor) != 1 || len(byID[s.full.ID].Waiting) != 1 ||
				byID[s.stray.ID].Level != domain.LevelAdvanced || len(byID[s.early.ID].BookedFor) != 1 {
				return fmt.Errorf("unexpected schedule %+v", got.Lessons)
			}

			return nil
		}
	}})
}

// TestLessonStoreFaults: a store failure while reading one lesson, its
// references, availability or the viewer's family is returned, for a
// member and for the office (whose details come from elsewhere).
func TestLessonStoreFaults(t *testing.T) {
	t.Parallel()

	for name, who := range map[string]func(f *staffFixture, t *testing.T) Principal{
		"member": (*staffFixture).earlyAnna,
		"office": func(f *staffFixture, t *testing.T) Principal { return f.principal(t, f.olga) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newStaffFixture(t)
				s := f.newScene(t)
				p := who(f, t)
				fs, svc := f.faultSchedule(t)

				return fs, func() error {
					_, err := svc.Lesson(t.Context(), p, s.stray.ID)

					return err
				}
			}})
		})
	}
}
