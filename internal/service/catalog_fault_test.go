package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// catalogFixture is the generation fixture with its location and a staff
// member linked to a person, and a CatalogService on a FaultStore.
type catalogFixture struct {
	*genFixture

	fs       *FaultStore
	svc      *CatalogService
	location domain.LocationID
	linked   domain.Staff
}

func newCatalogFixture(t *testing.T) catalogFixture {
	t.Helper()

	f := newGenFixture(t, seed.DemoSettings())
	c := catalogFixture{genFixture: f, fs: NewFaultStore(f.st)}
	c.svc = NewCatalogService(c.fs, f.seq)

	p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Clara", FamilyName: "Example"}
	c.linked = domain.Staff{
		ID: ids.New[domain.StaffID](f.seq), DisplayName: "Clara Example", Kind: domain.StaffInstructor,
		PersonID: p.ID, Active: true,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		room, err := tx.Rooms().Get(ctx, f.room)
		c.location = room.LocationID

		return errors.Join(err, tx.Persons().Create(ctx, p), tx.Staff().Create(ctx, c.linked))
	})

	return c
}

// TestCatalogEditRefusals: an edit of a known record with invalid fields
// or an unknown person is a validation error on the field, and nothing
// changes.
func TestCatalogEditRefusals(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit  func(c catalogFixture) error
		field string
	}{
		"location without a name": {func(c catalogFixture) error {
			_, err := c.svc.UpdateLocation(t.Context(), c.location, LocationData{Name: "  "})

			return err
		}, "name"},
		"room without capacity": {func(c catalogFixture) error {
			_, err := c.svc.UpdateRoom(t.Context(), c.room, RoomData{LocationID: c.location, Name: "Room One"})

			return err
		}, "capacity"},
		"staff without a kind": {func(c catalogFixture) error {
			_, err := c.svc.UpdateStaff(t.Context(), c.instructor, StaffData{DisplayName: "Ada Example"})

			return err
		}, "kind"},
		"staff with an unknown person": {func(c catalogFixture) error {
			_, err := c.svc.UpdateStaff(t.Context(), c.instructor, StaffData{
				DisplayName: "Ada Example", Kind: domain.StaffInstructor, PersonID: ids.New[domain.PersonID](c.seq),
			})

			return err
		}, "personId"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := newCatalogFixture(t)
			before := c.fs.Snapshot(t)

			wantFieldError(t, tt.edit(c), tt.field)

			if changed := before.Changed(c.fs.Snapshot(t)); len(changed) > 0 {
				t.Fatalf("a refused edit changed %v", changed)
			}
		})
	}
}

// TestCatalogWriteRaces: a reference that disappears between the check
// and the write, as when someone else deletes the location or the person
// meanwhile, is reported like an unknown one; a taken name stays a
// duplicate.
func TestCatalogWriteRaces(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("room's location", func(t *testing.T) {
		t.Parallel()

		c := newCatalogFixture(t)
		d := RoomData{LocationID: c.location, Name: "Room Two", Capacity: 8}

		c.fs.FailWith("Rooms.Create", 1, domain.ErrConflict)
		_, err := c.svc.CreateRoom(ctx, d)
		wantFieldError(t, err, "locationId")

		c.fs.FailWith("Rooms.Update", 1, domain.ErrConflict)
		_, err = c.svc.UpdateRoom(ctx, c.room, d)
		wantFieldError(t, err, "locationId")

		c.fs.FailWith("Rooms.Create", 2, domain.ErrDuplicate)
		if _, err := c.svc.CreateRoom(ctx, d); !errors.Is(err, domain.ErrDuplicate) || errors.As(err, new(*domain.FieldError)) {
			t.Fatalf("want ErrDuplicate, got %v", err)
		}
	})

	t.Run("staff's person", func(t *testing.T) {
		t.Parallel()

		c := newCatalogFixture(t)
		d := StaffData{DisplayName: "Clara Example", Kind: domain.StaffAccompanist, PersonID: c.linked.PersonID}

		c.fs.FailWith("Staff.Create", 1, domain.ErrConflict)
		_, err := c.svc.CreateStaff(ctx, d)
		wantFieldError(t, err, "personId")

		c.fs.FailWith("Staff.Update", 1, domain.ErrConflict)
		_, err = c.svc.UpdateStaff(ctx, c.linked.ID, d)
		wantFieldError(t, err, "personId")
	})
}

// TestCatalogReadStoreFaults: the catalog's lists return a store failure
// instead of a partial list.
func TestCatalogReadStoreFaults(t *testing.T) {
	t.Parallel()

	lists := map[string]func(svc *CatalogService) error{
		"Locations.List": func(svc *CatalogService) error {
			_, err := svc.Locations(t.Context())

			return err
		},
		"Staff.List": func(svc *CatalogService) error {
			_, err := svc.Staff(t.Context())

			return err
		},
		"CourseTypes.List": func(svc *CatalogService) error {
			_, err := svc.CourseTypes(t.Context())

			return err
		},
		"Levels.List": func(svc *CatalogService) error {
			_, err := svc.Levels(t.Context())

			return err
		},
	}

	for method, list := range lists {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			c := newCatalogFixture(t)
			c.fs.Fail(method, 1)

			if err := list(c.svc); !errors.Is(err, ErrInjected) {
				t.Fatalf("want the injected fault, got %v", err)
			}
		})
	}

	// The office's staff list reads the staff and the linked persons.
	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		c := newCatalogFixture(t)

		return c.fs, func() error {
			all, err := c.svc.AllStaff(t.Context())
			if err == nil && len(all) != 3 {
				t.Fatalf("all staff %+v, want 3", all)
			}

			return err
		}
	}})
}

