package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestImportNeedsDatabaseURL(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "members.csv")
	if err := os.WriteFile(file, servicetest.SampleCSV, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := execute(nil, "import", file)
	if !errors.Is(err, errNoDatabaseURL) {
		t.Fatalf("import error = %v, want errNoDatabaseURL\n%s", err, out)
	}

	if _, err := execute(nil, "import"); err == nil {
		t.Error("import without a file succeeded")
	}
}

func TestImportMappingFlags(t *testing.T) {
	t.Parallel()

	m, err := importMapping(servicetest.SampleCSV, nil, []string{"Kurs"})
	if err != nil || m.Columns[service.FieldMemberNo] != "Mitgliedsnummer" || m.Notes[0] != "Kurs" {
		t.Errorf("matched mapping %+v, %v", m, err)
	}

	m, err = importMapping(nil, []string{"memberNo=Nr", "givenName=First name"}, nil)
	if err != nil || len(m.Columns) != 2 || m.Columns[service.FieldGivenName] != "First name" {
		t.Errorf("flag mapping %+v, %v", m, err)
	}

	for _, bad := range []string{"memberNo", "shoeSize=Size"} {
		if _, err := importMapping(nil, []string{bad}, nil); err == nil {
			t.Errorf("--map %q accepted", bad)
		}
	}
}

// TestRunImport imports the sample into the demo data: a dry run writes
// nothing, --apply does.
func TestRunImport(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := memory.New()
	if err := seed.LoadDemo(ctx, st); err != nil {
		t.Fatal(err)
	}

	m, err := importMapping(servicetest.SampleCSV, nil, []string{"Kurs"})
	if err != nil {
		t.Fatal(err)
	}

	file := service.ImportFile{Name: "members.csv", Data: servicetest.SampleCSV}

	// The sample's row with an impossible date fails: the command exits
	// with status 1, after the report.
	var out bytes.Buffer
	if err := runImport(ctx, st, &out, file, m, false); !errors.Is(err, errRowsFailed) {
		t.Fatalf("dry run: %v, want errRowsFailed", err)
	}

	for _, want := range []string{
		"line 3  create  M-2001  Paula Amberly", "guardian Clara Amberly: unchanged, linked",
		"line 7  error   M-2005  Ina Fallowmere     birthDate: invalid\n",
		"would import 6 rows: 4 created, 1 updated, 0 unchanged, 1 failed", "dry run: nothing was written",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	if runs := importRuns(t, st); runs != 0 {
		t.Fatalf("the dry run recorded %d runs", runs)
	}

	out.Reset()

	// The good rows are imported all the same.
	err = runImport(ctx, st, &out, file, m, true)
	if !errors.Is(err, errRowsFailed) || err.Error() != "rows failed: 1 of 6 (see the lines marked error)" {
		t.Fatalf("apply: %v, want errRowsFailed", err)
	}

	if !strings.Contains(out.String(), "imported 6 rows: 4 created, 1 updated") || importRuns(t, st) != 1 {
		t.Errorf("apply:\n%s", out.String())
	}

	// Without failed rows it succeeds: the import again, without the bad row.
	good := service.ImportFile{Name: "members.csv", Data: withoutLine(servicetest.SampleCSV, "M-2005")}

	out.Reset()

	if err := runImport(ctx, st, &out, good, m, true); err != nil {
		t.Fatalf("a file without problems: %v\n%s", err, out.String())
	}
}

// withoutLine returns data without the lines that contain s.
func withoutLine(data []byte, s string) []byte {
	lines := bytes.SplitAfter(data, []byte("\n"))

	return bytes.Join(slices.DeleteFunc(lines, func(l []byte) bool { return bytes.Contains(l, []byte(s)) }), nil)
}

func importRuns(t *testing.T, st store.Store) int {
	t.Helper()

	var n int

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		runs, err := tx.Imports().List(ctx, 10)
		n = len(runs)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return n
}
