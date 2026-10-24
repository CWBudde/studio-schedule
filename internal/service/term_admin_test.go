package service

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The term tests run at 12:00 on Monday 2026-10-19 in Berlin, the week
// before the end of summer time; the horizon ends on 2026-11-16 (see
// TestClassSync).

// termData is the fixture term with the given dates.
func (f *genFixture) termData(t *testing.T, startsOn, endsOn string) TermData {
	t.Helper()

	return TermData{Name: f.term.Name, StartsOn: mustDate(t, startsOn), EndsOn: mustDate(t, endsOn)}
}

func TestTermAdmin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.timetable(t, "2026-10-19T10:00:00Z")

	t.Run("create", func(t *testing.T) {
		d := f.termData(t, "2027-08-01", "2027-08-31")
		d.Name = "Fictional Summer"

		term, err := svc.CreateTerm(ctx, d)
		if err != nil {
			t.Fatal(err)
		}

		if term.ID.IsZero() || term.Name != d.Name || term.StartsOn != d.StartsOn || term.EndsOn != d.EndsOn {
			t.Fatalf("created %+v", term)
		}

		terms, err := svc.Terms(ctx)
		if err != nil || !slices.Contains(terms, term) {
			t.Fatalf("terms %+v, %v; want the new one", terms, err)
		}
	})

	for field, d := range map[string]TermData{
		"name":   {StartsOn: mustDate(t, "2027-08-01"), EndsOn: mustDate(t, "2027-08-31")},
		"endsOn": {Name: "Backwards", StartsOn: mustDate(t, "2027-08-31"), EndsOn: mustDate(t, "2027-08-01")},
	} {
		t.Run("invalid "+field, func(t *testing.T) {
			_, err := svc.CreateTerm(ctx, d)

			var fe *domain.FieldError
			if !errors.As(err, &fe) || fe.Field != field {
				t.Fatalf("want a validation error on %s, got %v", field, err)
			}
		})
	}

	t.Run("unknown term", func(t *testing.T) {
		_, err := svc.UpdateTerm(ctx, ids.New[domain.TermID](f.seq), f.termData(t, "2026-09-01", "2027-07-31"))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("update: want ErrNotFound, got %v", err)
		}

		if err := svc.DeleteTerm(ctx, ids.New[domain.TermID](f.seq)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("delete: want ErrNotFound, got %v", err)
		}
	})

	t.Run("delete", func(t *testing.T) {
		term, err := svc.CreateTerm(ctx, f.termData(t, "2027-09-01", "2027-09-30"))
		if err != nil {
			t.Fatal(err)
		}

		if err := svc.DeleteTerm(ctx, term.ID); err != nil {
			t.Fatal(err)
		}

		terms, err := svc.Terms(ctx)
		if err != nil || slices.Contains(terms, term) {
			t.Fatalf("terms %+v, %v; want it deleted", terms, err)
		}
	})

	t.Run("delete a term with classes", func(t *testing.T) {
		if _, err := svc.CreateClass(ctx, f.term.ID, f.classData()); err != nil {
			t.Fatal(err)
		}

		if err := svc.DeleteTerm(ctx, f.term.ID); !errors.Is(err, domain.ErrInUse) {
			t.Fatalf("want ErrInUse, got %v", err)
		}
	})
}

