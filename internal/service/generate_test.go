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

func berlin(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	return loc
}

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()

	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func mustInstant(t *testing.T, s string) time.Time {
	t.Helper()

	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}

	return ts
}

// starts returns the start instants of slots in UTC, as RFC 3339 strings.
func starts(slots []slot) []string {
	out := make([]string, len(slots))
	for i, s := range slots {
		out[i] = s.Start.UTC().Format(time.RFC3339)
	}

	return out
}

func TestClassSlots(t *testing.T) {
	t.Parallel()

	loc := berlin(t)
	schoolYear := domain.Term{StartsOn: mustDate(t, "2026-09-01"), EndsOn: mustDate(t, "2027-07-31")}
	// 2026-09-01 and 2026-09-15 are Tuesdays.
	shortTerm := domain.Term{StartsOn: mustDate(t, "2026-09-01"), EndsOn: mustDate(t, "2026-09-15")}

	tests := []struct {
		name     string
		weekday  time.Weekday
		start    domain.TimeOfDay
		term     domain.Term
		from, to string
		want     []string
	}{
		{
			// Clocks go back on Sunday 2026-10-25: 18:00 is CEST before, CET after.
			name: "fall back keeps the wall clock", weekday: time.Sunday, start: domain.TimeOfDay{Hour: 18},
			term: schoolYear, from: "2026-10-18", to: "2026-10-26",
			want: []string{"2026-10-18T16:00:00Z", "2026-10-25T17:00:00Z"},
		},
		{
			// Clocks go forward on Sunday 2027-03-28.
			name: "spring forward keeps the wall clock", weekday: time.Sunday, start: domain.TimeOfDay{Hour: 18},
			term: schoolYear, from: "2027-03-21", to: "2027-03-29",
			want: []string{"2027-03-21T17:00:00Z", "2027-03-28T16:00:00Z"},
		},
		{
			// 02:30 happens twice on 2026-10-25: the earlier (CEST) instant wins.
			name: "ambiguous wall clock resolves to the earlier instant", weekday: time.Sunday,
			start: domain.TimeOfDay{Hour: 2, Minute: 30}, term: schoolYear, from: "2026-10-25", to: "2026-10-26",
			want: []string{"2026-10-25T00:30:00Z"},
		},
		{
			// 02:30 does not exist on 2027-03-28: it moves forward to 03:30 CEST.
			name: "missing wall clock moves forward by the gap", weekday: time.Sunday,
			start: domain.TimeOfDay{Hour: 2, Minute: 30}, term: schoolYear, from: "2027-03-28", to: "2027-03-29",
			want: []string{"2027-03-28T01:30:00Z"},
		},
		{
			name: "term bounds are inclusive", weekday: time.Tuesday, start: domain.TimeOfDay{Hour: 10},
			term: shortTerm, from: "2026-08-18", to: "2026-09-30",
			want: []string{"2026-09-01T08:00:00Z", "2026-09-08T08:00:00Z", "2026-09-15T08:00:00Z"},
		},
		{
			name: "range start is inclusive, range end exclusive", weekday: time.Sunday,
			start: domain.TimeOfDay{Hour: 18}, term: schoolYear, from: "2026-10-18", to: "2026-10-25",
			want: []string{"2026-10-18T16:00:00Z"},
		},
		{
			name: "empty range", weekday: time.Sunday, start: domain.TimeOfDay{Hour: 18},
			term: schoolYear, from: "2026-10-25", to: "2026-10-25",
			want: []string{},
		},
		{
			name: "range outside the term", weekday: time.Tuesday, start: domain.TimeOfDay{Hour: 10},
			term: shortTerm, from: "2026-09-16", to: "2026-10-30",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := domain.Class{Weekday: tt.weekday, StartTime: tt.start, Duration: 90 * time.Minute}
			got := classSlots(c, tt.term, mustDate(t, tt.from), mustDate(t, tt.to), loc)

			if !slices.Equal(starts(got), tt.want) {
				t.Fatalf("starts = %v, want %v", starts(got), tt.want)
			}

			for _, s := range got {
				if d := s.End.Sub(s.Start); d != c.Duration {
					t.Errorf("slot %s lasts %s, want %s", s.Start, d, c.Duration)
				}
			}
		})
	}
}

