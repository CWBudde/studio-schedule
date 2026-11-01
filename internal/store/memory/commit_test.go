package memory

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
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
// change, so the demo saves it, except a write to a transient table, which
// the snapshot leaves out.
func TestChangedCoversEveryTable(t *testing.T) {
	t.Parallel()

	for _, name := range stateFields() {
		var before, after state

		f := field(&after, name)
		if f.Kind() == reflect.Pointer {
			f.Set(reflect.New(f.Type().Elem()))
		} else {
			gen := f.FieldByName("gen")
			reflect.NewAt(gen.Type(), unsafe.Pointer(gen.UnsafeAddr())).Elem().SetUint(7)
		}

		switch got := changed(&before, &after); {
		case transientTable(name) && got:
			t.Errorf("a write to the transient table %s is seen as a change", name)
		case !transientTable(name) && !got:
			t.Errorf("a write to %s is not seen as a change", name)
		}
	}

	var before, after state
	if changed(&before, &after) {
		t.Error("an unchanged state is seen as changed")
	}
}

// TestOnCommitIgnoresJobRuns pins that a transaction that only records a
// job run does not notify OnCommit: the demo's jobs record a run every
// minute, and saving the demo for each would be wasted work
// (docs/demo-mode.md, "Saved data"). The run is committed all the same, and
// a transaction that also writes another table still notifies.
func TestOnCommitIgnoresJobRuns(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	st := New()
	src := ids.NewSequence()
	at := time.Date(2026, 9, 14, 17, 30, 0, 0, time.UTC)

	calls := 0
	st.OnCommit(func() { calls++ })

	run := func(job string) domain.JobRun {
		return domain.JobRun{ID: ids.New[domain.JobRunID](src), Job: job, StartedAt: at, EndedAt: at, Result: domain.JobOK}
	}

	err := st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.JobRuns().Create(ctx, run("deliver-mail"))
	})
	if err != nil {
		t.Fatalf("recording a job run: %v", err)
	}

	if calls != 0 {
		t.Errorf("a transaction that only recorded a job run notified %d times, want none", calls)
	}

	err = st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.JobRuns().Create(ctx, run("generate-lessons")),
			tx.Locations().Create(ctx, domain.Location{ID: ids.New[domain.LocationID](src), Name: "Studio North"}),
		)
	})
	if err != nil {
		t.Fatalf("recording a job run and a location: %v", err)
	}

	if calls != 1 {
		t.Errorf("a transaction that also wrote a location notified %d times, want 1", calls)
	}

	var jobs []string

	err = st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		runs, err := tx.JobRuns().Latest(ctx)
		for _, r := range runs {
			jobs = append(jobs, r.Job)
		}

		return err
	})
	if err != nil {
		t.Fatalf("listing the job runs: %v", err)
	}

	if !slices.Equal(jobs, []string{"deliver-mail", "generate-lessons"}) {
		t.Errorf("committed job runs %v, want both", jobs)
	}
}
