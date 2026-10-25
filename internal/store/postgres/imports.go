package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- import runs

type importRunRepo struct{ q *sqlc.Queries }

func (r importRunRepo) Create(ctx context.Context, run domain.ImportRun) error {
	var rowErrors []byte // NULL without errors

	if len(run.Errors) > 0 {
		var err error
		if rowErrors, err = json.Marshal(run.Errors); err != nil {
			return fmt.Errorf("postgres: import errors: %w", err)
		}
	}

	c := run.Counts

	counts := make([]int32, 0, 8)

	for _, n := range []int{
		c.Rows, c.Created, c.Updated, c.Unchanged, c.Failed, c.GuardiansCreated, c.GuardiansUpdated, c.LinksAdded,
	} {
		v, err := int4(n)
		if err != nil {
			return err
		}

		counts = append(counts, v)
	}

	return mapErr(r.q.CreateImportRun(ctx, sqlc.CreateImportRunParams{
		ID:               pgID(run.ID),
		FileName:         run.FileName,
		ActorID:          pgID(run.ActorID),
		StartedAt:        pgInstant(run.StartedAt),
		FinishedAt:       pgInstant(run.FinishedAt),
		RowsTotal:        counts[0],
		Created:          counts[1],
		Updated:          counts[2],
		Unchanged:        counts[3],
		Failed:           counts[4],
		GuardiansCreated: counts[5],
		GuardiansUpdated: counts[6],
		LinksAdded:       counts[7],
		Errors:           rowErrors,
	}))
}

func (r importRunRepo) List(ctx context.Context, limit int) ([]domain.ImportRun, error) {
	rows, err := r.q.ListImportRuns(ctx, rowLimit(limit))
	if err != nil {
		return nil, mapErr(err)
	}

	runs := make([]domain.ImportRun, 0, len(rows))

	for _, row := range rows {
		run := domain.ImportRun{
			ID:         fromPgID[domain.ImportRunID](row.ID),
			FileName:   row.FileName,
			ActorID:    fromPgID[domain.AccountID](row.ActorID),
			StartedAt:  fromPgInstant(row.StartedAt),
			FinishedAt: fromPgInstant(row.FinishedAt),
			Counts: domain.ImportCounts{
				Rows: int(row.RowsTotal), Created: int(row.Created), Updated: int(row.Updated),
				Unchanged: int(row.Unchanged), Failed: int(row.Failed), GuardiansCreated: int(row.GuardiansCreated),
				GuardiansUpdated: int(row.GuardiansUpdated), LinksAdded: int(row.LinksAdded),
			},
		}

		if row.Errors != nil {
			if err := json.Unmarshal(row.Errors, &run.Errors); err != nil {
				return nil, fmt.Errorf("postgres: errors of import run %v: %w", run.ID, err)
			}
		}

		runs = append(runs, run)
	}

	return runs, nil
}

// ---------------------------------------------------------------- duplicate dismissals

type dismissalRepo struct{ q *sqlc.Queries }

func (r dismissalRepo) Create(ctx context.Context, d domain.DuplicateDismissal) error {
	return mapErr(r.q.CreateDuplicateDismissal(ctx, sqlc.CreateDuplicateDismissalParams{
		PersonA:     pgID(d.PersonA),
		PersonB:     pgID(d.PersonB),
		DismissedBy: pgID(d.DismissedBy),
		DismissedAt: pgInstant(d.DismissedAt),
	}))
}

func (r dismissalRepo) List(ctx context.Context) ([]domain.DuplicateDismissal, error) {
	rows, err := r.q.ListDuplicateDismissals(ctx)

	return convertRows(rows, err, toDismissal)
}

func (r dismissalRepo) ListByPerson(ctx context.Context, person domain.PersonID) ([]domain.DuplicateDismissal, error) {
	rows, err := r.q.ListDuplicateDismissalsByPerson(ctx, pgID(person))

	return convertRows(rows, err, toDismissal)
}

func (r dismissalRepo) Delete(ctx context.Context, a, b domain.PersonID) error {
	d := domain.NewDuplicateDismissal(a, b, domain.AccountID{}, time.Time{})
	n, err := r.q.DeleteDuplicateDismissal(ctx, sqlc.DeleteDuplicateDismissalParams{
		PersonA: pgID(d.PersonA), PersonB: pgID(d.PersonB),
	})

	return affected("duplicate dismissal", n, err)
}

func toDismissal(row sqlc.PersonDuplicateDismissal) domain.DuplicateDismissal {
	return domain.DuplicateDismissal{
		PersonA:     fromPgID[domain.PersonID](row.PersonA),
		PersonB:     fromPgID[domain.PersonID](row.PersonB),
		DismissedBy: fromPgID[domain.AccountID](row.DismissedBy),
		DismissedAt: fromPgInstant(row.DismissedAt),
	}
}
