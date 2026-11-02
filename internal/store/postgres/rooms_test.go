package postgres

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// roomPlaces returns the location's rooms as "position name".
func roomPlaces(t *testing.T, svc *service.CatalogService, location domain.LocationID) []string {
	t.Helper()

	rooms, err := svc.Rooms(t.Context(), location)
	if err != nil {
		t.Fatal(err)
	}

	places := make([]string, 0, len(rooms))
	for _, r := range rooms {
		places = append(places, fmt.Sprintf("%d %s", r.Position, r.Name))
	}

	return places
}

// TestAddRoomsConcurrently adds rooms to a location and moves rooms into it
// from many goroutines: under the location's lock each one takes its own
// place after the last (ADR 0007, amendment "room order").
func TestAddRoomsConcurrently(t *testing.T) {
	t.Parallel()

	const n = 4

	ctx := t.Context()
	svc := service.NewCatalogService(newTestStore(t), ids.V7{})

	hall, err := svc.CreateLocation(ctx, service.LocationData{Name: "Main Hall"})
	if err != nil {
		t.Fatal(err)
	}

	annex, err := svc.CreateLocation(ctx, service.LocationData{Name: "Annex"})
	if err != nil {
		t.Fatal(err)
	}

	moving := make([]domain.Room, n)
	for i := range n {
		if moving[i], err = svc.CreateRoom(ctx, service.RoomData{
			LocationID: annex.ID, Name: fmt.Sprintf("Moved %d", i), Capacity: 10,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			d := service.RoomData{LocationID: hall.ID, Name: fmt.Sprintf("Added %d", i), Capacity: 10}
			if _, err := svc.CreateRoom(ctx, d); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			d := service.RoomData{LocationID: hall.ID, Name: moving[i].Name, Capacity: 10}
			if _, err := svc.UpdateRoom(ctx, moving[i].ID, d); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	rooms, err := svc.Rooms(ctx, hall.ID)
	if err != nil {
		t.Fatal(err)
	}

	positions := make([]int, 0, len(rooms))
	for _, r := range rooms {
		positions = append(positions, r.Position)
	}

	if want := []int{1, 2, 3, 4, 5, 6, 7, 8}; !slices.Equal(positions, want) {
		t.Errorf("positions = %v, want %v", positions, want)
	}
}

// TestAddRoomWaitsForReorder adds a room while a reorder holds the
// location: the new room waits and lands after the reordered ones, instead of
// slipping past an order that misses it.
func TestAddRoomWaitsForReorder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := newTestStore(t)
	svc := service.NewCatalogService(st, ids.V7{})

	hall, err := svc.CreateLocation(ctx, service.LocationData{Name: "Main Hall"})
	if err != nil {
		t.Fatal(err)
	}

	room := func(name string) domain.Room {
		r, err := svc.CreateRoom(ctx, service.RoomData{LocationID: hall.ID, Name: name, Capacity: 10})
		if err != nil {
			t.Fatal(err)
		}

		return r
	}
	up, down := room("Upstairs"), room("Downstairs")

	locked, release := make(chan struct{}), make(chan struct{})
	reordered := make(chan error, 1)

	go func() {
		reordered <- st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			if _, err := tx.Locations().Lock(ctx, hall.ID); err != nil {
				return err
			}

			close(locked)
			<-release

			return tx.Rooms().SetOrder(ctx, hall.ID, []domain.RoomID{down.ID, up.ID})
		})
	}()

	<-locked

	created := make(chan error, 1)

	go func() {
		_, err := svc.CreateRoom(ctx, service.RoomData{LocationID: hall.ID, Name: "Studio", Capacity: 10})
		created <- err
	}()

	var early bool

	select {
	case err := <-created:
		// Not Fatal: the reorder must still be released and end.
		t.Errorf("create finished while the location was locked: %v", err)

		early = true
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	if err := <-reordered; err != nil {
		t.Fatal(err)
	}

	if !early {
		if err := <-created; err != nil {
			t.Fatal(err)
		}
	}

	if got, want := roomPlaces(t, svc, hall.ID), []string{"1 Downstairs", "2 Upstairs", "3 Studio"}; !slices.Equal(got, want) {
		t.Errorf("rooms = %v, want %v", got, want)
	}
}
