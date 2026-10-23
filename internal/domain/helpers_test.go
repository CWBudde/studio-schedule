package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
)

// t0 is a fixed instant used by the fixtures.
var t0 = time.Date(2026, time.October, 23, 17, 0, 0, 0, time.UTC)

var testIDs = ids.NewSequence()

// newID returns a fresh, non-zero ID of type I.
func newID[I domain.AnyID]() I { return ids.New[I](testIDs) }

type validator interface{ Validate() error }

// checkValidate asserts that fixture() is valid and that each mutation of a
// fresh fixture fails with ErrValidation.
func checkValidate[T validator](t *testing.T, fixture func() T, cases map[string]func(*T)) {
	t.Helper()

	if err := fixture().Validate(); err != nil {
		t.Fatalf("valid fixture: unexpected error %v", err)
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			v := fixture()
			mutate(&v)

			if err := v.Validate(); !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}

// checkEnum asserts that every listed value is valid and unique, and that
// the empty, an unknown and a wrongly cased value are invalid.
func checkEnum[E interface {
	~string
	Valid() bool
}](t *testing.T, all []E,
) {
	t.Helper()

	if len(all) == 0 {
		t.Fatal("enum has no values")
	}

	seen := make(map[E]bool, len(all))

	for _, e := range all {
		if !e.Valid() {
			t.Errorf("%q should be valid", e)
		}

		if seen[e] {
			t.Errorf("%q listed twice", e)
		}

		seen[e] = true
	}

	for _, e := range []E{"", "bogus", E(strings.ToUpper(string(all[0])))} {
		if e.Valid() {
			t.Errorf("%q should be invalid", e)
		}
	}
}
