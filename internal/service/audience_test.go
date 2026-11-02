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
)

// scale returns three levels A, B and C at the positions 1, 2 and 3.
func scale(seq *ids.Sequence) (a, b, c domain.Level) {
	a = domain.Level{ID: ids.New[domain.LevelID](seq), Name: "A", Code: "A", Position: 1}
	b = domain.Level{ID: ids.New[domain.LevelID](seq), Name: "B", Code: "B", Position: 2}
	c = domain.Level{ID: ids.New[domain.LevelID](seq), Name: "C", Code: "C", Position: 3}

	return a, b, c
}

func span(from, to domain.Level) domain.LevelRange {
	return domain.LevelRange{From: from.ID, To: to.ID}
}

func TestAudienceOf(t *testing.T) {
	t.Parallel()

	seq := ids.NewSequence()
	a, b, c := scale(seq)
	levels := levelIndex([]domain.Level{c, a, b})

	t.Run("ends ordered by position whichever way stored", func(t *testing.T) {
		t.Parallel()

		for _, r := range []domain.LevelRange{span(a, c), span(c, a)} {
			got, err := audienceOf(domain.Class{Levels: r, Stage: 2, AgeFrom: 48, AgeTo: 72}, levels)
			if err != nil {
				t.Fatal(err)
			}

			want := Audience{Low: a, High: c, Stage: 2, AgeFrom: 48, AgeTo: 72}
			if got != want {
				t.Errorf("audienceOf(%v) = %+v, want %+v", r, got, want)
			}
		}
	})

	t.Run("equal positions order by ID", func(t *testing.T) {
		t.Parallel()

		seq := ids.NewSequence()
		first, second := ids.New[domain.LevelID](seq), ids.New[domain.LevelID](seq)
		p := domain.Level{ID: second, Name: "P", Position: 2}
		q := domain.Level{ID: first, Name: "Q", Position: 2}

		got, err := audienceOf(domain.Class{Levels: span(p, q)}, levelIndex([]domain.Level{p, q}))
		if err != nil || got.Low != q || got.High != p {
			t.Fatalf("audienceOf = %+v, %v, want Q then P", got, err)
		}
	})

	t.Run("one level", func(t *testing.T) {
		t.Parallel()

		got, err := audienceOf(domain.Class{Levels: span(b, b)}, levels)
		if err != nil || got.Low != b || got.High != b {
			t.Fatalf("audienceOf = %+v, %v", got, err)
		}
	})

	t.Run("no level", func(t *testing.T) {
		t.Parallel()

		got, err := audienceOf(domain.Class{AllLevels: true, AgeTo: 96}, nil)
		if err != nil || got != (Audience{AllLevels: true, AgeTo: 96}) {
			t.Fatalf("audienceOf = %+v, %v", got, err)
		}
	})

	unknown := domain.Level{ID: ids.New[domain.LevelID](seq)}
	for name, r := range map[string]domain.LevelRange{"from": span(unknown, a), "to": span(a, unknown)} {
		t.Run("unknown "+name, func(t *testing.T) {
			t.Parallel()

			_, err := audienceOf(domain.Class{Levels: r}, levels)
			if !errors.Is(err, errBrokenReference) {
				t.Fatalf("want errBrokenReference, got %v", err)
			}
		})
	}
}

func TestAudienceCovers(t *testing.T) {
	t.Parallel()

	a, b, c := scale(ids.NewSequence())
	tests := []struct {
		name     string
		audience Audience
		level    domain.Level
		want     bool
	}{
		{"inside", Audience{Low: a, High: c}, b, true},
		{"lower end", Audience{Low: a, High: b}, a, true},
		{"upper end", Audience{Low: a, High: b}, b, true},
		{"above", Audience{Low: a, High: b}, c, false},
		{"below", Audience{Low: b, High: c}, a, false},
		{"one level", Audience{Low: b, High: b}, b, true},
		{"next to one level", Audience{Low: b, High: b}, c, false},
		{"all levels", Audience{AllLevels: true}, c, true},
		{"no level", Audience{Stage: 1, AgeFrom: 48}, a, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.audience.Covers(tt.level); got != tt.want {
				t.Fatalf("Covers(%s) = %v, want %v", tt.level.Name, got, tt.want)
			}
		})
	}
}

// audienceFixture is the class fixture with the levels A, B and C stored.
type audienceFixture struct {
	*genFixture

	a, b, c domain.Level
}

