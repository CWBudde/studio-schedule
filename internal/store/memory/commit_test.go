package memory

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"unsafe"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func TestOnCommit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := New()

	if err := seed.LoadDemo(ctx, st); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	calls := 0
	st.OnCommit(func() { calls++ })

	rename := func(ctx context.Context, tx store.Tx) error {
		locations, err := tx.Locations().List(ctx)
		if err != nil {
			return err
		}

		l := locations[0]
		l.Name += " (renamed)"

		return tx.Locations().Update(ctx, l)
	}
	list := func(ctx context.Context, tx store.Tx) error {
		_, err := tx.Locations().List(ctx)

		return err
	}
	errBoom := errors.New("boom")

	steps := []struct {
		name  string
		mode  store.TxMode
		fn    func(context.Context, store.Tx) error
		calls int
	}{
		{"a write", store.ReadWrite, rename, 1},
		{"a read-only transaction", store.ReadOnly, list, 1},
		{"a read-write transaction without writes", store.ReadWrite, list, 1},
		{"a failed write", store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			return errors.Join(rename(ctx, tx), errBoom)
		}, 1},
		{"a settings change", store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			s, err := tx.Settings().Get(ctx)
			if err != nil {
				return err
			}

			s.SchoolName += "!"

			return tx.Settings().Put(ctx, s)
		}, 2},
		{"another write", store.ReadWrite, rename, 3},
	}

	for _, step := range steps {
		err := st.InTx(ctx, step.mode, step.fn)
		if err != nil && !errors.Is(err, errBoom) {
			t.Fatalf("%s: %v", step.name, err)
		}

		if calls != step.calls {
			t.Fatalf("after %s: %d commit notifications, want %d", step.name, calls, step.calls)
		}
	}
}

// TestChangedCoversEveryTable checks that a write to any table counts as a
// change, so the demo saves it.
func TestChangedCoversEveryTable(t *testing.T) {
	t.Parallel()

	for _, name := range stateFields() {
		var before, after state

		f := field(&after, name)
		if f.Kind() == reflect.Pointer {
			f.Set(reflect.ValueOf(&domain.Settings{}))
		} else {
			gen := f.FieldByName("gen")
			reflect.NewAt(gen.Type(), unsafe.Pointer(gen.UnsafeAddr())).Elem().SetUint(7)
		}

		if !changed(&before, &after) {
			t.Errorf("a write to %s is not seen as a change", name)
		}
	}

	var before, after state
	if changed(&before, &after) {
		t.Error("an unchanged state is seen as changed")
	}
}
