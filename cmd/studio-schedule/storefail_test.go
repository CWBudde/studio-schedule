package main

import (
	"strings"
	"testing"
)

// TestCommandsStoreFailure fails the n-th transaction of each command
// below on fresh demo data, for every n until the command runs through
// (wantStoreFailures): each failure exits 1 with the error on stderr and
// nothing on stdout. The office commands act as the office account, the
// admin ones as the admin, receipts verify and db check as the operator.
func TestCommandsStoreFailure(t *testing.T) {
	t.Parallel()

	settings := settingsFile(t, `{"version": 1, "passes": {"redemptionMode": "booking"}}`)

	for _, args := range [][]string{
		{"passes", "list", "--as", demoOffice, "--person", ppClara},
		{"persons", "show", "--as", demoOffice, ppClara},
		{"persons", "anonymize", "--as", demoOffice, "--yes", ppEllaID},
		{"persons", "duplicates", "dismiss", "--as", demoOffice, pdJonasA, pdJonasB},
		{"receipts", "list", "--as", demoOffice},
		{"receipts", "verify"},
		{"db", "check"},
		{"settings", "get", "--as", demoOffice},
		{"settings", "set", "school", "schoolName=Studio Example", "--as", demoAdmin},
		{"settings", "import", "--as", demoAdmin, "--yes", settings},
		{"mail", "list", "--as", demoAdmin},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			t.Parallel()
			wantStoreFailures(t, args...)
		})
	}
}
