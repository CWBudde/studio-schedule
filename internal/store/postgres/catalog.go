package postgres

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- locations

type locationRepo struct{ q *sqlc.Queries }

func toLocation(row sqlc.Location) domain.Location {
	return domain.Location{ID: fromPgID[domain.LocationID](row.ID), Name: row.Name, Address: row.Address.String}
}

func (r locationRepo) Get(ctx context.Context, id domain.LocationID) (domain.Location, error) {
	row, err := r.q.GetLocation(ctx, pgID(id))
	if err != nil {
		return domain.Location{}, notFound("location", err)
	}

	return toLocation(row), nil
}

func (r locationRepo) Create(ctx context.Context, l domain.Location) error {
	return mapErr(r.q.CreateLocation(ctx, sqlc.CreateLocationParams{
		ID: pgID(l.ID), Name: l.Name, Address: pgText(l.Address),
	}))
}

func (r locationRepo) Update(ctx context.Context, l domain.Location) error {
	n, err := r.q.UpdateLocation(ctx, sqlc.UpdateLocationParams{
		ID: pgID(l.ID), Name: l.Name, Address: pgText(l.Address),
	})

	return affected("location", n, err)
}

func (r locationRepo) Delete(ctx context.Context, id domain.LocationID) error {
	n, err := r.q.DeleteLocation(ctx, pgID(id))

	return affected("location", n, err)
}

func (r locationRepo) List(ctx context.Context) ([]domain.Location, error) {
	rows, err := r.q.ListLocations(ctx)

	return convertRows(rows, err, toLocation)
}

// ---------------------------------------------------------------- rooms

type roomRepo struct{ q *sqlc.Queries }

func toRoom(row sqlc.Room) domain.Room {
	return domain.Room{
		ID:         fromPgID[domain.RoomID](row.ID),
		LocationID: fromPgID[domain.LocationID](row.LocationID),
		Name:       row.Name,
		Capacity:   int(row.Capacity),
		Position:   int(row.Position),
	}
}

func (r roomRepo) Get(ctx context.Context, id domain.RoomID) (domain.Room, error) {
	row, err := r.q.GetRoom(ctx, pgID(id))
	if err != nil {
		return domain.Room{}, notFound("room", err)
	}

	return toRoom(row), nil
}

func (r roomRepo) Create(ctx context.Context, rm domain.Room) error {
	capacity, position, err := roomNumbers(rm)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateRoom(ctx, sqlc.CreateRoomParams{
		ID: pgID(rm.ID), LocationID: pgID(rm.LocationID), Name: rm.Name, Capacity: capacity, Position: position,
	}))
}

func (r roomRepo) Update(ctx context.Context, rm domain.Room) error {
	capacity, position, err := roomNumbers(rm)
	if err != nil {
		return err
	}

	n, err := r.q.UpdateRoom(ctx, sqlc.UpdateRoomParams{
		ID: pgID(rm.ID), LocationID: pgID(rm.LocationID), Name: rm.Name, Capacity: capacity, Position: position,
	})

	return affected("room", n, err)
}

// roomNumbers converts the room's capacity and position for their integer
// columns.
func roomNumbers(rm domain.Room) (capacity, position int32, err error) {
	if capacity, err = int4(rm.Capacity); err != nil {
		return 0, 0, err
	}

	position, err = int4(rm.Position)

	return capacity, position, err
}

func (r roomRepo) Delete(ctx context.Context, id domain.RoomID) error {
	n, err := r.q.DeleteRoom(ctx, pgID(id))

	return affected("room", n, err)
}

func (r roomRepo) List(ctx context.Context, location domain.LocationID) ([]domain.Room, error) {
	if location.IsZero() {
		rows, err := r.q.ListRooms(ctx)

		return convertRows(rows, err, toRoom)
	}

	rows, err := r.q.ListRoomsByLocation(ctx, pgID(location))

	return convertRows(rows, err, toRoom)
}

