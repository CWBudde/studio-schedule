package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// The store-fault tests of the timetable run on the class fixture at 12:00
// on Monday 2026-10-19 in Berlin (see TestClassSync): a class created then
// has lessons on the Tuesdays 2026-10-20 to 2026-11-10.
const faultNow = "2026-10-19T10:00:00Z"

// faultTimetable returns a TimetableService on a FaultStore around the
// fixture's store.
func (f *genFixture) faultTimetable(t *testing.T, now string) (*FaultStore, *TimetableService) {
	t.Helper()

	fs := NewFaultStore(f.st)

	return fs, NewTimetableService(fs, clock.NewFixed(mustInstant(t, now)), f.seq, testMails(newTestRenderer(t)))
}

// addMember stores a new person as a member of class c.
func (f *genFixture) addMember(t *testing.T, c domain.ClassID, given string) domain.Person {
	t.Helper()

	p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: given, FamilyName: "Example"}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Persons().Create(ctx, p),
			tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: c, PersonID: p.ID}),
		)
	})

	return p
}

// classFixture is the fixture with the class of classData created at
// faultNow, through the fixture's own store.
func classFixture(t *testing.T) (*genFixture, TimetableClass) {
	t.Helper()

	f := newGenFixture(t, seed.DemoSettings())

	c, err := f.timetable(t, faultNow).CreateClass(t.Context(), f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	return f, c
}

// TestCreateClassStoreFaults: a store failure anywhere in creating a class
// is returned, and neither the class nor one of its lessons or member
// bookings stays.
func TestCreateClassStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newGenFixture(t, seed.DemoSettings())
		fs, svc := f.faultTimetable(t, faultNow)
		d := f.classData()
		d.AccompanistID = f.pianist

		return fs, func() error {
			_, err := svc.CreateClass(t.Context(), f.term.ID, d)

			return err
		}
	}})
}

// TestUpdateClassStoreFaults: a store failure anywhere in a class edit is
// returned, and the class and its lessons stay as they were: an edit moves
// the future lessons consistently or not at all.
func TestUpdateClassStoreFaults(t *testing.T) {
	t.Parallel()

	edits := map[string]func(f *genFixture) ClassData{
		// Thursday at 17:00 with a lower capacity: the followers move, the
		// booked one is checked against the capacity, and the member is
		// booked into the lesson of the week the horizon now reaches.
		"move": func(f *genFixture) ClassData {
			d := f.classData()
			d.Weekday, d.StartTime, d.Capacity = time.Thursday, domain.TimeOfDay{Hour: 17}, 5

			return d
		},
		// A higher capacity offers the new places to the waiting list.
		"more places": func(f *genFixture) ClassData {
			d := f.classData()
			d.Capacity = 12

			return d
		},
		// A retired class's followers are deleted, the booked one stays.
		"retire": func(f *genFixture) ClassData {
			d := f.classData()
			d.Retired = true

			return d
		},
	}

	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f, c := classFixture(t)
				f.addMember(t, c.ID, "Mia")
				f.book(t, f.lessonAt(t, c.ID, "2026-10-27"), 2)

				// A day later the horizon reaches one more week.
				fs, svc := f.faultTimetable(t, "2026-10-26T10:00:00Z")

				return fs, func() error {
					_, err := svc.UpdateClass(t.Context(), c.ID, edit(f))

					return err
				}
			}})
		})
	}
}

// TestUpdateClassRefusals: an edit with invalid fields or an unknown
// reference is a validation error on the field, and nothing changes.
func TestUpdateClassRefusals(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit  func(f *genFixture, d *ClassData)
		field string
	}{
		"no capacity": {func(_ *genFixture, d *ClassData) { d.Capacity = 0 }, "capacity"},
		"past midnight": {func(_ *genFixture, d *ClassData) {
			d.StartTime, d.Duration = domain.TimeOfDay{Hour: 23}, 2*time.Hour
		}, "durationMinutes"},
		"unknown room":        {func(f *genFixture, d *ClassData) { d.RoomID = ids.New[domain.RoomID](f.seq) }, "roomId"},
		"unknown course type": {func(f *genFixture, d *ClassData) { d.CourseTypeID = ids.New[domain.CourseTypeID](f.seq) }, "courseTypeId"},
		"unknown instructor":  {func(f *genFixture, d *ClassData) { d.InstructorID = ids.New[domain.StaffID](f.seq) }, "instructorId"},
		"unknown accompanist": {func(f *genFixture, d *ClassData) { d.AccompanistID = ids.New[domain.StaffID](f.seq) }, "accompanistId"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f, c := classFixture(t)
			fs := NewFaultStore(f.st)
			before := fs.Snapshot(t)

			d := f.classData()
			d.StartTime = domain.TimeOfDay{Hour: 19}
			tt.edit(f, &d)

			_, err := f.timetable(t, faultNow).UpdateClass(t.Context(), c.ID, d)
			wantFieldError(t, err, tt.field)

			if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
				t.Fatalf("a refused edit changed %v", changed)
			}
		})
	}
}

