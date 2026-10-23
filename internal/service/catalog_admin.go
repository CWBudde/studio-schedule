package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office maintains the catalog (ADR 0007). The HTTP layer admits only
// office and admin accounts to these methods.

// LocationData are the fields the office edits on a location. Address is
// empty when unknown.
type LocationData struct {
	Name    string
	Address string
}

// RoomData are the fields the office edits on a room.
type RoomData struct {
	LocationID domain.LocationID
	Name       string
	Capacity   int
}

// CreateLocation adds a location. A taken name is domain.ErrDuplicate.
func (s *CatalogService) CreateLocation(ctx context.Context, d LocationData) (domain.Location, error) {
	l := d.location(ids.New[domain.LocationID](s.ids))

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := l.Validate(); err != nil {
			return err
		}

		return tx.Locations().Create(ctx, l)
	})
	if err != nil {
		return domain.Location{}, fmt.Errorf("create location: %w", err)
	}

	return l, nil
}

// UpdateLocation replaces a location's name and address.
func (s *CatalogService) UpdateLocation(
	ctx context.Context, id domain.LocationID, d LocationData,
) (domain.Location, error) {
	l := d.location(id)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// An unknown location is a 404 before anything about its fields.
		if _, err := tx.Locations().Get(ctx, id); err != nil {
			return err
		}

		if err := l.Validate(); err != nil {
			return err
		}

		return tx.Locations().Update(ctx, l)
	})
	if err != nil {
		return domain.Location{}, fmt.Errorf("update location: %w", err)
	}

	return l, nil
}

// DeleteLocation removes a location without rooms; one with rooms is
// domain.ErrInUse.
func (s *CatalogService) DeleteLocation(ctx context.Context, id domain.LocationID) error {
	err := s.remove(ctx, func(ctx context.Context, tx store.Tx) error { return tx.Locations().Delete(ctx, id) })
	if err != nil {
		return fmt.Errorf("delete location: %w", err)
	}

	return nil
}

// CreateRoom adds a room to a location. An unknown location is a
// validation error on locationId.
func (s *CatalogService) CreateRoom(ctx context.Context, d RoomData) (domain.Room, error) {
	r := d.room(ids.New[domain.RoomID](s.ids))

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := r.Validate(); err != nil {
			return err
		}

		if err := locationExists(ctx, tx, r.LocationID); err != nil {
			return err
		}

		return roomWriteErr(tx.Rooms().Create(ctx, r))
	})
	if err != nil {
		return domain.Room{}, fmt.Errorf("create room: %w", err)
	}

	return r, nil
}

// UpdateRoom replaces a room's location, name and capacity. Lessons keep
// the capacity they were created with.
func (s *CatalogService) UpdateRoom(ctx context.Context, id domain.RoomID, d RoomData) (domain.Room, error) {
	r := d.room(id)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// An unknown room is a 404 before anything about its fields.
		if _, err := tx.Rooms().Get(ctx, id); err != nil {
			return err
		}

		if err := r.Validate(); err != nil {
			return err
		}

		if err := locationExists(ctx, tx, r.LocationID); err != nil {
			return err
		}

		return roomWriteErr(tx.Rooms().Update(ctx, r))
	})
	if err != nil {
		return domain.Room{}, fmt.Errorf("update room: %w", err)
	}

	return r, nil
}

// DeleteRoom removes a room that no class or lesson uses; a used one is
// domain.ErrInUse.
func (s *CatalogService) DeleteRoom(ctx context.Context, id domain.RoomID) error {
	err := s.remove(ctx, func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Delete(ctx, id) })
	if err != nil {
		return fmt.Errorf("delete room: %w", err)
	}

	return nil
}

// remove runs a delete in a read-write transaction. The stores refuse to
// delete a record others refer to with domain.ErrConflict, which is
// reported as domain.ErrInUse. (A delete has no other conflict, except a
// PostgreSQL transaction that gave up retrying, which is rare enough to
// share the answer.)
func (s *CatalogService) remove(ctx context.Context, fn func(context.Context, store.Tx) error) error {
	err := s.store.InTx(ctx, store.ReadWrite, fn)
	if errors.Is(err, domain.ErrConflict) {
		return fmt.Errorf("%w (%w)", domain.ErrInUse, err)
	}

	return err
}

// errUnknownLocation is a room's location that does not exist.
var errUnknownLocation error = &domain.FieldError{Field: "locationId", Msg: "location does not exist"}

// locationExists reports an unknown location as a validation error on the
// field that names it.
func locationExists(ctx context.Context, tx store.Tx, id domain.LocationID) error {
	_, err := tx.Locations().Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return errUnknownLocation
	}

	return err
}

// roomWriteErr maps the result of a room write after locationExists. Besides
// a taken name, the store's only conflict is a missing location: one deleted
// by someone else in the meantime, reported like an unknown one.
func roomWriteErr(err error) error {
	if errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrDuplicate) {
		return fmt.Errorf("%w (%w)", errUnknownLocation, err)
	}

	return err
}

func (d LocationData) location(id domain.LocationID) domain.Location {
	return domain.Location{ID: id, Name: strings.TrimSpace(d.Name), Address: strings.TrimSpace(d.Address)}
}

func (d RoomData) room(id domain.RoomID) domain.Room {
	return domain.Room{ID: id, LocationID: d.LocationID, Name: strings.TrimSpace(d.Name), Capacity: d.Capacity}
}