// genFixture is a memory store with fictional settings in Europe/Berlin, one
// room, two instructors, a course type and a school-year term.
type genFixture struct {
	st         *memory.Store
	seq        *ids.Sequence
	term       domain.Term
	room       domain.RoomID
	courseType domain.CourseTypeID
	instructor domain.StaffID
	pianist    domain.StaffID
}

func newGenFixture(t *testing.T, settings domain.Settings) *genFixture {
	t.Helper()

	seq := ids.NewSequence()
	f := &genFixture{
		st:  memory.New(),
		seq: seq,
		term: domain.Term{
			ID:       ids.New[domain.TermID](seq),
			Name:     "Fictional Year",
			StartsOn: mustDate(t, "2026-09-01"),
			EndsOn:   mustDate(t, "2027-07-31"),
		},
		room:       ids.New[domain.RoomID](seq),
		courseType: ids.New[domain.CourseTypeID](seq),
		instructor: ids.New[domain.StaffID](seq),
		pianist:    ids.New[domain.StaffID](seq),
	}
	location := ids.New[domain.LocationID](seq)

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Settings().Put(ctx, settings),
			tx.Locations().Create(ctx, domain.Location{ID: location, Name: "Studio Nowhere"}),
			tx.Rooms().Create(ctx, domain.Room{ID: f.room, LocationID: location, Name: "Room One", Capacity: 12}),
			tx.CourseTypes().Create(ctx, domain.CourseType{ID: f.courseType, Name: "Imaginary Ballet"}),
			tx.Staff().Create(ctx, domain.Staff{
				ID: f.instructor, DisplayName: "Ada Example", Kind: domain.StaffInstructor, Active: true,
			}),
			tx.Staff().Create(ctx, domain.Staff{
				ID: f.pianist, DisplayName: "Ben Example", Kind: domain.StaffAccompanist, Active: true,
			}),
			tx.Terms().Create(ctx, f.term),
		)
	})

	return f
}

func (f *genFixture) write(t *testing.T, fn func(ctx context.Context, tx store.Tx) error) {
	t.Helper()

	if err := f.st.InTx(t.Context(), store.ReadWrite, fn); err != nil {
		t.Fatal(err)
	}
}

// addClass stores a weekly class of the fixture term.
func (f *genFixture) addClass(t *testing.T, weekday time.Weekday, start domain.TimeOfDay) domain.Class {
	t.Helper()

	c := domain.Class{
		ID:            ids.New[domain.ClassID](f.seq),
		TermID:        f.term.ID,
		CourseTypeID:  f.courseType,
		RoomID:        f.room,
		InstructorID:  f.instructor,
		AccompanistID: f.pianist,
		Level:         domain.LevelBeginner,
		Weekday:       weekday,
		StartTime:     start,
		Duration:      75 * time.Minute,
		Capacity:      9,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Classes().Create(ctx, c) })

	return c
}

// lessons returns all lessons between 2026 and 2028.
func (f *genFixture) lessons(t *testing.T) []domain.Lesson {
	t.Helper()

	var out []domain.Lesson

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		out, err = tx.Lessons().ListRange(ctx,
			time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2028, time.January, 1, 0, 0, 0, 0, time.UTC))

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func lessonStarts(ls []domain.Lesson) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.StartsAt.UTC().Format(time.RFC3339)
	}

	return out
}