func newAudienceFixture(t *testing.T) *audienceFixture {
	t.Helper()

	f := &audienceFixture{genFixture: newGenFixture(t, seed.DemoSettings())}
	f.a, f.b, f.c = scale(f.seq)
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Levels().Create(ctx, f.a), tx.Levels().Create(ctx, f.b), tx.Levels().Create(ctx, f.c))
	})

	return f
}

// levels reads the levels back from the store, by ID.
func (f *audienceFixture) levels(t *testing.T) map[domain.LevelID]domain.Level {
	t.Helper()

	var levels []domain.Level

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		levels, err = tx.Levels().List(ctx)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return levelIndex(levels)
}

// TestAudienceAfterReorder (review focus 2): a class's span stays valid when
// the scale is reordered. It is read lower level first, and a level moved
// between its ends belongs to it.
func TestAudienceAfterReorder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAudienceFixture(t)
	svc := f.timetable(t, "2026-10-21T10:00:00Z")

	d := f.classData()
	d.AllLevels, d.Levels = false, span(f.b, f.a) // stored the other way round

	created, err := svc.CreateClass(ctx, f.term.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	if created.Audience.Low != f.a || created.Audience.High != f.b || created.Audience.Covers(f.c) {
		t.Fatalf("created audience = %+v", created.Audience)
	}

	reorder := func(order ...domain.Level) (Audience, map[domain.LevelID]domain.Level) {
		t.Helper()

		f.write(t, func(ctx context.Context, tx store.Tx) error {
			ids := make([]domain.LevelID, len(order))
			for i, l := range order {
				ids[i] = l.ID
			}

			return tx.Levels().SetOrder(ctx, ids)
		})

		classes, err := svc.AllClasses(ctx, f.term.ID)
		if err != nil || len(classes) != 1 {
			t.Fatalf("classes = %+v, %v", classes, err)
		}

		return classes[0].Audience, f.levels(t)
	}

	// C moves between A and B: the span now includes it.
	got, levels := reorder(f.a, f.c, f.b)
	if got.Low.ID != f.a.ID || got.High.ID != f.b.ID || got.High.Position != 3 {
		t.Fatalf("after A, C, B: audience = %+v", got)
	}

	if !got.Covers(levels[f.c.ID]) || !got.Covers(levels[f.a.ID]) || !got.Covers(levels[f.b.ID]) {
		t.Fatalf("after A, C, B: %+v does not cover all of %+v", got, levels)
	}

	// B before A: the span turns round and is still read lower level first,
	// with C in between.
	got, levels = reorder(f.b, f.c, f.a)
	if got.Low.ID != f.b.ID || got.High.ID != f.a.ID || !got.Covers(levels[f.c.ID]) {
		t.Fatalf("after B, C, A: audience = %+v", got)
	}

	// C after both ends: no longer covered.
	got, levels = reorder(f.b, f.a, f.c)
	if got.Low.ID != f.b.ID || got.High.ID != f.a.ID || got.Covers(levels[f.c.ID]) {
		t.Fatalf("after B, A, C: audience = %+v", got)
	}
}