func (r roomRepo) SetOrder(ctx context.Context, location domain.LocationID, order []domain.RoomID) error {
	rows, err := r.q.LockRoomsByLocation(ctx, pgID(location))
	if err != nil {
		return mapErr(err)
	}

	open := make(map[domain.RoomID]bool, len(rows))
	for _, row := range rows {
		open[fromPgID[domain.RoomID](row.ID)] = true
	}

	for _, id := range order {
		if !open[id] {
			return fmt.Errorf("room order: %s is not a room of the location or repeated: %w", id, domain.ErrValidation)
		}

		delete(open, id)
	}

	if len(open) > 0 {
		return fmt.Errorf("room order: %d rooms of the location are missing: %w", len(open), domain.ErrValidation)
	}

	var position int32
	for _, id := range order {
		position++
		if err := r.q.SetRoomPosition(ctx, sqlc.SetRoomPositionParams{ID: pgID(id), Position: position}); err != nil {
			return mapErr(err)
		}
	}

	return nil
}

// ---------------------------------------------------------------- staff

type staffRepo struct{ q *sqlc.Queries }

func toStaff(row sqlc.Staff) domain.Staff {
	return domain.Staff{
		ID:          fromPgID[domain.StaffID](row.ID),
		PersonID:    fromPgID[domain.PersonID](row.PersonID),
		DisplayName: row.DisplayName,
		Kind:        domain.StaffKind(row.Kind),
		Active:      row.Active,
	}
}

func (r staffRepo) Get(ctx context.Context, id domain.StaffID) (domain.Staff, error) {
	row, err := r.q.GetStaff(ctx, pgID(id))
	if err != nil {
		return domain.Staff{}, notFound("staff", err)
	}

	return toStaff(row), nil
}

func (r staffRepo) Create(ctx context.Context, s domain.Staff) error {
	return mapErr(r.q.CreateStaff(ctx, sqlc.CreateStaffParams{
		ID: pgID(s.ID), PersonID: pgID(s.PersonID), DisplayName: s.DisplayName, Kind: string(s.Kind), Active: s.Active,
	}))
}

func (r staffRepo) Update(ctx context.Context, s domain.Staff) error {
	n, err := r.q.UpdateStaff(ctx, sqlc.UpdateStaffParams{
		ID: pgID(s.ID), PersonID: pgID(s.PersonID), DisplayName: s.DisplayName, Kind: string(s.Kind), Active: s.Active,
	})

	return affected("staff", n, err)
}

func (r staffRepo) Delete(ctx context.Context, id domain.StaffID) error {
	n, err := r.q.DeleteStaff(ctx, pgID(id))

	return affected("staff", n, err)
}

func (r staffRepo) List(ctx context.Context) ([]domain.Staff, error) {
	rows, err := r.q.ListStaff(ctx)

	return convertRows(rows, err, toStaff)
}

func (r staffRepo) ListByPerson(ctx context.Context, person domain.PersonID) ([]domain.Staff, error) {
	rows, err := r.q.ListStaffByPerson(ctx, pgID(person))

	return convertRows(rows, err, toStaff)
}

// ---------------------------------------------------------------- course types

type courseTypeRepo struct{ q *sqlc.Queries }

func toCourseType(row sqlc.CourseType) domain.CourseType {
	return domain.CourseType{
		ID: fromPgID[domain.CourseTypeID](row.ID), Name: row.Name, Description: row.Description.String,
	}
}

func (r courseTypeRepo) Get(ctx context.Context, id domain.CourseTypeID) (domain.CourseType, error) {
	row, err := r.q.GetCourseType(ctx, pgID(id))
	if err != nil {
		return domain.CourseType{}, notFound("course type", err)
	}

	return toCourseType(row), nil
}

func (r courseTypeRepo) Create(ctx context.Context, c domain.CourseType) error {
	return mapErr(r.q.CreateCourseType(ctx, sqlc.CreateCourseTypeParams{
		ID: pgID(c.ID), Name: c.Name, Description: pgText(c.Description),
	}))
}

func (r courseTypeRepo) Update(ctx context.Context, c domain.CourseType) error {
	n, err := r.q.UpdateCourseType(ctx, sqlc.UpdateCourseTypeParams{
		ID: pgID(c.ID), Name: c.Name, Description: pgText(c.Description),
	})

	return affected("course type", n, err)
}

func (r courseTypeRepo) Delete(ctx context.Context, id domain.CourseTypeID) error {
	n, err := r.q.DeleteCourseType(ctx, pgID(id))

	return affected("course type", n, err)
}

func (r courseTypeRepo) List(ctx context.Context) ([]domain.CourseType, error) {
	rows, err := r.q.ListCourseTypes(ctx)

	return convertRows(rows, err, toCourseType)
}
