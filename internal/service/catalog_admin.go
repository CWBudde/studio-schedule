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

// CreateRoom adds a room to a location, after its last room. An unknown
// location is a validation error on locationId.
func (s *CatalogService) CreateRoom(ctx context.Context, d RoomData) (domain.Room, error) {
	r := d.room(ids.New[domain.RoomID](s.ids))
	r.Position = 1 // for Validate; the place comes once the location is known

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := r.Validate(); err != nil {
			return err
		}

		if err := locationExists(ctx, tx, r.LocationID); err != nil {
			return err
		}

		var err error
		if r.Position, err = nextRoomPosition(ctx, tx, r.LocationID); err != nil {
			return err
		}

		return roomWriteErr(tx.Rooms().Create(ctx, r))
	})
	if err != nil {
		return domain.Room{}, fmt.Errorf("create room: %w", err)
	}

	return r, nil
}

// UpdateRoom replaces a room's location, name and capacity. The room keeps
// its place, or goes after the last room of its new location. Lessons keep
// the capacity they were created with.
func (s *CatalogService) UpdateRoom(ctx context.Context, id domain.RoomID, d RoomData) (domain.Room, error) {
	r := d.room(id)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// An unknown room is a 404 before anything about its fields. The
		// lock makes a concurrent SetRoomOrder either finish first, so the
		// position read here is its own, or wait for this write; an
		// unlocked read would write a stale position back over it.
		old, err := tx.Rooms().Lock(ctx, id)
		if err != nil {
			return err
		}

		r.Position = old.Position

		if err := r.Validate(); err != nil {
			return err
		}

		if err := locationExists(ctx, tx, r.LocationID); err != nil {
			return err
		}

		if r.LocationID != old.LocationID {
			if r.Position, err = nextRoomPosition(ctx, tx, r.LocationID); err != nil {
				return err
			}
		}

		return roomWriteErr(tx.Rooms().Update(ctx, r))
	})
	if err != nil {
		return domain.Room{}, fmt.Errorf("update room: %w", err)
	}

	return r, nil
}

// SetRoomOrder numbers the rooms of a location from 1 in the order given.
// An unknown location is domain.ErrNotFound; an order that is not exactly
// the location's rooms, each once, is a validation error on roomIds, also
// when a room was added or removed since the caller read the list.
func (s *CatalogService) SetRoomOrder(ctx context.Context, location domain.LocationID, order []domain.RoomID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Locations().Get(ctx, location); err != nil {
			return err
		}

		err := tx.Rooms().SetOrder(ctx, location, order)
		if errors.Is(err, domain.ErrValidation) {
			return fmt.Errorf("%w (%w)", errRoomOrder, err)
		}

		return err
	})
	if err != nil {
		return fmt.Errorf("set room order: %w", err)
	}

	return nil
}

// errRoomOrder is a room order that is not exactly the location's rooms.
var errRoomOrder error = &domain.FieldError{Field: "roomIds", Msg: "must list each room of the location exactly once"}

// nextRoomPosition returns the position after the location's last room, 1
// for its first.
func nextRoomPosition(ctx context.Context, tx store.Tx, location domain.LocationID) (int, error) {
	rooms, err := tx.Rooms().List(ctx, location)
	if err != nil {
		return 0, err
	}

	next := 1
	for _, r := range rooms {
		next = max(next, r.Position+1)
	}

	return next, nil
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

// CourseTypeData are the fields the office edits on a course type.
// Description is empty when there is none.
type CourseTypeData struct {
	Name        string
	Description string
}

// CreateCourseType adds a course type. A taken name is domain.ErrDuplicate.
func (s *CatalogService) CreateCourseType(ctx context.Context, d CourseTypeData) (domain.CourseType, error) {
	c := d.courseType(ids.New[domain.CourseTypeID](s.ids))

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := c.Validate(); err != nil {
			return err
		}

		return tx.CourseTypes().Create(ctx, c)
	})
	if err != nil {
		return domain.CourseType{}, fmt.Errorf("create course type: %w", err)
	}

	return c, nil
}

// UpdateCourseType replaces a course type's name and description. Classes
// and lessons refer to it by ID and show the new name.
func (s *CatalogService) UpdateCourseType(
	ctx context.Context, id domain.CourseTypeID, d CourseTypeData,
) (domain.CourseType, error) {
	c := d.courseType(id)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// An unknown course type is a 404 before anything about its fields.
		if _, err := tx.CourseTypes().Get(ctx, id); err != nil {
			return err
		}

		if err := c.Validate(); err != nil {
			return err
		}

		return tx.CourseTypes().Update(ctx, c)
	})
	if err != nil {
		return domain.CourseType{}, fmt.Errorf("update course type: %w", err)
	}

	return c, nil
}

// DeleteCourseType removes a course type that no class or lesson uses; a
// used one is domain.ErrInUse.
func (s *CatalogService) DeleteCourseType(ctx context.Context, id domain.CourseTypeID) error {
	err := s.remove(ctx, func(ctx context.Context, tx store.Tx) error { return tx.CourseTypes().Delete(ctx, id) })
	if err != nil {
		return fmt.Errorf("delete course type: %w", err)
	}

	return nil
}

// StaffData are the fields the office edits on a staff member. PersonID is
// zero for staff without a person record.
type StaffData struct {
	DisplayName string
	Kind        domain.StaffKind
	PersonID    domain.PersonID
	Active      bool
}