// TestClassAudienceWrites: a class's audience is written and read back with
// its levels; an unknown level is a validation error on its field.
func TestClassAudienceWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAudienceFixture(t)
	svc := f.timetable(t, "2026-10-21T10:00:00Z")

	d := f.classData()
	d.AllLevels, d.Levels, d.Stage, d.AgeFrom, d.AgeTo = false, span(f.c, f.a), 3, 96, 144

	created, err := svc.CreateClass(ctx, f.term.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	want := Audience{Low: f.a, High: f.c, Stage: 3, AgeFrom: 96, AgeTo: 144}
	if created.Audience != want || created.Levels != d.Levels {
		t.Fatalf("created %+v with %+v, want %+v", created.Audience, created.Levels, want)
	}

	d.Levels, d.Stage, d.AgeFrom = domain.LevelRange{}, 0, 0
	d.AllLevels = true

	updated, err := svc.UpdateClass(ctx, created.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	if want := (Audience{AllLevels: true, AgeTo: 144}); updated.Audience != want {
		t.Fatalf("updated %+v, want %+v", updated.Audience, want)
	}

	all, err := svc.AllClasses(ctx, f.term.ID)
	if err != nil || len(all) != 1 || all[0].Audience != updated.Audience || all[0].Class != updated.Class {
		t.Fatalf("classes = %+v, %v", all, err)
	}

	unknown := domain.Level{ID: ids.New[domain.LevelID](f.seq)}
	for field, r := range map[string]domain.LevelRange{
		"levelFromId": span(unknown, f.a),
		"levelToId":   span(f.a, unknown),
	} {
		t.Run("unknown "+field, func(t *testing.T) {
			bad := f.classData()
			bad.AllLevels, bad.Levels = false, r

			_, err := svc.CreateClass(ctx, f.term.ID, bad)
			wantFieldError(t, err, field)

			_, err = svc.UpdateClass(ctx, created.ID, bad)
			wantFieldError(t, err, field)
		})
	}

	t.Run("refused", func(t *testing.T) {
		for field, edit := range map[string]func(*ClassData){
			"allLevels": func(d *ClassData) { d.Levels = span(f.a, f.b) },
			"levelToId": func(d *ClassData) { d.AllLevels, d.Levels = false, domain.LevelRange{From: f.a.ID} },
			"stage":     func(d *ClassData) { d.Stage = domain.MaxStage + 1 },
		} {
			bad := f.classData()
			edit(&bad)

			_, err := svc.CreateClass(ctx, f.term.ID, bad)
			wantFieldError(t, err, field)
		}
	})

	if all, err := svc.AllClasses(ctx, f.term.ID); err != nil || len(all) != 1 || all[0].Audience != updated.Audience {
		t.Fatalf("refused writes changed the classes: %+v, %v", all, err)
	}
}

// TestScheduleLevelFilter (review focus 3): a level filter on a level
// strictly between a class's ends finds the class, and an all-levels class;
// classes on one side of it, a class without a level and a workshop do not.
// An unknown level matches nothing.
func TestScheduleLevelFilter(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAudienceFixture(t)
	now := "2026-10-21T10:00:00Z"
	svc := f.timetable(t, now)

	classes := map[domain.ClassID]string{}

	for i, c := range []struct {
		name string
		edit func(*ClassData)
	}{
		{"A–C", func(d *ClassData) { d.AllLevels, d.Levels = false, span(f.c, f.a) }},
		{"A–A", func(d *ClassData) { d.AllLevels, d.Levels = false, span(f.a, f.a) }},
		{"C–C", func(d *ClassData) { d.AllLevels, d.Levels = false, span(f.c, f.c) }},
		{"all", func(*ClassData) {}},
		{"none", func(d *ClassData) { d.AllLevels, d.AgeFrom, d.AgeTo = false, 48, 72 }},
	} {
		d := f.classData()
		d.StartTime = domain.TimeOfDay{Hour: 9 + i}
		c.edit(&d)

		created, err := svc.CreateClass(ctx, f.term.ID, d)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}

		classes[created.ID] = c.name
	}

	start := mustInstant(t, "2026-10-28T15:00:00Z")
	workshop := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor,
		StartsAt: start, EndsAt: start.Add(time.Hour), Status: domain.LessonScheduled,
		Visibility: domain.VisibilityPublic, Kind: domain.LessonWorkshop, Capacity: 10,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, workshop) })

	sched := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, now)))
	office := Principal{AccountID: ids.New[domain.AccountID](f.seq), Role: domain.RoleOffice}
	week := ScheduleQuery{From: mustDate(t, "2026-10-26"), To: mustDate(t, "2026-11-02")}

	names := func(level domain.LevelID) []string {
		t.Helper()

		q := week
		q.Level = level

		s, err := sched.Schedule(ctx, office, q)
		if err != nil {
			t.Fatal(err)
		}

		var out []string

		for _, sl := range s.Lessons {
			if sl.Lesson.ID == workshop.ID {
				out = append(out, "workshop")
			} else {
				out = append(out, classes[sl.Lesson.ClassID])
			}
		}

		return out
	}

	if got, want := names(domain.LevelID{}), []string{"A–C", "A–A", "C–C", "all", "none", "workshop"}; !slices.Equal(got, want) {
		t.Fatalf("unfiltered = %v, want %v", got, want)
	}

	for _, tt := range []struct {
		level domain.LevelID
		want  []string
	}{
		{f.b.ID, []string{"A–C", "all"}},
		{f.a.ID, []string{"A–C", "A–A", "all"}},
		{f.c.ID, []string{"A–C", "C–C", "all"}},
		{ids.New[domain.LevelID](f.seq), nil},
	} {
		if got := names(tt.level); !slices.Equal(got, tt.want) {
			t.Errorf("level %v: %v, want %v", tt.level, got, tt.want)
		}
	}
}
