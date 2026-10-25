package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLedgerIsTheOnlyWriter keeps every write of a booking, and every
// update of a lesson, in ledger.go, which settles their passes (ADR 0012):
// a new write path that wrote through the store directly would skip it.
func TestLedgerIsTheOnlyWriter(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	writes := []string{"Bookings().Create(", "Bookings().Update(", "Lessons().Update("}

	for _, name := range files {
		if name == "ledger.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}

		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		for _, w := range writes {
			if strings.Contains(string(src), w) {
				t.Errorf("%s calls %s directly; write through the ledger (ledger.go)", name, w)
			}
		}
	}
}
