package service

//go:generate go run faultgen.go

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// ErrInjected is the error a FaultStore injects: a failure only a broken
// database produces (a lost connection, a full disk). Services must return
// it, possibly wrapped, and roll back what the transaction wrote.
var ErrInjected = errors.New("injected store fault")

// FaultCall names one call through a FaultStore: the method, as the
// repository accessor and the method ("Lessons.Lock"), "InTx" for starting a
// transaction and "Commit" for committing one, and its number among the
// calls of that method, from 1.
type FaultCall struct {
	Method string
	N      int
}

func (c FaultCall) String() string { return fmt.Sprintf("%s#%d", c.Method, c.N) }

// FaultStore is a store.Store for tests that delegates to a memory store and
// fails chosen calls with ErrInjected. Every repository method that returns
// an error can fail (fault_gen_test.go, written by faultgen.go), as can
// starting a transaction ("InTx") and committing it ("Commit": the callback
// succeeded, but the store rolls back and returns the error, as PostgreSQL
// does when the commit fails). It records every call, so a test can first
// run an operation to learn its calls and then fail each in turn
// (SweepFaults). It is safe for concurrent use.
type FaultStore struct {
	inner *memory.Store

	mu     sync.Mutex
	counts map[string]int
	calls  []FaultCall
	fail   map[FaultCall]error
}

// NewFaultStore wraps inner. Data written through inner directly (a test's
// fixture) is neither counted nor failed.
func NewFaultStore(inner *memory.Store) *FaultStore {
	return &FaultStore{inner: inner, counts: map[string]int{}, fail: map[FaultCall]error{}}
}

// Fail makes the nth call of method (counted from 1) fail with ErrInjected.
func (s *FaultStore) Fail(method string, n int) *FaultStore {
	return s.FailWith(method, n, ErrInjected)
}

// FailWith makes the nth call of method (counted from 1) fail with err.
func (s *FaultStore) FailWith(method string, n int, err error) *FaultStore {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fail[FaultCall{Method: method, N: n}] = err

	return s
}

// Calls returns the calls so far, in order.
func (s *FaultStore) Calls() []FaultCall {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.calls)
}

// Count returns how often method was called.
func (s *FaultStore) Count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.counts[method]
}

// StoreData is the complete data of a store, table by table, each table's
// rows as JSON in sorted order (a memory snapshot lists them in map order).
type StoreData map[string][]string

// Snapshot returns the complete data of the wrapped memory store.
func (s *FaultStore) Snapshot(t *testing.T) StoreData {
	t.Helper()

	b, err := s.inner.Snapshot("faults")
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}

	data := StoreData{}

	for name, raw := range doc.Tables {
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			rows = []json.RawMessage{raw} // a single record (the settings)
		}

		for _, r := range rows {
			data[name] = append(data[name], string(r))
		}

		slices.Sort(data[name])
	}

	return data
}

// Changed returns the tables whose rows differ between d and other, sorted.
func (d StoreData) Changed(other StoreData) []string {
	var changed []string

	for _, name := range slices.Sorted(maps.Keys(d)) {
		if !slices.Equal(d[name], other[name]) {
			changed = append(changed, name)
		}
	}

	for name := range other {
		if _, ok := d[name]; !ok {
			changed = append(changed, name)
		}
	}

	return changed
}

// hit records a call of method and returns the error it is to fail with.
func (s *FaultStore) hit(method string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.counts[method]++
	c := FaultCall{Method: method, N: s.counts[method]}
	s.calls = append(s.calls, c)

	if err, ok := s.fail[c]; ok {
		return fmt.Errorf("%s: %w", c, err)
	}

	return nil
}

// InTx implements store.Store.
func (s *FaultStore) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	if err := s.hit("InTx"); err != nil {
		return err
	}

	return s.inner.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		if err := fn(ctx, &faultTx{Tx: tx, f: s}); err != nil {
			return err
		}

		return s.hit("Commit")
	})
}

