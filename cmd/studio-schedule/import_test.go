package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
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

// importWrite writes data as a CSV file in a temporary directory and
// returns its path.
func importWrite(t *testing.T, name string, data []byte) string {
	t.Helper()

	file := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return file
}

// importSeeded returns the URL of a new migrated database with the demo
// data, and its store.
func importSeeded(t *testing.T) (string, store.Store) {
	t.Helper()

	databaseURL := newTestDatabase(t)
	st := openTestStore(t, databaseURL)

	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	return databaseURL, st
}

// importPersons returns the persons of st by member number, and how many
// persons there are.
func importPersons(t *testing.T, st store.Store) (map[string]domain.Person, int) {
	t.Helper()

	byNo := map[string]domain.Person{}

	var n int

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		persons, err := tx.Persons().List(ctx)
		for _, p := range persons {
			if p.MemberNo != "" {
				byNo[p.MemberNo] = p
			}
		}

		n = len(persons)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return byNo, n
}

// TestImportCommandOnPostgres imports the sample file into the demo data
// through the command line: the dry run reports every row and writes
// nothing, --apply writes the good rows and records the run, and the same
// file again changes nothing.
func TestImportCommandOnPostgres(t *testing.T) {
	t.Parallel()

	databaseURL, st := importSeeded(t)
	file := importWrite(t, "members.csv", servicetest.SampleCSV)
	_, persons := importPersons(t, st)

	out, err := execute(map[string]string{"DATABASE_URL": databaseURL}, "import", file, "--notes", "Kurs")
	if !errors.Is(err, errRowsFailed) {
		t.Fatalf("dry run: %v, want errRowsFailed\n%s", err, out)
	}

	for _, want := range []string{
		// An update names the fields it writes, a create every field it fills.
		"line 2  update  M-1042  Lena Quillfeather  phone, notes\n",
		"line 3  create  M-2001  Paula Amberly      memberNo, givenName, familyName, notes, birthDate; " +
			"guardian Clara Amberly: unchanged, linked; guardian Tom Amberly: unchanged, linked\n",
		"line 4  create  M-2002  Theo Windmere      memberNo, givenName, familyName, notes, birthDate; " +
			"guardian Greta Windmere: create, linked\n",
		// The guardian an earlier row creates is found again.
		"guardian Greta Windmere: unchanged, linked; guardian Henrik Windmere: create, linked\n",
		"line 7  error   M-2005  Ina Fallowmere     birthDate: invalid\n",
		"would import 6 rows: 4 created, 1 updated, 0 unchanged, 1 failed; guardians: 2 created, 0 updated, " +
			"5 links added\n",
		"dry run: nothing was written; run again with --apply to import\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output lacks %q:\n%s", want, out)
		}
	}

	byNo, n := importPersons(t, st)
	if _, ok := byNo["M-2001"]; ok || n != persons || importRuns(t, st) != 0 {
		t.Fatalf("the dry run wrote: %d persons (was %d), %d runs", n, persons, importRuns(t, st))
	}

	// --apply, with the database from the flag: the good rows are imported.
	out, err = execute(nil, "import", "--apply", "--database-url", databaseURL, "--notes", "Kurs", file)
	if !errors.Is(err, errRowsFailed) || !strings.Contains(out, "imported 6 rows: 4 created, 1 updated, 0 unchanged, "+
		"1 failed; guardians: 2 created, 0 updated, 5 links added\n") || strings.Contains(out, "dry run") {
		t.Fatalf("apply: %v\n%s", err, out)
	}

	byNo, n = importPersons(t, st)

	// Four members and two guardians are new.
	if n != persons+6 || importRuns(t, st) != 1 {
		t.Errorf("after the import: %d persons (was %d), %d runs", n, persons, importRuns(t, st))
	}

	if p := byNo["M-2001"]; p.GivenName != "Paula" || p.FamilyName != "Amberly" || p.BirthDate.String() != "2018-03-14" ||
		!strings.Contains(p.Notes, "Kreativer Kindertanz") {
		t.Errorf("M-2001 = %+v", p)
	}

	if p, ok := byNo["M-2005"]; ok {
		t.Errorf("the failed row was imported: %+v", p)
	}

	// Again: nothing changes, and the failed row still fails.
	out, err = execute(map[string]string{"DATABASE_URL": databaseURL}, "import", file, "--notes", "Kurs", "--apply")
	if !errors.Is(err, errRowsFailed) || !strings.Contains(out, "line 3  unchanged  M-2001  Paula Amberly      "+
		"guardian Clara Amberly: unchanged; guardian Tom Amberly: unchanged\n") ||
		!strings.Contains(out, "imported 6 rows: 0 created, 0 updated, 5 unchanged, 1 failed; guardians: 0 created, "+
			"0 updated, 0 links added\n") {
		t.Errorf("second import: %v\n%s", err, out)
	}

	if _, again := importPersons(t, st); again != n {
		t.Errorf("the second import changed the persons: %d, was %d", again, n)
	}
}