func generatorAt(st store.Store, now time.Time) *LessonGenerator {
	return NewLessonGenerator(st, clock.NewFixed(now), ids.NewSequence())
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	c := f.addClass(t, time.Sunday, domain.TimeOfDay{Hour: 18})
	gen := NewLessonGenerator(f.st, clock.NewFixed(mustInstant(t, "2026-10-23T10:00:00Z")), f.seq)
	from, to := mustDate(t, "2026-10-18"), mustDate(t, "2026-10-26")

	created, err := gen.Generate(t.Context(), from, to)
	if err != nil {
		t.Fatal(err)
	}

	if created != 2 {
		t.Fatalf("created = %d, want 2", created)
	}

	got := f.lessons(t)
	if want := []string{"2026-10-18T16:00:00Z", "2026-10-25T17:00:00Z"}; !slices.Equal(lessonStarts(got), want) {
		t.Fatalf("lessons start at %v, want %v", lessonStarts(got), want)
	}

	for _, l := range got {
		want := domain.Lesson{
			ID:            l.ID,
			ClassID:       c.ID,
			SlotDate:      domain.DateOf(l.StartsAt, berlin(t)),
			CourseTypeID:  c.CourseTypeID,
			RoomID:        c.RoomID,
			InstructorID:  c.InstructorID,
			AccompanistID: c.AccompanistID,
			StartsAt:      l.StartsAt,
			EndsAt:        l.StartsAt.Add(c.Duration),
			Status:        domain.LessonScheduled,
			Visibility:    domain.VisibilityPublic,
			Kind:          domain.LessonRegular,
			Capacity:      c.Capacity,
		}
		if l != want || l.ID.IsZero() {
			t.Errorf("lesson = %+v, want %+v", l, want)
		}
	}

	// A second run over an overlapping range creates only what is missing.
	created, err = gen.Generate(t.Context(), from, to)
	if err != nil || created != 0 {
		t.Fatalf("second run: created %d, err %v; want 0, nil", created, err)
	}

	created, err = gen.Generate(t.Context(), from, mustDate(t, "2026-11-02"))
	if err != nil || created != 1 {
		t.Fatalf("extended run: created %d, err %v; want 1, nil", created, err)
	}

	if n := len(f.lessons(t)); n != 3 {
		t.Fatalf("%d lessons after three runs, want 3", n)
	}
}

func TestGenerateKeepsExistingLessons(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	c := f.addClass(t, time.Sunday, domain.TimeOfDay{Hour: 18})

	// The lesson of 2026-10-25 exists already and was canceled.
	existing := domain.Lesson{
		ID:           ids.New[domain.LessonID](f.seq),
		ClassID:      c.ID,
		SlotDate:     mustDate(t, "2026-10-25"),
		CourseTypeID: c.CourseTypeID,
		RoomID:       c.RoomID,
		InstructorID: c.InstructorID,
		StartsAt:     mustInstant(t, "2026-10-25T17:00:00Z"),
		EndsAt:       mustInstant(t, "2026-10-25T18:00:00Z"),
		Status:       domain.LessonCanceled,
		Visibility:   domain.VisibilityMembers,
		Kind:         domain.LessonRegular,
		Capacity:     3,
	}
	// A one-off workshop at the start of the 2026-10-18 lesson.
	oneOff := domain.Lesson{
		ID:           ids.New[domain.LessonID](f.seq),
		CourseTypeID: c.CourseTypeID,
		RoomID:       c.RoomID,
		InstructorID: f.pianist,
		StartsAt:     mustInstant(t, "2026-10-18T16:00:00Z"),
		EndsAt:       mustInstant(t, "2026-10-18T19:00:00Z"),
		Status:       domain.LessonScheduled,
		Visibility:   domain.VisibilityPublic,
		Kind:         domain.LessonWorkshop,
		Capacity:     20,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Lessons().Create(ctx, existing), tx.Lessons().Create(ctx, oneOff))
	})

	created, err := generatorAt(f.st, mustInstant(t, "2026-10-23T10:00:00Z")).
		Generate(t.Context(), mustDate(t, "2026-10-18"), mustDate(t, "2026-10-26"))
	if err != nil {
		t.Fatal(err)
	}

	if created != 1 {
		t.Fatalf("created = %d, want 1", created)
	}

	got := f.lessons(t)
	if len(got) != 3 {
		t.Fatalf("lessons = %+v, want the one-off, one generated and the existing one", got)
	}

	var generated int

	for _, l := range got {
		switch l.ID {
		case existing.ID:
			if l != existing {
				t.Errorf("existing lesson changed: %+v, want %+v", l, existing)
			}
		case oneOff.ID:
			if l != oneOff {
				t.Errorf("one-off lesson changed: %+v, want %+v", l, oneOff)
			}
		default:
			generated++

			if l.ClassID != c.ID || !l.StartsAt.Equal(mustInstant(t, "2026-10-18T16:00:00Z")) {
				t.Errorf("unexpected generated lesson %+v", l)
			}
		}
	}

	if generated != 1 {
		t.Fatalf("%d generated lessons, want 1", generated)
	}
}