// StaffEntry is a staff member as the office sees it: with the linked
// person, which is zero when there is none.
type StaffEntry struct {
	domain.Staff

	Person domain.Person
}

// AllStaff returns all staff, former ones included, with their persons,
// ordered by display name: the office's list, where the public one (Staff)
// shows only the active staff.
func (s *CatalogService) AllStaff(ctx context.Context) ([]StaffEntry, error) {
	var entries []StaffEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		staff, err := tx.Staff().List(ctx)
		if err != nil {
			return err
		}

		entries = make([]StaffEntry, 0, len(staff))
		for _, m := range staff {
			e, err := staffEntry(ctx, tx, m)
			if err != nil {
				return err
			}

			entries = append(entries, e)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list all staff: %w", err)
	}

	return entries, nil
}

// CreateStaff adds a staff member. An unknown person is a validation error
// on personId.
func (s *CatalogService) CreateStaff(ctx context.Context, d StaffData) (StaffEntry, error) {
	e := StaffEntry{Staff: d.staff(ids.New[domain.StaffID](s.ids))}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := e.Validate(); err != nil {
			return err
		}

		var err error
		if e.Person, err = linkedPerson(ctx, tx, e.PersonID); err != nil {
			return err
		}

		return staffWriteErr(tx.Staff().Create(ctx, e.Staff))
	})
	if err != nil {
		return StaffEntry{}, fmt.Errorf("create staff: %w", err)
	}

	return e, nil
}

// UpdateStaff replaces a staff member's name, kind, person link and whether
// they are active. Deactivated staff leave the public lists but stay on
// their lessons, past and future (ADR 0007).
func (s *CatalogService) UpdateStaff(ctx context.Context, id domain.StaffID, d StaffData) (StaffEntry, error) {
	e := StaffEntry{Staff: d.staff(id)}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// An unknown staff member is a 404 before anything about the fields.
		if _, err := tx.Staff().Get(ctx, id); err != nil {
			return err
		}

		if err := e.Validate(); err != nil {
			return err
		}

		var err error
		if e.Person, err = linkedPerson(ctx, tx, e.PersonID); err != nil {
			return err
		}

		return staffWriteErr(tx.Staff().Update(ctx, e.Staff))
	})
	if err != nil {
		return StaffEntry{}, fmt.Errorf("update staff: %w", err)
	}

	return e, nil
}

// DeleteStaff removes a staff member no class or lesson refers to, such as
// one created by mistake; a referenced one is domain.ErrInUse and is
// deactivated instead (ADR 0007).
func (s *CatalogService) DeleteStaff(ctx context.Context, id domain.StaffID) error {
	err := s.remove(ctx, func(ctx context.Context, tx store.Tx) error { return tx.Staff().Delete(ctx, id) })
	if err != nil {
		return fmt.Errorf("delete staff: %w", err)
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

// errUnknownPerson is a staff member's person that does not exist.
var errUnknownPerson error = &domain.FieldError{Field: "personId", Msg: "person does not exist"}

// linkedPerson locks and returns the person a staff member links to, or
// the zero person for a zero ID. An unknown or merged person is a
// validation error on the field that names it.
func linkedPerson(ctx context.Context, tx store.Tx, id domain.PersonID) (domain.Person, error) {
	if id.IsZero() {
		return domain.Person{}, nil
	}

	// A staff link makes a dependent managed: under the lock, a guardian's
	// edit or removal of the person waits for it (ADR 0008).
	p, err := lockLivePerson(ctx, tx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Person{}, errUnknownPerson
	}

	return p, err
}

// staffEntry adds the linked person to a stored staff member.
func staffEntry(ctx context.Context, tx store.Tx, m domain.Staff) (StaffEntry, error) {
	if m.PersonID.IsZero() {
		return StaffEntry{Staff: m}, nil
	}

	p, err := tx.Persons().Get(ctx, m.PersonID)
	if err != nil {
		return StaffEntry{}, fmt.Errorf("person of staff %s: %w", m.ID, err)
	}

	return StaffEntry{Staff: m, Person: p}, nil
}

// staffWriteErr maps the result of a staff write after linkedPerson: the
// store's only conflict is a missing person, deleted by someone else in the
// meantime, and reported like an unknown one.
func staffWriteErr(err error) error {
	if errors.Is(err, domain.ErrConflict) {
		return fmt.Errorf("%w (%w)", errUnknownPerson, err)
	}

	return err
}

func (d LocationData) location(id domain.LocationID) domain.Location {
	return domain.Location{ID: id, Name: strings.TrimSpace(d.Name), Address: strings.TrimSpace(d.Address)}
}

func (d RoomData) room(id domain.RoomID) domain.Room {
	return domain.Room{ID: id, LocationID: d.LocationID, Name: strings.TrimSpace(d.Name), Capacity: d.Capacity}
}

func (d CourseTypeData) courseType(id domain.CourseTypeID) domain.CourseType {
	return domain.CourseType{ID: id, Name: strings.TrimSpace(d.Name), Description: strings.TrimSpace(d.Description)}
}

func (d StaffData) staff(id domain.StaffID) domain.Staff {
	return domain.Staff{
		ID: id, PersonID: d.PersonID, DisplayName: strings.TrimSpace(d.DisplayName), Kind: d.Kind, Active: d.Active,
	}
}