// TestDeleteClassStoreFaults: a store failure anywhere in deleting a class
// is returned, and the class keeps all its lessons.
func TestDeleteClassStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f, c := classFixture(t)
		fs, svc := f.faultTimetable(t, faultNow)

		return fs, func() error { return svc.DeleteClass(t.Context(), c.ID) }
	}})
}

// TestDeleteClassRace: a reference the store finds only on the delete, as a
// booking or member added by a concurrent writer would be, makes the class
// in use, and nothing is deleted.
func TestDeleteClassRace(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"Lessons.Delete", "Classes.Delete"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			f, c := classFixture(t)
			fs, svc := f.faultTimetable(t, faultNow)
			before := fs.Snapshot(t)
			fs.FailWith(method, 1, domain.ErrConflict)

			err := svc.DeleteClass(t.Context(), c.ID)
			if !errors.Is(err, domain.ErrInUse) || !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("want ErrInUse wrapping the conflict, got %v", err)
			}

			if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
				t.Fatalf("a refused delete changed %v", changed)
			}
		})
	}
}

// noSchoolFixture is the fixture's catalog and term without school
// settings, as before `init` ran.
func noSchoolFixture(t *testing.T) *genFixture {
	t.Helper()

	seq := ids.NewSequence()
	f := &genFixture{
		st: memory.New(), seq: seq,
		term: domain.Term{
			ID: ids.New[domain.TermID](seq), Name: "Fictional Year",
			StartsOn: mustDate(t, "2026-09-01"), EndsOn: mustDate(t, "2027-07-31"),
		},
		room: ids.New[domain.RoomID](seq), courseType: ids.New[domain.CourseTypeID](seq),
		instructor: ids.New[domain.StaffID](seq),
	}
	location := ids.New[domain.LocationID](seq)

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Locations().Create(ctx, domain.Location{ID: location, Name: "Studio Nowhere"}),
			tx.Rooms().Create(ctx, domain.Room{ID: f.room, LocationID: location, Name: "Room One", Capacity: 12, Position: 1}),
			tx.CourseTypes().Create(ctx, domain.CourseType{ID: f.courseType, Name: "Imaginary Ballet"}),
			tx.Staff().Create(ctx, domain.Staff{
				ID: f.instructor, DisplayName: "Ada Example", Kind: domain.StaffInstructor, Active: true,
			}),
			tx.Terms().Create(ctx, f.term),
		)
	})

	return f
}

// TestTimetableWithoutSchool: without school settings there are no lessons,
// so a class is created and a term's dates change without generating or
// fitting any.
func TestTimetableWithoutSchool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := noSchoolFixture(t)
	svc := f.timetable(t, faultNow)

	c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	d := f.classData()
	d.Weekday = time.Friday

	if _, err := svc.UpdateClass(ctx, c.ID, d); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateTerm(ctx, f.term.ID, f.termData(t, "2026-10-01", "2027-06-30")); err != nil {
		t.Fatal(err)
	}

	if got := f.lessons(t); len(got) != 0 {
		t.Fatalf("lessons without a school: %+v", got)
	}

	if all, err := svc.AllClasses(ctx, f.term.ID); err != nil || len(all) != 1 || all[0].Weekday != time.Friday {
		t.Fatalf("classes %+v, %v", all, err)
	}
}

// TestUpdateTermStoreFaults: a store failure anywhere in a change of a
// term's dates is returned, and the term and its lessons stay as they were:
// the lessons the new dates leave out are deleted atomically.
func TestUpdateTermStoreFaults(t *testing.T) {
	t.Parallel()

	// An earlier end leaves out the lessons of 11-03 and 11-10; the second
	// class, with a member, gets its lessons from the day after the later
	// start.
	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f, _ := classFixture(t)
		c2 := f.addClass(t, time.Thursday, domain.TimeOfDay{Hour: 17})
		f.addMember(t, c2.ID, "Mia")

		fs, svc := f.faultTimetable(t, faultNow)

		return fs, func() error {
			_, err := svc.UpdateTerm(t.Context(), f.term.ID, f.termData(t, "2026-10-21", "2026-11-01"))

			return err
		}
	}})
}

// TestRenameTerm: a new name alone changes no lesson, not even one that
// lies outside the term's dates and has started (left by older data),
// which a change of the dates would refuse.
func TestRenameTerm(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f, c := classFixture(t)
	svc := f.timetable(t, faultNow)

	// A lesson last August, before the term starts.
	old := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: mustDate(t, "2026-08-25"),
		CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor,
		StartsAt: mustInstant(t, "2026-08-25T16:00:00Z"), EndsAt: mustInstant(t, "2026-08-25T17:00:00Z"),
		Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 10,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, old) })

	before := f.classLessons(t, c.ID)

	d := f.termData(t, "2026-09-01", "2027-07-31")
	d.Name = "  Fictional Renamed Year  "

	term, err := svc.UpdateTerm(ctx, f.term.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	if term.Name != "Fictional Renamed Year" {
		t.Fatalf("name %q, want it trimmed", term.Name)
	}

	if got := f.classLessons(t, c.ID); !slices.Equal(got, before) {
		t.Fatalf("a rename changed the lessons: %+v, want %+v", got, before)
	}

	// The same lesson refuses a change of the dates.
	_, err = svc.UpdateTerm(ctx, f.term.ID, f.termData(t, "2026-09-01", "2027-06-30"))
	wantFieldError(t, err, "startsOn")
}