// A lesson moved away from its slot keeps the slot: generation does not
// create the slot again, also when the lesson moved out of the range.
func TestGenerateSkipsMovedLessons(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	c := f.addClass(t, time.Sunday, domain.TimeOfDay{Hour: 18})

	// The Sunday lesson of 2026-10-25 moved to the Saturday before.
	moved := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: mustDate(t, "2026-10-25"),
		CourseTypeID: c.CourseTypeID, RoomID: c.RoomID, InstructorID: c.InstructorID,
		StartsAt: mustInstant(t, "2026-10-24T16:00:00Z"), EndsAt: mustInstant(t, "2026-10-24T17:15:00Z"),
		Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: c.Capacity,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, moved) })

	created, err := generatorAt(f.st, mustInstant(t, "2026-10-23T10:00:00Z")).
		Generate(t.Context(), mustDate(t, "2026-10-25"), mustDate(t, "2026-11-02"))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"2026-10-24T16:00:00Z", "2026-11-01T17:00:00Z"}
	if got := lessonStarts(f.lessons(t)); created != 1 || !slices.Equal(got, want) {
		t.Fatalf("created %d, lessons start at %v; want 1 and %v", created, got, want)
	}
}

// GenerateAhead does not create lessons that have started: a class created
// today has no history.
func TestGenerateAheadSkipsThePast(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	f.addClass(t, time.Friday, domain.TimeOfDay{Hour: 19})
	f.addClass(t, time.Friday, domain.TimeOfDay{Hour: 21})

	// 20:00 on Friday 2026-10-23 in Berlin.
	if _, err := generatorAt(f.st, mustInstant(t, "2026-10-23T18:00:00Z")).GenerateAhead(t.Context()); err != nil {
		t.Fatal(err)
	}

	got := lessonStarts(f.lessons(t))
	if len(got) < 2 || got[0] != "2026-10-23T19:00:00Z" || got[1] != "2026-10-30T18:00:00Z" {
		t.Fatalf("lessons start at %v, want the 21:00 lesson of today first, then next Friday's (after DST)", got)
	}
}

func TestGenerateRespectsTermBounds(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	// 2026-09-01 is the first day of the term, 2027-07-31 the last; both are
	// covered by this Tuesday and Saturday class respectively.
	f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 17})
	f.addClass(t, time.Saturday, domain.TimeOfDay{Hour: 9, Minute: 30})
	gen := generatorAt(f.st, mustInstant(t, "2026-10-23T10:00:00Z"))

	for _, r := range [][2]string{{"2026-08-20", "2026-09-06"}, {"2027-07-25", "2027-08-10"}} {
		if _, err := gen.Generate(t.Context(), mustDate(t, r[0]), mustDate(t, r[1])); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{
		"2026-09-01T15:00:00Z", // Tuesday, first day of the term
		"2026-09-05T07:30:00Z", // Saturday
		"2027-07-27T15:00:00Z", // Tuesday
		"2027-07-31T07:30:00Z", // Saturday, last day of the term
	}
	if got := lessonStarts(f.lessons(t)); !slices.Equal(got, want) {
		t.Fatalf("lessons start at %v, want %v", got, want)
	}
}

// retryingStore runs every transaction callback twice, like the PostgreSQL
// store after a serialization failure: the first attempt is rolled back.
type retryingStore struct{ inner store.Store }

var errForcedRetry = errors.New("forced retry")

func (s retryingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	err := s.inner.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}

		return errForcedRetry
	})
	if !errors.Is(err, errForcedRetry) {
		return err
	}

	return s.inner.InTx(ctx, mode, fn)
}

func TestGenerateCountsOnlyTheCommittedAttempt(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	f.addClass(t, time.Sunday, domain.TimeOfDay{Hour: 18})

	created, err := generatorAt(retryingStore{f.st}, mustInstant(t, "2026-10-23T10:00:00Z")).
		Generate(t.Context(), mustDate(t, "2026-10-18"), mustDate(t, "2026-10-26"))
	if err != nil {
		t.Fatal(err)
	}

	if n := len(f.lessons(t)); created != 2 || n != 2 {
		t.Fatalf("created = %d with %d lessons stored, want 2 and 2", created, n)
	}
}

