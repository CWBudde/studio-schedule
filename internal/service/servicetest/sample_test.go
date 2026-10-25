package servicetest_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
)

// TestSampleCopy keeps the web app's copy of the sample file, which the
// import page offers as a download, the same as the one the tests import.
func TestSampleCopy(t *testing.T) {
	t.Parallel()

	web, err := os.ReadFile("../../../web/src/features/import/members-sample.csv")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(web, servicetest.SampleCSV) {
		t.Error("web/src/features/import/members-sample.csv differs from testdata/members-sample.csv; copy it over")
	}
}
