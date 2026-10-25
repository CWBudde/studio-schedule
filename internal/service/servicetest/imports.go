package servicetest

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/charmap"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// SampleCSV is the fictional sample file of the import page (the web app
// has a copy for the demo's download): German headers and dates,
// semicolons, two guardians per row, one row with an impossible date.
//
//go:embed testdata/members-sample.csv
var SampleCSV []byte

// Import runs the member import scenarios (ADR 0015) on fresh stores from
// newStore.
func Import(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	for name, fn := range map[string]func(t *testing.T, w *world){
		"Windows1252":      importWindows1252,
		"UTF8WithBOM":      importUTF8WithBOM,
		"Matching":         importMatching,
		"RowProblems":      importRowProblems,
		"MappingProblems":  importMappingProblems,
		"SampleOnTheWorld": importSample,
		"FamilyEmail":      importFamilyEmail,
		"DryRunIDs":        importDryRunIDs,
		"Concurrent":       importConcurrent,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, newWorld(t, newStore(t)))
		})
	}
}

// germanFile is what a German spreadsheet saves: Windows-1252, semicolons,
// dates as DD.MM.YYYY, CRLF line ends, a quoted cell with a semicolon.
const germanFile = "Mitglieds-Nr.;Vorname;Nachname;Geburtsdatum;E-Mail;Bemerkung;Vorname Mutter;Nachname Mutter;" +
	"E-Mail Mutter;Telefon Mutter;Vorname Vater;Nachname Vater;Gruppe\r\n" +
	"A-1;Jürgen;Müßig;03.04.2015;;\"Allergie; Nüsse\";Käthe;Müßig;kaethe@example.org;+49 000 0000301;Björn;Müßig;Mo 17 Uhr\r\n" +
	"A-2;Zoë;Müßig;7.9.2017;;;Käthe;Müßig;KAETHE@example.org;;;;Mi 16 Uhr\r\n"

var germanMapping = service.ImportMapping{
	Columns: map[service.ImportField]string{
		service.FieldMemberNo: "Mitglieds-Nr.", service.FieldGivenName: "Vorname", service.FieldFamilyName: "Nachname",
		service.FieldBirthDate: "Geburtsdatum", service.FieldEmail: "E-Mail", service.FieldNotes: "Bemerkung",
		service.FieldGuardian1GivenName: "Vorname Mutter", service.FieldGuardian1FamilyName: "Nachname Mutter",
		service.FieldGuardian1Email: "E-Mail Mutter", service.FieldGuardian1Phone: "Telefon Mutter",
		service.FieldGuardian2GivenName: "Vorname Vater", service.FieldGuardian2FamilyName: "Nachname Vater",
	},
	Notes: []string{"Gruppe"},
}