// TestImportCommandDuplicates: a new member with the email, or the name and
// birth date, of a person (one an earlier row creates included) is reported
// as a possible duplicate, and imported all the same; two candidates of the
// same name are named once.
func TestImportCommandDuplicates(t *testing.T) {
	t.Parallel()

	databaseURL, st := importSeeded(t)
	file := importWrite(t, "members.csv", []byte("Member no;Given name;Family name;Birth date;Email\n"+
		"M-3001;Nora;Example;2016-05-01;nora.example@example.test\n"+
		"M-3002;Nora;Example;2016-05-01;\n"+
		"M-3003;Nora;Example;2016-05-01;Nora.Example@example.test\n"+
		"M-3004;Nils;Other;;nora.example@example.test\n"))

	out, err := execute(map[string]string{"DATABASE_URL": databaseURL}, "import", file, "--apply")
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}

	for _, want := range []string{
		"line 2  create  M-3001  Nora Example  memberNo, givenName, familyName, email, birthDate\n",
		"line 3  create  M-3002  Nora Example  memberNo, givenName, familyName, birthDate; possible duplicate of Nora Example\n",
		// M-3001 (email, name) and M-3002 (name): one name.
		"line 4  create  M-3003  Nora Example  memberNo, givenName, familyName, email, birthDate; " +
			"possible duplicate of Nora Example\n",
		"line 5  create  M-3004  Nils Other    memberNo, givenName, familyName, email; possible duplicate of Nora Example\n",
		"imported 4 rows: 4 created, 0 updated, 0 unchanged, 0 failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	byNo, _ := importPersons(t, st)
	for _, no := range []string{"M-3001", "M-3002", "M-3003", "M-3004"} {
		if _, ok := byNo[no]; !ok {
			t.Errorf("%s was not imported", no)
		}
	}
}

// TestImportCommandMap: --map replaces the header matching, so a file with
// headers the matching does not know imports; a column the file lacks is
// an error on the mapping, after the database is opened, that writes
// nothing.
func TestImportCommandMap(t *testing.T) {
	t.Parallel()

	databaseURL, st := importSeeded(t)
	env := map[string]string{"DATABASE_URL": databaseURL}
	file := importWrite(t, "members.csv", []byte("Nr,Rufname,Zuname,Klasse\nM-3101,Ida,Example,Ballett 2\n"))

	out, err := execute(env, "import", file, "--apply", "--map", "memberNo=Nr", "--map", "givenName=Rufname",
		"--map", "familyName=Zuname", "--notes", "Klasse")
	if err != nil || !strings.Contains(out, "line 2  create  M-3101  Ida Example  memberNo, givenName, familyName, notes\n") {
		t.Fatalf("import: %v\n%s", err, out)
	}

	if p := mustPerson(t, st, "M-3101"); p.GivenName != "Ida" || !strings.Contains(p.Notes, "Ballett 2") {
		t.Errorf("M-3101 = %+v", p)
	}

	out, err = execute(env, "import", file, "--apply", "--map", "memberNo=Mitgliedsnummer")
	if err == nil || !strings.Contains(err.Error(), `the file has no column "Mitgliedsnummer"`) || out != "" {
		t.Errorf("unknown column: %v\n%s", err, out)
	}

	if runs := importRuns(t, st); runs != 1 {
		t.Errorf("%d import runs, want only the first", runs)
	}
}

func mustPerson(t *testing.T, st store.Store, memberNo string) domain.Person {
	t.Helper()

	var p domain.Person

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		p, err = tx.Persons().GetByMemberNo(ctx, memberNo)

		return err
	})
	if err != nil {
		t.Fatalf("person %s: %v", memberNo, err)
	}

	return p
}

// TestImportCommandErrors: what fails before the database is needed fails
// without connecting (nothing listens at the URL), in the order of the
// checks: the file, the database URL, the mapping.
func TestImportCommandErrors(t *testing.T) {
	t.Parallel()

	unreachable := map[string]string{"DATABASE_URL": "postgres://app@127.0.0.1:1/studio?connect_timeout=2"}
	sample := importWrite(t, "members.csv", servicetest.SampleCSV)

	// A missing file is the file's error, even without a database URL.
	missing := filepath.Join(t.TempDir(), "missing.csv")
	if _, err := execute(nil, "import", missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: %v, want fs.ErrNotExist", err)
	}

	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"bad --map":      {args: []string{sample, "--map", "shoeSize=Size"}, want: `invalid --map "shoeSize=Size"`},
		"--map no pair":  {args: []string{sample, "--map", "memberNo"}, want: `invalid --map "memberNo"`},
		"empty file":     {args: []string{importWrite(t, "empty.csv", nil)}, want: "the file is empty"},
		"too many files": {args: []string{sample, sample}, want: "accepts 1 arg(s), received 2"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := execute(unreachable, append([]string{"import"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("import %v: %v, want %q\n%s", tc.args, err, tc.want, out)
			}
		})
	}

	// An unmigrated database is refused.
	out, err := execute(map[string]string{"DATABASE_URL": newUnmigratedDatabase(t)}, "import", sample)
	if !errors.Is(err, errSchemaOutdated) {
		t.Errorf("unmigrated: %v, want errSchemaOutdated\n%s", err, out)
	}
}

// TestRunImportWriteFails: an import whose report cannot be written (stdout
// closed) fails with the write error.
func TestRunImportWriteFails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := memory.New()
	if err := seed.LoadDemo(ctx, st); err != nil {
		t.Fatal(err)
	}

	m, err := importMapping(servicetest.SampleCSV, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	broken := errors.New("stdout is closed")
	file := service.ImportFile{Name: "members.csv", Data: servicetest.SampleCSV}

	if err := runImport(ctx, st, initFailingWriter{broken}, file, m, false); !errors.Is(err, broken) {
		t.Errorf("runImport: %v, want the write error", err)
	}
}
