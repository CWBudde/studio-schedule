package memory

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/store"
)

// checkedContext reports, by closing checked, the first time InTx asks for
// its error: InTx asks once before it takes the store's lock and once after.
type checkedContext struct {
	context.Context

	once    sync.Once
	checked chan struct{}
}

func (c *checkedContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })

	return err
}

// TestInTxCanceledWhileWaiting pins that a transaction whose context is
// canceled while it waits for the store's lock fails with the context's
// error and never runs its function.
//
// No sleeps: the first transaction holds the lock until the second has
// passed its check before the lock (checked), then cancels the second's
// context and only then returns. Whether the second has reached Lock by then
// or not, it takes the lock only after the cancellation.
func TestInTxCanceledWhileWaiting(t *testing.T) {
	t.Parallel()

	st := New()

	inner, cancel := context.WithCancel(t.Context())
	defer cancel()

	second := &checkedContext{Context: inner, checked: make(chan struct{})}

	holding := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		firstDone <- st.InTx(t.Context(), store.ReadWrite, func(context.Context, store.Tx) error {
			close(holding)
			<-second.checked
			cancel()

			return nil
		})
	}()

	<-holding

	ran := false

	err := st.InTx(second, store.ReadWrite, func(context.Context, store.Tx) error {
		ran = true

		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("InTx = %v, want context.Canceled", err)
	}

	if ran {
		t.Error("the canceled transaction ran its function")
	}

	if err := <-firstDone; err != nil {
		t.Errorf("the transaction holding the lock: %v", err)
	}

	// The lock is free again.
	if err := st.InTx(t.Context(), store.ReadOnly, func(context.Context, store.Tx) error { return nil }); err != nil {
		t.Errorf("InTx after the cancellation: %v", err)
	}
}
