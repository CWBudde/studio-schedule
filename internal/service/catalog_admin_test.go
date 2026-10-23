package service_test

import (
	"context"
	"errors"
	"testing"

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
	var used domain.RoomID

	err = st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		terms, err := tx.Terms().List(ctx)
		if err != nil {
			return err
		}

		classes, err := tx.Classes().ListByTerm(ctx, terms[0].ID)
		if err != nil {
			return err
		}

		used = classes[0].RoomID

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	wantIs(t, svc.DeleteRoom(ctx, used), domain.ErrInUse)

	if err := svc.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatal(err)
	}

	wantIs(t, svc.DeleteRoom(ctx, room.ID), domain.ErrNotFound)
}
