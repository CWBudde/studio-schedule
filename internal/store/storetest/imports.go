package storetest

import (
	"bytes"
	"cmp"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testImportRuns(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	list := func(e *env, limit int) []domain.ImportRun {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.ImportRun, error) {
			return tx.Imports().List(ctx, limit)
		})
	}

	create := func(e *env, r domain.ImportRun) error {
		return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Imports().Create(ctx, r) })
	}

	run := func(e *env, actor domain.AccountID, at time.Time) domain.ImportRun {
		e.t.Helper()

		r := domain.ImportRun{
			ID: newID[domain.ImportRunID](e), FileName: "members.csv", ActorID: actor, StartedAt: at,
			FinishedAt: at.Add(2 * time.Second), Counts: domain.ImportCounts{Rows: 1, Unchanged: 1},
		}
		valid(e.t, r)

		if err := create(e, r); err != nil {
			e.t.Fatalf("create import run: %v", err)
		}

		return r
	}

	cases(t, newStore, map[string]func(e *env){
		"Empty": func(e *env) { wantEqual(e.t, len(list(e, 10)), 0) },
		"RoundTrip": func(e *env) {
			actor := e.account(e.person("Olga", "Office"), "olga@example.org")
			r := domain.ImportRun{
				ID: newID[domain.ImportRunID](e), FileName: "Mitglieder 2026.csv", ActorID: actor.ID,
				// A fraction of a microsecond is cut off like in PostgreSQL.
				StartedAt: t0.Add(500 * time.Nanosecond), FinishedAt: t0.Add(time.Second),
				Counts: domain.ImportCounts{
					Rows: 9, Created: 3, Updated: 2, Unchanged: 1, Failed: 3, GuardiansCreated: 2, GuardiansUpdated: 1,
					LinksAdded: 4,
				},
				Errors: []domain.ImportRowError{
					{Line: 4, Code: "required", Field: "givenName"}, {Line: 7, Code: "member-no-repeated"},
				},
			}
			valid(e.t, r)

			if err := create(e, r); err != nil {
				e.t.Fatalf("create: %v", err)
			}

			got := list(e, 10)
			if len(got) != 1 {
				e.t.Fatalf("got %d runs, want 1", len(got))
			}

			r.StartedAt = t0
			wantEqual(e.t, got[0].Counts, r.Counts)
			wantSlice(e.t, got[0].Errors, r.Errors)

			got[0].Errors, r.Errors = nil, nil
			wantEqual(e.t, got[0].ID, r.ID)
			wantEqual(e.t, got[0].FileName, r.FileName)
			wantEqual(e.t, got[0].ActorID, r.ActorID)
			wantEqual(e.t, got[0].StartedAt, r.StartedAt)
			wantEqual(e.t, got[0].FinishedAt, r.FinishedAt)
		},
		"OrderAndLimit": func(e *env) {
			// The command line runs without an account.
			older := run(e, domain.AccountID{}, t0)
			newest := run(e, domain.AccountID{}, t0.Add(time.Hour))
			same := run(e, domain.AccountID{}, t0) // same start: the ID decides

			ids := func(runs []domain.ImportRun) []domain.ImportRunID {
				out := make([]domain.ImportRunID, len(runs))
				for i, r := range runs {
					out[i] = r.ID
				}

				return out
			}

			wantSlice(e.t, ids(list(e, 10)), []domain.ImportRunID{newest.ID, same.ID, older.ID})
			wantSlice(e.t, ids(list(e, 2)), []domain.ImportRunID{newest.ID, same.ID})
			wantSlice(e.t, ids(list(e, 0)), nil)
		},
		"Conflicts": func(e *env) {
			r := run(e, domain.AccountID{}, t0)
			wantErr(e.t, create(e, r), domain.ErrConflict)

			r.ID, r.ActorID = newID[domain.ImportRunID](e), newID[domain.AccountID](e)
			wantErr(e.t, create(e, r), domain.ErrConflict)

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Imports().Create(ctx, r) })
			wantErr(e.t, err, store.ErrReadOnly)
		},
	})
}