func importWindows1252(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()

	data, err := charmap.Windows1252.NewEncoder().String(germanFile)
	if err != nil {
		t.Fatal(err)
	}

	file := service.ImportFile{Name: "Mitglieder.csv", Data: []byte(data)}

	insp, err := svc.Inspect(file)
	if err != nil {
		t.Fatal(err)
	}

	if insp.Encoding != "windows-1252" || insp.Delimiter != "semicolon" || insp.Rows != 2 ||
		insp.Sample[0][1] != "Jürgen" || insp.Sample[0][5] != "Allergie; Nüsse" {
		t.Errorf("inspection %+v", insp)
	}

	// The headers match by their German names.
	if !mappingEqual(insp.Suggested, service.ImportMapping{Columns: germanMapping.Columns}) {
		t.Errorf("suggested %+v, want %+v", insp.Suggested.Columns, germanMapping.Columns)
	}

	before := w.count()

	// The dry run tells what would happen and writes nothing.
	dry, err := svc.Import(ctx, w.office.AccountID, file, germanMapping, false)
	if err != nil {
		t.Fatal(err)
	}

	want := domain.ImportCounts{Rows: 2, Created: 2, GuardiansCreated: 2, LinksAdded: 3}
	if dry.Applied || !dry.RunID.IsZero() || dry.Counts != want {
		t.Errorf("dry run: applied %v, run %v, counts %+v; want %+v", dry.Applied, dry.RunID, dry.Counts, want)
	}

	if dry.Rows[0].PersonID != (domain.PersonID{}) || dry.Rows[0].Action != service.ImportCreate {
		t.Errorf("dry row %+v", dry.Rows[0])
	}

	if got := w.count(); got != before {
		t.Errorf("the dry run wrote: %+v, before %+v", got, before)
	}

	applied, err := svc.Import(ctx, w.office.AccountID, file, germanMapping, true)
	if err != nil {
		t.Fatal(err)
	}

	if !applied.Applied || applied.RunID.IsZero() || applied.Counts != want {
		t.Errorf("applied: %+v, counts %+v", applied.RunID, applied.Counts)
	}

	jurgen := w.byMemberNo("A-1")
	if jurgen.GivenName != "Jürgen" || jurgen.FamilyName != "Müßig" || jurgen.BirthDate != (domain.Date{Year: 2015, Month: time.April, Day: 3}) ||
		jurgen.Notes != "Allergie; Nüsse\nGruppe: Mo 17 Uhr" {
		t.Errorf("Jürgen %+v", jurgen)
	}

	zoe := w.byMemberNo("A-2")
	if zoe.GivenName != "Zoë" || zoe.BirthDate != (domain.Date{Year: 2017, Month: time.September, Day: 7}) {
		t.Errorf("Zoë %+v", zoe)
	}

	// Käthe was created once, with the phone of the first row, and found
	// again by her email in the second; both children are hers.
	kaethe := w.guardiansOf(jurgen.ID)
	if len(kaethe) != 2 || kaethe[0].GivenName != "Björn" || kaethe[1].GivenName != "Käthe" ||
		kaethe[1].Phone != "+49 000 0000301" {
		t.Fatalf("Jürgen's guardians %+v", kaethe)
	}

	if g := w.guardiansOf(zoe.ID); len(g) != 1 || g[0].ID != kaethe[1].ID {
		t.Errorf("Zoë's guardians %+v", g)
	}

	if applied.Rows[1].Guardians[0].MatchedBy != "email" || applied.Rows[1].Guardians[0].Action != service.ImportUnchanged {
		t.Errorf("second row's guardian %+v", applied.Rows[1].Guardians[0])
	}

	// The run is recorded with who ran it.
	runs, err := svc.ImportRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0].Run.FileName != "Mitglieder.csv" || runs[0].Run.Counts != want ||
		runs[0].Actor.Account.ID != w.office.AccountID {
		t.Fatalf("runs %+v, %v", runs, err)
	}

	// Importing the same file again changes nothing.
	again, err := svc.Import(ctx, domain.AccountID{}, file, germanMapping, true)
	if err != nil {
		t.Fatal(err)
	}

	if again.Counts != (domain.ImportCounts{Rows: 2, Unchanged: 2}) {
		t.Errorf("re-import counts %+v", again.Counts)
	}

	after := w.count()
	after.runs--

	if after != w.countAfter(before, 4, 3) {
		t.Errorf("re-import wrote: %+v", after)
	}
}

func importUTF8WithBOM(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()
	file := service.ImportFile{Name: "members.csv", Data: []byte("\xEF\xBB\xBFMember No,First name,Last name,Date of birth,Phone\n" +
		"7,Ada,Lovelace-Example,1990-12-10,+49 000 0000401\n\n" +
		"8,\"Grace, Jr.\",Example,,\n")}

	insp, err := svc.Inspect(file)
	if err != nil || insp.Encoding != "utf-8" || insp.Delimiter != "comma" || insp.Headers[0] != "Member No" || insp.Rows != 2 {
		t.Fatalf("inspection %+v, %v", insp, err)
	}

	res, err := svc.Import(ctx, w.office.AccountID, file, insp.Suggested, true)
	if err != nil {
		t.Fatal(err)
	}

	if res.Counts.Created != 2 || res.Rows[1].Line != 4 {
		t.Errorf("result %+v", res)
	}

	if p := w.byMemberNo("8"); p.GivenName != "Grace, Jr." || !p.BirthDate.IsZero() {
		t.Errorf("Grace %+v", p)
	}
}

