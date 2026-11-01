package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/service/servicetest"
	"github.com/cwbudde/studio-schedule/internal/store"
)

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

// withoutLine returns data without the lines that contain s.
func withoutLine(data []byte, s string) []byte {
	lines := bytes.SplitAfter(data, []byte("\n"))

	return bytes.Join(slices.DeleteFunc(lines, func(l []byte) bool { return bytes.Contains(l, []byte(s)) }), nil)
}

// storeOf is the store a run opens: the memory store, or the PostgreSQL
// database of its DATABASE_URL.
func storeOf(t *testing.T, r cliRun) store.Store {
	t.Helper()

	if r.st != nil {
		return r.st
	}

	return openTestStore(t, r.env["DATABASE_URL"])
}

// importRuns returns the recorded import runs of st, the latest first.
func importRuns(t *testing.T, st store.Store) []domain.ImportRun {
	t.Helper()

	var runs []domain.ImportRun

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		runs, err = tx.Imports().List(ctx, 10)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return runs
}

// accountID returns the ID of the account of email in st.
func accountID(t *testing.T, st store.Store, email string) domain.AccountID {
	t.Helper()

	var id domain.AccountID

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().GetByEmail(ctx, email)
		id = acc.ID

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return id
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

// errRowsFailedLine is what the import with the sample's bad row prints on
// stderr.
const errRowsFailedLine = "error: rows failed: 1 of 6 (see the lines marked error)\n"

// TestImportCommand imports the sample file into the demo data as the
// office account: the dry run reports every row and writes nothing,
// --apply writes the good rows and records the run under the account of
// --as, and the same file again changes nothing. The sample's row with an
// impossible date fails each time: the command exits with status 1.
func TestImportCommand(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		file := importWrite(t, "members.csv", servicetest.SampleCSV)
		_, persons := importPersons(t, st)

		got := r.exec(t, "import", "--as", demoOffice, file, "--notes", "Kurs")
		if got.code != 1 || got.stderr != errRowsFailedLine {
			t.Fatalf("dry run: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "import/dry-run.txt", got.stdout)

		byNo, n := importPersons(t, st)
		if _, ok := byNo["M-2001"]; ok || n != persons || len(importRuns(t, st)) != 0 {
			t.Fatalf("the dry run wrote: %d persons (was %d), %d runs", n, persons, len(importRuns(t, st)))
		}

		// The same dry run as the response body of runImport.
		got = r.exec(t, "import", "--as", demoOffice, file, "--notes", "Kurs", "--output", "json")
		// The report says the row failed: stderr gets no problem document.
		if got.code != 1 || got.stderr != "" {
			t.Fatalf("dry run, JSON: exit %d, stderr %q; want 1 and no problem document", got.code, got.stderr)
		}

		golden(t, "import/dry-run.json", got.stdout)

		var body struct {
			Applied bool `json:"applied"`
			Counts  struct {
				Rows   int `json:"rows"`
				Failed int `json:"failed"`
			} `json:"counts"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || body.Applied || body.Counts.Rows != 6 ||
			body.Counts.Failed != 1 {
			t.Errorf("JSON body %+v, %v", body, err)
		}

		// --apply: the good rows are imported, the run is the office's.
		got = r.exec(t, "import", "--apply", "--as", demoOffice, "--notes", "Kurs", file)
		if got.code != 1 || got.stderr != errRowsFailedLine {
			t.Fatalf("apply: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "import/apply.txt", got.stdout)

		runs := importRuns(t, st)
		if len(runs) != 1 || runs[0].ActorID != accountID(t, st, demoOffice) || runs[0].FileName != "members.csv" ||
			runs[0].Counts.Created != 4 || runs[0].Counts.Failed != 1 {
			t.Errorf("import runs = %+v, want one of the office account", runs)
		}

		byNo, n = importPersons(t, st)

		// Four members and two guardians are new.
		if n != persons+6 {
			t.Errorf("after the import: %d persons (was %d)", n, persons)
		}

		if p := byNo["M-2001"]; p.GivenName != "Paula" || p.FamilyName != "Amberly" ||
			p.BirthDate.String() != "2018-03-14" || !strings.Contains(p.Notes, "Kreativer Kindertanz") {
			t.Errorf("M-2001 = %+v", p)
		}

		if p, ok := byNo["M-2005"]; ok {
			t.Errorf("the failed row was imported: %+v", p)
		}

		// Again, the admin with STUDIO_AS: nothing changes, and the
		// failed row still fails.
		again := r
		again.env = map[string]string{"STUDIO_AS": demoAdmin}

		for k, v := range r.env {
			again.env[k] = v
		}

		got = again.exec(t, "import", file, "--notes", "Kurs", "--apply")
		if got.code != 1 || got.stderr != errRowsFailedLine {
			t.Fatalf("second import: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "import/again.txt", got.stdout)

		if _, n2 := importPersons(t, st); n2 != n {
			t.Errorf("the second import changed the persons: %d, was %d", n2, n)
		}

		if runs := importRuns(t, st); len(runs) != 2 || runs[0].ActorID != accountID(t, st, demoAdmin) {
			t.Errorf("import runs = %+v, want the admin's latest", runs)
		}

		// A file without problems exits with 0.
		good := importWrite(t, "good.csv", withoutLine(servicetest.SampleCSV, "M-2005"))
		if got := r.exec(t, "import", "--as", demoOffice, "--apply", good); got.code != 0 || got.stderr != "" ||
			!strings.Contains(got.stdout, "imported 5 rows: 0 created, 0 updated, 5 unchanged, 0 failed") {
			t.Errorf("a file without problems: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}
	})
}

// TestImportCommandDuplicates: a new member with the email, or the name and
// birth date, of a person (one an earlier row creates included) is reported
// as a possible duplicate, and imported all the same; two candidates of the
// same name are named once.
func TestImportCommandDuplicates(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		file := importWrite(t, "members.csv", []byte("Member no;Given name;Family name;Birth date;Email\n"+
			"M-3001;Nora;Example;2016-05-01;nora.example@example.test\n"+
			"M-3002;Nora;Example;2016-05-01;\n"+
			"M-3003;Nora;Example;2016-05-01;Nora.Example@example.test\n"+
			"M-3004;Nils;Other;;nora.example@example.test\n"))

		got := r.exec(t, "import", "--as", demoOffice, file, "--apply")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("import: exit %d, stderr %q\n%s", got.code, got.stderr, got.stdout)
		}

		golden(t, "import/duplicates.txt", got.stdout)

		byNo, _ := importPersons(t, st)
		for _, no := range []string{"M-3001", "M-3002", "M-3003", "M-3004"} {
			if _, ok := byNo[no]; !ok {
				t.Errorf("%s was not imported", no)
			}
		}
	})
}

// TestImportCommandMap: --map replaces the header matching, so a file with
// headers the matching does not know imports; a column the file lacks is
// a usage error on --map that writes nothing.
func TestImportCommandMap(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		file := importWrite(t, "members.csv", []byte("Nr,Rufname,Zuname,Klasse\nM-3101,Ida,Example,Ballett 2\n"))

		got := r.exec(t, "import", "--as", demoOffice, file, "--apply", "--map", "memberNo=Nr",
			"--map", "givenName=Rufname", "--map", "familyName=Zuname", "--notes", "Klasse")
		if got.code != 0 ||
			!strings.Contains(got.stdout, "line 2  create  M-3101  Ida Example  memberNo, givenName, familyName, notes\n") {
			t.Fatalf("import: exit %d, stderr %q\n%s", got.code, got.stderr, got.stdout)
		}

		byNo, _ := importPersons(t, st)
		if p := byNo["M-3101"]; p.GivenName != "Ida" || !strings.Contains(p.Notes, "Ballett 2") {
			t.Errorf("M-3101 = %+v", p)
		}

		got = r.exec(t, "import", "--as", demoOffice, file, "--apply", "--map", "memberNo=Mitgliedsnummer")
		if got.code != 2 || got.stdout != "" ||
			got.stderr != "error: --map memberNo: the file has no column \"Mitgliedsnummer\"\n" {
			t.Errorf("unknown column: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		if runs := importRuns(t, st); len(runs) != 1 {
			t.Errorf("%d import runs, want only the first", len(runs))
		}
	})
}

// TestImportCommandRefusals: what the command refuses runs no import: the
// account of --as (none, an unknown one, a role below office), the
// arguments and flags, the file.
func TestImportCommandRefusals(t *testing.T) {
	t.Parallel()

	sample := importWrite(t, "members.csv", servicetest.SampleCSV)
	missing := filepath.Join(t.TempDir(), "missing.csv")

	for name, tc := range map[string]struct {
		env    map[string]string
		args   []string
		code   int
		stderr string
	}{
		"no --as": {
			args: []string{sample}, code: 2,
			stderr: "error: studio-schedule import acts as an account: give --as <email> or set STUDIO_AS\n",
		},
		"unknown --as": {
			args: []string{"--as", "nobody@example.org", sample}, code: 3,
			stderr: "error: --as nobody@example.org: not found: no account has this email\n",
		},
		"--as a member": {
			args: []string{"--as", demoMember, sample}, code: 4,
			stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below office, " +
				"which this command needs\n",
		},
		"--as an instructor": {args: []string{"--as", demoInstructor, sample}, code: 4},
		"STUDIO_AS a member": {env: map[string]string{"STUDIO_AS": demoMember}, args: []string{sample}, code: 4},
		"bad --map": {
			args: []string{"--as", demoOffice, sample, "--map", "shoeSize=Size"}, code: 2,
			stderr: `error: invalid --map "shoeSize=Size": want field=Header with a field of ` + fieldNames() + "\n",
		},
		"--map no pair": {args: []string{"--as", demoOffice, sample, "--map", "memberNo"}, code: 2},
		"no member number": {
			args: []string{"--as", demoOffice, importWrite(t, "names.csv", []byte("Vorname,Nachname\nIda,Example\n"))},
			code: 2, stderr: "error: --map memberNo: the field must be mapped\n",
		},
		"unknown --notes": {
			args: []string{"--as", demoOffice, sample, "--notes", "Shoe size"}, code: 2,
			stderr: "error: --notes: the file has no column \"Shoe size\"\n",
		},
		"missing file": {
			args: []string{"--as", demoOffice, missing}, code: 2,
			stderr: "error: open " + missing + ": no such file or directory\n",
		},
		"no file": {args: []string{"--as", demoOffice}, code: 2},
		"too many files": {
			args: []string{"--as", demoOffice, sample, sample}, code: 2,
			stderr: "error: accepts 1 arg(s), received 2\n",
		},
		"no database URL": {
			env: map[string]string{"DATABASE_URL": ""}, args: []string{"--as", demoOffice, sample},
			code: 2, stderr: "error: " + errNoDatabaseURL.Error() + "\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := demoStore(t)

			got := cliRun{st: st, env: tc.env}.exec(t, append([]string{"import"}, tc.args...)...)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("import %v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.args, got.code,
					got.stdout, got.stderr, tc.code, tc.stderr)
			}

			if runs := importRuns(t, st); len(runs) != 0 {
				t.Errorf("%d import runs", len(runs))
			}
		})
	}
}

// TestImportCommandEmptyFile: an empty file is the file's field error,
// named by the argument.
func TestImportCommandEmptyFile(t *testing.T) {
	t.Parallel()

	got := cliRun{st: demoStore(t)}.exec(t, "import", "--as", demoOffice, importWrite(t, "empty.csv", nil))
	if got.code != 2 || !strings.HasPrefix(got.stderr, "error: <file.csv>: ") ||
		!strings.Contains(got.stderr, "the file is empty") {
		t.Errorf("exit %d, stderr %q", got.code, got.stderr)
	}
}

// TestImportCommandUnmigrated: a database without the schema is refused
// before anything runs.
func TestImportCommandUnmigrated(t *testing.T) {
	t.Parallel()

	r := cliRun{env: map[string]string{"DATABASE_URL": newUnmigratedDatabase(t)}}

	got := r.exec(t, "import", "--as", demoOffice, importWrite(t, "members.csv", servicetest.SampleCSV))
	if got.code != 1 || got.stderr != "error: "+errSchemaOutdated.Error()+"\n" {
		t.Errorf("exit %d, stderr %q, want %v", got.code, got.stderr, errSchemaOutdated)
	}
}

// TestImportWriteFails: an import whose report cannot be written (stdout
// closed) fails with the write error.
func TestImportWriteFails(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	broken := errors.New("stdout is closed")

	root := newRoot(cli.System{
		Lookup: func(key string) (string, bool) {
			return "postgres://memory.example.test/studio", key == "DATABASE_URL"
		},
		OSUser: testOSUser,
		Open:   func(context.Context, string) (store.Store, func(), error) { return st, func() {}, nil },
		Clock:  clock.NewFixed(testNow),
		IDs:    idsOf(t),
	})
	root.SetArgs([]string{"import", "--as", demoOffice, importWrite(t, "members.csv", servicetest.SampleCSV)})
	root.SetOut(initFailingWriter{broken})

	var stderr bytes.Buffer

	root.SetErr(&stderr)

	if code := runMain(root, &stderr); code != 1 || stderr.String() != "error: stdout is closed\n" {
		t.Errorf("exit %d, stderr %q, want the write error", code, stderr.String())
	}
}
