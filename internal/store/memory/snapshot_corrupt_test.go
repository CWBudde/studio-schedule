package memory

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// header is the start of a snapshot of the current layout written by build,
// up to the opening brace of its tables.
func header(build string) string {
	return `{"format":` + strconv.Itoa(snapshotFormat) + `,"layout":"` + layout() + `","build":"` + build + `","tables":{`
}

// snapshotOf returns a snapshot of the current layout whose tables are the
// members in tables (the JSON between the braces).
func snapshotOf(tables string) []byte {
	return []byte(header("dev") + tables + "}}")
}

// storeWithLocation returns a store holding one location, so a test can see
// whether a refused Restore kept it.
func storeWithLocation(t *testing.T) (*Store, domain.Location) {
	t.Helper()

	st := New()
	loc := domain.Location{ID: ids.New[domain.LocationID](ids.NewSequence()), Name: "Studio North"}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Locations().Create(ctx, loc)
	})
	if err != nil {
		t.Fatalf("creating the location: %v", err)
	}

	return st, loc
}

// TestRestoreRefusesCorruptSnapshots feeds Restore snapshots that are cut
// off or hold values of the wrong kind. Each fails, and the store keeps the
// data it had: Restore decodes into a new state and swaps it in only at the
// end.
func TestRestoreRefusesCorruptSnapshots(t *testing.T) {
	t.Parallel()

	const (
		personA  = `"00000000-0000-7000-8000-00000000000a"`
		personB  = `"00000000-0000-7000-8000-00000000000b"`
		guardian = `{"GuardianID":` + personA + `,"DependentID":` + personB + `}`
	)

	tests := []struct {
		name string
		data []byte
		// mismatch: the error is ErrSnapshotMismatch (the demo then starts
		// from the seed); otherwise it must not be, as the data is corrupt
		// rather than from another version.
		mismatch bool
		// in is a part of the error message that names what failed.
		in string
		// cut: the error is io.ErrUnexpectedEOF.
		cut bool
	}{
		// The document itself.
		{name: "empty", data: nil, in: "memory: restore:"},
		{name: "not an object", data: []byte(`[]`), in: "want an object"},
		{name: "cut off in the header", data: []byte(`{"format":1,`), in: "memory: restore:", cut: true},
		{name: "unknown member", data: []byte(`{"format":1,"version":2}`), mismatch: true, in: `unknown member "version"`},
		{name: "no tables", data: []byte(`{"format":1,"layout":"` + layout() + `","build":"dev"}`), mismatch: true, in: "no tables"},

		// The tables object.
		{name: "tables not an object", data: []byte(strings.TrimSuffix(header("dev"), "{") + `[]}`), in: "tables: want an object"},
		{name: "cut off in the tables", data: []byte(header("dev")), in: "memory: restore:", cut: true},
		{name: "unknown table", data: snapshotOf(`"rehearsals":[]`), mismatch: true, in: `unknown or repeated table "rehearsals"`},
		{name: "repeated table", data: snapshotOf(`"locations":[],"locations":[]`), in: "duplicate object member"},

		// A table of rows.
		{name: "rows not an array", data: snapshotOf(`"locations":"Studio North"`), in: "locations:"},
		{name: "ID not a string", data: snapshotOf(`"locations":[{"ID":5}]`), in: "an ID must be a string"},
		{name: "ID cut off", data: []byte(header("dev") + `"locations":[{"ID":`), in: "locations:", cut: true},
		{name: "ID not a UUID", data: snapshotOf(`"locations":[{"ID":"studio-north"}]`), in: "locations:"},

		// A table that is a set of keys.
		{name: "keys not an array", data: snapshotOf(`"guardians":{}`), in: "guardians:"},
		{name: "duplicate key", data: snapshotOf(`"guardians":[` + guardian + `,` + guardian + `]`), in: "guardians: duplicate key"},

		// The settings' durations.
		{name: "duration not a number", data: snapshotOf(`"settings":{"BookingCutoff":"2h"}`), in: "a duration must be a number"},
		{name: "duration not an integer", data: snapshotOf(`"settings":{"BookingCutoff":1.5}`), in: "settings:"},
		{name: "duration out of range", data: snapshotOf(`"settings":{"BookingCutoff":1e30}`), in: "settings:"},
		{name: "duration cut off", data: []byte(header("dev") + `"settings":{"BookingCutoff":`), in: "settings:", cut: true},

		// The images.
		{name: "logo not an object", data: snapshotOf(`"logo":"logo.png"`), in: "logo:"},
		{name: "background not an object", data: snapshotOf(`"background":"background.jpg"`), in: "background:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st, loc := storeWithLocation(t)

			err := st.Restore(tt.data, "dev")
			if err == nil {
				t.Fatal("Restore succeeded")
			}

			if got := errors.Is(err, ErrSnapshotMismatch); got != tt.mismatch {
				t.Errorf("Restore error %q: errors.Is(ErrSnapshotMismatch) = %t, want %t", err, got, tt.mismatch)
			}

			if tt.cut && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("Restore error %q, want io.ErrUnexpectedEOF", err)
			}

			if !strings.Contains(err.Error(), tt.in) {
				t.Errorf("Restore error %q does not contain %q", err, tt.in)
			}

			if got := st.state.locations.rows; len(got) != 1 || got[loc.ID] != loc {
				t.Errorf("a refused snapshot changed the store: locations %v", got)
			}
		})
	}
}

