package seed_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/seed"
)

// programmeDigest is the SHA-256 of the programme school's dataset for
// FixedDemoAnchor, JSON-encoded. A deliberate change to the programme data
// updates it: the test prints the new digest when it fails.
const programmeDigest = "9d702f81c06732ac2bdf25322460fac9bdc91502d04a5a6f2f7336cf0b143155"

// TestProgrammeDatasetUnchanged pins every field of the programme school's
// dataset, so that building the demo schools from their own tables leaves
// the programme school's data exactly as it was.
func TestProgrammeDatasetUnchanged(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(seed.DemoAt(seed.FixedDemoAnchor))
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != programmeDigest {
		t.Fatalf("the programme dataset changed: digest %s, want %s", got, programmeDigest)
	}
}