func importMatching(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()

	// Mia is a member already; her mother has an account; her father is a
	// person without one.
	mia := w.person("Mia", "Example", "mia@example.org")
	mia.MemberNo, mia.Phone, mia.Notes = "M-1", "+49 000 0000501", "Office note."
	mia = w.update(mia)

	mother := w.person("Maren", "Example", "")
	w.account(mother, "Maren.Example@example.org")
	father := w.person("Felix", "Example", "felix@example.org")

	file := service.ImportFile{Name: "update.csv", Data: []byte(
		"memberNo;givenName;familyName;email;phone;notes;guardian1Email;guardian2GivenName;guardian2FamilyName\n" +
			"M-1;Mia;Example-Neu;;;;maren.example@EXAMPLE.org;Felix;Example\n")}
	mapping := service.ImportMapping{Columns: map[service.ImportField]string{
		service.FieldMemberNo: "memberNo", service.FieldGivenName: "givenName", service.FieldFamilyName: "familyName",
		service.FieldEmail: "email", service.FieldPhone: "phone", service.FieldNotes: "notes",
		service.FieldGuardian1Email: "guardian1Email", service.FieldGuardian1GivenName: "givenName",
		service.FieldGuardian1FamilyName: "familyName",
		service.FieldGuardian2GivenName:  "guardian2GivenName", service.FieldGuardian2FamilyName: "guardian2FamilyName",
	}}

	// A column fills one field only.
	_, err := svc.Import(ctx, w.office.AccountID, file, mapping, true)
	wantField(t, err, "mapping.columns.guardian1GivenName")

	mapping.Columns[service.FieldGuardian1GivenName] = ""
	mapping.Columns[service.FieldGuardian1FamilyName] = ""
	_, err = svc.Import(ctx, w.office.AccountID, file, mapping, true)
	wantField(t, err, "mapping.columns.guardian1GivenName") // a guardian needs its names mapped

	delete(mapping.Columns, service.FieldGuardian1GivenName)
	delete(mapping.Columns, service.FieldGuardian1FamilyName)
	delete(mapping.Columns, service.FieldGuardian1Email)
	file.Data = []byte("memberNo;givenName;familyName;email;phone;notes;g1Given;g1Family;g1Email;guardian2GivenName;guardian2FamilyName\n" +
		"M-1;Mia;Example-Neu;;;;Maren;Example;maren.example@EXAMPLE.org;Felix;Example\n")
	mapping.Columns[service.FieldGuardian1GivenName] = "g1Given"
	mapping.Columns[service.FieldGuardian1FamilyName] = "g1Family"
	mapping.Columns[service.FieldGuardian1Email] = "g1Email"

	res, err := svc.Import(ctx, w.office.AccountID, file, mapping, true)
	if err != nil {
		t.Fatal(err)
	}

	row := res.Rows[0]
	if row.Action != service.ImportUpdate || row.PersonID != mia.ID || !slices.Equal(row.Changes, []service.ImportField{service.FieldFamilyName}) {
		t.Errorf("row %+v", row)
	}

	// Only the filled field changed; the empty cells kept the rest.
	got := w.get(mia.ID)
	if got.FamilyName != "Example-Neu" || got.Phone != "+49 000 0000501" || got.Email != "mia@example.org" ||
		got.Notes != "Office note." {
		t.Errorf("Mia %+v", got)
	}

	// The mother by her account's email, ignoring case; the father by name.
	if len(row.Guardians) != 2 || row.Guardians[0].PersonID != mother.ID || row.Guardians[0].MatchedBy != "email" ||
		row.Guardians[1].PersonID != father.ID || row.Guardians[1].MatchedBy != "name" ||
		!row.Guardians[0].Linked || !row.Guardians[1].Linked {
		t.Errorf("guardians %+v", row.Guardians)
	}

	if g := w.guardiansOf(mia.ID); len(g) != 2 {
		t.Errorf("Mia's guardians %+v", g)
	}

	// A guardian link is never removed: a file without guardians keeps them.
	file.Data = []byte("memberNo;givenName;familyName;email;phone;notes;g1Given;g1Family;g1Email;guardian2GivenName;guardian2FamilyName\n" +
		"M-1;Mia;Example-Neu;;;;;;;;\n")

	res, err = svc.Import(ctx, w.office.AccountID, file, mapping, true)
	if err != nil || res.Counts != (domain.ImportCounts{Rows: 1, Unchanged: 1}) {
		t.Errorf("unchanged row: %+v, %v", res.Counts, err)
	}

	if g := w.guardiansOf(mia.ID); len(g) != 2 {
		t.Errorf("Mia's guardians after a file without them: %+v", g)
	}
}

