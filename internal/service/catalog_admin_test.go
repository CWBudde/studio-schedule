package service_test

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
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// wantField fails unless err is a validation error about field.
func wantField(t *testing.T, err error, field string) {
	t.Helper()

	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("want a validation error on %q, got %v", field, err)
	}
}

// demoClass returns a class of the demo timetable in st.
func demoClass(t *testing.T, st store.Store) domain.Class {
	t.Helper()

	var class domain.Class

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		terms, err := tx.Terms().List(ctx)
		if err != nil {
			return err
		}

		classes, err := tx.Classes().ListByTerm(ctx, terms[0].ID)
		if err != nil {
			return err
		}

		class = classes[0]

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return class
}

func TestLocationAdmin(t *testing.T) {
	t.Parallel()

	svc := service.NewCatalogService(memory.New(), ids.V7{})
	ctx := t.Context()

	north, err := svc.CreateLocation(ctx, service.LocationData{Name: "  North Studio ", Address: " Example Road 2 "})
	if err != nil {
		t.Fatal(err)
	}

	if north.ID.IsZero() || north.Name != "North Studio" || north.Address != "Example Road 2" {
		t.Errorf("created %+v, want a new ID and trimmed fields", north)
	}

	_, err = svc.CreateLocation(ctx, service.LocationData{Name: " "})
	wantField(t, err, "name")

	_, err = svc.CreateLocation(ctx, service.LocationData{Name: "north studio"})
	wantIs(t, err, domain.ErrDuplicate)

	renamed, err := svc.UpdateLocation(ctx, north.ID, service.LocationData{Name: "North Hall"})
	if err != nil || renamed.Name != "North Hall" || renamed.Address != "" {
		t.Fatalf("update: %+v, %v", renamed, err)
	}

	// An unknown location is a 404 even with invalid fields.
	_, err = svc.UpdateLocation(ctx, ids.New[domain.LocationID](ids.V7{}), service.LocationData{Name: " "})
	wantIs(t, err, domain.ErrNotFound)

	// A location with rooms is in use.
	room, err := svc.CreateRoom(ctx, service.RoomData{LocationID: north.ID, Name: "Studio 1", Capacity: 12})
	if err != nil {
		t.Fatal(err)
	}

	wantIs(t, svc.DeleteLocation(ctx, north.ID), domain.ErrInUse)

	if err := svc.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteLocation(ctx, north.ID); err != nil {
		t.Fatal(err)
	}

	wantIs(t, svc.DeleteLocation(ctx, north.ID), domain.ErrNotFound)
}

func TestRoomAdmin(t *testing.T) {
	t.Parallel()

	st := catalogDemoStore(t)
	svc := service.NewCatalogService(st, ids.V7{})
	ctx := t.Context()

	loc, err := svc.CreateLocation(ctx, service.LocationData{Name: "West Studio"})
	if err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		data  service.RoomData
		field string
	}{
		"no name":          {service.RoomData{LocationID: loc.ID, Capacity: 5}, "name"},
		"zero capacity":    {service.RoomData{LocationID: loc.ID, Name: "A"}, "capacity"},
		"no location":      {service.RoomData{Name: "A", Capacity: 5}, "locationId"},
		"unknown location": {service.RoomData{LocationID: ids.New[domain.LocationID](ids.V7{}), Name: "A", Capacity: 5}, "locationId"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.CreateRoom(ctx, tc.data)
			wantField(t, err, tc.field)
		})
	}

	room, err := svc.CreateRoom(ctx, service.RoomData{LocationID: loc.ID, Name: " Studio A ", Capacity: 10})
	if err != nil || room.Name != "Studio A" {
		t.Fatalf("create: %+v, %v", room, err)
	}

	_, err = svc.CreateRoom(ctx, service.RoomData{LocationID: loc.ID, Name: "STUDIO A", Capacity: 3})
	wantIs(t, err, domain.ErrDuplicate)

	updated, err := svc.UpdateRoom(ctx, room.ID, service.RoomData{LocationID: loc.ID, Name: "Studio A", Capacity: 14})
	if err != nil || updated.Capacity != 14 {
		t.Fatalf("update: %+v, %v", updated, err)
	}

	_, err = svc.UpdateRoom(ctx, ids.New[domain.RoomID](ids.V7{}), service.RoomData{LocationID: loc.ID, Name: " "})
	wantIs(t, err, domain.ErrNotFound)

	_, err = svc.UpdateRoom(ctx, room.ID, service.RoomData{LocationID: ids.New[domain.LocationID](ids.V7{}), Name: "B", Capacity: 1})
	wantField(t, err, "locationId")

	// The room of a demo class is in use.
	wantIs(t, svc.DeleteRoom(ctx, demoClass(t, st).RoomID), domain.ErrInUse)

	if err := svc.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatal(err)
	}

	wantIs(t, svc.DeleteRoom(ctx, room.ID), domain.ErrNotFound)
}

