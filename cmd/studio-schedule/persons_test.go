package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The demo persons the persons commands name (seed.LoadDemo).
const (
	ppClara     = "00000000-0000-7000-8000-000000000001" // guardian of Ella and Finn, demoMember
	ppEllaID    = "00000000-0000-7000-8000-000000000005" // dependent of Clara and Tom, two classes
	ppFinnID    = "00000000-0000-7000-8000-000000000006" // dependent of Clara and Tom
	ppHanna     = "00000000-0000-7000-8000-000000000007" // the only guardian of Lotta and Mia
	ppMia       = "00000000-0000-7000-8000-00000000000a" // dependent of Hanna
	ppOlga      = "00000000-0000-7000-8000-00000000003d" // demoOffice
	ppArno      = "00000000-0000-7000-8000-00000000003f" // demoAdmin
	ppLena      = "00000000-0000-7000-8000-000000000326" // Lena Quillfeather, M-1042
	ppLenaTwin  = "00000000-0000-7000-8000-000000000327" // her duplicate
	ppUnknownID = "00000000-0000-7000-8000-00000000ffff" // no person
)

// ppWith is r with the environment variables of kv (key, value, …) added.
func ppWith(r cliRun, kv ...string) cliRun {
	env := map[string]string{}
	for k, v := range r.env {
		env[k] = v
	}

	for i := 0; i+1 < len(kv); i += 2 {
		env[kv[i]] = kv[i+1]
	}

	r.env = env

	return r
}

// ppPerson returns the person id of st.
func ppPerson(t *testing.T, st store.Store, id string) domain.Person {
	t.Helper()

	pid, err := domain.ParseID[domain.PersonID](id)
	if err != nil {
		t.Fatal(err)
	}

	var p domain.Person

	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		p, err = tx.Persons().Get(ctx, pid)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return p
}

// ppGuardians returns how many guardians the person id of st has.
func ppGuardians(t *testing.T, st store.Store, id string) int {
	t.Helper()

	pid, err := domain.ParseID[domain.PersonID](id)
	if err != nil {
		t.Fatal(err)
	}

	var n int

	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		gs, err := tx.Persons().ListGuardians(ctx, pid)
		n = len(gs)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return n
}

// ppAudit returns the latest audit events of st, newest first.
func ppAudit(t *testing.T, st store.Store) []domain.AuditEvent {
	t.Helper()

	var events []domain.AuditEvent

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		events, err = tx.Audit().List(ctx, 20)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return events
}

// ppMergeLena merges Lena's duplicate into her as the office, which makes
// the duplicate a tombstone.
func ppMergeLena(t *testing.T, r cliRun) {
	t.Helper()

	got := r.exec(t, "persons", "merge", "--as", demoOffice, "--apply", "--yes", ppLena, ppLenaTwin)
	if got.code != 0 {
		t.Fatalf("merge: exit %d, stderr %q", got.code, got.stderr)
	}
}

// ppAnonymize anonymizes the person id as the office with --yes.
func ppAnonymize(t *testing.T, r cliRun, id string) result {
	t.Helper()

	got := r.exec(t, "persons", "anonymize", "--as", demoOffice, "--yes", id)
	if got.code != 0 || got.stderr != "" {
		t.Fatalf("anonymize %s: exit %d, stderr %q", id, got.code, got.stderr)
	}

	return got
}