func importRowProblems(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()

	// Two persons with the same name and no email make a guardian by that
	// name ambiguous; Kai is a dependent of Uli, so Kai cannot be Uli's
	// guardian.
	w.person("Sam", "Twin", "")
	w.person("Sam", "Twin", "")

	uli := w.person("Uli", "Example", "")
	uli.MemberNo = "U-1"
	uli = w.update(uli)
	kai := w.person("Kai", "Example", "kai@example.org")
	w.link(uli, kai)

	file := service.ImportFile{Name: "problems.csv", Data: []byte(
		"no;given;family;born;mail;g given;g family;g mail\n" +
			";Nora;Example;;;;;\n" + // no member number
			"P-1;Pia;;;;;;\n" + // no family name
			"P-2;Pit;Example;2030-01-01;not-an-email;;;\n" + // a future birth date and a bad email
			"P-3;Paul;Example;12/03/2015;;;;\n" + // a date in neither form
			"P-4;Pina;Example;;;Sam;Twin;\n" + // an ambiguous guardian
			"U-1;Uli;Example;;;Kai;Example;kai@example.org\n" + // a cycle
			"P-5;Pete;Example;;;;;\n" +
			"P-5;Pete;Example;;;;;\n")} // the member number again
	mapping := service.ImportMapping{Columns: map[service.ImportField]string{
		service.FieldMemberNo: "no", service.FieldGivenName: "given", service.FieldFamilyName: "family",
		service.FieldBirthDate: "born", service.FieldEmail: "mail", service.FieldGuardian1GivenName: "g given",
		service.FieldGuardian1FamilyName: "g family", service.FieldGuardian1Email: "g mail",
	}}

	before := w.count()

	res, err := svc.Import(ctx, w.office.AccountID, file, mapping, true)
	if err != nil {
		t.Fatal(err)
	}

	want := [][]service.ImportIssue{
		{{Code: service.IssueRequired, Field: service.FieldMemberNo}},
		{{Code: service.IssueRequired, Field: service.FieldFamilyName}},
		{{Code: service.IssueInvalid, Field: service.FieldEmail}, {Code: service.IssueFuture, Field: service.FieldBirthDate}},
		{{Code: service.IssueInvalid, Field: service.FieldBirthDate}},
		{{Code: service.IssueGuardianAmbiguous, Field: service.FieldGuardian1GivenName}},
		{{Code: service.IssueGuardianCycle, Field: service.FieldGuardian1GivenName}},
		nil,
		{{Code: service.IssueMemberNoRepeated, Field: service.FieldMemberNo}},
	}

	for i, r := range res.Rows {
		if !slices.Equal(r.Issues, want[i]) {
			t.Errorf("line %d: issues %+v, want %+v", r.Line, r.Issues, want[i])
		}
	}

	if res.Counts != (domain.ImportCounts{Rows: 8, Created: 1, Failed: 7}) {
		t.Errorf("counts %+v", res.Counts)
	}

	// Only Pete was created; the run records the first problem of each
	// failed row.
	if got := w.count(); got != w.countAfter(before, 1, 0) {
		t.Errorf("wrote %+v, before %+v", got, before)
	}

	runs, err := w.imports().ImportRuns(ctx)
	if err != nil || len(runs) != 1 || len(runs[0].Run.Errors) != 7 ||
		runs[0].Run.Errors[0] != (domain.ImportRowError{Line: 2, Code: "required", Field: "memberNo"}) {
		t.Errorf("runs %+v, %v", runs, err)
	}
}

