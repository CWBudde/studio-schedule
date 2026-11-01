package service

import (
	"errors"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// The integrity checks' findings on both stores are the scenarios in
// servicetest/integrity.go; these tests cover what needs no data.

// TestIntegrityChecksOrder: the checks run in this order.
func TestIntegrityChecksOrder(t *testing.T) {
	t.Parallel()

	want := []IntegrityCheck{CheckPasses, CheckCapacity, CheckTombstones, CheckReceiptCounters}
	if got := IntegrityChecks(); !slices.Equal(got, want) {
		t.Fatalf("checks %v, want %v", got, want)
	}
}

// TestIntegrityUnknownCheck: an unknown check, alone or beside known ones,
// is a field error on checks that names the checks, before the store is
// touched.
func TestIntegrityUnknownCheck(t *testing.T) {
	t.Parallel()

	for _, checks := range [][]IntegrityCheck{
		{"nope"},
		{CheckPasses, "nope", CheckCapacity},
		{""},
	} {
		fs := NewFaultStore(memory.New())

		got, err := NewIntegrityService(fs).Check(t.Context(), checks...)

		var fe *domain.FieldError
		if !errors.As(err, &fe) || !errors.Is(err, domain.ErrValidation) || fe.Field != "checks" {
			t.Fatalf("checks %q: %v, want a field error on checks", checks, err)
		}

		bad := checks[slices.IndexFunc(checks, func(c IntegrityCheck) bool { return !slices.Contains(IntegrityChecks(), c) })]
		if want := `unknown check "` + string(bad) + `"; the checks are passes, capacity, tombstones, receipt-counters`; fe.Msg != want {
			t.Errorf("message %q, want %q", fe.Msg, want)
		}

		if got != nil {
			t.Errorf("findings %v beside the error", got)
		}

		if calls := fs.Calls(); len(calls) != 0 {
			t.Errorf("checks %q touched the store: %v", checks, calls)
		}
	}
}