func testDuplicateDismissals(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	list := func(e *env) []domain.DuplicateDismissal {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.DuplicateDismissal, error) {
			return tx.DuplicateDismissals().List(ctx)
		})
	}

	dismiss := func(e *env, d domain.DuplicateDismissal) error {
		return e.write(func(ctx context.Context, tx store.Tx) error { return tx.DuplicateDismissals().Create(ctx, d) })
	}

	cases(t, newStore, map[string]func(e *env){
		"Empty": func(e *env) { wantSlice(e.t, list(e), nil) },
		"CreateKeepsTheFirst": func(e *env) {
			office := e.account(e.person("Olga", "Office"), "olga@example.org")
			a, b, c := e.person("Ada", "Same"), e.person("Ada", "Same"), e.person("Ada", "Same")

			first := domain.NewDuplicateDismissal(b.ID, a.ID, office.ID, t0)
			valid(e.t, first)

			if err := dismiss(e, first); err != nil {
				e.t.Fatalf("dismiss: %v", err)
			}

			if err := dismiss(e, domain.NewDuplicateDismissal(a.ID, b.ID, office.ID, t0.Add(time.Hour))); err != nil {
				e.t.Fatalf("dismiss again: %v", err)
			}

			second := domain.NewDuplicateDismissal(c.ID, a.ID, office.ID, t0)
			if err := dismiss(e, second); err != nil {
				e.t.Fatalf("dismiss: %v", err)
			}

			third := domain.NewDuplicateDismissal(c.ID, b.ID, office.ID, t0)
			if err := dismiss(e, third); err != nil {
				e.t.Fatalf("dismiss: %v", err)
			}

			// Ordered by the pair; the second dismissal of a and b kept the first.
			wantSlice(e.t, list(e), []domain.DuplicateDismissal{first, second, third})
		},
		"GoesWithAPerson": func(e *env) {
			office := e.account(e.person("Olga", "Office"), "olga@example.org")
			a, b, c := e.person("Ada", "Same"), e.person("Ada", "Same"), e.person("Ada", "Same")
			kept := domain.NewDuplicateDismissal(a.ID, c.ID, office.ID, t0)

			for _, d := range []domain.DuplicateDismissal{domain.NewDuplicateDismissal(a.ID, b.ID, office.ID, t0), kept} {
				if err := dismiss(e, d); err != nil {
					e.t.Fatalf("dismiss: %v", err)
				}
			}

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Delete(ctx, b.ID) })
			wantSlice(e.t, list(e), []domain.DuplicateDismissal{kept})
		},
		"ListByPersonAndDelete": func(e *env) {
			office := e.account(e.person("Olga", "Office"), "olga@example.org")
			a, b, c := e.person("Ada", "Same"), e.person("Ada", "Same"), e.person("Ada", "Same")
			ab := domain.NewDuplicateDismissal(a.ID, b.ID, office.ID, t0)
			bc := domain.NewDuplicateDismissal(b.ID, c.ID, office.ID, t0)
			ac := domain.NewDuplicateDismissal(a.ID, c.ID, office.ID, t0)

			for _, d := range []domain.DuplicateDismissal{ab, bc, ac} {
				if err := dismiss(e, d); err != nil {
					e.t.Fatalf("dismiss: %v", err)
				}
			}

			byPerson := func(p domain.PersonID) []domain.DuplicateDismissal {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.DuplicateDismissal, error) {
					return tx.DuplicateDismissals().ListByPerson(ctx, p)
				})
			}

			wantSlice(e.t, byPerson(b.ID), sortedDismissals(ab, bc))

			// Named in either order.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.DuplicateDismissals().Delete(ctx, c.ID, b.ID)
			})
			wantSlice(e.t, byPerson(b.ID), []domain.DuplicateDismissal{ab})
			wantSlice(e.t, list(e), sortedDismissals(ab, ac))

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.DuplicateDismissals().Delete(ctx, b.ID, c.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.read(func(ctx context.Context, tx store.Tx) error {
				return tx.DuplicateDismissals().Delete(ctx, a.ID, b.ID)
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"Conflicts": func(e *env) {
			office := e.account(e.person("Olga", "Office"), "olga@example.org")
			a := e.person("Ada", "Same")

			err := dismiss(e, domain.NewDuplicateDismissal(a.ID, newID[domain.PersonID](e), office.ID, t0))
			wantErr(e.t, err, domain.ErrConflict)

			err = dismiss(e, domain.NewDuplicateDismissal(a.ID, e.person("Ada", "Same").ID, newID[domain.AccountID](e), t0))
			wantErr(e.t, err, domain.ErrConflict)

			err = e.read(func(ctx context.Context, tx store.Tx) error {
				return tx.DuplicateDismissals().Create(ctx, domain.NewDuplicateDismissal(a.ID, office.PersonID, office.ID, t0))
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
	})
}

// sortedDismissals orders dismissals like DuplicateDismissalRepo.List.
func sortedDismissals(ds ...domain.DuplicateDismissal) []domain.DuplicateDismissal {
	slices.SortFunc(ds, func(x, y domain.DuplicateDismissal) int {
		return cmp.Or(bytes.Compare(x.PersonA[:], y.PersonA[:]), bytes.Compare(x.PersonB[:], y.PersonB[:]))
	})

	return ds
}
