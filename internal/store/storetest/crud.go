package storetest

import (
	"context"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// crud adapts one repository with ID-keyed records to the generic cases of
// testCRUD.
type crud[I domain.AnyID, E comparable] struct {
	id     func(E) I
	get    func(ctx context.Context, tx store.Tx, id I) (E, error)
	create func(ctx context.Context, tx store.Tx, v E) error
	update func(ctx context.Context, tx store.Tx, v E) error
	// remove is nil for repositories without Delete.
	remove func(ctx context.Context, tx store.Tx, id I) error
	// fixtures returns two versions of one entity with the same ID, creating
	// the records they reference: minimal leaves every optional field zero,
	// full sets all of them. Neither is created.
	fixtures func(e *env) (minimal, full E)
}

// testCRUD checks create, read, update and delete of single records,
// including NULL handling (optional fields round-trip in both directions),
// rollback of every mutation and read-only transactions.
func testCRUD[I domain.AnyID, E comparable](t *testing.T, newStore func(*testing.T) store.Store, c crud[I, E]) {
	t.Helper()

	get := func(e *env, id I) (E, error) {
		return query(e, func(ctx context.Context, tx store.Tx) (E, error) { return c.get(ctx, tx, id) })
	}
	create := func(e *env, v E) {
		e.t.Helper()
		e.mustWrite(func(ctx context.Context, tx store.Tx) error { return c.create(ctx, tx, v) })
	}
	mustGet := func(e *env, id I) E {
		e.t.Helper()

		v, err := get(e, id)
		if err != nil {
			e.t.Fatalf("get: %v", err)
		}

		return v
	}

	cs := map[string]func(e *env){
		"GetMissingIsNotFound": func(e *env) {
			_, err := get(e, newID[I](e))
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"CreateThenGet": func(e *env) {
			minimal, _ := c.fixtures(e)
			create(e, minimal)
			wantEqual(e.t, mustGet(e, c.id(minimal)), minimal)
		},
		"CreateFullThenGet": func(e *env) {
			_, full := c.fixtures(e)
			create(e, full)
			wantEqual(e.t, mustGet(e, c.id(full)), full)
		},
		"CreateDuplicateIDIsConflict": func(e *env) {
			minimal, _ := c.fixtures(e)
			create(e, minimal)

			err := e.write(func(ctx context.Context, tx store.Tx) error { return c.create(ctx, tx, minimal) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"UpdateSetsAndClearsOptionalFields": func(e *env) {
			minimal, full := c.fixtures(e)
			create(e, minimal)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return c.update(ctx, tx, full) })
			wantEqual(e.t, mustGet(e, c.id(full)), full)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return c.update(ctx, tx, minimal) })
			wantEqual(e.t, mustGet(e, c.id(minimal)), minimal)
		},
		"UpdateMissingIsNotFound": func(e *env) {
			minimal, _ := c.fixtures(e)

			err := e.write(func(ctx context.Context, tx store.Tx) error { return c.update(ctx, tx, minimal) })
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"RollbackCreate": func(e *env) {
			minimal, _ := c.fixtures(e)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				if err := c.create(ctx, tx, minimal); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)

			_, err = get(e, c.id(minimal))
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"RollbackUpdate": func(e *env) {
			minimal, full := c.fixtures(e)
			create(e, minimal)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				if err := c.update(ctx, tx, full); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)
			wantEqual(e.t, mustGet(e, c.id(minimal)), minimal)
		},
		"ReadOnlyRejectsWrites": func(e *env) {
			minimal, full := c.fixtures(e)

			err := e.read(func(ctx context.Context, tx store.Tx) error { return c.create(ctx, tx, minimal) })
			wantErr(e.t, err, store.ErrReadOnly)

			create(e, minimal)

			err = e.read(func(ctx context.Context, tx store.Tx) error { return c.update(ctx, tx, full) })
			wantErr(e.t, err, store.ErrReadOnly)

			if c.remove != nil {
				err = e.read(func(ctx context.Context, tx store.Tx) error { return c.remove(ctx, tx, c.id(minimal)) })
				wantErr(e.t, err, store.ErrReadOnly)
			}

			wantEqual(e.t, mustGet(e, c.id(minimal)), minimal)
		},
	}

	if c.remove != nil {
		cs["DeleteThenGetIsNotFound"] = func(e *env) {
			minimal, _ := c.fixtures(e)
			create(e, minimal)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return c.remove(ctx, tx, c.id(minimal)) })

			_, err := get(e, c.id(minimal))
			wantErr(e.t, err, domain.ErrNotFound)
		}
		cs["DeleteMissingIsNotFound"] = func(e *env) {
			err := e.write(func(ctx context.Context, tx store.Tx) error { return c.remove(ctx, tx, newID[I](e)) })
			wantErr(e.t, err, domain.ErrNotFound)
		}
		cs["RollbackDelete"] = func(e *env) {
			minimal, _ := c.fixtures(e)
			create(e, minimal)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				if err := c.remove(ctx, tx, c.id(minimal)); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)
			wantEqual(e.t, mustGet(e, c.id(minimal)), minimal)
		}
	}

	cases(t, newStore, cs)
}
