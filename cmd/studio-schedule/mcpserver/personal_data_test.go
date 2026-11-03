package mcpserver

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// personDataTools are the registry's read tools that return person data,
// sorted.
var personDataTools = []string{
	"lesson", "mail_log", "person_bookings", "person_passes", "receipts", "search_persons",
}

// writeTools are the registry's write tools, sorted, and
// personDataWriteTools those of them that return person data.
var (
	writeTools           = []string{"book_lesson", "cancel_booking", "cancel_lesson", "requeue_mail", "run_job"}
	personDataWriteTools = []string{"book_lesson", "cancel_booking", "requeue_mail"}
)

// TestPersonalDataUnlisted pins Review focus 3, "unlisted means
// uncallable": without --personal-data a session lists none of the person
// tools, a call by name fails as the SDK's unknown tool, nothing runs and
// nothing is audited. With the flag each role lists exactly the person
// tools its level reaches, and the flag adds nothing else.
func TestPersonalDataUnlisted(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		as    string
		tools []string // the person tools the role lists with --personal-data
	}{
		{demoAdmin, personDataTools},
		{demoOffice, []string{"lesson", "person_bookings", "person_passes", "receipts", "search_persons"}},
		{demoInstructor, []string{"lesson"}},
		{demoMember, nil},
	} {
		t.Run(tc.as, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t)
			cs := f.session(t, tc.as, Options{}, registry)

			without := toolNames(t, cs)
			if listed := personDataOf(without); len(listed) != 0 {
				t.Errorf("without --personal-data the session lists %v", listed)
			}

			for _, name := range personDataTools {
				_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
				if err == nil || !strings.Contains(err.Error(), `unknown tool "`+name+`"`) {
					t.Errorf("call %s without --personal-data: %v", name, err)
				}
			}

			if calls := toolCalls(t, f.st); len(calls) != 0 {
				t.Errorf("calls of unlisted tools were audited: %+v", calls)
			}

			with := toolNames(t, f.session(t, tc.as, Options{PersonalData: true}, registry))
			if got := personDataOf(with); !slices.Equal(got, tc.tools) {
				t.Errorf("with --personal-data the person tools are %v, want %v", got, tc.tools)
			}

			others := slices.DeleteFunc(slices.Clone(with), func(n string) bool { return slices.Contains(personDataTools, n) })
			if !slices.Equal(others, without) {
				t.Errorf("--personal-data changed the other tools: %v, without it %v", others, without)
			}
		})
	}
}

// personDataOf are the person tools of names, in their order.
func personDataOf(names []string) []string {
	var out []string

	for _, n := range names {
		if slices.Contains(personDataTools, n) {
			out = append(out, n)
		}
	}

	return out
}

// TestGoldensWithoutBirthDates: no tool's document has a birth date, the
// person data the tools leave out (ADR 0022); every golden in testdata/
// is checked, whichever test wrote it.
func TestGoldensWithoutBirthDates(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("goldens %v, %v", paths, err)
	}

	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		for _, key := range []string{`"birthDate"`, `"dateOfBirth"`} {
			if strings.Contains(string(b), key) {
				t.Errorf("%s has %s", path, key)
			}
		}
	}
}