// TestRoomWriteStoreFaults: a store failure in a room write or the room
// order fails the operation and writes nothing.
func TestRoomWriteStoreFaults(t *testing.T) {
	t.Parallel()

	// withSecondLocation adds a location with a room, written past the
	// FaultStore, and returns its ID.
	withSecondLocation := func(t *testing.T, c catalogFixture) domain.LocationID {
		t.Helper()

		l := domain.Location{ID: ids.New[domain.LocationID](c.seq), Name: "Annex"}
		r := domain.Room{ID: ids.New[domain.RoomID](c.seq), LocationID: l.ID, Name: "Annex Hall", Capacity: 8, Position: 1}
		c.write(t, func(ctx context.Context, tx store.Tx) error {
			return errors.Join(tx.Locations().Create(ctx, l), tx.Rooms().Create(ctx, r))
		})

		return l.ID
	}

	tests := map[string]func(t *testing.T, c catalogFixture) func() error{
		"create": func(t *testing.T, c catalogFixture) func() error {
			return func() error {
				r, err := c.svc.CreateRoom(t.Context(), RoomData{LocationID: c.location, Name: "Room Two", Capacity: 8})
				if err == nil && r.Position != 2 {
					t.Fatalf("created at %d, want 2", r.Position)
				}

				return err
			}
		},
		"move": func(t *testing.T, c catalogFixture) func() error {
			annex := withSecondLocation(t, c)

			return func() error {
				r, err := c.svc.UpdateRoom(t.Context(), c.room, RoomData{LocationID: annex, Name: "Room One", Capacity: 8})
				if err == nil && r.Position != 2 {
					t.Fatalf("moved to %d, want 2", r.Position)
				}

				return err
			}
		},
		"order": func(t *testing.T, c catalogFixture) func() error {
			second := domain.Room{ID: ids.New[domain.RoomID](c.seq), LocationID: c.location, Name: "Room Two", Capacity: 8, Position: 2}
			c.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Create(ctx, second) })

			return func() error {
				return c.svc.SetRoomOrder(t.Context(), c.location, []domain.RoomID{second.ID, c.room})
			}
		},
	}

	for name, op := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				c := newCatalogFixture(t)

				return c.fs, op(t, c)
			}})
		})
	}
}

// TestLevelWriteStoreFaults: a store failure in a level write or the level
// order fails the operation and writes nothing.
func TestLevelWriteStoreFaults(t *testing.T) {
	t.Parallel()

	// withLevels adds two levels, written past the FaultStore.
	withLevels := func(t *testing.T, c catalogFixture) (first, second domain.LevelID) {
		t.Helper()

		a := domain.Level{ID: ids.New[domain.LevelID](c.seq), Name: "Level A", Code: "A", Position: 1}
		b := domain.Level{ID: ids.New[domain.LevelID](c.seq), Name: "Level B", Position: 2}
		c.write(t, func(ctx context.Context, tx store.Tx) error {
			return errors.Join(tx.Levels().Create(ctx, a), tx.Levels().Create(ctx, b))
		})

		return a.ID, b.ID
	}

	tests := map[string]func(t *testing.T, c catalogFixture) func() error{
		"create": func(t *testing.T, c catalogFixture) func() error {
			withLevels(t, c)

			return func() error {
				l, err := c.svc.CreateLevel(t.Context(), LevelData{Name: "Level C"})
				if err == nil && l.Position != 3 {
					t.Fatalf("created at %d, want 3", l.Position)
				}

				return err
			}
		},
		"update": func(t *testing.T, c catalogFixture) func() error {
			_, second := withLevels(t, c)

			return func() error {
				l, err := c.svc.UpdateLevel(t.Context(), second, LevelData{Name: "Level Two", Code: "2"})
				if err == nil && l.Position != 2 {
					t.Fatalf("updated at %d, want 2", l.Position)
				}

				return err
			}
		},
		"delete": func(t *testing.T, c catalogFixture) func() error {
			first, _ := withLevels(t, c)

			return func() error { return c.svc.DeleteLevel(t.Context(), first) }
		},
		"order": func(t *testing.T, c catalogFixture) func() error {
			first, second := withLevels(t, c)

			return func() error { return c.svc.SetLevelOrder(t.Context(), []domain.LevelID{second, first}) }
		},
	}

	for name, op := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				c := newCatalogFixture(t)

				return c.fs, op(t, c)
			}})
		})
	}
}

// TestDeleteLevelInUse: the store's refusal to delete a level that classes
// use is reported as in use.
func TestDeleteLevelInUse(t *testing.T) {
	t.Parallel()

	c := newCatalogFixture(t)
	l := domain.Level{ID: ids.New[domain.LevelID](c.seq), Name: "Level A", Position: 1}
	c.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Levels().Create(ctx, l) })

	c.fs.FailWith("Levels.Delete", 1, domain.ErrConflict)

	if err := c.svc.DeleteLevel(t.Context(), l.ID); !errors.Is(err, domain.ErrInUse) {
		t.Fatalf("want ErrInUse, got %v", err)
	}
}