// FaultSweep describes an operation whose store failures SweepFaults checks.
type FaultSweep struct {
	// Setup builds a fresh fixture whose services use the returned
	// FaultStore, and returns the operation under test. It runs once to
	// record the operation's calls, then once per call. The runs must be
	// deterministic (ids.Sequence, a fixed clock), so each makes the same
	// calls.
	Setup func(t *testing.T) (*FaultStore, func() error)
	// Tolerated lists methods whose failure the operation survives by
	// design, each with the reason; the operation must then succeed.
	Tolerated map[string]string
	// Check runs after each failed run instead of the default check, which
	// compares the store's data with the data before the operation: nothing
	// was written. An operation that commits more than one transaction
	// needs its own check.
	Check func(t *testing.T, fs *FaultStore, before StoreData, call FaultCall)
}

// SweepFaults runs the operation of sw once without a fault and then, once
// for each store call it made, with that call failing: the operation must
// return ErrInjected (wrapped) and leave the store as it was.
func SweepFaults(t *testing.T, sw FaultSweep) {
	t.Helper()

	fs, op := sw.Setup(t)
	if err := op(); err != nil {
		t.Fatalf("without a fault: %v", err)
	}

	calls := fs.Calls()
	if len(calls) == 0 {
		t.Fatal("the operation made no store call")
	}

	for _, call := range calls {
		t.Run(call.String(), func(t *testing.T) {
			fs, op := sw.Setup(t)
			before := fs.Snapshot(t)
			fs.Fail(call.Method, call.N)

			err := op()

			if reason, ok := sw.Tolerated[call.Method]; ok {
				if err != nil {
					t.Fatalf("a failing %s is tolerated (%s), but the operation failed: %v", call, reason, err)
				}

				return
			}

			if !errors.Is(err, ErrInjected) {
				t.Fatalf("with %s failing: err = %v, want the injected fault", call, err)
			}

			if sw.Check != nil {
				sw.Check(t, fs, before, call)

				return
			}

			if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
				t.Errorf("with %s failing, the store changed: %v", call, changed)
			}
		})
	}
}

// TestFaultStore checks the wrapper the fault tests rely on: it fails the
// chosen call only, a failed commit rolls the transaction back, and
// Snapshot sees a write.
func TestFaultStore(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mem := memory.New()
	fs := NewFaultStore(mem)
	loc := domain.Location{ID: ids.New[domain.LocationID](ids.NewSequence()), Name: "Studio Nowhere"}
	create := func(ctx context.Context, tx store.Tx) error { return tx.Locations().Create(ctx, loc) }
	before := fs.Snapshot(t)

	fs.Fail("Locations.Create", 2).Fail("Commit", 1)

	if err := fs.InTx(ctx, store.ReadWrite, create); !errors.Is(err, ErrInjected) {
		t.Fatalf("failed commit: %v, want the injected fault", err)
	}

	if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
		t.Fatalf("a failed commit kept %v", changed)
	}

	if err := fs.InTx(ctx, store.ReadWrite, create); !errors.Is(err, ErrInjected) {
		t.Fatalf("second create: %v, want the injected fault", err)
	}

	if err := fs.InTx(ctx, store.ReadWrite, create); err != nil {
		t.Fatalf("third create: %v", err)
	}

	if changed := before.Changed(fs.Snapshot(t)); !slices.Equal(changed, []string{"locations"}) {
		t.Fatalf("changed %v, want the locations", changed)
	}

	want := []FaultCall{
		{"InTx", 1},
		{"Locations.Create", 1},
		{"Commit", 1},
		{"InTx", 2},
		{"Locations.Create", 2},
		{"InTx", 3},
		{"Locations.Create", 3},
		{"Commit", 2},
	}
	if got := fs.Calls(); !slices.Equal(got, want) || fs.Count("Locations.Create") != 3 {
		t.Fatalf("calls %v, want %v", got, want)
	}
}