func TestGenerateAhead(t *testing.T) {
	t.Parallel()

	// 00:30 on Friday 2026-10-23 in Berlin, still Thursday in UTC: "today"
	// is the school date, so the horizon ends a week later than in UTC.
	now := mustInstant(t, "2026-10-22T22:30:00Z")

	tests := []struct {
		name        string
		weeksAhead  int
		visibleDays int
		accountDays int    // an account's visible-days override; 0 = none
		through     string // the manual released-through date; "" = none
		first, last string // first and last Friday lesson, school dates
	}{
		// max(7·(1+2), 14) + 7 = 28 days: [10-23, 11-21).
		{"release window dominates", 1, 14, 0, "", "2026-10-23", "2026-11-20"},
		// max(7·(1+2), 30) + 7 = 37 days: [10-23, 11-30).
		{"visible days dominate", 1, 30, 0, "", "2026-10-23", "2026-11-27"},
		// max(7·(3+2), 14) + 7 = 42 days: [10-23, 12-05).
		{"more weeks ahead", 3, 14, 0, "", "2026-10-23", "2026-12-04"},
		// max(7·(1+2), 45) + 7 = 52 days: [10-23, 12-15).
		{"an account's early access dominates", 1, 14, 45, "", "2026-10-23", "2026-12-11"},
		// A shorter override does not shorten the horizon.
		{"a shorter override", 1, 30, 5, "", "2026-10-23", "2026-11-27"},
		// The released-through date plus 7 days: [10-23, 2027-01-23).
		{"a released-through date dominates", 1, 14, 0, "2027-01-15", "2026-10-23", "2027-01-22"},
		// A date inside the horizon, or in the past, changes nothing.
		{"an earlier released-through date", 1, 14, 0, "2026-11-06", "2026-10-23", "2026-11-20"},
		{"a past released-through date", 1, 14, 0, "2026-10-01", "2026-10-23", "2026-11-20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := seed.DemoSettings()
			settings.ReleaseWeeksAhead = tt.weeksAhead
			settings.DefaultVisibleDays = tt.visibleDays

			if tt.through != "" {
				settings.ReleasedThrough = mustDate(t, tt.through)
			}

			f := newGenFixture(t, settings)
			f.addClass(t, time.Friday, domain.TimeOfDay{Hour: 19})

			if tt.accountDays > 0 {
				person := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Ada", FamilyName: "Example"}
				f.write(t, func(ctx context.Context, tx store.Tx) error {
					return errors.Join(
						tx.Persons().Create(ctx, person),
						tx.Accounts().Create(ctx, domain.Account{
							ID: ids.New[domain.AccountID](f.seq), PersonID: person.ID, Email: "ada@example.org",
							Role: domain.RoleMember, VisibleDays: tt.accountDays,
						}),
					)
				})
			}

			gen := generatorAt(f.st, now)

			created, err := gen.GenerateAhead(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			got := f.lessons(t)
			if created != len(got) || len(got) == 0 {
				t.Fatalf("created = %d, stored %d", created, len(got))
			}

			loc := berlin(t)
			first := domain.DateOf(got[0].StartsAt, loc).String()
			last := domain.DateOf(got[len(got)-1].StartsAt, loc).String()

			if first != tt.first || last != tt.last {
				t.Fatalf("lessons from %s to %s, want %s to %s", first, last, tt.first, tt.last)
			}

			if again, err := gen.GenerateAhead(t.Context()); err != nil || again != 0 {
				t.Fatalf("second run: created %d, err %v; want 0, nil", again, err)
			}
		})
	}
}

func TestGenerateAheadWithoutSettings(t *testing.T) {
	t.Parallel()

	_, err := generatorAt(memory.New(), mustInstant(t, "2026-10-23T10:00:00Z")).GenerateAhead(t.Context())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestGenerateDemoData(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	// One full week (Monday to Sunday) inside the demo term: every weekly
	// class has exactly one lesson.
	created, err := generatorAt(st, mustInstant(t, "2026-10-23T10:00:00Z")).
		Generate(t.Context(), mustDate(t, "2026-10-19"), mustDate(t, "2026-10-26"))
	if err != nil {
		t.Fatal(err)
	}

	if want := len(seed.Demo().Classes); created != want || created == 0 {
		t.Fatalf("created = %d, want one lesson per demo class (%d)", created, want)
	}
}
