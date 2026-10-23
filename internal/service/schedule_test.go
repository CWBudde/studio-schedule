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
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// scheduleFixture is the demo school with a few hand-made lessons. The
// clock stands on Wednesday 2026-10-21 12:00 school time, so with the demo
// release rule (Sunday 18:00, one week ahead) everything before Monday
// 2026-10-26 00:00 is released.
type scheduleFixture struct {
	svc     *ScheduleService
	data    seed.Dataset
	loc     *time.Location
	now     time.Time
	lessons map[string]domain.Lesson
}

func newScheduleFixture(t *testing.T) scheduleFixture {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	f := scheduleFixture{
		data:    seed.Demo(),
		loc:     mustZone(t, "Europe/Berlin"),
		lessons: map[string]domain.Lesson{},
	}
	f.now = time.Date(2026, time.October, 21, 12, 0, 0, 0, f.loc)
	f.svc = NewScheduleService(st, clock.NewFixed(f.now))

	monBallet := f.class(t, time.Monday, 16, 30)   // Classical Ballet, beginner, Studio A (North)
	monHipHop := f.class(t, time.Monday, 18, 0)    // Hip-Hop, beginner, Hall 1 (South)
	wedPointe := f.class(t, time.Wednesday, 18, 0) // Pointe Work, advanced, Studio B (North)
	tueBallet := f.class(t, time.Tuesday, 16, 0)   // Pre-Ballet
	tueJazz := f.class(t, time.Tuesday, 17, 0)     // Jazz Dance

	f.add("monBallet", f.fromClass(monBallet, schoolDate(2026, time.October, 19)))
	f.add("monBalletNextWeek", f.fromClass(monBallet, schoolDate(2026, time.October, 26)))
	f.add("monHipHop", f.fromClass(monHipHop, schoolDate(2026, time.October, 19)))

	canceled := f.fromClass(wedPointe, schoolDate(2026, time.October, 21))
	canceled.Status = domain.LessonCanceled
	f.add("wedPointeCanceled", canceled)

	hidden := f.fromClass(tueBallet, schoolDate(2026, time.October, 20))
	hidden.Visibility = domain.VisibilityHidden
	f.add("tueHidden", hidden)

	members := f.fromClass(tueJazz, schoolDate(2026, time.October, 20))
	members.Visibility = domain.VisibilityMembers
	f.add("tueMembers", members)

	workshop := domain.Lesson{
		CourseTypeID: f.courseType(t, "Contemporary"),
		RoomID:       f.room(t, "Studio C"),
		InstructorID: f.staff(t, "Marco Wrenfield"),
		StartsAt:     schoolDate(2026, time.November, 7).At(domain.TimeOfDay{Hour: 14}, f.loc),
		Status:       domain.LessonScheduled,
		Visibility:   domain.VisibilityPublic,
		Kind:         domain.LessonWorkshop,
		Capacity:     10,
	}
	workshop.EndsAt = workshop.StartsAt.Add(2 * time.Hour)
	f.add("workshop", workshop)

	hiddenWorkshop := workshop
	hiddenWorkshop.StartsAt = workshop.StartsAt.AddDate(0, 0, 1)
	hiddenWorkshop.EndsAt = workshop.EndsAt.AddDate(0, 0, 1)
	hiddenWorkshop.Visibility = domain.VisibilityHidden
	f.add("hiddenWorkshop", hiddenWorkshop)

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for _, l := range f.lessons {
			if err := l.Validate(); err != nil {
				return err
			}

			if err := tx.Lessons().Create(ctx, l); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return f
}

func schoolDate(y int, m time.Month, d int) domain.Date {
	return domain.Date{Year: y, Month: m, Day: d}
}

func (f *scheduleFixture) add(name string, l domain.Lesson) {
	id, err := domain.ParseID[domain.LessonID](fmt.Sprintf("00000000-0000-7000-8000-00000000a%03d", len(f.lessons)+1))
	if err != nil {
		panic(err)
	}

	l.ID = id
	f.lessons[name] = l
}

func (f *scheduleFixture) fromClass(c domain.Class, d domain.Date) domain.Lesson {
	start := d.At(c.StartTime, f.loc)

	return domain.Lesson{
		ClassID:       c.ID,
		CourseTypeID:  c.CourseTypeID,
		RoomID:        c.RoomID,
		InstructorID:  c.InstructorID,
		AccompanistID: c.AccompanistID,
		StartsAt:      start,
		EndsAt:        start.Add(c.Duration),
		Status:        domain.LessonScheduled,
		Visibility:    domain.VisibilityPublic,
		Kind:          domain.LessonRegular,
		Capacity:      c.Capacity,
	}
}

func (f *scheduleFixture) class(t *testing.T, day time.Weekday, hour, minute int) domain.Class {
	t.Helper()

	for _, c := range f.data.Classes {
		if c.Weekday == day && c.StartTime == (domain.TimeOfDay{Hour: hour, Minute: minute}) {
			return c
		}
	}

	t.Fatalf("no demo class on %v at %02d:%02d", day, hour, minute)

	return domain.Class{}
}

func (f *scheduleFixture) room(t *testing.T, name string) domain.RoomID {
	t.Helper()

	for _, r := range f.data.Rooms {
		if r.Name == name {
			return r.ID
		}
	}

	t.Fatalf("no demo room %q", name)

	return domain.RoomID{}
}

func (f *scheduleFixture) location(t *testing.T, name string) domain.LocationID {
	t.Helper()

	for _, l := range f.data.Locations {
		if l.Name == name {
			return l.ID
		}
	}

	t.Fatalf("no demo location %q", name)

	return domain.LocationID{}
}

func (f *scheduleFixture) staff(t *testing.T, name string) domain.StaffID {
	t.Helper()

	for _, s := range f.data.Staff {
		if s.DisplayName == name {
			return s.ID
		}
	}

	t.Fatalf("no demo staff %q", name)

	return domain.StaffID{}
}

func (f *scheduleFixture) courseType(t *testing.T, name string) domain.CourseTypeID {
	t.Helper()

	for _, c := range f.data.CourseTypes {
		if c.Name == name {
			return c.ID
		}
	}

	t.Fatalf("no demo course type %q", name)

	return domain.CourseTypeID{}
}

// names maps the lessons of a schedule back to their fixture names.
func (f *scheduleFixture) names(s Schedule) []string {
	byID := map[domain.LessonID]string{}
	for name, l := range f.lessons {
		byID[l.ID] = name
	}

	out := make([]string, 0, len(s.Lessons))
	for _, sl := range s.Lessons {
		out = append(out, byID[sl.Lesson.ID])
	}

	return out
}

func TestScheduleFilters(t *testing.T) {
	t.Parallel()

	f := newScheduleFixture(t)
	base := ScheduleQuery{From: schoolDate(2026, time.October, 19), To: schoolDate(2026, time.November, 9)}

	tests := []struct {
		name  string
		query func(q ScheduleQuery) ScheduleQuery
		want  []string
	}{
		{
			name:  "no filter: only visible lessons, in start order",
			query: func(q ScheduleQuery) ScheduleQuery { return q },
			want:  []string{"monBallet", "monHipHop", "wedPointeCanceled", "workshop"},
		},
		{
			name: "location North",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.Location = f.location(t, "Studio North")

				return q
			},
			want: []string{"monBallet", "wedPointeCanceled", "workshop"},
		},
		{
			name: "location South",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.Location = f.location(t, "Studio South")

				return q
			},
			want: []string{"monHipHop"},
		},
		{
			name: "instructor",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.Instructor = f.staff(t, "Lea Moonfield")

				return q
			},
			want: []string{"wedPointeCanceled"},
		},
		{
			name: "instructor matches the accompanist",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.Instructor = f.staff(t, "Felix Keysworth")

				return q
			},
			want: []string{"monBallet"},
		},
		{
			name: "course type",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.CourseType = f.courseType(t, "Hip-Hop")

				return q
			},
			want: []string{"monHipHop"},
		},
		{
			name: "level excludes one-off lessons",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.Level = domain.LevelBeginner

				return q
			},
			want: []string{"monBallet", "monHipHop"},
		},
		{
			name: "level advanced",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.Level = domain.LevelAdvanced

				return q
			},
			want: []string{"wedPointeCanceled"},
		},
		{
			name: "combined filters",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.Location = f.location(t, "Studio North")
				q.Level = domain.LevelBeginner

				return q
			},
			want: []string{"monBallet"},
		},
		{
			name: "to is exclusive",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.To = schoolDate(2026, time.October, 21)

				return q
			},
			want: []string{"monBallet", "monHipHop"},
		},
		{
			name: "single day",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.From, q.To = schoolDate(2026, time.October, 21), schoolDate(2026, time.October, 22)

				return q
			},
			want: []string{"wedPointeCanceled"},
		},
		{
			name: "only hidden and members lessons",
			query: func(q ScheduleQuery) ScheduleQuery {
				q.From, q.To = schoolDate(2026, time.October, 20), schoolDate(2026, time.October, 21)

				return q
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := f.svc.Schedule(t.Context(), tt.query(base))
			if err != nil {
				t.Fatal(err)
			}

			if got := f.names(s); !slices.Equal(got, tt.want) {
				t.Fatalf("lessons = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScheduleResult(t *testing.T) {
	t.Parallel()

	f := newScheduleFixture(t)
	q := ScheduleQuery{From: schoolDate(2026, time.October, 19), To: schoolDate(2026, time.November, 9)}

	s, err := f.svc.Schedule(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}

	if s.Timezone != "Europe/Berlin" || !s.Now.Equal(f.now) || s.From != q.From || s.To != q.To {
		t.Errorf("header = %q %v %v %v", s.Timezone, s.Now, s.From, s.To)
	}

	if want := ComputeReleaseWindow(seed.DemoSettings(), f.loc, f.now); s.Release != want {
		t.Errorf("release = %+v, want %+v", s.Release, want)
	}

	byName := map[string]ScheduledLesson{}
	for i, name := range f.names(s) {
		byName[name] = s.Lessons[i]
	}

	ballet := byName["monBallet"]
	if ballet.Lesson != f.lessons["monBallet"] {
		t.Errorf("lesson = %+v, want %+v", ballet.Lesson, f.lessons["monBallet"])
	}

	if ballet.Level != domain.LevelBeginner || ballet.CourseType.Name != "Classical Ballet" ||
		ballet.Room.Name != "Studio A" || ballet.Location.Name != "Studio North" ||
		ballet.Instructor.DisplayName != "Alina Starling" || ballet.Accompanist.DisplayName != "Felix Keysworth" {
		t.Errorf("resolved references = %+v", ballet)
	}

	if c := byName["wedPointeCanceled"]; c.Lesson.Status != domain.LessonCanceled {
		t.Errorf("canceled lesson status = %q", c.Lesson.Status)
	}

	ws := byName["workshop"]
	if ws.Level != "" || !ws.Accompanist.ID.IsZero() || ws.Location.Name != "Studio North" {
		t.Errorf("workshop = %+v", ws)
	}
}

func TestScheduleValidation(t *testing.T) {
	t.Parallel()

	f := newScheduleFixture(t)
	from := schoolDate(2026, time.October, 19)

	tests := []struct {
		name  string
		query ScheduleQuery
	}{
		{"missing from", ScheduleQuery{To: from}},
		{"missing to", ScheduleQuery{From: from}},
		{"invalid from", ScheduleQuery{From: schoolDate(2026, time.February, 30), To: from}},
		{"to equals from", ScheduleQuery{From: from, To: from}},
		{"to before from", ScheduleQuery{From: from, To: from.AddDays(-1)}},
		{"more than 42 days", ScheduleQuery{From: from, To: from.AddDays(MaxScheduleDays + 1)}},
		{"unknown level", ScheduleQuery{From: from, To: from.AddDays(7), Level: "expert"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := f.svc.Schedule(t.Context(), tt.query); !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}

	if _, err := f.svc.Schedule(t.Context(), ScheduleQuery{From: from, To: from.AddDays(MaxScheduleDays)}); err != nil {
		t.Fatalf("42 days: %v", err)
	}
}

func TestScheduleWithoutSettings(t *testing.T) {
	t.Parallel()

	svc := NewScheduleService(memory.New(), clock.NewFixed(time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)))

	_, err := svc.Schedule(t.Context(), ScheduleQuery{From: schoolDate(2026, time.October, 19), To: schoolDate(2026, time.October, 26)})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestLesson(t *testing.T) {
	t.Parallel()

	f := newScheduleFixture(t)

	for _, name := range []string{"monBallet", "wedPointeCanceled", "workshop"} {
		got, err := f.svc.Lesson(t.Context(), f.lessons[name].ID)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if got.Lesson != f.lessons[name] {
			t.Errorf("%s: lesson = %+v", name, got.Lesson)
		}
	}

	got, err := f.svc.Lesson(t.Context(), f.lessons["monBallet"].ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Level != domain.LevelBeginner || got.Location.Address == "" || got.CourseType.Description == "" ||
		got.Accompanist.DisplayName != "Felix Keysworth" {
		t.Errorf("resolved references = %+v", got)
	}

	unknown, err := domain.ParseID[domain.LessonID]("00000000-0000-7000-8000-00000000ffff")
	if err != nil {
		t.Fatal(err)
	}

	invisible := map[string]domain.LessonID{
		"hidden":         f.lessons["tueHidden"].ID,
		"members":        f.lessons["tueMembers"].ID,
		"unreleased":     f.lessons["monBalletNextWeek"].ID,
		"hidden one-off": f.lessons["hiddenWorkshop"].ID,
		"unknown":        unknown,
	}

	for name, id := range invisible {
		if _, err := f.svc.Lesson(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: want ErrNotFound, got %v", name, err)
		}
	}
}

// TestScheduleLessonOutsideItsTerm checks that a lesson whose class belongs
// to a term outside the requested range still resolves its class (only the
// classes of overlapping terms are preloaded).
func TestScheduleLessonOutsideItsTerm(t *testing.T) {
	t.Parallel()

	st := memory.New()
	seq := ids.NewSequence()
	loc := mustZone(t, "Europe/Berlin")
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, loc)

	location := domain.Location{ID: ids.New[domain.LocationID](seq), Name: "Example Hall"}
	room := domain.Room{ID: ids.New[domain.RoomID](seq), LocationID: location.ID, Name: "Room 1", Capacity: 10}
	staff := domain.Staff{ID: ids.New[domain.StaffID](seq), DisplayName: "Instructor Example", Kind: domain.StaffInstructor, Active: true}
	courseType := domain.CourseType{ID: ids.New[domain.CourseTypeID](seq), Name: "Ballet"}
	term := domain.Term{
		ID: ids.New[domain.TermID](seq), Name: "Winter",
		StartsOn: schoolDate(2026, time.January, 5), EndsOn: schoolDate(2026, time.January, 31),
	}
	class := domain.Class{
		ID: ids.New[domain.ClassID](seq), TermID: term.ID, CourseTypeID: courseType.ID, RoomID: room.ID,
		InstructorID: staff.ID, Level: domain.LevelAdvanced, Weekday: time.Monday,
		StartTime: domain.TimeOfDay{Hour: 18}, Duration: time.Hour, Capacity: 10,
	}
	start := schoolDate(2026, time.October, 19).At(class.StartTime, loc)
	lesson := domain.Lesson{
		ID: ids.New[domain.LessonID](seq), ClassID: class.ID, CourseTypeID: courseType.ID, RoomID: room.ID,
		InstructorID: staff.ID, StartsAt: start, EndsAt: start.Add(time.Hour), Status: domain.LessonScheduled,
		Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 10,
	}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Settings().Put(ctx, seed.DemoSettings()),
			tx.Locations().Create(ctx, location),
			tx.Rooms().Create(ctx, room),
			tx.Staff().Create(ctx, staff),
			tx.CourseTypes().Create(ctx, courseType),
			tx.Terms().Create(ctx, term),
			tx.Classes().Create(ctx, class),
			tx.Lessons().Create(ctx, lesson),
		)
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := NewScheduleService(st, clock.NewFixed(now)).Schedule(t.Context(), ScheduleQuery{
		From: schoolDate(2026, time.October, 19), To: schoolDate(2026, time.October, 26),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Lessons) != 1 || s.Lessons[0].Lesson.ID != lesson.ID || s.Lessons[0].Level != domain.LevelAdvanced {
		t.Fatalf("lessons = %+v, want the lesson with its class level", s.Lessons)
	}
}
