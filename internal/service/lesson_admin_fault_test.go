package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// faultLessons returns a LessonService on a FaultStore around the fixture's
// store, at lessonNow.
func (f *genFixture) faultLessons(t *testing.T) (*FaultStore, *LessonService) {
	t.Helper()

	fs := NewFaultStore(f.st)

	return fs, NewLessonService(fs, clock.NewFixed(mustInstant(t, lessonNow)), f.seq, testMails(newTestRenderer(t)))
}

// fullLesson is the one-off lesson of lessonData with two places, booked by
// two persons with accounts, and a third person waiting for a place.
func fullLesson(t *testing.T) (*genFixture, domain.Lesson) {
	t.Helper()

	f := newGenFixture(t, seed.DemoSettings())
	d := f.lessonData()
	d.Capacity = 2

	sl, err := f.lessonService(t, lessonNow).CreateLesson(t.Context(), NewLesson{LessonData: d, CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	f.bookFor(t, sl.Lesson,
		f.person(t, "Mia", "", "mia@example.org", "en"),
		f.person(t, "Tom", "", "tom@example.org", "de"))

	sam := f.person(t, "Sam", "", "sam@example.org", "en")
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return tx.Waitlist().Create(ctx, domain.WaitlistEntry{
			ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: sl.Lesson.ID, PersonID: sam.ID,
			Status: domain.WaitlistWaiting, JoinedAt: mustInstant(t, "2026-10-20T12:00:00Z"),
		})
	})

	return f, sl.Lesson
}

// TestCreateLessonStoreFaults: a store failure anywhere in creating a
// lesson is returned, and no lesson stays.
func TestCreateLessonStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newGenFixture(t, seed.DemoSettings())
		fs, svc := f.faultLessons(t)
		d := f.lessonData()
		d.AccompanistID = f.pianist

		return fs, func() error {
			_, err := svc.CreateLesson(t.Context(), NewLesson{LessonData: d, CourseTypeID: f.courseType})

			return err
		}
	}})
}

// TestUpdateLessonStoreFaults: a store failure anywhere in a lesson edit is
// returned, and the lesson, its bookings, the waiting list and the outbox
// stay as they were.
func TestUpdateLessonStoreFaults(t *testing.T) {
	t.Parallel()

	t.Run("move with more places", func(t *testing.T) {
		t.Parallel()

		// An hour later with a third place: the bookings are mailed the new
		// time, and Sam is invited to the new place.
		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f, l := fullLesson(t)
			fs, svc := f.faultLessons(t)
			d := dataOf(t, l)
			d.StartTime, d.Capacity = domain.TimeOfDay{Hour: 18}, 3

			return fs, func() error {
				_, err := svc.UpdateLesson(t.Context(), l.ID, d)

				return err
			}
		}})
	})

	t.Run("becomes a rental", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newGenFixture(t, seed.DemoSettings())

			sl, err := f.lessonService(t, lessonNow).CreateLesson(t.Context(),
				NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
			if err != nil {
				t.Fatal(err)
			}

			fs, svc := f.faultLessons(t)
			d := dataOf(t, sl.Lesson)
			d.Kind, d.InstructorID, d.Capacity, d.Visibility = domain.LessonRental, domain.StaffID{}, 0, domain.VisibilityHidden
			d.Renter = "Fictional Choir"

			return fs, func() error {
				_, err := svc.UpdateLesson(t.Context(), sl.Lesson.ID, d)

				return err
			}
		}})
	})
}

// TestUpdateLessonRefusals: an edit with invalid fields or an unknown
// reference is a validation error on the field, and the lesson stays.
func TestUpdateLessonRefusals(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit  func(f *genFixture, d *LessonData)
		field string
	}{
		"invalid date":  {func(_ *genFixture, d *LessonData) { d.Date = domain.Date{Year: 2026, Month: 2, Day: 30} }, "date"},
		"invalid start": {func(_ *genFixture, d *LessonData) { d.StartTime = domain.TimeOfDay{Hour: 25} }, "startTime"},
		"no duration":   {func(_ *genFixture, d *LessonData) { d.Duration = 0 }, "durationMinutes"},
		"part minute":   {func(_ *genFixture, d *LessonData) { d.Duration = 90 * time.Second }, "durationMinutes"},
		"past midnight": {func(_ *genFixture, d *LessonData) {
			d.StartTime, d.Duration = domain.TimeOfDay{Hour: 23}, 61*time.Minute
		}, "durationMinutes"},
		"unknown instructor": {func(f *genFixture, d *LessonData) { d.InstructorID = ids.New[domain.StaffID](f.seq) }, "instructorId"},
		"unknown room":       {func(f *genFixture, d *LessonData) { d.RoomID = ids.New[domain.RoomID](f.seq) }, "roomId"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f, l := fullLesson(t)
			fs, svc := f.faultLessons(t)
			before := fs.Snapshot(t)
			d := dataOf(t, l)
			tt.edit(f, &d)

			_, err := svc.UpdateLesson(t.Context(), l.ID, d)
			wantFieldError(t, err, tt.field)

			if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
				t.Fatalf("a refused edit changed %v", changed)
			}
		})
	}

	t.Run("a lesson may end at midnight", func(t *testing.T) {
		t.Parallel()

		f, l := fullLesson(t)
		_, svc := f.faultLessons(t)
		d := dataOf(t, l)
		d.StartTime, d.Duration = domain.TimeOfDay{Hour: 23}, time.Hour

		got, err := svc.UpdateLesson(t.Context(), l.ID, d)
		if err != nil {
			t.Fatal(err)
		}

		if want := mustInstant(t, "2026-10-30T23:00:00Z"); !got.Lesson.EndsAt.Equal(want) {
			t.Fatalf("ends at %v, want %v", got.Lesson.EndsAt, want)
		}
	})
}