func importMappingProblems(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()
	file := service.ImportFile{Name: "x.csv", Data: []byte("a;b;c\n1;2;3\n")}

	for want, m := range map[string]service.ImportMapping{
		"mapping.columns.memberNo": {Columns: map[service.ImportField]string{
			service.FieldGivenName: "b", service.FieldFamilyName: "c",
		}},
		"mapping.columns.givenName": {Columns: map[service.ImportField]string{
			service.FieldMemberNo: "a", service.FieldGivenName: "nope", service.FieldFamilyName: "c",
		}},
		"mapping.notes": {Columns: map[service.ImportField]string{
			service.FieldMemberNo: "a", service.FieldGivenName: "b", service.FieldFamilyName: "c",
		}, Notes: []string{"A"}},
		"mapping.columns": {Columns: map[service.ImportField]string{
			service.FieldMemberNo: "a", service.FieldGivenName: "b", service.FieldFamilyName: "c", "shoeSize": "x",
		}},
	} {
		_, err := svc.Import(ctx, w.office.AccountID, file, m, false)
		wantField(t, err, want)
	}

	for _, data := range []string{"", "\n\n", "a;a\n1;2\n", "a;\n", "a;b\n1;2;3\n"} {
		_, err := svc.Inspect(service.ImportFile{Name: "bad.csv", Data: []byte(data)})
		wantField(t, err, "content")
	}
}

// importSample imports the sample file of the import page into a world
// with a family it names: the guardians are found by account email and by
// name, a guardian of two rows is created once, and the impossible date is
// reported.
func importSample(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()

	lena := w.person("Lena", "Quillfeather", "lena.quillfeather@example.com")
	lena.MemberNo = "M-1042"
	w.update(lena)

	clara := w.person("Clara", "Amberly", "clara.amberly@example.org")
	w.account(clara, "clara.amberly@example.org")
	tom := w.person("Tom", "Amberly", "tom.amberly@example.org")
	w.account(tom, "tom.amberly@example.org")

	file := service.ImportFile{Name: "members-sample.csv", Data: SampleCSV}

	insp, err := svc.Inspect(file)
	if err != nil {
		t.Fatal(err)
	}

	mapping := insp.Suggested
	mapping.Notes = []string{"Kurs"}

	for _, f := range service.ImportFields() {
		if _, ok := mapping.Columns[f]; !ok && f != service.FieldNotes && f != service.FieldGuardian2Phone {
			t.Errorf("the sample's %s is not recognized: %v", f, insp.Headers)
		}
	}

	res, err := svc.Import(ctx, w.office.AccountID, file, mapping, true)
	if err != nil {
		t.Fatal(err)
	}

	want := domain.ImportCounts{Rows: 6, Created: 4, Updated: 1, Failed: 1, GuardiansCreated: 2, LinksAdded: 5}
	if res.Counts != want {
		t.Errorf("counts %+v, want %+v", res.Counts, want)
	}

	if g := res.Rows[1].Guardians; len(g) != 2 || g[0].PersonID != clara.ID || g[1].PersonID != tom.ID {
		t.Errorf("Paula's guardians %+v", g)
	}

	if issues := res.Rows[5].Issues; len(issues) != 1 || issues[0].Field != service.FieldBirthDate {
		t.Errorf("Ina's issues %+v", issues)
	}
}

