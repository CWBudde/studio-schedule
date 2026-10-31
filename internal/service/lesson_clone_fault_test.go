package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// cloneAcrossTerms is the fixture at lessonNow with the fixture term ending
// on 2026-10-31, a later term from 2026-11-01 and one in 2028. The source
// day, Thursday 2026-10-22, has a lesson of a Thursday class, a lesson that
// stayed with its booking when its class retired, and a one-off workshop;
// the first term also has a Monday class. The later term has a Thursday
// class at 17:00. It returns the request that clones the source day to
// Thursday 2026-11-05.
func cloneAcrossTerms(t *testing.T) (*genFixture, CloneRequest, domain.ClassID) {
	t.Helper()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	f.term.EndsOn = mustDate(t, "2026-10-31")
	later := domain.Term{
		ID: ids.New[domain.TermID](f.seq), Name: "Fictional Winter",
		StartsOn: mustDate(t, "2026-11-01"), EndsOn: mustDate(t, "2027-07-31"),
	}
	far := domain.Term{
		ID: ids.New[domain.TermID](f.seq), Name: "Fictional Far Year",
		StartsOn: mustDate(t, "2028-09-01"), EndsOn: mustDate(t, "2029-07-31"),
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Terms().Update(ctx, f.term), tx.Terms().Create(ctx, later), tx.Terms().Create(ctx, far))
	})

	timetable := f.timetable(t, lessonNow)
	create := func(term domain.TermID, wd time.Weekday, hour int) TimetableClass {
		d := f.classData()
		d.Weekday, d.StartTime = wd, domain.TimeOfDay{Hour: hour}

		c, err := timetable.CreateClass(ctx, term, d)
		if err != nil {
			t.Fatal(err)
		}

		return c
	}

	thursday := create(f.term.ID, time.Thursday, 19)
	create(f.term.ID, time.Monday, 10)
	create(later.ID, time.Thursday, 17)

	retired := create(f.term.ID, time.Thursday, 9)
	f.book(t, f.lessonAt(t, retired.ID, "2026-10-22"), 1)

	d := f.classData()
	d.Weekday, d.StartTime, d.Retired = time.Thursday, domain.TimeOfDay{Hour: 9}, true

	if _, err := timetable.UpdateClass(ctx, retired.ID, d); err != nil {
		t.Fatal(err)
	}

	f.oneOff(t, f.lessonService(t, lessonNow), "2026-10-22", domain.TimeOfDay{Hour: 12}, 60)

	return f, CloneRequest{Unit: CloneDay, Source: mustDate(t, "2026-10-22"), Target: mustDate(t, "2026-11-05")}, thursday.ID
}

// TestCloneAcrossTerms: the lesson of a class whose term ends before the
// target is copied like the lesson its retired class left behind, as both
// classes make no lesson in the target week.
func TestCloneAcrossTerms(t *testing.T) {
	t.Parallel()

	f, req, _ := cloneAcrossTerms(t)

	res, err := f.lessonService(t, lessonNow).Clone(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range res.Created {
		got = append(got, c.StartsAt.UTC().Format(time.RFC3339))
	}

	slices.Sort(got)

	// 09:00, 12:00 and 19:00 in Berlin's winter time.
	want := []string{"2026-11-05T08:00:00Z", "2026-11-05T11:00:00Z", "2026-11-05T18:00:00Z"}
	if !slices.Equal(got, want) || len(res.Skipped) != 0 {
		t.Fatalf("created %v, skipped %+v; want %v", got, res.Skipped, want)
	}
}

// TestCloneStoreFaults: a store failure anywhere in a clone, or its
// preview, is returned, and no copy stays.
func TestCloneStoreFaults(t *testing.T) {
	t.Parallel()

	for name, preview := range map[string]bool{"clone": false, "preview": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f, req, _ := cloneAcrossTerms(t)
				fs, svc := f.faultLessons(t)

				return fs, func() error {
					var err error
					if preview {
						_, err = svc.PreviewClone(t.Context(), req)
					} else {
						_, err = svc.Clone(t.Context(), req)
					}

					return err
				}
			}})
		})
	}
}

// cloneMovedOutOfTerm is cloneAcrossTerms with the Thursday class's lesson
// of 2026-10-29 moved to 15:00 on 2026-11-05, outside its term, and the
// request that clones that day to 2026-11-12.
func cloneMovedOutOfTerm(t *testing.T) (*genFixture, CloneRequest, domain.LessonID) {
	t.Helper()

	f, _, thursday := cloneAcrossTerms(t)
	l := f.lessonAt(t, thursday, "2026-10-29")
	d := dataOf(t, l)
	d.Date, d.StartTime = mustDate(t, "2026-11-05"), domain.TimeOfDay{Hour: 15}

	if _, err := f.lessonService(t, lessonNow).UpdateLesson(t.Context(), l.ID, d); err != nil {
		t.Fatal(err)
	}

	return f, CloneRequest{Unit: CloneDay, Source: mustDate(t, "2026-11-05"), Target: mustDate(t, "2026-11-12")}, l.ID
}

// TestCloneLessonMovedOutOfItsTerm: a class lesson the office moved past
// its term's end is copied, as its class has no slot in the target week;
// the lesson of the target term's class is skipped for its class slot.
func TestCloneLessonMovedOutOfItsTerm(t *testing.T) {
	t.Parallel()

	f, req, moved := cloneMovedOutOfTerm(t)

	res, err := f.lessonService(t, lessonNow).Clone(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Created) != 1 || res.Created[0].Source.Lesson.ID != moved ||
		!res.Created[0].StartsAt.Equal(mustInstant(t, "2026-11-12T14:00:00Z")) {
		t.Fatalf("created %+v, want the moved lesson at 15:00", res.Created)
	}

	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipClassSlot {
		t.Fatalf("skipped %+v, want the later term's class lesson", res.Skipped)
	}

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f, req, _ := cloneMovedOutOfTerm(t)
		fs, svc := f.faultLessons(t)

		return fs, func() error {
			_, err := svc.Clone(t.Context(), req)

			return err
		}
	}})
}

// TestCloneRefusesBrokenSource:a source lesson whose copy would be
// invalid (data no validated write leaves, here a workshop without
// capacity) fails the clone, and no copy, of it or another lesson, stays.
func TestCloneRefusesBrokenSource(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)
	f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 9}, 60)

	broken := f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 12}, 60)
	broken.Capacity = 0
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, broken) })

	before := len(f.lessons(t))

	_, err := svc.Clone(t.Context(), CloneRequest{
		Unit: CloneDay, Source: mustDate(t, "2026-10-22"), Target: mustDate(t, "2026-10-29"),
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want a validation error, got %v", err)
	}

	if after := len(f.lessons(t)); after != before {
		t.Fatalf("a failed clone left %d copies", after-before)
	}
}
