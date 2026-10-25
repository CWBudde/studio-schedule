package memory

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ---------------------------------------------------------------- import runs

type importRunRepo struct{ tx *tx }

func (r importRunRepo) Create(_ context.Context, run domain.ImportRun) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case s.importRuns.has(run.ID):
		return conflict("import run %s exists", run.ID)
	case !run.ActorID.IsZero() && !s.accounts.has(run.ActorID):
		return conflict("import run references a missing account")
	}

	run.StartedAt, run.FinishedAt = instant(run.StartedAt), instant(run.FinishedAt)
	// Rows hold no references (memory.go): the store keeps its own copy of
	// the errors, and an empty list is none, like NULL in PostgreSQL.
	run.Errors = slices.Clip(slices.Clone(run.Errors))
	if len(run.Errors) == 0 {
		run.Errors = nil
	}

	s.importRuns.put(r.tx.gen, run.ID, run)

	return nil
}

func (r importRunRepo) List(_ context.Context, limit int) ([]domain.ImportRun, error) {
	runs := sorted(r.tx.state.importRuns.filter(all), func(a, b domain.ImportRun) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), compareID(b.ID, a.ID))
	})

	runs = runs[:min(len(runs), max(limit, 0))]
	for i := range runs {
		runs[i].Errors = slices.Clone(runs[i].Errors)
	}

	return runs, nil
}

// ---------------------------------------------------------------- duplicate dismissals

// pairKey is the key of a duplicate dismissal.
type pairKey struct{ a, b domain.PersonID }

type dismissalRepo struct{ tx *tx }

func (r dismissalRepo) Create(_ context.Context, d domain.DuplicateDismissal) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch key := (pairKey{d.PersonA, d.PersonB}); {
	case !s.persons.has(d.PersonA) || !s.persons.has(d.PersonB) || !s.accounts.has(d.DismissedBy):
		return conflict("duplicate dismissal references a missing record")
	case s.dismissals.has(key):
		return nil // the first stays
	default:
		d.DismissedAt = instant(d.DismissedAt)
		s.dismissals.put(r.tx.gen, key, d)

		return nil
	}
}

func (r dismissalRepo) List(context.Context) ([]domain.DuplicateDismissal, error) {
	return sorted(r.tx.state.dismissals.filter(all), compareDismissals), nil
}

func (r dismissalRepo) ListByPerson(_ context.Context, person domain.PersonID) ([]domain.DuplicateDismissal, error) {
	return sorted(r.tx.state.dismissals.filter(func(d domain.DuplicateDismissal) bool {
		return d.PersonA == person || d.PersonB == person
	}), compareDismissals), nil
}

func (r dismissalRepo) Delete(_ context.Context, a, b domain.PersonID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	d := domain.NewDuplicateDismissal(a, b, domain.AccountID{}, time.Time{})

	key := pairKey{d.PersonA, d.PersonB}
	if !s.dismissals.has(key) {
		return notFound("duplicate dismissal")
	}

	s.dismissals.delete(r.tx.gen, key)

	return nil
}

func compareDismissals(a, b domain.DuplicateDismissal) int {
	return cmp.Or(compareID(a.PersonA, b.PersonA), compareID(a.PersonB, b.PersonB))
}
