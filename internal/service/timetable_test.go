package service_test

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestTimetableTerms(t *testing.T) {
	t.Parallel()

	got, err := service.NewTimetableService(memory.New(), clock.NewFixed(time.Time{}), ids.NewSequence()).Terms(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("empty store: Terms() = %v, %v; want no terms", got, err)
	}

	st := catalogDemoStore(t)
	demo := seed.Demo()

	// A second, earlier term must come first.
	earlier := domain.Term{
		ID:       mustParseID[domain.TermID](t, unknownID),
		Name:     "Summer Workshop Weeks",
		StartsOn: demo.Terms[0].StartsOn.AddDays(-60),
		EndsOn:   demo.Terms[0].StartsOn.AddDays(-30),
	}

	err = st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Terms().Create(ctx, earlier)
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err = service.NewTimetableService(st, clock.NewFixed(time.Time{}), ids.NewSequence()).Terms(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if want := []domain.Term{earlier, demo.Terms[0]}; !slices.Equal(got, want) {
		t.Fatalf("Terms() = %+v, want %+v", got, want)
	}
}

func TestTimetableClasses(t *testing.T) {
	t.Parallel()

	st := catalogDemoStore(t)
	svc := service.NewTimetableService(st, clock.NewFixed(time.Time{}), ids.NewSequence())
	demo := seed.Demo()

	t.Run("demo term", func(t *testing.T) {
		t.Parallel()

		got, err := svc.Classes(t.Context(), demo.Terms[0].ID)
		if err != nil {
			t.Fatal(err)
		}

		want := slices.SortedFunc(slices.Values(demo.Classes), func(a, b domain.Class) int {
			return cmp.Or(cmp.Compare(a.Weekday, b.Weekday), a.StartTime.Compare(b.StartTime),
				slices.Compare(a.ID[:], b.ID[:]))
		})
		classes := make([]domain.Class, 0, len(got))
		for _, c := range got {
			classes = append(classes, c.Class)

			// Staff are named from the store, active or not.
			if c.Instructor.ID != c.InstructorID || c.Instructor.DisplayName == "" || c.Accompanist.ID != c.AccompanistID {
				t.Errorf("class %s has staff %+v / %+v", c.ID, c.Instructor, c.Accompanist)
			}
		}

		if len(want) == 0 || !slices.Equal(classes, want) {
			t.Fatalf("Classes() = %+v, want %+v", classes, want)
		}
	})

	t.Run("unknown term", func(t *testing.T) {
		t.Parallel()

		_, err := svc.Classes(t.Context(), mustParseID[domain.TermID](t, unknownID))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("zero term", func(t *testing.T) {
		t.Parallel()

		if _, err := svc.Classes(t.Context(), domain.TermID{}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

func TestTimetableClassesTermWithoutClasses(t *testing.T) {
	t.Parallel()

	st := memory.New()
	term := domain.Term{
		ID:       mustParseID[domain.TermID](t, unknownID),
		Name:     "Empty Term",
		StartsOn: domain.Date{Year: 2027, Month: time.August, Day: 1},
		EndsOn:   domain.Date{Year: 2027, Month: time.August, Day: 31},
	}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Terms().Create(ctx, term)
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.NewTimetableService(st, clock.NewFixed(time.Time{}), ids.NewSequence()).Classes(t.Context(), term.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("Classes() = %v, %v; want no classes", got, err)
	}
}