// TestPersonsSearch: the term comes from stdin, after the prompt "Search: "
// on stderr on a terminal only; the table's header line is printed only on
// a terminal; JSON is the body of searchPersons; no term, or one given as
// an argument, is a usage error.
func TestPersonsSearch(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		r.stdin = "quill\n"

		got := r.exec(t, "persons", "search", "--as", demoOffice)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("search: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/search.txt", got.stdout)

		if strings.Contains(got.stdout, "Member no.") {
			t.Errorf("a header line without a terminal:\n%s", got.stdout)
		}

		got = r.exec(t, "persons", "search", "--as", demoOffice, "--output", "json")
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("search, JSON: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/search.json", got.stdout)

		var body struct {
			Items []struct {
				ID         string `json:"id"`
				FamilyName string `json:"familyName"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || len(body.Items) != 3 ||
			body.Items[0].ID != ppLena || body.Items[2].FamilyName != "Quillon" {
			t.Errorf("JSON body %+v, %v", body, err)
		}

		// On a terminal: the prompt on stderr, the header on stdout.
		term := ppWith(r, "NO_COLOR", "1")
		term.terminal = true
		term.stdin = "  QUILL  \n"

		got = term.exec(t, "persons", "search", "--as", demoOffice)
		if got.code != 0 || got.stderr != "Search: " {
			t.Fatalf("search on a terminal: exit %d, stderr %q", got.code, got.stderr)
		}

		lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
		if len(lines) != 4 || !strings.HasPrefix(lines[0], "ID  ") || !strings.Contains(lines[0], "Member no.  Email") ||
			!strings.Contains(lines[1], "Lena Quillfeather  M-1042") {
			t.Errorf("search on a terminal:\n%s", got.stdout)
		}

		// No term on stdin.
		for _, stdin := range []string{"", "\n", "   \n"} {
			r.stdin = stdin

			got = r.exec(t, "persons", "search", "--as", demoOffice)
			if got.code != 2 || got.stdout != "" || got.stderr != "error: no search term on stdin\n" {
				t.Errorf("stdin %q: exit %d, stdout %q, stderr %q", stdin, got.code, got.stdout, got.stderr)
			}
		}

		term.stdin = ""

		got = term.exec(t, "persons", "search", "--as", demoOffice)
		if got.code != 2 || got.stdout != "" || got.stderr != "Search: error: no search term on stdin\n" {
			t.Errorf("empty stdin on a terminal: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		// The term as an argument is refused before anything is read.
		r.stdin = "quill\n"

		got = r.exec(t, "persons", "search", "--as", demoOffice, "quill")
		if got.code != 2 || got.stdout != "" ||
			got.stderr != "error: unknown command \"quill\" for \"studio-schedule persons search\"\n" {
			t.Errorf("term as an argument: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}
	})
}

// TestPersonsShow: a person with guardians and classes, as text and as the
// body of getPerson; a tombstone names the person it was merged into, an
// anonymized person is "Deleted person" with the time in the school's time
// zone; an unknown ID is not found, an invalid one a usage error.
func TestPersonsShow(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "persons", "show", "--as", demoOffice, ppEllaID)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("show: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/show.txt", got.stdout)

		for _, want := range []string{"Guardians:\n", "Clara Amberly", "Tom Amberly", "Classes:\n", "Classical Ballet"} {
			if !strings.Contains(got.stdout, want) {
				t.Errorf("show lacks %q:\n%s", want, got.stdout)
			}
		}

		got = r.exec(t, "persons", "show", "--as", demoOffice, "--output", "json", ppEllaID)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("show, JSON: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/show.json", got.stdout)

		var body struct {
			ID        string            `json:"id"`
			GivenName string            `json:"givenName"`
			Guardians []json.RawMessage `json:"guardians"`
			Classes   []json.RawMessage `json:"classes"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || body.ID != ppEllaID ||
			body.GivenName != "Ella" || len(body.Guardians) != 2 || len(body.Classes) != 2 {
			t.Errorf("JSON body %+v, %v", body, err)
		}

		// A guardian with an account and dependents.
		got = r.exec(t, "persons", "show", "--as", demoOffice, ppClara)
		if got.code != 0 || !strings.Contains(got.stdout, "Account     clara.amberly@example.org (member, active)\n") ||
			!strings.Contains(got.stdout, "Dependents:\n") {
			t.Errorf("show Clara: exit %d, stderr %q\n%s", got.code, got.stderr, got.stdout)
		}

		// The tombstone of a merge, and the survivor listing it.
		ppMergeLena(t, r)

		got = r.exec(t, "persons", "show", "--as", demoOffice, ppLenaTwin)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("show tombstone: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/show-merged.txt", got.stdout)

		if !strings.Contains(got.stdout, "Merged into  Lena Quillfeather ("+ppLena+")\n") {
			t.Errorf("the tombstone does not name its survivor:\n%s", got.stdout)
		}

		got = r.exec(t, "persons", "show", "--as", demoOffice, ppLena)
		if got.code != 0 || !strings.Contains(got.stdout, "Merged:\n"+ppLenaTwin+"  Lena Quillfeather") {
			t.Errorf("show survivor: exit %d, stderr %q\n%s", got.code, got.stderr, got.stdout)
		}

		// An anonymized person.
		ppAnonymize(t, r, ppEllaID)

		got = r.exec(t, "persons", "show", "--as", demoOffice, ppEllaID)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("show anonymized: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/show-anonymized.txt", got.stdout)

		// testNow, 08:00 UTC, is 10:00 in the school's Europe/Berlin.
		if !strings.Contains(got.stdout, "Name        Deleted person\n") ||
			!strings.Contains(got.stdout, "Anonymized  2026-09-22 10:00\n") || strings.Contains(got.stdout, "Ella") {
			t.Errorf("show anonymized:\n%s", got.stdout)
		}

		// Not found, invalid.
		got = r.exec(t, "persons", "show", "--as", demoOffice, ppUnknownID)
		if got.code != 3 || got.stdout != "" || !strings.Contains(got.stderr, "not found") {
			t.Errorf("unknown: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		got = r.exec(t, "persons", "show", "--as", demoOffice, "not-an-id")
		if got.code != 2 || got.stdout != "" || !strings.HasPrefix(got.stderr, "error: ") {
			t.Errorf("invalid ID: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		got = r.exec(t, "persons", "show", "--as", demoOffice)
		if got.code != 2 || got.stdout != "" || got.stderr != "error: accepts 1 arg(s), received 0\n" {
			t.Errorf("no ID: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}
	})
}

// TestPersonsExport: the document goes to stdout alone, the same in both
// output formats; --file writes the same bytes into a 0600 file, refuses
// an existing one unless --force, which also makes it 0600; --force needs
// --file; an unknown person is not found, a tombstone is exported.
func TestPersonsExport(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "persons", "export", "--as", demoOffice, ppClara)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("export: exit %d, stderr %q", got.code, got.stderr)
		}

		doc := got.stdout

		golden(t, "persons/export.json", doc)

		got = r.exec(t, "persons", "export", "--as", demoOffice, "--output", "json", ppClara)
		if got.code != 0 || got.stderr != "" || got.stdout != doc {
			t.Errorf("export, JSON: exit %d, stderr %q; stdout differs from text: %v", got.code, got.stderr,
				got.stdout != doc)
		}

		var body struct {
			Format string `json:"format"`
			Person struct {
				ID string `json:"id"`
			} `json:"person"`
			Dependents []json.RawMessage `json:"dependents"`
		}
		if err := json.Unmarshal([]byte(doc), &body); err != nil || body.Format != "studio-schedule.person-export" ||
			body.Person.ID != ppClara || len(body.Dependents) != 2 {
			t.Errorf("document %+v, %v", body, err)
		}

		// --file: the same bytes, mode 0600, one line on stdout.
		dir := t.TempDir()
		file := filepath.Join(dir, "clara.json")

		got = r.exec(t, "persons", "export", "--as", demoOffice, ppClara, "--file", file)
		if got.code != 0 || got.stderr != "" ||
			got.stdout != "exported Clara Amberly ("+ppClara+") to "+file+"\n" {
			t.Errorf("--file: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		ppFileIs(t, file, doc)

		// An existing file is refused and left alone.
		got = r.exec(t, "persons", "export", "--as", demoOffice, ppEllaID, "-f", file)
		if got.code != 2 || got.stdout != "" ||
			got.stderr != "error: --file "+file+" exists; give --force to overwrite it\n" {
			t.Errorf("existing file: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		ppFileIs(t, file, doc)

		// --force replaces a longer 0644 file and makes it 0600; with
		// --output json, stdout stays empty.
		other := filepath.Join(dir, "other.json")
		if err := os.WriteFile(other, []byte(strings.Repeat("an older export\n", len(doc))), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(other, 0o644); err != nil {
			t.Fatal(err)
		}

		got = r.exec(t, "persons", "export", "--as", demoOffice, "--output", "json", "--force", "--file", other, ppClara)
		if got.code != 0 || got.stdout != "" || got.stderr != "" {
			t.Errorf("--force: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		ppFileIs(t, other, doc)

		// With --output json and a new file, stdout stays empty too.
		third := filepath.Join(dir, "third.json")

		got = r.exec(t, "persons", "export", "--as", demoOffice, "-o", "json", "-f", third, ppClara)
		if got.code != 0 || got.stdout != "" || got.stderr != "" {
			t.Errorf("--file, JSON: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		ppFileIs(t, third, doc)

		// --force needs --file.
		got = r.exec(t, "persons", "export", "--as", demoOffice, "--force", ppClara)
		if got.code != 2 || got.stdout != "" || got.stderr != "error: --force: give --file too\n" {
			t.Errorf("--force alone: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		// Unknown and invalid persons; nothing is written.
		missing := filepath.Join(dir, "missing.json")

		got = r.exec(t, "persons", "export", "--as", demoOffice, ppUnknownID, "--file", missing)
		if got.code != 3 || got.stdout != "" || !strings.Contains(got.stderr, "not found") {
			t.Errorf("unknown: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		got = r.exec(t, "persons", "export", "--as", demoOffice, "not-an-id", "--file", missing)
		if got.code != 2 || got.stdout != "" {
			t.Errorf("invalid ID: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Errorf("a refused export wrote %s: %v", missing, err)
		}

		// A tombstone is exported too.
		ppMergeLena(t, r)

		got = r.exec(t, "persons", "export", "--as", demoOffice, ppLenaTwin)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("export tombstone: exit %d, stderr %q", got.code, got.stderr)
		}

		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || body.Person.ID != ppLenaTwin {
			t.Errorf("tombstone document %+v, %v", body, err)
		}
	})
}

// ppFileIs checks that the file name holds want with mode 0600.
func ppFileIs(t *testing.T, name, want string) {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != want {
		t.Errorf("%s holds %d bytes, not the export's %d", name, len(data), len(want))
	}

	fi, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}

	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("%s has mode %v, want 0600", name, perm)
	}
}

// TestPersonsAnonymize: without a terminal it needs --yes; on a terminal it
// asks, and only y anonymizes; the result as text and as the body of
// anonymizePerson, the person anonymized in the store and the audit event
// naming the command line; the service's refusals with their exit codes.
func TestPersonsAnonymize(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		before := ppPerson(t, st, ppEllaID)

		// Neither a terminal nor --yes.
		got := r.exec(t, "persons", "anonymize", "--as", demoOffice, ppEllaID)
		if got.code != 2 || got.stdout != "" || got.stderr != "error: studio-schedule persons anonymize changes data: "+
			"confirm with --yes when stdin is not a terminal; nothing was changed\n" {
			t.Errorf("without --yes: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}

		// On a terminal, any answer but y is no.
		term := ppWith(r, "NO_COLOR", "1")
		term.terminal = true

		for _, answer := range []string{"n\n", "\n", ""} {
			term.stdin = answer

			got = term.exec(t, "persons", "anonymize", "--as", demoOffice, ppEllaID)
			if got.code != 2 || got.stdout != "" ||
				got.stderr != "Anonymize Ella Amberly? [y/N] error: not confirmed; nothing was changed\n" {
				t.Errorf("answer %q: exit %d, stdout %q, stderr %q", answer, got.code, got.stdout, got.stderr)
			}
		}

		if p := ppPerson(t, st, ppEllaID); p != before || ppGuardians(t, st, ppEllaID) != 2 {
			t.Fatalf("a refused anonymize changed Ella: %+v", p)
		}

		// y on a terminal anonymizes, the table with its header.
		term.stdin = "y\n"

		got = term.exec(t, "persons", "anonymize", "--as", demoOffice, ppMia)
		if got.code != 0 || got.stderr != "Anonymize Mia Birchmoor? [y/N] " ||
			!strings.HasPrefix(got.stdout, "anonymized Mia Birchmoor ("+ppMia+")\nWhat  ") ||
			!strings.Contains(got.stdout, "guardian links removed          1\n") {
			t.Errorf("y on a terminal: exit %d, stderr %q\n%s", got.code, got.stderr, got.stdout)
		}

		if !ppPerson(t, st, ppMia).Anonymized() {
			t.Error("Mia is not anonymized")
		}

		// --yes: text and JSON.
		got = ppAnonymize(t, r, ppEllaID)
		golden(t, "persons/anonymize.txt", got.stdout)

		ella := ppPerson(t, st, ppEllaID)
		if !ella.Anonymized() || ella.GivenName == "Ella" || !ella.BirthDate.IsZero() ||
			ppGuardians(t, st, ppEllaID) != 0 {
			t.Errorf("Ella after anonymize: %+v, %d guardians", ella, ppGuardians(t, st, ppEllaID))
		}

		events := ppAudit(t, st)
		if len(events) == 0 || events[0].Action != domain.AuditPersonAnonymized ||
			events[0].TargetPersonID != ella.ID || events[0].ActorID != accountID(t, st, demoOffice) ||
			events[0].Details.Via != (domain.AuditVia{Channel: domain.ChannelCLI, OSUser: testOSUser}) {
			t.Errorf("latest audit event %+v, want Ella's anonymization by the office via the CLI", events[0])
		}

		got = r.exec(t, "persons", "anonymize", "--as", demoOffice, "--yes", "--output", "json", ppFinnID)
		if got.code != 0 || got.stderr != "" {
			t.Fatalf("anonymize, JSON: exit %d, stderr %q", got.code, got.stderr)
		}

		golden(t, "persons/anonymize.json", got.stdout)

		var body struct {
			Person struct {
				ID         string `json:"id"`
				GivenName  string `json:"givenName"`
				FamilyName string `json:"familyName"`
			} `json:"person"`
			LinksRemoved int `json:"linksRemoved"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || body.Person.ID != ppFinnID ||
			body.Person.GivenName+" "+body.Person.FamilyName != "Deleted person" || body.LinksRemoved != 2 {
			t.Errorf("JSON body %+v, %v", body, err)
		}

		// The refusals, with --yes.
		ppMergeLena(t, r)

		for _, tc := range []struct {
			name, as, id string
			code         int
			stderr       string
		}{
			{name: "unknown", as: demoOffice, id: ppUnknownID, code: 3},
			{name: "invalid ID", as: demoOffice, id: "not-an-id", code: 2},
			{
				name: "anonymized", as: demoOffice, id: ppEllaID, code: 4,
				stderr: "error: anonymize person " + ppEllaID + ": conflict: the person is anonymized already\n",
			},
			{name: "merged", as: demoOffice, id: ppLenaTwin, code: 3},
			{name: "own person", as: demoOffice, id: ppOlga, code: 4},
			{
				name: "an admin's person", as: demoOffice, id: ppArno, code: 4,
				stderr: "error: anonymize person " + ppArno +
					": forbidden: only an admin anonymizes the person of an admin account\n",
			},
			{
				name: "sole guardian", as: demoOffice, id: ppHanna, code: 4,
				stderr: "error: anonymize person " + ppHanna +
					": conflict: the person is the only guardian of a dependent\n",
			},
		} {
			audit := len(ppAudit(t, st))

			got := r.exec(t, "persons", "anonymize", "--as", tc.as, "--yes", tc.id)
			if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.name, got.code, got.stdout,
					got.stderr, tc.code, tc.stderr)
			}

			if n := len(ppAudit(t, st)); n != audit {
				t.Errorf("%s: %d audit events, was %d", tc.name, n, audit)
			}
		}

		for _, id := range []string{ppOlga, ppArno, ppHanna} {
			if ppPerson(t, st, id).Anonymized() {
				t.Errorf("a refused anonymize anonymized %s", id)
			}
		}
	})
}

// TestPersonsSearchShowExportAnonymizeAccess: each command acts as an
// office account: without --as, with an unknown one or a role below the
// office it prints nothing and changes nothing.
func TestPersonsSearchShowExportAnonymizeAccess(t *testing.T) {
	t.Parallel()

	commands := map[string][]string{
		"search":    {"persons", "search"},
		"show":      {"persons", "show", ppClara},
		"export":    {"persons", "export", ppClara},
		"anonymize": {"persons", "anonymize", "--yes", ppEllaID},
	}

	for cmd, args := range commands {
		for name, tc := range map[string]struct {
			as     []string
			code   int
			stderr string
		}{
			"no --as": {
				code:   2,
				stderr: "error: studio-schedule " + strings.Join(args[:2], " ") + " acts as an account: give --as <email> or set STUDIO_AS\n",
			},
			"unknown --as": {
				as: []string{"--as", "nobody@example.org"}, code: 3,
				stderr: "error: --as nobody@example.org: not found: no account has this email\n",
			},
			"--as a member": {
				as: []string{"--as", demoMember}, code: 4,
				stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below office, " +
					"which this command needs\n",
			},
			"--as an instructor": {as: []string{"--as", demoInstructor}, code: 4},
		} {
			t.Run(cmd+"/"+name, func(t *testing.T) {
				t.Parallel()

				st := demoStore(t)
				audit := len(ppAudit(t, st))

				got := cliRun{st: st, stdin: "quill\n"}.exec(t, append(append([]string{}, args...), tc.as...)...)
				if got.code != tc.code || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
					t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", args, got.code, got.stdout,
						got.stderr, tc.code, tc.stderr)
				}

				if ppPerson(t, st, ppEllaID).Anonymized() || len(ppAudit(t, st)) != audit {
					t.Error("a refused command changed the data")
				}
			})
		}
	}
}

// ppAlina is the person of the instructor Alina Starling, linked to her
// staff record.
const ppAlina = "00000000-0000-7000-8000-000000000038"

// TestPersonsShowStaff: a person linked to a staff record lists it, with
// its kind and whether it is active.
func TestPersonsShowStaff(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "persons", "show", "--as", demoOffice, ppAlina)
		if got.code != 0 || got.stderr != "" || !strings.Contains(got.stdout, "Staff:\n") {
			t.Fatalf("show: exit %d, stderr %q\n%s", got.code, got.stderr, got.stdout)
		}

		golden(t, "persons/show-staff.txt", got.stdout)
	})
}

// TestPersonsShowMergedIntoAnonymized: a tombstone whose survivor was
// anonymized names it "Deleted person", as the web does.
func TestPersonsShowMergedIntoAnonymized(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}
	ppMergeLena(t, r)
	ppAnonymize(t, r, ppLena)

	got := r.exec(t, "persons", "show", "--as", demoOffice, ppLenaTwin)
	if want := "Merged into  " + deletedPersonName + " (" + ppLena + ")\n"; got.code != 0 ||
		!strings.Contains(got.stdout, want) || strings.Contains(got.stdout, "Lena Quillfeather (") {
		t.Errorf("show: exit %d, stderr %q, want %q in\n%s", got.code, got.stderr, want, got.stdout)
	}
}

// ppFaultStore is a store whose listing of the persons fails.
type ppFaultStore struct{ store.Store }

// errPPFault is the failure of a ppFaultStore.
var errPPFault = errors.New("the persons table is unreadable")

func (s ppFaultStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error { return fn(ctx, ppFaultTx{tx}) })
}

type ppFaultTx struct{ store.Tx }

func (tx ppFaultTx) Persons() store.PersonRepo { return ppFaultPersons{tx.Tx.Persons()} }

type ppFaultPersons struct{ store.PersonRepo }

func (ppFaultPersons) List(context.Context) ([]domain.Person, error) { return nil, errPPFault }

// TestPersonsSearchStoreFails: a store failure of the search is exit status
// 1, with nothing on stdout.
func TestPersonsSearchStoreFails(t *testing.T) {
	t.Parallel()

	got := cliRun{st: ppFaultStore{demoStore(t)}, stdin: "quill\n"}.exec(t, "persons", "search", "--as", demoOffice)
	if got.code != 1 || got.stdout != "" || got.stderr != "error: search persons: "+errPPFault.Error()+"\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}
}

// TestPersonsExportNoDirectory: a --file in a directory that does not
// exist fails with exit status 1, and stdout stays empty.
func TestPersonsExportNoDirectory(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "missing", "clara.json")

	got := cliRun{st: demoStore(t)}.exec(t, "persons", "export", "--as", demoOffice, "--file", file, ppClara)
	if got.code != 1 || got.stdout != "" || got.stderr != "error: open "+file+": no such file or directory\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}
}