// roomNames returns the names and positions of the location's rooms in the
// catalog's order.
func roomNames(t *testing.T, svc *service.CatalogService, location domain.LocationID) []string {
	t.Helper()

	rooms, err := svc.Rooms(t.Context(), location)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, len(rooms))
	for i, r := range rooms {
		names[i] = fmt.Sprintf("%d %s", r.Position, r.Name)
	}

	return names
}

func wantRooms(t *testing.T, svc *service.CatalogService, location domain.LocationID, want ...string) {
	t.Helper()

	if got := roomNames(t, svc, location); !slices.Equal(got, want) {
		t.Fatalf("rooms %q, want %q", got, want)
	}
}

// TestRoomPositions: a new room goes after the location's last one, an edit
// keeps the place unless the room moves to another location, at whose end
// it goes.
func TestRoomPositions(t *testing.T) {
	t.Parallel()

	svc := service.NewCatalogService(memory.New(), ids.V7{})
	ctx := t.Context()

	north, err := svc.CreateLocation(ctx, service.LocationData{Name: "North Studio"})
	if err != nil {
		t.Fatal(err)
	}

	south, err := svc.CreateLocation(ctx, service.LocationData{Name: "South Studio"})
	if err != nil {
		t.Fatal(err)
	}

	create := func(location domain.LocationID, name string) domain.Room {
		t.Helper()

		r, err := svc.CreateRoom(ctx, service.RoomData{LocationID: location, Name: name, Capacity: 10})
		if err != nil {
			t.Fatal(err)
		}

		return r
	}

	// Appended in the order of creation, not by name.
	zeta, alpha := create(north.ID, "Zeta"), create(north.ID, "Alpha")
	mid := create(north.ID, "Mid")

	if zeta.Position != 1 || alpha.Position != 2 || mid.Position != 3 {
		t.Fatalf("positions %d, %d, %d; want 1, 2, 3", zeta.Position, alpha.Position, mid.Position)
	}

	wantRooms(t, svc, north.ID, "1 Zeta", "2 Alpha", "3 Mid")

	// Each location counts on its own.
	create(south.ID, "Hall")
	wantRooms(t, svc, south.ID, "1 Hall")

	// An edit at the same location keeps the place.
	renamed, err := svc.UpdateRoom(ctx, alpha.ID, service.RoomData{LocationID: north.ID, Name: "Aleph", Capacity: 12})
	if err != nil || renamed.Position != 2 {
		t.Fatalf("rename: %+v, %v; want position 2", renamed, err)
	}

	// A move appends the room to its new location; the old one keeps a gap.
	moved, err := svc.UpdateRoom(ctx, zeta.ID, service.RoomData{LocationID: south.ID, Name: "Zeta", Capacity: 10})
	if err != nil || moved.Position != 2 {
		t.Fatalf("move: %+v, %v; want position 2", moved, err)
	}

	wantRooms(t, svc, north.ID, "2 Aleph", "3 Mid")
	wantRooms(t, svc, south.ID, "1 Hall", "2 Zeta")

	// After the highest position, not after the number of rooms.
	create(north.ID, "New")
	wantRooms(t, svc, north.ID, "2 Aleph", "3 Mid", "4 New")
}

