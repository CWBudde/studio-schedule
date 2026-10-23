package service_test

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// unknownID is a well-formed ID that the demo data set does not use.
const unknownID = "ffffffff-ffff-7fff-bfff-ffffffffffff"

// catalogDemoStore returns a memory store loaded with the demo data set.
func catalogDemoStore(t *testing.T) store.Store {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	return st
}

// sortedByName orders items like the store lists: by name, then by the
// bytes of the ID.
func sortedByName[T any](items []T, name func(T) string, id func(T) [16]byte) []T {
	return slices.SortedFunc(slices.Values(items), func(a, b T) int {
		ia, ib := id(a), id(b)

		return cmp.Or(cmp.Compare(name(a), name(b)), slices.Compare(ia[:], ib[:]))
	})
}

func mustParseID[I domain.AnyID](t *testing.T, s string) I {
	t.Helper()

	id, err := domain.ParseID[I](s)
	if err != nil {
		t.Fatal(err)
	}

	return id
}

func TestCatalogLocations(t *testing.T) {
	t.Parallel()

	got, err := service.NewCatalogService(memory.New()).Locations(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("empty store: Locations() = %v, %v; want no locations", got, err)
	}

	got, err = service.NewCatalogService(catalogDemoStore(t)).Locations(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	want := sortedByName(seed.Demo().Locations,
		func(l domain.Location) string { return l.Name },
		func(l domain.Location) [16]byte { return [16]byte(l.ID) })
	if !slices.Equal(got, want) {
		t.Fatalf("Locations() = %+v, want %+v", got, want)
	}
}

func TestCatalogRooms(t *testing.T) {
	t.Parallel()

	got, err := service.NewCatalogService(memory.New()).Rooms(t.Context(), domain.LocationID{})
	if err != nil || len(got) != 0 {
		t.Fatalf("empty store: Rooms() = %v, %v; want no rooms", got, err)
	}

	demo := seed.Demo()
	svc := service.NewCatalogService(catalogDemoStore(t))
	byName := func(rooms []domain.Room) []domain.Room {
		return sortedByName(rooms,
			func(r domain.Room) string { return r.Name },
			func(r domain.Room) [16]byte { return [16]byte(r.ID) })
	}

	t.Run("all", func(t *testing.T) {
		t.Parallel()

		got, err := svc.Rooms(t.Context(), domain.LocationID{})
		if err != nil {
			t.Fatal(err)
		}

		if want := byName(demo.Rooms); !slices.Equal(got, want) {
			t.Fatalf("Rooms() = %+v, want %+v", got, want)
		}
	})

	t.Run("one location", func(t *testing.T) {
		t.Parallel()

		for _, l := range demo.Locations {
			got, err := svc.Rooms(t.Context(), l.ID)
			if err != nil {
				t.Fatal(err)
			}

			want := byName(slices.DeleteFunc(slices.Clone(demo.Rooms), func(r domain.Room) bool {
				return r.LocationID != l.ID
			}))
			if len(want) == 0 || !slices.Equal(got, want) {
				t.Fatalf("Rooms(%s) = %+v, want %+v", l.Name, got, want)
			}
		}
	})

	t.Run("unknown location", func(t *testing.T) {
		t.Parallel()

		_, err := svc.Rooms(t.Context(), mustParseID[domain.LocationID](t, unknownID))
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})
}

func TestCatalogRoomsLocationWithoutRooms(t *testing.T) {
	t.Parallel()

	st := memory.New()
	loc := domain.Location{ID: mustParseID[domain.LocationID](t, unknownID), Name: "Empty Annex"}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Locations().Create(ctx, loc)
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.NewCatalogService(st).Rooms(t.Context(), loc.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("Rooms() = %v, %v; want no rooms", got, err)
	}
}

func TestCatalogStaff(t *testing.T) {
	t.Parallel()

	got, err := service.NewCatalogService(memory.New()).Staff(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("empty store: Staff() = %v, %v; want no staff", got, err)
	}

	got, err = service.NewCatalogService(catalogDemoStore(t)).Staff(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	demo := seed.Demo()
	active := slices.DeleteFunc(slices.Clone(demo.Staff), func(s domain.Staff) bool { return !s.Active })

	if len(active) == len(demo.Staff) {
		t.Fatal("the demo data set needs inactive staff for this test")
	}

	want := sortedByName(active,
		func(s domain.Staff) string { return s.DisplayName },
		func(s domain.Staff) [16]byte { return [16]byte(s.ID) })
	if !slices.Equal(got, want) {
		t.Fatalf("Staff() = %+v, want the active staff %+v", got, want)
	}
}

func TestCatalogCourseTypes(t *testing.T) {
	t.Parallel()

	got, err := service.NewCatalogService(memory.New()).CourseTypes(t.Context())
	if err != nil || len(got) != 0 {
		t.Fatalf("empty store: CourseTypes() = %v, %v; want no course types", got, err)
	}

	got, err = service.NewCatalogService(catalogDemoStore(t)).CourseTypes(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	want := sortedByName(seed.Demo().CourseTypes,
		func(c domain.CourseType) string { return c.Name },
		func(c domain.CourseType) [16]byte { return [16]byte(c.ID) })
	if !slices.Equal(got, want) {
		t.Fatalf("CourseTypes() = %+v, want %+v", got, want)
	}
}