func TestTermDates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := "2026-10-19T10:00:00Z"

	// The class has lessons on Tuesdays at 18:00 from 2026-10-20 (summer
	// time) through the horizon.
	setup := func(t *testing.T) (*genFixture, *TimetableService, TimetableClass) {
		t.Helper()

		f := newGenFixture(t, seed.DemoSettings())
		svc := f.timetable(t, now)

		c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
		if err != nil {
			t.Fatal(err)
		}

		return f, svc, c
	}

	all := []string{"2026-10-20T16:00:00Z", "2026-10-27T17:00:00Z", "2026-11-03T17:00:00Z", "2026-11-10T17:00:00Z"}

	t.Run("an earlier end deletes the lessons after it", func(t *testing.T) {
		t.Parallel()

		f, svc, c := setup(t)
		d := f.termData(t, "2026-09-01", "2026-11-01")
		d.Name = "Fictional Autumn"

		term, err := svc.UpdateTerm(ctx, f.term.ID, d)
		if err != nil {
			t.Fatal(err)
		}

		if term.ID != f.term.ID || term.Name != d.Name || term.EndsOn != d.EndsOn {
			t.Fatalf("updated %+v", term)
		}

		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, all[:2]) {
			t.Fatalf("lessons start at %v, want %v", got, all[:2])
		}
	})

	t.Run("a later start deletes the lessons before it", func(t *testing.T) {
		t.Parallel()

		f, svc, c := setup(t)

		if _, err := svc.UpdateTerm(ctx, f.term.ID, f.termData(t, "2026-10-21", "2027-07-31")); err != nil {
			t.Fatal(err)
		}

		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, all[1:]) {
			t.Fatalf("lessons start at %v, want %v", got, all[1:])
		}
	})

	t.Run("a longer term generates the lessons ahead across DST", func(t *testing.T) {
		t.Parallel()

		f, svc, c := setup(t)

		if _, err := svc.UpdateTerm(ctx, f.term.ID, f.termData(t, "2026-09-01", "2026-10-19")); err != nil {
			t.Fatal(err)
		}

		if got := f.classLessons(t, c.ID); len(got) != 0 {
			t.Fatalf("lessons left: %v", lessonStarts(got))
		}

		// From an earlier start too: no lessons in the past.
		if _, err := svc.UpdateTerm(ctx, f.term.ID, f.termData(t, "2026-08-01", "2027-07-31")); err != nil {
			t.Fatal(err)
		}

		if got := lessonStarts(f.classLessons(t, c.ID)); !slices.Equal(got, all) {
			t.Fatalf("lessons start at %v, want %v", got, all)
		}
	})

	for name, tc := range map[string]struct {
		prepare          func(t *testing.T, f *genFixture, c domain.Class)
		startsOn, endsOn string
		field            string
	}{
		"a lesson has started": {
			prepare: func(t *testing.T, f *genFixture, _ domain.Class) {
				t.Helper()

				_, err := generatorAt(f.st, mustInstant(t, now)).Generate(ctx, mustDate(t, "2026-10-13"), mustDate(t, "2026-10-14"))
				if err != nil {
					t.Fatal(err)
				}
			},
			startsOn: "2026-10-14", endsOn: "2027-07-31", field: "startsOn",
		},
		"a lesson is booked": {
			prepare: func(t *testing.T, f *genFixture, c domain.Class) {
				t.Helper()
				f.book(t, f.lessonAt(t, c.ID, "2026-11-10"), 1)
			},
			startsOn: "2026-09-01", endsOn: "2026-11-01", field: "endsOn",
		},
		"a lesson was edited": {
			prepare: func(t *testing.T, f *genFixture, c domain.Class) {
				t.Helper()

				edited := f.lessonAt(t, c.ID, "2026-11-10")
				edited.Capacity = 4
				f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, edited) })
			},
			startsOn: "2026-09-01", endsOn: "2026-11-01", field: "endsOn",
		},
	} {
		t.Run("refused: "+name, func(t *testing.T) {
			t.Parallel()

			f, svc, c := setup(t)
			tc.prepare(t, f, c.Class)
			before := f.classLessons(t, c.ID)

			_, err := svc.UpdateTerm(ctx, f.term.ID, f.termData(t, tc.startsOn, tc.endsOn))

			var fe *domain.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Fatalf("want a validation error on %s, got %v", tc.field, err)
			}

			if after := f.classLessons(t, c.ID); !slices.Equal(after, before) {
				t.Fatalf("lessons changed: %v", lessonStarts(after))
			}

			if terms, err := svc.Terms(ctx); err != nil || !slices.Equal(terms, []domain.Term{f.term}) {
				t.Fatalf("terms %+v, %v; want them unchanged", terms, err)
			}
		})
	}
}
