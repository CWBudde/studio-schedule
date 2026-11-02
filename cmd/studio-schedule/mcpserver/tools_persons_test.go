package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Persons of the demo data the person tests read.
const (
	// personsTestVera has four Barre Fitness bookings, one waiting-list
	// entry (the full workshop) and a voided pass.
	personsTestVera = "00000000-0000-7000-8000-000000000033"
	// personsTestElla has a 10-visit pass her mother Clara paid, used once.
	personsTestElla = "00000000-0000-7000-8000-000000000005"
	// personsTestFinn has no pass.
	personsTestFinn = "00000000-0000-7000-8000-000000000006"
	// personsTestKai has past bookings, a booking of the full workshop and
	// a 30-day pass; he has an account and is nobody's guardian.
	personsTestKai = "00000000-0000-7000-8000-000000000029"
	// personsTestLena and personsTestLenaTwin are the strong duplicate
	// pair of Lena Quillfeather; the twin has a birth date.
	personsTestLena     = "00000000-0000-7000-8000-00000000032a"
	personsTestLenaTwin = "00000000-0000-7000-8000-00000000032b"
)

// personsTestTools are the person tools this file tests, with an
// argument each takes.
var personsTestTools = []struct {
	name string
	args map[string]any
}{
	{"lesson", map[string]any{"lessonId": lessonsTestWorkshop}},
	{"search_persons", map[string]any{"term": "Amberly"}},
	{"person_bookings", map[string]any{"personId": personsTestVera}},
	{"person_passes", map[string]any{"personId": personsTestElla}},
}

// personsTestIDs returns the id of each entry of the list key of doc.
func personsTestIDs(t *testing.T, doc map[string]any, key, id string) []string {
	t.Helper()

	list, ok := doc[key].([]any)
	if !ok {
		t.Fatalf("%s: %v is no list", key, doc[key])
	}

	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, fmt.Sprint(e.(map[string]any)[id]))
	}

	return out
}

// TestPersonToolsNoBirthDate: no result of the person tools names a date
// of birth, the merged twin's (which has one) included.
func TestPersonToolsNoBirthDate(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	calls := append(slices.Clone(personsTestTools), []struct {
		name string
		args map[string]any
	}{
		{"search_persons", map[string]any{}},
		{"person_bookings", map[string]any{"personId": personsTestLenaTwin}},
		{"person_bookings", map[string]any{"personId": personsTestElla}},
		{"person_passes", map[string]any{"personId": personsTestVera}},
	}...)

	for _, c := range calls {
		text, isError := callTool(t, cs, c.name, c.args)
		if isError {
			t.Fatalf("%s %v: %s", c.name, c.args, text)
		}

		lessonsTestNoBirthDate(t, c.name, text)
	}
}

// TestSearchPersons: every word of term must match a name, an email or a
// member number, ignoring case; the matches are ordered by family and
// given name; a term without a match is an empty list, not an error.
func TestSearchPersons(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	text, isError := callTool(t, cs, "search_persons", map[string]any{"term": "Amberly"})
	if isError {
		t.Fatalf("search_persons: %s", text)
	}

	golden(t, "search_persons.json", text)
	lessonsTestNoBirthDate(t, "search_persons", text)

	for _, tc := range []struct {
		term string
		want []string
	}{
		{"amberly", []string{
			"00000000-0000-7000-8000-000000000001", personsTestElla, personsTestFinn,
			"00000000-0000-7000-8000-000000000003",
		}},
		{"AMBERLY ella", []string{personsTestElla}},
		{"kai.lanternby@example", []string{personsTestKai}},
		{"m-1042", []string{personsTestLena}},
		{"lena quillfeather", []string{personsTestLena, personsTestLenaTwin}},
		{"Zebulon", []string{}},
		{"amberly rushwick", []string{}},
	} {
		got := personsTestIDs(t, document(t, cs, "search_persons", map[string]any{"term": tc.term}), "items", "id")
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q: %v, want %v", tc.term, got, tc.want)
		}
	}

	if strings.Contains(f.log.String(), "Zebulon") {
		t.Errorf("the log has the term: %s", f.log.String())
	}

	for _, e := range toolCalls(t, f.st) {
		if e.Details.Tool != "search_persons" || e.Details.Outcome != "ok" || strings.Contains(fmt.Sprint(e), "Zebulon") {
			t.Errorf("audited %+v", e)
		}
	}
}

