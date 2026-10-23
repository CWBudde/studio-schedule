// Package storetest contains contract tests that every store.Store
// implementation must pass.
package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

var errBoom = errors.New("storetest: boom")

// Run executes the store contract tests. newStore must return a fresh, empty
// store for every call.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	t.Run("Settings", func(t *testing.T) { testSettings(t, newStore) })
}

func testSettings(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	want := domain.Settings{SchoolName: "Contract School", Timezone: "Europe/Berlin", DefaultLocale: "en"}

	t.Run("GetEmptyIsNotFound", func(t *testing.T) {
		st := newStore(t)

		_, err := getSettings(t.Context(), st)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Get on empty store: want ErrNotFound, got %v", err)
		}
	})

	t.Run("PutThenGet", func(t *testing.T) {
		st := newStore(t)
		mustPut(t, st, want)

		got, err := getSettings(t.Context(), st)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if got != want {
			t.Fatalf("Get = %+v, want %+v", got, want)
		}
	})

	t.Run("PutReplaces", func(t *testing.T) {
		st := newStore(t)
		mustPut(t, st, want)

		updated := want
		updated.SchoolName = "Renamed School"
		mustPut(t, st, updated)

		got, err := getSettings(t.Context(), st)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if got != updated {
			t.Fatalf("Get = %+v, want %+v", got, updated)
		}
	})

	t.Run("RollbackOnError", func(t *testing.T) {
		st := newStore(t)
		mustPut(t, st, want)

		err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			changed := want
			changed.SchoolName = "Must Not Persist"

			if err := tx.Settings().Put(ctx, changed); err != nil {
				return err
			}

			return errBoom
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("InTx: want callback error, got %v", err)
		}

		got, err := getSettings(t.Context(), st)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if got != want {
			t.Fatalf("after rollback Get = %+v, want %+v", got, want)
		}
	})

	t.Run("ReadOnlyRejectsWrites", func(t *testing.T) {
		st := newStore(t)

		err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			return tx.Settings().Put(ctx, want)
		})
		if err == nil {
			t.Fatal("Put in read-only transaction: want error, got nil")
		}

		if _, err := getSettings(t.Context(), st); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("after rejected write: want ErrNotFound, got %v", err)
		}
	})
}

func getSettings(ctx context.Context, st store.Store) (domain.Settings, error) {
	var s domain.Settings

	err := st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		s, err = tx.Settings().Get(ctx)

		return err
	})

	return s, err
}

func mustPut(t *testing.T, st store.Store, s domain.Settings) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, s)
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
}
