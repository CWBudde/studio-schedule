package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/store"
)

// errStoreDown is the failure of failingStore.
var errStoreDown = errors.New("the database is unavailable")

// failingStore fails its transactions from the failFrom-th on (counted
// from 1), without running them.
type failingStore struct {
	store.Store

	failFrom int64
	calls    *atomic.Int64
}

func (s failingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	if s.calls.Add(1) >= s.failFrom {
		return errStoreDown
	}

	return s.Store.InTx(ctx, mode, fn)
}

// TestTimetableCommandsStoreFailure fails the n-th transaction of each
// timetable command on fresh demo data, for every n until the command runs
// through: each failure exits 1 with the error on stderr and nothing on
// stdout.
func TestTimetableCommandsStoreFailure(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"lessons", "list", "--as", demoOffice},
		{"lessons", "generate", "--as", demoOffice, "--until", "2026-10-31"},
		{"lessons", "cancel", "--as", demoOffice, "--yes", demoJazz},
		{"lessons", "restore", "--as", demoOffice, "--yes", demoJazz},
		{"terms", "list", "--as", demoOffice},
		{"classes", "list", "--as", demoOffice},
		{"classes", "list", "--as", demoOffice, "--term", demoTerm},
		{"stats", "--as", demoOffice},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			wantStoreFailures(t, args...)
		})
	}
}

// wantStoreFailures fails the n-th transaction of the command args on
// fresh demo data, for every n until the command runs through: each
// failure must exit 1 with the error on stderr and nothing on stdout.
func wantStoreFailures(t *testing.T, args ...string) {
	t.Helper()

	for n := int64(1); ; n++ {
		st := failingStore{Store: demoStore(t), failFrom: n, calls: new(atomic.Int64)}

		got := cliRun{st: st}.exec(t, args...)
		if got.code == 0 {
			if n == 1 {
				t.Fatal("the command ran without a transaction")
			}

			return
		}

		if got.code != 1 || !strings.Contains(got.stderr, errStoreDown.Error()) || got.stdout != "" {
			t.Fatalf("transaction %d failing: exit %d, stdout %q, stderr %q", n, got.code, got.stdout, got.stderr)
		}
	}
}

// TestCount counts in the singular and the plural.
func TestCount(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		n     int
		thing string
		want  string
	}{
		{0, "lesson", "0 lessons"},
		{1, "lesson", "1 lesson"},
		{373, "lesson", "373 lessons"},
		{1, "active booking", "1 active booking"},
		{3, "active booking", "3 active bookings"},
	} {
		if got := count(c.n, c.thing); got != c.want {
			t.Errorf("count(%d, %q) = %q, want %q", c.n, c.thing, got, c.want)
		}
	}
}
