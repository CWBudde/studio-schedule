package service

import (
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// TestIntegrityStoreFaults: a store failure in any read of the checks, in
// starting their transaction or in ending it, is returned. The demo school
// gives every check data to read.
func TestIntegrityStoreFaults(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*FaultStore, func() error) {
		t.Helper()

		mem := memory.New()
		if err := seed.LoadDemo(t.Context(), mem); err != nil {
			t.Fatal(err)
		}

		fs := NewFaultStore(mem)

		return fs, func() error {
			_, err := NewIntegrityService(fs).Check(t.Context())

			return err
		}
	}

	// Every check's read happens, in the checks' order.
	fs, op := setup(t)
	if err := op(); err != nil {
		t.Fatal(err)
	}

	want := []FaultCall{
		{"InTx", 1},
		{"Passes.ListUnbalanced", 1},
		{"Redemptions.ListStray", 1},
		{"Bookings.ListOverCapacity", 1},
		{"Persons.ListTombstoneReferences", 1},
		{"ReceiptCounters.ListStates", 1},
		{"Commit", 1},
	}
	if got := fs.Calls(); !slices.Equal(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}

	SweepFaults(t, FaultSweep{Setup: setup})
}