// counts are what an import may write.
type counts struct{ persons, links, runs int }

func (w *world) count() counts {
	w.t.Helper()

	return read(w, func(ctx context.Context, tx store.Tx) (counts, error) {
		persons, err := tx.Persons().List(ctx)
		if err != nil {
			return counts{}, err
		}

		ids := mapIDs(persons, func(p domain.Person) domain.PersonID { return p.ID })

		links, err := tx.Persons().ListGuardianLinks(ctx, ids)
		if err != nil {
			return counts{}, err
		}

		runs, err := tx.Imports().List(ctx, 1000)

		return counts{len(persons), len(links), len(runs)}, err
	})
}

// countAfter is before with persons and links added and one run.
func (w *world) countAfter(before counts, persons, links int) counts {
	return counts{before.persons + persons, before.links + links, before.runs + 1}
}

func (w *world) byMemberNo(no string) domain.Person {
	w.t.Helper()

	return read(w, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
		return tx.Persons().GetByMemberNo(ctx, no)
	})
}

func (w *world) guardiansOf(id domain.PersonID) []domain.Person {
	w.t.Helper()

	return read(w, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
		return tx.Persons().ListGuardians(ctx, id)
	})
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()

	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Errorf("want a validation error on %s, got %v", field, err)
	}
}

func mappingEqual(a, b service.ImportMapping) bool {
	if len(a.Columns) != len(b.Columns) || !slices.Equal(a.Notes, b.Notes) {
		return false
	}

	for f, h := range a.Columns {
		if !strings.EqualFold(b.Columns[f], h) {
			return false
		}
	}

	return true
}

// familyMapping maps the headers of the family files below.
var familyMapping = service.ImportMapping{Columns: map[service.ImportField]string{
	service.FieldMemberNo: "no", service.FieldGivenName: "given", service.FieldFamilyName: "family",
	service.FieldEmail: "mail", service.FieldGuardian1GivenName: "g given", service.FieldGuardian1FamilyName: "g family",
	service.FieldGuardian1Email: "g mail",
}}

// importFamilyEmail: children often carry their parent's email. A member
// or a sibling with the guardian's email is not the guardian: an email
// match counts only with the guardian's name; and a new member is no
// possible duplicate of its own family by the email alone.
func importFamilyEmail(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()

	// Mia and Tom, members with their mother's email; the mother is a
	// person without one.
	mia := w.person("Mia", "Example", "parent@example.org")
	mia.MemberNo = "F-1"
	mia = w.update(mia)
	tom := w.person("Tom", "Example", "parent@example.org")
	tom.MemberNo = "F-2"
	w.update(tom)
	anna := w.person("Anna", "Example", "")

	file := service.ImportFile{Name: "family.csv", Data: []byte(
		"no;given;family;mail;g given;g family;g mail\n" +
			"F-1;Mia;Example;parent@example.org;Anna;Example;parent@example.org\n" +
			"F-2;Tom;Example;parent@example.org;Anna;Example;parent@example.org\n" +
			"F-3;Lea;Example;parent@example.org;Anna;Example;parent@example.org\n")}

	res, err := svc.Import(ctx, w.office.AccountID, file, familyMapping, true)
	if err != nil {
		t.Fatal(err)
	}

	// The first row finds Anna by name and gives her the email; the later
	// rows find her by it.
	for i, r := range res.Rows {
		by := "email"
		if i == 0 {
			by = "name"
		}

		if len(r.Issues) > 0 || len(r.Guardians) != 1 || r.Guardians[0].PersonID != anna.ID ||
			r.Guardians[0].MatchedBy != by || !r.Guardians[0].Linked {
			t.Errorf("line %d: issues %+v, guardians %+v; want Anna, found by %s", r.Line, r.Issues, r.Guardians, by)
		}
	}

	// Lea is new, with her siblings' and mother's email: no duplicate.
	if lea := res.Rows[2]; lea.Action != service.ImportCreate || len(lea.Duplicates) != 0 {
		t.Errorf("Lea %+v; want created without duplicates", lea)
	}

	if g := w.guardiansOf(mia.ID); len(g) != 1 || g[0].ID != anna.ID {
		t.Errorf("Mia's guardians %+v", g)
	}

	// Again, nothing changes.
	res, err = svc.Import(ctx, w.office.AccountID, file, familyMapping, true)
	if err != nil {
		t.Fatal(err)
	}

	if g := res.Rows[0].Guardians; res.Rows[0].Action != service.ImportUnchanged || g[0].PersonID != anna.ID ||
		g[0].Action != service.ImportUnchanged || g[0].Linked {
		t.Errorf("again: %+v, guardians %+v", res.Rows[0], g)
	}
}

