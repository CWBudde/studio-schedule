package postgres

import (
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- terms

type termRepo struct{ q *sqlc.Queries }

func toTerm(row sqlc.Term) domain.Term {
	return domain.Term{
		ID:       fromPgID[domain.TermID](row.ID),
		Name:     row.Name,
		StartsOn: fromPgDate(row.StartsOn),
		EndsOn:   fromPgDate(row.EndsOn),
	}
}

func (r termRepo) Get(ctx context.Context, id domain.TermID) (domain.Term, error) {
	row, err := r.q.GetTerm(ctx, pgID(id))
	if err != nil {
		return domain.Term{}, notFound("term", err)
	}

	return toTerm(row), nil
}

func (r termRepo) Create(ctx context.Context, t domain.Term) error {
	return mapErr(r.q.CreateTerm(ctx, sqlc.CreateTermParams{
		ID: pgID(t.ID), Name: t.Name, StartsOn: pgDate(t.StartsOn), EndsOn: pgDate(t.EndsOn),
	}))
}

func (r termRepo) Update(ctx context.Context, t domain.Term) error {
	n, err := r.q.UpdateTerm(ctx, sqlc.UpdateTermParams{
		ID: pgID(t.ID), Name: t.Name, StartsOn: pgDate(t.StartsOn), EndsOn: pgDate(t.EndsOn),
	})

	return affected("term", n, err)
}

func (r termRepo) Delete(ctx context.Context, id domain.TermID) error {
	n, err := r.q.DeleteTerm(ctx, pgID(id))

	return affected("term", n, err)
}

func (r termRepo) List(ctx context.Context) ([]domain.Term, error) {
	rows, err := r.q.ListTerms(ctx)

	return convertRows(rows, err, toTerm)
}

// ---------------------------------------------------------------- classes

type classRepo struct{ q *sqlc.Queries }

func toClass(row sqlc.Class) domain.Class {
	return domain.Class{
		ID:            fromPgID[domain.ClassID](row.ID),
		TermID:        fromPgID[domain.TermID](row.TermID),
		CourseTypeID:  fromPgID[domain.CourseTypeID](row.CourseTypeID),
		RoomID:        fromPgID[domain.RoomID](row.RoomID),
		InstructorID:  fromPgID[domain.StaffID](row.InstructorID),
		AccompanistID: fromPgID[domain.StaffID](row.AccompanistID),
		Level:         domain.Level(row.Level),
		Weekday:       time.Weekday(row.Weekday),
		StartTime:     fromPgTimeOfDay(row.StartTime),
		Duration:      fromPgInterval(row.Duration),
		Capacity:      int(row.Capacity),
		Retired:       row.Retired,
	}
}

// classParams converts c to the parameters shared by insert and update.
func classParams(c domain.Class) (sqlc.CreateClassParams, error) {
	day, err := weekday(c.Weekday)
	if err != nil {
		return sqlc.CreateClassParams{}, err
	}

	capacity, err := int4(c.Capacity)
	if err != nil {
		return sqlc.CreateClassParams{}, err
	}

	return sqlc.CreateClassParams{
		ID:            pgID(c.ID),
		TermID:        pgID(c.TermID),
		CourseTypeID:  pgID(c.CourseTypeID),
		RoomID:        pgID(c.RoomID),
		InstructorID:  pgID(c.InstructorID),
		AccompanistID: pgID(c.AccompanistID),
		Level:         string(c.Level),
		Weekday:       day,
		StartTime:     pgTimeOfDay(c.StartTime),
		Duration:      pgInterval(c.Duration),
		Capacity:      capacity,
		Retired:       c.Retired,
	}, nil
}

func (r classRepo) Get(ctx context.Context, id domain.ClassID) (domain.Class, error) {
	row, err := r.q.GetClass(ctx, pgID(id))
	if err != nil {
		return domain.Class{}, notFound("class", err)
	}

	return toClass(row), nil
}

// Lock reads the class with SELECT … FOR UPDATE. In a read-only transaction
// PostgreSQL rejects the statement (SQLSTATE 25006, store.ErrReadOnly).
func (r classRepo) Lock(ctx context.Context, id domain.ClassID) (domain.Class, error) {
	row, err := r.q.LockClass(ctx, pgID(id))
	if err != nil {
		return domain.Class{}, notFound("class", err)
	}

	return toClass(row), nil
}

func (r classRepo) Create(ctx context.Context, c domain.Class) error {
	p, err := classParams(c)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateClass(ctx, p))
}

func (r classRepo) Update(ctx context.Context, c domain.Class) error {
	p, err := classParams(c)
	if err != nil {
		return err
	}

	n, err := r.q.UpdateClass(ctx, sqlc.UpdateClassParams(p))

	return affected("class", n, err)
}

func (r classRepo) Delete(ctx context.Context, id domain.ClassID) error {
	n, err := r.q.DeleteClass(ctx, pgID(id))

	return affected("class", n, err)
}

func (r classRepo) ListByTerm(ctx context.Context, term domain.TermID) ([]domain.Class, error) {
	rows, err := r.q.ListClassesByTerm(ctx, pgID(term))

	return convertRows(rows, err, toClass)
}

func (r classRepo) AddMember(ctx context.Context, m domain.ClassMember) error {
	return mapErr(r.q.AddClassMember(ctx, sqlc.AddClassMemberParams{
		ClassID: pgID(m.ClassID), PersonID: pgID(m.PersonID), Since: pgDate(m.Since),
	}))
}

func (r classRepo) RemoveMember(ctx context.Context, class domain.ClassID, person domain.PersonID) error {
	n, err := r.q.RemoveClassMember(ctx, sqlc.RemoveClassMemberParams{ClassID: pgID(class), PersonID: pgID(person)})

	return affected("class member", n, err)
}

func (r classRepo) ListMembers(ctx context.Context, class domain.ClassID) ([]domain.ClassMember, error) {
	rows, err := r.q.ListClassMembers(ctx, pgID(class))

	return convertRows(rows, err, func(row sqlc.ClassMember) domain.ClassMember {
		return domain.ClassMember{
			ClassID:  fromPgID[domain.ClassID](row.ClassID),
			PersonID: fromPgID[domain.PersonID](row.PersonID),
			Since:    fromPgDate(row.Since),
		}
	})
}