// TestSetRoomOrder: the order lists each room of the location once and
// numbers them from 1; anything else is a field error on roomIds.
func TestSetRoomOrder(t *testing.T) {
	t.Parallel()

	svc := service.NewCatalogService(memory.New(), ids.V7{})
	ctx := t.Context()

	north, err := svc.CreateLocation(ctx, service.LocationData{Name: "North Studio"})
	if err != nil {
		t.Fatal(err)
	}

	south, err := svc.CreateLocation(ctx, service.LocationData{Name: "South Studio"})
	if err != nil {
		t.Fatal(err)
	}

	create := func(location domain.LocationID, name string) domain.RoomID {
		t.Helper()

		r, err := svc.CreateRoom(ctx, service.RoomData{LocationID: location, Name: name, Capacity: 10})
		if err != nil {
			t.Fatal(err)
		}

		return r.ID
	}

	a, b, c := create(north.ID, "Room A"), create(north.ID, "Room B"), create(north.ID, "Room C")
	hall := create(south.ID, "Hall")

	if err := svc.SetRoomOrder(ctx, north.ID, []domain.RoomID{c, a, b}); err != nil {
		t.Fatal(err)
	}

	wantRooms(t, svc, north.ID, "1 Room C", "2 Room A", "3 Room B")

	for name, order := range map[string][]domain.RoomID{
		"missing room":            {c, a},
		"repeated room":           {c, a, a},
		"another location's room": {c, a, b, hall},
		"empty":                   nil,
	} {
		t.Run(name, func(t *testing.T) {
			err := svc.SetRoomOrder(ctx, north.ID, order)
			wantField(t, err, "roomIds")

			var fe *domain.FieldError
			if errors.As(err, &fe) && fe.Msg != "must list each room of the location exactly once" {
				t.Errorf("message %q", fe.Msg)
			}
		})
	}

	wantRooms(t, svc, north.ID, "1 Room C", "2 Room A", "3 Room B")
	wantRooms(t, svc, south.ID, "1 Hall")

	// An unknown location is a 404, whatever the order.
	wantIs(t, svc.SetRoomOrder(ctx, ids.New[domain.LocationID](ids.V7{}), nil), domain.ErrNotFound)

	// A location without rooms takes the empty order.
	empty, err := svc.CreateLocation(ctx, service.LocationData{Name: "Empty Annex"})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.SetRoomOrder(ctx, empty.ID, nil); err != nil {
		t.Fatal(err)
	}
}

// TestSetRoomOrderStale: an order sent from a list read before someone
// added a room misses that room and is refused, so the new room is never
// silently given a place by the old list.
func TestSetRoomOrderStale(t *testing.T) {
	t.Parallel()

	svc := service.NewCatalogService(memory.New(), ids.V7{})
	ctx := t.Context()

	loc, err := svc.CreateLocation(ctx, service.LocationData{Name: "North Studio"})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"Room A", "Room B"} {
		if _, err := svc.CreateRoom(ctx, service.RoomData{LocationID: loc.ID, Name: name, Capacity: 10}); err != nil {
			t.Fatal(err)
		}
	}

	// The office reads the list…
	read, err := svc.Rooms(ctx, loc.ID)
	if err != nil {
		t.Fatal(err)
	}

	// …someone else adds a room…
	if _, err := svc.CreateRoom(ctx, service.RoomData{LocationID: loc.ID, Name: "Room C", Capacity: 10}); err != nil {
		t.Fatal(err)
	}

	// …and the office sends the read list swapped.
	wantField(t, svc.SetRoomOrder(ctx, loc.ID, []domain.RoomID{read[1].ID, read[0].ID}), "roomIds")
	wantRooms(t, svc, loc.ID, "1 Room A", "2 Room B", "3 Room C")
}

func TestCourseTypeAdmin(t *testing.T) {
	t.Parallel()

	st := catalogDemoStore(t)
	svc := service.NewCatalogService(st, ids.V7{})
	ctx := t.Context()

	salsa, err := svc.CreateCourseType(ctx, service.CourseTypeData{Name: " Salsa ", Description: " Partner work. "})
	if err != nil {
		t.Fatal(err)
	}

	if salsa.ID.IsZero() || salsa.Name != "Salsa" || salsa.Description != "Partner work." {
		t.Errorf("created %+v, want a new ID and trimmed fields", salsa)
	}

	_, err = svc.CreateCourseType(ctx, service.CourseTypeData{Name: "  "})
	wantField(t, err, "name")

	// Demo course types count too.
	_, err = svc.CreateCourseType(ctx, service.CourseTypeData{Name: "TAP"})
	wantIs(t, err, domain.ErrDuplicate)

	_, err = svc.UpdateCourseType(ctx, salsa.ID, service.CourseTypeData{Name: "tap"})
	wantIs(t, err, domain.ErrDuplicate)

	renamed, err := svc.UpdateCourseType(ctx, salsa.ID, service.CourseTypeData{Name: "Salsa Cubana"})
	if err != nil || renamed.Name != "Salsa Cubana" || renamed.Description != "" {
		t.Fatalf("update: %+v, %v", renamed, err)
	}

	_, err = svc.UpdateCourseType(ctx, ids.New[domain.CourseTypeID](ids.V7{}), service.CourseTypeData{Name: " "})
	wantIs(t, err, domain.ErrNotFound)

	wantIs(t, svc.DeleteCourseType(ctx, demoClass(t, st).CourseTypeID), domain.ErrInUse)

	if err := svc.DeleteCourseType(ctx, salsa.ID); err != nil {
		t.Fatal(err)
	}

	wantIs(t, svc.DeleteCourseType(ctx, salsa.ID), domain.ErrNotFound)
}

