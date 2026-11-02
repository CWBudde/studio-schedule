package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// freeRoomsQuery is a valid query: Wednesday 4 November 2026, 18:00 for
// an hour, every location.
func freeRoomsQuery() FreeRoomsQuery {
	return FreeRoomsQuery{
		Date: domain.Date{Year: 2026, Month: time.November, Day: 4}, Start: domain.TimeOfDay{Hour: 18},
		Duration: time.Hour,
	}
}

// TestFreeRoomsValidation: an invalid date, start or duration is a field
// error on that field, found before the store is touched; exactly
// MaxFreeRoomsDuration is accepted.
func TestFreeRoomsValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit  func(q *FreeRoomsQuery)
		field string
	}{
		"zero date":          {func(q *FreeRoomsQuery) { q.Date = domain.Date{} }, "date"},
		"no such date":       {func(q *FreeRoomsQuery) { q.Date.Month, q.Date.Day = time.February, 30 }, "date"},
		"start at 24:00":     {func(q *FreeRoomsQuery) { q.Start.Hour = 24 }, "start"},
		"start at minute 60": {func(q *FreeRoomsQuery) { q.Start.Minute = 60 }, "start"},
		"zero duration":      {func(q *FreeRoomsQuery) { q.Duration = 0 }, "duration"},
		"negative duration":  {func(q *FreeRoomsQuery) { q.Duration = -time.Hour }, "duration"},
		"too long":           {func(q *FreeRoomsQuery) { q.Duration = MaxFreeRoomsDuration + time.Minute }, "duration"},
		// The first invalid field is the one reported.
		"date before start": {func(q *FreeRoomsQuery) { q.Date, q.Start.Hour = domain.Date{}, 24 }, "date"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := NewFaultStore(memory.New())
			q := freeRoomsQuery()
			tt.edit(&q)

			rooms, err := NewCatalogService(fs, ids.NewSequence()).FreeRooms(t.Context(), q)
			wantFieldError(t, err, tt.field)

			if rooms != nil {
				t.Errorf("rooms %v with an invalid query", rooms)
			}

			if calls := fs.Calls(); len(calls) != 0 {
				t.Errorf("an invalid query reached the store: %v", calls)
			}
		})
	}

	t.Run("longest duration", func(t *testing.T) {
		t.Parallel()

		c := newCatalogFixture(t)
		q := freeRoomsQuery()
		q.Duration = MaxFreeRoomsDuration

		rooms, err := c.svc.FreeRooms(t.Context(), q)
		if err != nil || len(rooms) != 1 || rooms[0].ID != c.room {
			t.Fatalf("free rooms for %s: %v, %v; want the fixture's room", q.Duration, rooms, err)
		}
	})
}

// TestFreeRoomsNoSchool: without school settings there is no time zone to
// read the date in.
func TestFreeRoomsNoSchool(t *testing.T) {
	t.Parallel()

	_, err := NewCatalogService(memory.New(), ids.NewSequence()).FreeRooms(t.Context(), freeRoomsQuery())
	if !errors.Is(err, ErrNoSchool) {
		t.Fatalf("want ErrNoSchool, got %v", err)
	}

	if !strings.HasPrefix(err.Error(), "free rooms: ") {
		t.Errorf("error %q does not say it is the free rooms'", err)
	}
}

// TestFreeRoomsStoreFaults: a store failure anywhere, the settings, the
// location, the rooms or any room's lessons, fails the query instead of
// giving a partial list.
func TestFreeRoomsStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		c := newCatalogFixture(t)
		second := domain.Room{ID: ids.New[domain.RoomID](c.seq), LocationID: c.location, Name: "Room Two", Capacity: 8, Position: 2}
		c.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Create(ctx, second) })

		return c.fs, func() error {
			q := freeRoomsQuery()
			q.Location = c.location

			rooms, err := c.svc.FreeRooms(t.Context(), q)
			if err == nil && len(rooms) != 2 {
				t.Fatalf("free rooms %v, want both", rooms)
			}

			if err != nil && rooms != nil {
				t.Fatalf("failed with rooms %v", rooms)
			}

			return err
		}
	}})
}