// importDryRunIDs: a dry run names no person it would create, also where a
// later row finds it again or names it as a possible duplicate.
func importDryRunIDs(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()
	file := service.ImportFile{Name: "new.csv", Data: []byte(
		"no;given;family;mail;g given;g family;g mail\n" +
			"N-1;Ada;Example;ada@example.org;Gina;Example;gina@example.org\n" +
			"N-2;Ben;Example;;Gina;Example;gina@example.org\n" +
			"N-3;Cleo;Other;ada@example.org;;;\n")}

	check := func(apply bool) {
		t.Helper()

		res, err := svc.Import(ctx, w.office.AccountID, file, familyMapping, apply)
		if err != nil {
			t.Fatal(err)
		}

		gina, cleo := res.Rows[1].Guardians, res.Rows[2]
		if len(gina) != 1 || gina[0].Action != service.ImportUnchanged || gina[0].MatchedBy != "email" ||
			len(cleo.Duplicates) != 1 || cleo.Duplicates[0].GivenName != "Ada" {
			t.Fatalf("apply %v: Gina %+v, Cleo's duplicates %+v", apply, gina, cleo.Duplicates)
		}

		ids := []domain.PersonID{res.Rows[0].PersonID, res.Rows[0].Guardians[0].PersonID, gina[0].PersonID, cleo.Duplicates[0].ID}
		for i, id := range ids {
			if id.IsZero() == apply {
				t.Errorf("apply %v: ID %d is %v", apply, i, id)
			}
		}

		if apply && (gina[0].PersonID != res.Rows[0].Guardians[0].PersonID || cleo.Duplicates[0].ID != res.Rows[0].PersonID) {
			t.Errorf("applied IDs differ: %v", ids)
		}
	}

	check(false)
	check(true)
}

// importConcurrent runs imports that touch the same members in opposite
// orders at once: they lock their persons in one order and do not
// deadlock.
func importConcurrent(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.imports()

	for _, no := range []string{"C-1", "C-2", "C-3"} {
		p := w.person("Cora", "Example-"+no, "")
		p.MemberNo = no
		w.update(p)
	}

	mapping := service.ImportMapping{Columns: map[service.ImportField]string{
		service.FieldMemberNo: "no", service.FieldGivenName: "given", service.FieldFamilyName: "family",
		service.FieldEmail: "mail",
	}}

	// Each round, two files update the three members, one in each order.
	file := func(round, i int, order ...string) service.ImportFile {
		data := "no;given;family;mail\n"
		for _, no := range order {
			data += fmt.Sprintf("%s;Cora;Example-%s;r%d.f%d@example.org\n", no, no, round, i)
		}

		return service.ImportFile{Name: fmt.Sprintf("round-%d-%d.csv", round, i), Data: []byte(data)}
	}

	for round := range 5 {
		files := []service.ImportFile{file(round, 0, "C-1", "C-2", "C-3"), file(round, 1, "C-3", "C-2", "C-1")}
		errs := make(chan error, len(files))

		for _, f := range files {
			go func() {
				_, err := svc.Import(ctx, w.office.AccountID, f, mapping, true)
				errs <- err
			}()
		}

		for range files {
			if err := <-errs; err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
	}
}
