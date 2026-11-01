package postgres

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

type levelRepo struct{ q *sqlc.Queries }

func toLevel(row sqlc.Level) domain.Level {
	return domain.Level{
		ID:       fromPgID[domain.LevelID](row.ID),
		Name:     row.Name,
		Code:     row.Code.String,
		Position: int(row.Position),
	}
}

func (r levelRepo) Get(ctx context.Context, id domain.LevelID) (domain.Level, error) {
	row, err := r.q.GetLevel(ctx, pgID(id))
	if err != nil {
		return domain.Level{}, notFound("level", err)
	}

	return toLevel(row), nil
}

func (r levelRepo) Create(ctx context.Context, l domain.Level) error {
	position, err := int4(l.Position)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateLevel(ctx, sqlc.CreateLevelParams{
		ID: pgID(l.ID), Name: l.Name, Code: pgText(l.Code), Position: position,
	}))
}

// Update writes the name and the code; only SetOrder writes the position.
func (r levelRepo) Update(ctx context.Context, l domain.Level) error {
	n, err := r.q.UpdateLevel(ctx, sqlc.UpdateLevelParams{ID: pgID(l.ID), Name: l.Name, Code: pgText(l.Code)})

	return affected("level", n, err)
}

func (r levelRepo) Delete(ctx context.Context, id domain.LevelID) error {
	n, err := r.q.DeleteLevel(ctx, pgID(id))

	return affected("level", n, err)
}

func (r levelRepo) List(ctx context.Context) ([]domain.Level, error) {
	rows, err := r.q.ListLevels(ctx)

	return convertRows(rows, err, toLevel)
}

func (r levelRepo) SetOrder(ctx context.Context, order []domain.LevelID) error {
	rows, err := r.q.LockLevels(ctx)
	if err != nil {
		return mapErr(err)
	}

	open := make(map[domain.LevelID]bool, len(rows))
	for _, row := range rows {
		open[fromPgID[domain.LevelID](row.ID)] = true
	}

	for _, id := range order {
		if !open[id] {
			return fmt.Errorf("level order: %s is not a level or repeated: %w", id, domain.ErrValidation)
		}

		delete(open, id)
	}

	if len(open) > 0 {
		return fmt.Errorf("level order: %d levels are missing: %w", len(open), domain.ErrValidation)
	}

	var position int32
	for _, id := range order {
		position++

		n, err := r.q.SetLevelPosition(ctx, sqlc.SetLevelPositionParams{ID: pgID(id), Position: position})
		if err := affected("level", n, err); err != nil {
			return err
		}
	}

	return nil
}

func (r levelRepo) LockScale(ctx context.Context) error {
	locked, err := r.q.LockLevelScale(ctx)

	switch {
	case err != nil:
		return mapErr(err)
	case !locked:
		return store.ErrReadOnly
	}

	return nil
}