// TestSearchPersonsEmptyTerm: no term, or one of blanks only, lists the
// first persons, at most service.MaxPersonMatches of them.
func TestSearchPersonsEmptyTerm(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	// The demo's live persons, fewer than the most; then two more than the
	// most, the extra ones last by name.
	all := personsTestIDs(t, document(t, cs, "search_persons", nil), "items", "id")
	if len(all) != 44 {
		t.Fatalf("the demo's persons: %d, want 44", len(all))
	}

	err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		for i := range service.MaxPersonMatches - len(all) + 2 {
			id, err := domain.ParseID[domain.PersonID](fmt.Sprintf("00000000-0000-7000-8000-%012x", 0xfff000+i))
			if err != nil {
				return err
			}

			if err := tx.Persons().Create(ctx, domain.Person{
				ID: id, GivenName: "Extra", FamilyName: fmt.Sprintf("Zz%d", i),
			}); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, args := range []any{nil, map[string]any{"term": ""}, map[string]any{"term": "  "}} {
		got := personsTestIDs(t, document(t, cs, "search_persons", args), "items", "id")
		if len(got) != service.MaxPersonMatches || !slices.Equal(got[:len(all)], all) {
			t.Errorf("%v: %d persons, want the first %d", args, len(got), service.MaxPersonMatches)
		}
	}
}

// TestSearchPersonsMerged: a merged person (a tombstone) is not found any
// more, neither by its name nor by its email; the survivor is. Its
// bookings are what person_bookings still returns of it: the tombstone
// itself, with no booking or waiting-list entry left.
func TestSearchPersonsMerged(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	survivor, merged := personsTestID(t, personsTestLena),
		personsTestID(t, personsTestLenaTwin)
	if _, err := f.svc.Persons.Merge(t.Context(), f.principal(t, demoOffice),
		service.MergeRequest{SurvivorID: survivor, MergedID: merged}, true); err != nil {
		t.Fatal(err)
	}

	for _, term := range []string{"lena quillfeather", "LENA.Quillfeather@example.com", "Quillfeather"} {
		got := personsTestIDs(t, document(t, cs, "search_persons", map[string]any{"term": term}), "items", "id")
		if !slices.Equal(got, []string{personsTestLena}) {
			t.Errorf("%q after the merge: %v, want only the survivor", term, got)
		}
	}

	text, isError := callTool(t, cs, "person_bookings", map[string]any{"personId": personsTestLenaTwin})
	if want := `{"person":{"id":"` + personsTestLenaTwin + `","givenName":"Lena","familyName":"Quillfeather",` +
		`"email":"LENA.Quillfeather@example.com"},"bookings":[],"waitlistEntries":[]}`; isError || text != want {
		t.Errorf("person_bookings of the tombstone: %s (error %t), want %s", text, isError, want)
	}
}

// TestPersonBookings: the person and its bookings, canceled ones
// included, by lesson start, and its waiting-list entries; an unknown
// person is not found, and personId is required and must be an ID.
func TestPersonBookings(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	text, isError := callTool(t, cs, "person_bookings", map[string]any{"personId": personsTestVera})
	if isError {
		t.Fatalf("person_bookings: %s", text)
	}

	golden(t, "person_bookings.json", text)
	lessonsTestNoBirthDate(t, "person_bookings", text)

	doc := document(t, cs, "person_bookings", map[string]any{"personId": personsTestVera})
	if person := doc["person"].(map[string]any); person["id"] != personsTestVera || person["familyName"] != "Rushwick" {
		t.Errorf("person %v", person)
	}

	if got := len(personsTestIDs(t, doc, "bookings", "id")); got != 4 {
		t.Errorf("%d bookings, want 4", got)
	}

	if got := personsTestIDs(t, doc, "waitlistEntries", "id"); len(got) != 1 {
		t.Errorf("waiting-list entries %v, want one", got)
	}

	personsTestInput(t, cs, "person_bookings", "export person "+lessonsTestUnknown+": person: not found")
}

// TestPersonPasses: the person's passes with their uses and who paid; a
// person without a pass has none, an unknown person is not found, and
// personId is required and must be an ID.
func TestPersonPasses(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	text, isError := callTool(t, cs, "person_passes", map[string]any{"personId": personsTestElla})
	if isError {
		t.Fatalf("person_passes: %s", text)
	}

	golden(t, "person_passes.json", text)
	lessonsTestNoBirthDate(t, "person_passes", text)

	for _, tc := range []struct {
		person   string
		statuses string
	}{
		{personsTestElla, "[active]"},
		{personsTestVera, "[voided]"},
		{personsTestFinn, "[]"},
	} {
		doc := document(t, cs, "person_passes", map[string]any{"personId": tc.person})
		if got := fmt.Sprint(personsTestIDs(t, doc, "items", "status")); got != tc.statuses {
			t.Errorf("%s: passes %s, want %s", tc.person, got, tc.statuses)
		}
	}

	if text, _ := callTool(t, cs, "person_passes", map[string]any{"personId": personsTestFinn}); text != `{"items":[]}` {
		t.Errorf("no passes: %s", text)
	}

	personsTestInput(t, cs, "person_passes", "passes of person "+lessonsTestUnknown+": person: not found")
}

// personsTestInput checks the personId argument of tool: required, an ID,
// a person that exists (notFound is the error of an unknown one), and no
// other property.
func personsTestInput(t *testing.T, cs *mcp.ClientSession, tool, notFound string) {
	t.Helper()

	for _, tc := range []struct {
		args any
		text string
	}{
		{nil, "validation failed: personId is required"},
		{map[string]any{"personId": ""}, "validation failed: personId is required"},
		{map[string]any{"personId": "Zebulon"}, "validation failed: personId is not an ID"},
		{map[string]any{"personId": lessonsTestUnknown}, notFound},
		{
			map[string]any{"personId": personsTestVera, "term": "x"},
			`validation failed: the arguments do not fit the tool's input schema: json: unknown field "term"`,
		},
	} {
		wantError(t, cs, tool, tc.args, tc.text)
	}
}

// TestPersonAnonymized: an anonymized person is marked anonymized with
// the fixed name wherever the tools name it, never with its former name,
// and the search finds it neither by that name nor by the fixed one. Its
// past bookings stay; its booking of a lesson ahead is canceled on time,
// and that lesson still lists it, anonymized (Review focus 4).
func TestPersonAnonymized(t *testing.T) {
	t.Parallel()

	// The canceled booking frees a place of the full workshop, whose
	// waiting list's first is mailed an invite with a link.
	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	before := map[string]string{}
	for _, b := range document(t, cs, "person_bookings", map[string]any{"personId": personsTestKai})["bookings"].([]any) {
		b := b.(map[string]any)
		before[fmt.Sprint(b["id"])] = fmt.Sprint(b["status"], b["late"], b["canceledAt"])
	}

	sum, err := f.svc.Persons.Anonymize(t.Context(), f.principal(t, demoOffice),
		personsTestID(t, personsTestKai))
	if err != nil {
		t.Fatal(err)
	}

	if sum.BookingsCanceled != 2 || !sum.AccountDeleted {
		t.Fatalf("anonymize: %+v", sum)
	}

	former := []string{"Kai", "Lanternby", "kai.lanternby"}
	noFormerName := func(tool, text string) {
		t.Helper()

		for _, s := range former {
			if strings.Contains(text, s) {
				t.Errorf("%s names the former %q: %s", tool, s, text)
			}
		}
	}

	text, isError := callTool(t, cs, "person_bookings", map[string]any{"personId": personsTestKai})
	if isError {
		t.Fatalf("person_bookings: %s", text)
	}

	noFormerName("person_bookings", text)
	lessonsTestNoBirthDate(t, "person_bookings", text)

	doc := document(t, cs, "person_bookings", map[string]any{"personId": personsTestKai})
	if person := fmt.Sprint(doc["person"]); person != "map[anonymized:true familyName:person givenName:Deleted id:"+
		personsTestKai+"]" {
		t.Errorf("person %s", person)
	}

	// The bookings of lessons that start after testNow are canceled on
	// time, the others are as they were.
	bookings := doc["bookings"].([]any)

	var past, ahead []map[string]any

	for _, b := range bookings {
		b := b.(map[string]any)

		starts, err := time.Parse(time.RFC3339, fmt.Sprint(b["lesson"].(map[string]any)["startsAt"]))
		if err != nil {
			t.Fatal(err)
		}

		if starts.After(testNow) {
			ahead = append(ahead, b)
		} else {
			past = append(past, b)
		}
	}

	if len(bookings) != len(before) || len(ahead) != sum.BookingsCanceled || len(past) == 0 {
		t.Fatalf("bookings after anonymizing: %v", bookings)
	}

	for _, b := range ahead {
		if b["status"] != "canceled" || b["late"] != false {
			t.Errorf("a booking ahead: %v", b)
		}
	}

	for _, b := range past {
		if was := before[fmt.Sprint(b["id"])]; fmt.Sprint(b["status"], b["late"], b["canceledAt"]) != was {
			t.Errorf("a past booking: %v, was %s", b, was)
		}
	}

	if ahead[len(ahead)-1]["lesson"].(map[string]any)["id"] != lessonsTestWorkshop {
		t.Errorf("the workshop's booking is not among those ahead: %v", ahead)
	}

	for _, lesson := range []string{lessonsTestWorkshop, fmt.Sprint(past[0]["lesson"].(map[string]any)["id"])} {
		text, isError := callTool(t, cs, "lesson", map[string]any{"lessonId": lesson})
		if isError {
			t.Fatalf("lesson %s: %s", lesson, text)
		}

		noFormerName("lesson", text)

		doc := document(t, cs, "lesson", map[string]any{"lessonId": lesson})

		var found map[string]any

		for _, b := range doc["bookings"].([]any) {
			if b := b.(map[string]any); b["personId"] == personsTestKai {
				found = b
			}
		}

		if found == nil || found["anonymized"] != true || found["name"] != "Deleted person" {
			t.Errorf("lesson %s: Kai's booking %v", lesson, found)
		}
	}

	for _, term := range []string{"Lanternby", "kai.lanternby@example.org", "Deleted person"} {
		if got := personsTestIDs(t, document(t, cs, "search_persons", map[string]any{"term": term}), "items",
			"id"); len(got) != 0 {
			t.Errorf("%q finds %v", term, got)
		}
	}
}

// TestPersonToolsStoreFails: a store that fails while the tool reads is
// an unexpected error: "internal error" for the client, the cause in the
// log.
func TestPersonToolsStoreFails(t *testing.T) {
	t.Parallel()

	for _, tl := range personsTestTools[1:] {
		st := &faultStore{Store: newFixture(t).st}
		f := fixtureOn(st)
		office := f.principal(t, demoOffice)
		cs := f.sessionOf(t, office, func(context.Context) (service.Principal, error) { return office, nil },
			Options{PersonalData: true}, registry)

		st.failing(store.ReadOnly, errors.New("connection reset"))

		wantError(t, cs, tl.name, tl.args, "internal error")

		if !strings.Contains(f.log.String(), "connection reset") {
			t.Errorf("%s: log %q", tl.name, f.log.String())
		}
	}
}

// personsTestID parses the person ID s.
func personsTestID(t *testing.T, s string) domain.PersonID {
	t.Helper()

	id, err := domain.ParseID[domain.PersonID](s)
	if err != nil {
		t.Fatal(err)
	}

	return id
}