func TestStaffAdmin(t *testing.T) {
	t.Parallel()

	st := catalogDemoStore(t)
	svc := service.NewCatalogService(st, ids.V7{})
	ctx := t.Context()

	persons := service.NewPersonService(st, clock.NewFixed(time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)), ids.V7{}, service.Mails{})

	found, err := persons.SearchPersons(ctx, "clara amberly")
	if err != nil || len(found) != 1 {
		t.Fatalf("search: %v, %v", found, err)
	}

	clara := found[0].ID

	for name, tc := range map[string]struct {
		data  service.StaffData
		field string
	}{
		"no name":        {service.StaffData{Kind: domain.StaffInstructor}, "displayName"},
		"unknown kind":   {service.StaffData{DisplayName: "A", Kind: "dancer"}, "kind"},
		"unknown person": {service.StaffData{DisplayName: "A", Kind: domain.StaffInstructor, PersonID: ids.New[domain.PersonID](ids.V7{})}, "personId"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.CreateStaff(ctx, tc.data)
			wantField(t, err, tc.field)
		})
	}

	ivy, err := svc.CreateStaff(ctx, service.StaffData{DisplayName: " Ivy Lanthorn ", Kind: domain.StaffInstructor, Active: true})
	if err != nil || ivy.ID.IsZero() || ivy.DisplayName != "Ivy Lanthorn" || !ivy.PersonID.IsZero() {
		t.Fatalf("create: %+v, %v", ivy, err)
	}

	linked, err := svc.UpdateStaff(ctx, ivy.ID, service.StaffData{
		DisplayName: "Ivy Lanthorn", Kind: domain.StaffAccompanist, PersonID: clara, Active: true,
	})
	if err != nil || linked.PersonID != clara || linked.Person.GivenName != "Clara" || linked.Kind != domain.StaffAccompanist {
		t.Fatalf("update: %+v, %v", linked, err)
	}

	_, err = svc.UpdateStaff(ctx, ids.New[domain.StaffID](ids.V7{}), service.StaffData{})
	wantIs(t, err, domain.ErrNotFound)

	// A deactivated instructor stays on their classes, leaves the public
	// list and stays in the office's.
	instructor := demoClass(t, st).InstructorID

	all, err := svc.AllStaff(ctx)
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(all, func(m service.StaffEntry) bool { return m.ID == instructor })
	if i < 0 {
		t.Fatalf("instructor %s not in AllStaff", instructor)
	}

	data := service.StaffData{DisplayName: all[i].DisplayName, Kind: all[i].Kind, PersonID: all[i].PersonID}
	if _, err := svc.UpdateStaff(ctx, instructor, data); err != nil {
		t.Fatalf("deactivate an instructor with classes: %v", err)
	}

	public, err := svc.Staff(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if slices.ContainsFunc(public, func(m domain.Staff) bool { return m.ID == instructor }) {
		t.Error("public staff list shows a deactivated instructor")
	}

	again, err := svc.AllStaff(ctx)
	if err != nil || len(again) != len(all) {
		t.Errorf("AllStaff after deactivating: %d staff, %v; want %d", len(again), err, len(all))
	}

	wantIs(t, svc.DeleteStaff(ctx, instructor), domain.ErrInUse)

	if err := svc.DeleteStaff(ctx, ivy.ID); err != nil {
		t.Fatal(err)
	}

	wantIs(t, svc.DeleteStaff(ctx, ivy.ID), domain.ErrNotFound)
}