// TestGenerateStoreFaults: a store failure anywhere in a generation run is
// returned, and none of its lessons or member bookings stays.
func TestGenerateStoreFaults(t *testing.T) {
	t.Parallel()

	// Two classes with a member each in the fixture term, and a class in a
	// term outside the range, which generation leaves alone.
	setup := func(t *testing.T) (*genFixture, *FaultStore, *LessonGenerator) {
		t.Helper()

		f := newGenFixture(t, seed.DemoSettings())

		for _, wd := range []time.Weekday{time.Tuesday, time.Thursday} {
			c := f.addClass(t, wd, domain.TimeOfDay{Hour: 17})
			f.addMember(t, c.ID, "Mia")
		}

		later := domain.Term{
			ID: ids.New[domain.TermID](f.seq), Name: "Fictional Later Year",
			StartsOn: mustDate(t, "2027-09-01"), EndsOn: mustDate(t, "2028-07-31"),
		}
		f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Terms().Create(ctx, later) })

		c := f.addClass(t, time.Monday, domain.TimeOfDay{Hour: 9})
		c.TermID = later.ID
		f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Classes().Update(ctx, c) })

		fs := NewFaultStore(f.st)

		return f, fs, NewLessonGenerator(fs, clock.NewFixed(mustInstant(t, faultNow)), f.seq)
	}

	t.Run("range", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			_, fs, g := setup(t)

			return fs, func() error {
				_, err := g.Generate(t.Context(), mustDate(t, "2026-10-12"), mustDate(t, "2026-10-26"))

				return err
			}
		}})
	})

	t.Run("ahead", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			_, fs, g := setup(t)

			return fs, func() error {
				_, err := g.GenerateAhead(t.Context())

				return err
			}
		}})
	})

	t.Run("the other term stays alone", func(t *testing.T) {
		t.Parallel()

		f, fs, g := setup(t)

		n, err := g.Generate(t.Context(), mustDate(t, "2026-10-12"), mustDate(t, "2026-10-26"))
		if err != nil || n != 4 {
			t.Fatalf("created %d, %v; want 4", n, err)
		}

		// Locked are the fixture term only, and its two classes.
		if got := fs.Count("Terms.Lock"); got != 1 {
			t.Fatalf("locked %d terms, want 1", got)
		}

		for _, l := range f.lessons(t) {
			if l.StartsAt.Weekday() == time.Monday {
				t.Fatalf("lesson of the later term's class: %+v", l)
			}
		}
	})
}

// TestGenerateEmptyRange: a range that ends where it starts, or before,
// creates nothing and reads no term.
func TestGenerateEmptyRange(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 17})
	fs := NewFaultStore(f.st)
	g := NewLessonGenerator(fs, clock.NewFixed(mustInstant(t, faultNow)), f.seq)

	for _, r := range [][2]string{{"2026-10-20", "2026-10-20"}, {"2026-10-27", "2026-10-20"}} {
		n, err := g.Generate(t.Context(), mustDate(t, r[0]), mustDate(t, r[1]))
		if err != nil || n != 0 {
			t.Fatalf("Generate(%s, %s) = %d, %v; want nothing", r[0], r[1], n, err)
		}
	}

	if fs.Count("Terms.List") != 0 || len(f.lessons(t)) != 0 {
		t.Fatalf("an empty range read the terms (%d) or created lessons", fs.Count("Terms.List"))
	}
}

// TestGenerateRefusesBrokenClass: a stored class whose lessons would be
// invalid (data no validated write leaves, here no capacity) fails the run,
// and no lesson of it, or of another class, is written.
func TestGenerateRefusesBrokenClass(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	f.addClass(t, time.Monday, domain.TimeOfDay{Hour: 17})
	broken := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 17})
	broken.Capacity = 0
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Classes().Update(ctx, broken) })

	_, err := generatorAt(f.st, mustInstant(t, faultNow)).Generate(t.Context(), mustDate(t, "2026-10-19"), mustDate(t, "2026-10-26"))
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want a validation error, got %v", err)
	}

	if got := f.lessons(t); len(got) != 0 {
		t.Fatalf("a failed run left lessons: %+v", got)
	}
}

// TestTimetableReadStoreFaults: the timetable's reads return a store
// failure instead of a partial list.
func TestTimetableReadStoreFaults(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"Terms.List", "Terms.Get", "Classes.ListByTerm", "Staff.List"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			f, _ := classFixture(t)
			fs, svc := f.faultTimetable(t, faultNow)
			fs.Fail(method, 1)

			var err error
			if method == "Terms.List" {
				_, err = svc.Terms(t.Context())
			} else {
				_, err = svc.Classes(t.Context(), f.term.ID)
			}

			if !errors.Is(err, ErrInjected) {
				t.Fatalf("want the injected fault, got %v", err)
			}
		})
	}
}
