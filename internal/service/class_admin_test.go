package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The class tests run in the demo time zone, Europe/Berlin, around the end
// of summer time on Sunday 2026-10-25. Their term lasts from 2026-09-01 to
// 2027-07-31 (genFixture); the horizon is 29 days with the demo settings.

func (f *genFixture) timetable(t *testing.T, now string) *TimetableService {
	t.Helper()

	return NewTimetableService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, testMails(newTestRenderer(t)))
}

// classData is a Tuesday class at 18:00 for 60 minutes.
func (f *genFixture) classData() ClassData {
	return ClassData{
		CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor, Level: domain.LevelOpen,
		Weekday: time.Tuesday, StartTime: domain.TimeOfDay{Hour: 18}, Duration: time.Hour, Capacity: 10,
	}
}

// book adds n bookings by new persons to the lesson.
func (f *genFixture) book(t *testing.T, l domain.Lesson, n int) {
	t.Helper()

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		for i := range n {
			p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: fmt.Sprint("Guest ", i), FamilyName: "Example"}
			if err := tx.Persons().Create(ctx, p); err != nil {
				return err
			}

			err := tx.Bookings().Create(ctx, domain.Booking{
				ID: ids.New[domain.BookingID](f.seq), LessonID: l.ID, PersonID: p.ID,
				Status: domain.BookingBooked, Source: domain.BookingSourceSelf, BookedAt: l.StartsAt.Add(-48 * time.Hour),
			})
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// classLessons returns the lessons of class c by start.
func (f *genFixture) classLessons(t *testing.T, c domain.ClassID) []domain.Lesson {
	t.Helper()

	return slices.DeleteFunc(f.lessons(t), func(l domain.Lesson) bool { return l.ClassID != c })
}

// lessonAt returns the lesson of class c on the school date, or fails.
func (f *genFixture) lessonAt(t *testing.T, c domain.ClassID, slot string) domain.Lesson {
	t.Helper()

	for _, l := range f.classLessons(t, c) {
		if l.SlotDate == mustDate(t, slot) {
			return l
		}
	}

	t.Fatalf("no lesson of class %s in the slot of %s", c, slot)

	return domain.Lesson{}
}

func TestClassAdmin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	// 12:00 on Wednesday 2026-10-21 in Berlin.
	svc := f.timetable(t, "2026-10-21T10:00:00Z")

	t.Run("create", func(t *testing.T) {
		c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
		if err != nil {
			t.Fatal(err)
		}

		if c.Instructor.ID != f.instructor || c.TermID != f.term.ID || c.Retired {
			t.Fatalf("created %+v", c)
		}

		// Only lessons that have not started: from next Tuesday (in winter
		// time) through the horizon, which ends on 2026-11-18.
		want := []string{"2026-10-27T17:00:00Z", "2026-11-03T17:00:00Z", "2026-11-10T17:00:00Z", "2026-11-17T17:00:00Z"}
		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, want) {
			t.Fatalf("lessons start at %v, want %v", got, want)
		}
	})

	t.Run("unknown term", func(t *testing.T) {
		_, err := svc.CreateClass(ctx, ids.New[domain.TermID](f.seq), f.classData())
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	for field, mutate := range map[string]func(*ClassData){
		"courseTypeId":    func(d *ClassData) { d.CourseTypeID = ids.New[domain.CourseTypeID](f.seq) },
		"roomId":          func(d *ClassData) { d.RoomID = ids.New[domain.RoomID](f.seq) },
		"instructorId":    func(d *ClassData) { d.InstructorID = ids.New[domain.StaffID](f.seq) },
		"accompanistId":   func(d *ClassData) { d.AccompanistID = ids.New[domain.StaffID](f.seq) },
		"capacity":        func(d *ClassData) { d.Capacity = 0 },
		"durationMinutes": func(d *ClassData) { d.StartTime = domain.TimeOfDay{Hour: 23, Minute: 30} },
	} {
		t.Run("invalid "+field, func(t *testing.T) {
			d := f.classData()
			mutate(&d)

			_, err := svc.CreateClass(ctx, f.term.ID, d)

			var fe *domain.FieldError
			if !errors.As(err, &fe) || fe.Field != field {
				t.Fatalf("want a validation error on %s, got %v", field, err)
			}
		})
	}

	t.Run("unknown class", func(t *testing.T) {
		_, err := svc.UpdateClass(ctx, ids.New[domain.ClassID](f.seq), ClassData{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("update: want ErrNotFound, got %v", err)
		}

		if err := svc.DeleteClass(ctx, ids.New[domain.ClassID](f.seq)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("delete: want ErrNotFound, got %v", err)
		}
	})
}

func TestClassDelete(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := "2026-10-21T10:00:00Z"

	t.Run("without history", func(t *testing.T) {
		t.Parallel()

		f := newGenFixture(t, seed.DemoSettings())
		svc := f.timetable(t, now)

		c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
		if err != nil {
			t.Fatal(err)
		}

		if err := svc.DeleteClass(ctx, c.ID); err != nil {
			t.Fatal(err)
		}

		if got := f.classLessons(t, c.ID); len(got) != 0 {
			t.Fatalf("lessons left: %+v", got)
		}

		if all, err := svc.AllClasses(ctx, f.term.ID); err != nil || len(all) != 0 {
			t.Fatalf("classes left: %+v, %v", all, err)
		}
	})

	for name, prepare := range map[string]func(t *testing.T, f *genFixture, c domain.Class){
		"a lesson has started": func(t *testing.T, f *genFixture, _ domain.Class) {
			t.Helper()

			_, err := generatorAt(f.st, mustInstant(t, now)).Generate(ctx, mustDate(t, "2026-10-20"), mustDate(t, "2026-10-21"))
			if err != nil {
				t.Fatal(err)
			}
		},
		"a lesson is booked": func(t *testing.T, f *genFixture, c domain.Class) {
			t.Helper()
			f.book(t, f.lessonAt(t, c.ID, "2026-11-03"), 1)
		},
		"the class has members": func(t *testing.T, f *genFixture, c domain.Class) {
			t.Helper()

			p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Mia", FamilyName: "Example"}
			f.write(t, func(ctx context.Context, tx store.Tx) error {
				return errors.Join(
					tx.Persons().Create(ctx, p),
					tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: c.ID, PersonID: p.ID}),
				)
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newGenFixture(t, seed.DemoSettings())
			svc := f.timetable(t, now)

			tc, err := svc.CreateClass(ctx, f.term.ID, f.classData())
			if err != nil {
				t.Fatal(err)
			}

			prepare(t, f, tc.Class)
			before := f.classLessons(t, tc.ID)

			if err := svc.DeleteClass(ctx, tc.ID); !errors.Is(err, domain.ErrInUse) {
				t.Fatalf("want ErrInUse, got %v", err)
			}

			if got := f.classLessons(t, tc.ID); !slices.Equal(got, before) {
				t.Fatalf("lessons changed: %+v, want %+v", got, before)
			}
		})
	}
}

func TestClassSync(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	// 12:00 on Monday 2026-10-19 in Berlin, the week before the end of summer
	// time; the horizon ends on 2026-11-16. The class also has a lesson that
	// started last Tuesday.
	setup := func(t *testing.T) (*genFixture, *TimetableService, TimetableClass) {
		t.Helper()

		f := newGenFixture(t, seed.DemoSettings())
		svc := f.timetable(t, "2026-10-19T10:00:00Z")

		c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
		if err != nil {
			t.Fatal(err)
		}

		_, err = generatorAt(f.st, mustInstant(t, "2026-10-19T10:00:00Z")).
			Generate(ctx, mustDate(t, "2026-10-13"), mustDate(t, "2026-10-14"))
		if err != nil {
			t.Fatal(err)
		}

		return f, svc, c
	}

	t.Run("a new time keeps the wall clock across DST", func(t *testing.T) {
		t.Parallel()

		f, svc, c := setup(t)
		d := f.classData()
		d.StartTime = domain.TimeOfDay{Hour: 19, Minute: 30}
		d.Duration = 90 * time.Minute

		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		want := []string{
			"2026-10-13T16:00:00Z",                         // started: stays
			"2026-10-20T17:30:00Z",                         // summer time
			"2026-10-27T18:30:00Z",                         // winter time
			"2026-11-03T18:30:00Z", "2026-11-10T18:30:00Z", // the horizon ends on 11-16
		}
		got := f.classLessons(t, c.ID)

		if starts := lessonStarts(got); !slices.Equal(starts, want) {
			t.Fatalf("lessons start at %v, want %v", starts, want)
		}

		if l := got[1]; !l.EndsAt.Equal(l.StartsAt.Add(90 * time.Minute)) {
			t.Fatalf("lesson ends at %v", l.EndsAt)
		}
	})

	t.Run("edited and booked lessons", func(t *testing.T) {
		t.Parallel()

		f, svc, c := setup(t)

		// Edited one by one: a smaller capacity.
		edited := f.lessonAt(t, c.ID, "2026-10-27")
		edited.Capacity = 4
		f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, edited) })

		booked := f.lessonAt(t, c.ID, "2026-11-03")
		f.book(t, booked, 3)

		d := f.classData()
		d.StartTime = domain.TimeOfDay{Hour: 17}
		d.AccompanistID = f.pianist
		d.Capacity = 3

		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		if got := f.lessonAt(t, c.ID, "2026-10-27"); got != edited {
			t.Fatalf("edited lesson changed: %+v, want %+v", got, edited)
		}

		got := f.lessonAt(t, c.ID, "2026-11-03")
		if got.ID != booked.ID || !got.StartsAt.Equal(mustInstant(t, "2026-11-03T16:00:00Z")) ||
			got.AccompanistID != f.pianist || got.Capacity != 3 {
			t.Fatalf("booked lesson %+v did not follow", got)
		}

		// Below its three bookings: refused, and nothing changes.
		before := f.classLessons(t, c.ID)
		d.Capacity = 2

		_, err := svc.UpdateClass(ctx, c.ID, d)

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != "capacity" {
			t.Fatalf("want a validation error on capacity, got %v", err)
		}

		if after := f.classLessons(t, c.ID); !slices.Equal(after, before) {
			t.Fatalf("lessons changed: %+v", after)
		}
	})

	t.Run("a new weekday", func(t *testing.T) {
		t.Parallel()

		f, svc, c := setup(t)
		f.book(t, f.lessonAt(t, c.ID, "2026-10-20"), 1)

		// Moved to Monday at 11:00: today's 11:00 has passed, so tomorrow's
		// booked lesson stays where it is; the others move a day earlier.
		d := f.classData()
		d.Weekday = time.Monday
		d.StartTime = domain.TimeOfDay{Hour: 11}

		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		want := []string{
			"2026-10-13T16:00:00Z", // started
			"2026-10-20T16:00:00Z", // booked, its new slot has passed
			"2026-10-26T10:00:00Z", "2026-11-02T10:00:00Z", "2026-11-09T10:00:00Z", "2026-11-16T10:00:00Z",
		}
		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, want) {
			t.Fatalf("lessons start at %v, want %v", got, want)
		}

		// Back to Tuesday at 18:00: the Monday lessons move a day later, and
		// the booked lesson, which never moved, is what the class generates
		// again.
		if _, err := svc.UpdateClass(ctx, c.ID, f.classData()); err != nil {
			t.Fatal(err)
		}

		want = []string{
			"2026-10-13T16:00:00Z", "2026-10-20T16:00:00Z",
			"2026-10-27T17:00:00Z", "2026-11-03T17:00:00Z", "2026-11-10T17:00:00Z", "2026-11-17T17:00:00Z",
		}
		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, want) {
			t.Fatalf("lessons start at %v, want %v", got, want)
		}
	})

	t.Run("retire and reactivate", func(t *testing.T) {
		t.Parallel()

		f, svc, c := setup(t)
		f.book(t, f.lessonAt(t, c.ID, "2026-11-03"), 1)

		d := f.classData()
		d.Retired = true

		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		want := []string{"2026-10-13T16:00:00Z", "2026-11-03T17:00:00Z"} // started and booked
		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, want) {
			t.Fatalf("lessons start at %v, want %v", got, want)
		}

		if n, err := svc.gen.GenerateAhead(ctx); err != nil || n != 0 {
			t.Fatalf("generation for a retired class: %d, %v", n, err)
		}

		public, err := svc.Classes(ctx, f.term.ID)
		if err != nil || len(public) != 0 {
			t.Fatalf("public classes %+v, %v; want none", public, err)
		}

		all, err := svc.AllClasses(ctx, f.term.ID)
		if err != nil || len(all) != 1 || !all[0].Retired {
			t.Fatalf("all classes %+v, %v; want the retired one", all, err)
		}

		if _, err := svc.UpdateClass(ctx, c.ID, f.classData()); err != nil {
			t.Fatal(err)
		}

		want = []string{
			"2026-10-13T16:00:00Z", "2026-10-20T16:00:00Z", "2026-10-27T17:00:00Z", "2026-11-03T17:00:00Z",
			"2026-11-10T17:00:00Z",
		}
		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, want) {
			t.Fatalf("after reactivating: lessons start at %v, want %v", got, want)
		}
	})

	t.Run("a lesson that stayed behind", func(t *testing.T) {
		t.Parallel()

		f, svc, c := setup(t)
		f.book(t, f.lessonAt(t, c.ID, "2026-11-03"), 1)

		// Retired, moved to Thursday and reactivated: the booked Tuesday
		// lesson stays and still is the lesson of its week.
		d := f.classData()
		d.Retired = true

		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		d.Weekday = time.Thursday
		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		d.Retired = false
		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		want := []string{
			"2026-10-13T16:00:00Z", "2026-10-22T16:00:00Z", "2026-10-29T17:00:00Z",
			"2026-11-03T17:00:00Z", // booked, on Tuesday
			"2026-11-12T17:00:00Z",
		}
		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, want) {
			t.Fatalf("lessons start at %v, want %v", got, want)
		}

		// On to Friday: the Thursday lessons follow, the Tuesday one does not
		// become a Wednesday one.
		d.Weekday = time.Friday
		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		want = []string{
			"2026-10-13T16:00:00Z", "2026-10-23T16:00:00Z", "2026-10-30T17:00:00Z",
			"2026-11-03T17:00:00Z", "2026-11-13T17:00:00Z",
		}
		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, want) {
			t.Fatalf("lessons start at %v, want %v", got, want)
		}
	})

	t.Run("a new weekday at the end of the term", func(t *testing.T) {
		t.Parallel()

		f := newGenFixture(t, seed.DemoSettings())
		// Monday 2027-07-19; the term ends on Saturday 2027-07-31.
		svc := f.timetable(t, "2027-07-19T08:00:00Z")
		d := f.classData()
		d.Weekday = time.Saturday

		c, err := svc.CreateClass(ctx, f.term.ID, d)
		if err != nil {
			t.Fatal(err)
		}

		d.Weekday = time.Sunday

		if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
			t.Fatal(err)
		}

		// Saturday 07-31 would move to Sunday 08-01, after the term.
		want := []string{"2027-07-25T16:00:00Z"}
		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, want) {
			t.Fatalf("lessons start at %v, want %v", got, want)
		}
	})
}