// TestRestoreReportsTruncation pins that a snapshot cut off anywhere is
// refused: the demo saves the snapshot in the browser, where a write may be
// cut short.
func TestRestoreReportsTruncation(t *testing.T) {
	t.Parallel()

	data, err := fullStore(t).Snapshot("dev")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// The encoder ends the document with a newline; without it, the document
	// is still whole.
	data = bytes.TrimSuffix(data, []byte("\n"))

	for _, n := range []int{1, len(data) / 3, len(data) / 2, len(data) - 1} {
		st, loc := storeWithLocation(t)

		err := st.Restore(data[:n], "dev")
		if err == nil {
			t.Fatalf("Restore of the first %d of %d bytes succeeded", n, len(data))
		}

		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("Restore of the first %d bytes: %v, want io.ErrUnexpectedEOF", n, err)
		}

		if got := st.state.locations.rows; len(got) != 1 || got[loc.ID] != loc {
			t.Errorf("Restore of the first %d bytes changed the store", n)
		}
	}
}

// TestSnapshotRefusesInvalidUTF8 pins that Snapshot fails, rather than
// writing a document Restore could not read, when a text is not UTF-8: the
// build name, or a text in a row (the API and the CSV import only ever pass
// UTF-8 on).
func TestSnapshotRefusesInvalidUTF8(t *testing.T) {
	t.Parallel()

	t.Run("build", func(t *testing.T) {
		t.Parallel()

		data, err := New().Snapshot("v1.0.0-\xff")
		if err == nil {
			t.Fatalf("Snapshot succeeded: %s", data)
		}

		if !strings.HasPrefix(err.Error(), "memory: snapshot: ") {
			t.Errorf("Snapshot error = %q", err)
		}
	})

	t.Run("row", func(t *testing.T) {
		t.Parallel()

		st, loc := storeWithLocation(t)
		loc.Name = "Studio \xfcber"

		err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			return tx.Locations().Update(ctx, loc)
		})
		if err != nil {
			t.Fatalf("renaming the location: %v", err)
		}

		data, err := st.Snapshot("dev")
		if err == nil {
			t.Fatalf("Snapshot succeeded: %s", data)
		}

		if !strings.HasPrefix(err.Error(), "memory: snapshot of locations: ") {
			t.Errorf("Snapshot error = %q", err)
		}
	})
}
