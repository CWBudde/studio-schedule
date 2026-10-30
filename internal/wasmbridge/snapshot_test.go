package wasmbridge_test

import (
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
	"github.com/cwbudde/studio-schedule/internal/wasmbridge"
)

func TestSnapshotBuild(t *testing.T) {
	t.Parallel()

	if got, want := wasmbridge.SnapshotBuild("v1.2.3", string(seed.SchoolWerkstatt)), "v1.2.3 (werkstatt)"; got != want {
		t.Errorf("SnapshotBuild = %q, want %q", got, want)
	}

	// Every version and school has its own build, and none is the plain
	// version, which snapshots saved before the demo had schools carry.
	seen := map[string]bool{"v1": true, "v2": true}

	for _, version := range []string{"v1", "v2"} {
		for _, school := range seed.Schools() {
			b := wasmbridge.SnapshotBuild(version, string(school))
			if seen[b] {
				t.Errorf("SnapshotBuild(%q, %q) = %q, not unique", version, school, b)
			}

			seen[b] = true
		}
	}
}

// TestSnapshotOfAnotherSchool: a snapshot saved for one school is refused
// for the other, like one of another version, and restores for its own.
func TestSnapshotOfAnotherSchool(t *testing.T) {
	t.Parallel()

	src := memory.New()
	if err := seed.LoadDemoSchoolAt(t.Context(), src, seed.FixedDemoAnchor, seed.SchoolWerkstatt); err != nil {
		t.Fatal(err)
	}

	data, err := src.Snapshot(wasmbridge.SnapshotBuild("v1", string(seed.SchoolWerkstatt)))
	if err != nil {
		t.Fatal(err)
	}

	for _, build := range []string{
		wasmbridge.SnapshotBuild("v1", string(seed.SchoolProgramme)),
		wasmbridge.SnapshotBuild("v2", string(seed.SchoolWerkstatt)),
		"v1",
	} {
		if err := memory.New().Restore(data, build); !errors.Is(err, memory.ErrSnapshotMismatch) {
			t.Errorf("restore as %q: error = %v, want %v", build, err, memory.ErrSnapshotMismatch)
		}
	}

	if err := memory.New().Restore(data, wasmbridge.SnapshotBuild("v1", string(seed.SchoolWerkstatt))); err != nil {
		t.Errorf("restore for the same school: %v", err)
	}
}
