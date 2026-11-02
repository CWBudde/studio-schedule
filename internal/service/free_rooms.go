package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// MaxFreeRoomsDuration is the longest time FreeRooms looks at.
const MaxFreeRoomsDuration = 12 * time.Hour

// FreeRoomsQuery asks which rooms are free from Start on Date (a wall clock
// time on a school date, in the school's time zone) for Duration.
type FreeRoomsQuery struct {
	Date     domain.Date
	Start    domain.TimeOfDay
	Duration time.Duration     // > 0, at most MaxFreeRoomsDuration
	Location domain.LocationID // optional: zero for every location
}

// validate checks q's fields, each as a field error.
func (q FreeRoomsQuery) validate() error {
	switch {
	case !q.Date.Valid():
		return &domain.FieldError{Field: "date", Msg: "the date is not a valid school date"}
	case !q.Start.Valid():
		return &domain.FieldError{Field: "start", Msg: "the start is not a valid time of day"}
	case q.Duration <= 0 || q.Duration > MaxFreeRoomsDuration:
		return &domain.FieldError{
			Field: "duration", Msg: fmt.Sprintf("the duration must be more than 0 and at most %s", MaxFreeRoomsDuration),
		}
	}

	return nil
}

// FreeRooms returns the rooms of q's location (all locations when zero)
// that no lesson that is not canceled overlaps on q's date from its start
// for its duration, ordered as Rooms orders them: by location name, then by
// position and name. A lesson that ends when the time starts, or starts
// when it ends, leaves the room free. Every kind of lesson blocks its room,
// a room rental and a hidden lesson too: the overlap is the one the office
// meets when it places a lesson (roomFree). An unknown location is
// domain.ErrNotFound, and so is a school without settings (ErrNoSchool),
// whose time zone the date needs.
func (s *CatalogService) FreeRooms(ctx context.Context, q FreeRoomsQuery) ([]domain.Room, error) {
	if err := q.validate(); err != nil {
		return nil, err
	}

	var free []domain.Room

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		// The room list cannot tell an unknown location from one without
		// rooms.
		if !q.Location.IsZero() {
			if _, err := tx.Locations().Get(ctx, q.Location); err != nil {
				return err
			}
		}

		rooms, err := tx.Rooms().List(ctx, q.Location)
		if err != nil {
			return err
		}

		from := q.Date.At(q.Start, loc)
		to := from.Add(q.Duration)

		for _, r := range rooms {
			taken, err := tx.Lessons().ListRoomOverlaps(ctx, r.ID, from, to, domain.LessonID{})
			if err != nil {
				return err
			}

			if len(taken) == 0 {
				free = append(free, r)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("free rooms: %w", err)
	}

	return free, nil
}