// TestLessonStatusStoreFaults: a store failure anywhere in canceling or
// restoring a lesson is returned, and the lesson, the passes, the waiting
// list and the outbox stay as they were.
func TestLessonStatusStoreFaults(t *testing.T) {
	t.Parallel()

	t.Run("cancel", func(t *testing.T) {
		t.Parallel()

		// Canceled by an office account with a name, which the result shows.
		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f, l := fullLesson(t)
			olga := f.person(t, "Olga", "", "olga@example.org", "en")
			p := Principal{AccountID: f.accountOf(t, olga.ID), Role: domain.RoleOffice}
			fs, svc := f.faultLessons(t)

			return fs, func() error {
				sl, err := svc.CancelLesson(t.Context(), p, l.ID, "Studio closed")
				if err == nil && sl.Admin.CanceledBy.Name != "Olga Example" {
					t.Fatalf("canceled by %+v, want Olga", sl.Admin.CanceledBy)
				}

				return err
			}
		}})
	})

	t.Run("restore", func(t *testing.T) {
		t.Parallel()

		// A canceled lesson with a free place: restored, its bookings are
		// mailed and Sam is invited.
		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f, l := fullLesson(t)
			svc := f.lessonService(t, lessonNow)

			d := dataOf(t, l)
			d.Capacity = 3

			if _, err := svc.UpdateLesson(t.Context(), l.ID, d); err != nil {
				t.Fatal(err)
			}

			if _, err := svc.CancelLesson(t.Context(), Principal{}, l.ID, ""); err != nil {
				t.Fatal(err)
			}

			fs, fsvc := f.faultLessons(t)

			return fs, func() error {
				_, err := fsvc.RestoreLesson(t.Context(), l.ID, false)

				return err
			}
		}})
	})
}

// TestDeleteLessonStoreFaults: a store failure anywhere in deleting a
// one-off lesson is returned, and the lesson stays.
func TestDeleteLessonStoreFaults(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*FaultStore, *LessonService, domain.LessonID) {
		t.Helper()

		f := newGenFixture(t, seed.DemoSettings())

		sl, err := f.lessonService(t, lessonNow).CreateLesson(t.Context(),
			NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
		if err != nil {
			t.Fatal(err)
		}

		fs, svc := f.faultLessons(t)

		return fs, svc, sl.Lesson.ID
	}

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		fs, svc, id := setup(t)

		return fs, func() error { return svc.DeleteLesson(t.Context(), id) }
	}})

	// A booking the store finds only on the delete, as one made meanwhile
	// would be, makes the lesson in use.
	t.Run("race", func(t *testing.T) {
		t.Parallel()

		fs, svc, id := setup(t)
		before := fs.Snapshot(t)
		fs.FailWith("Lessons.Delete", 1, domain.ErrConflict)

		err := svc.DeleteLesson(t.Context(), id)
		if !errors.Is(err, domain.ErrInUse) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("want ErrInUse wrapping the conflict, got %v", err)
		}

		if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
			t.Fatalf("a refused delete changed %v", changed)
		}
	})
}

// TestRoomTakenStoreFaults: a store failure while naming the lesson that
// takes the room is returned instead of a RoomTakenError, for a new lesson
// and a restored one alike.
func TestRoomTakenStoreFaults(t *testing.T) {
	t.Parallel()

	// The op succeeds when it is refused with the RoomTakenError.
	taken := func(err error) error {
		if rt := (*RoomTakenError)(nil); errors.As(err, &rt) {
			return nil
		}

		if err == nil {
			return errors.New("saved despite the taken room")
		}

		return err
	}

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newGenFixture(t, seed.DemoSettings())
			if _, err := f.lessonService(t, lessonNow).CreateLesson(t.Context(),
				NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType}); err != nil {
				t.Fatal(err)
			}

			fs, svc := f.faultLessons(t)

			return fs, func() error {
				_, err := svc.CreateLesson(t.Context(), NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})

				return taken(err)
			}
		}})
	})

	t.Run("move", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f, l := fullLesson(t)

			d := f.lessonData()
			d.StartTime = domain.TimeOfDay{Hour: 19}
			if _, err := f.lessonService(t, lessonNow).CreateLesson(t.Context(),
				NewLesson{LessonData: d, CourseTypeID: f.courseType}); err != nil {
				t.Fatal(err)
			}

			fs, svc := f.faultLessons(t)
			moved := dataOf(t, l)
			moved.StartTime = domain.TimeOfDay{Hour: 18}

			return fs, func() error {
				_, err := svc.UpdateLesson(t.Context(), l.ID, moved)

				return taken(err)
			}
		}})
	})
}
