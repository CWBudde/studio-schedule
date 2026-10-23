package memory_test

import (
	"context"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/store/storetest"
)

func TestContract(t *testing.T) {
	t.Parallel()

	storetest.Run(t, func(*testing.T) store.Store { return memory.New() })
}

func TestRollbackOnPanic(t *testing.T) {
	t.Parallel()

	st := memory.New()

	func() {
		defer func() { _ = recover() }()

		_ = st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			if err := tx.Settings().Put(ctx, domain.Settings{SchoolName: "x"}); err != nil {
				return err
			}

			panic("boom")
		})
	}()

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, err := tx.Settings().Get(ctx)

		return err
	})
	if err == nil {
		t.Fatal("settings written by a panicking transaction must be rolled back")
	}
}

func TestCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	called := false

	err := memory.New().InTx(ctx, store.ReadOnly, func(context.Context, store.Tx) error {
		called = true

		return nil
	})
	if err == nil || called {
		t.Fatalf("InTx with canceled context: err=%v called=%v", err, called)
	}
}
