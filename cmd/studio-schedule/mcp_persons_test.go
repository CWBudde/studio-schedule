package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listOf returns the array at key of the object doc; it fails the test if
// doc is no object or the key holds no array.
func listOf(t *testing.T, doc any, key string) []any {
	t.Helper()

	obj, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("%T is no object", doc)
	}

	list, ok := obj[key].([]any)
	if !ok {
		t.Fatalf("%s is no array: %v", key, obj[key])
	}

	return list
}

// wantSome fails the test if list is empty: a comparison of empty lists
// proves nothing.
func wantSome(t *testing.T, what string, list []any) {
	t.Helper()

	if len(list) == 0 {
		t.Errorf("%s is empty: the test proves nothing", what)
	}
}

// personIDOf returns the ID of the item of a search_persons document whose
// email is email.
func personIDOf(t *testing.T, doc any, email string) string {
	t.Helper()

	for _, item := range listOf(t, doc, "items") {
		if p, _ := item.(map[string]any); p["email"] == email {
			id, _ := p["id"].(string)

			return id
		}
	}

	t.Fatalf("no person with the email %s in %v", email, doc)

	return ""
}

// pick returns the keys of the object doc that it has.
func pick(t *testing.T, doc any, keys ...string) map[string]any {
	t.Helper()

	obj, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("%T is no object", doc)
	}

	out := map[string]any{}

	for _, k := range keys {
		if v, ok := obj[k]; ok {
			out[k] = v
		}
	}

	return out
}

// TestMCPPersonParity: each person tool returns its command's document on
// the same data. The demo has no outbox mails, so the mail log's mails of
// mail_test.go are added first.
func TestMCPPersonParity(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		addMailLog(t, storeOf(t, r))

		s := mcpSession(t, r, "--as", demoOffice, "--personal-data")

		// search_persons: the term on the command's stdin.
		amberly := s.document(t, "search_persons", map[string]any{"term": "amberly"})
		if n := len(listOf(t, amberly, "items")); n < 2 {
			t.Errorf("amberly matches %d persons, want several", n)
		}

		search := r
		search.stdin = "amberly\n"

		if want := commandDocument(t, search, "persons", "search", "--as", demoOffice); !reflect.DeepEqual(amberly, want) {
			t.Errorf("search_persons:\n%v\npersons search:\n%v", amberly, want)
		}

		// person_bookings: Vera has bookings and is on the full workshop's
		// waiting list.
		rushwick := s.document(t, "search_persons", map[string]any{"term": "rushwick"})
		vera := personIDOf(t, rushwick, "vera.rushwick@example.com")

		got := s.document(t, "person_bookings", map[string]any{"personId": vera})
		export := commandDocument(t, r, "persons", "export", "--as", demoOffice, vera)

		for _, key := range []string{"bookings", "waitlistEntries"} {
			list := listOf(t, got, key)
			wantSome(t, "person_bookings' "+key, list)

			if want := listOf(t, export, key); !reflect.DeepEqual(list, want) {
				t.Errorf("person_bookings' %s:\n%v\npersons export's:\n%v", key, list, want)
			}
		}

		show := commandDocument(t, r, "persons", "show", "--as", demoOffice, vera)
		if person, want := pick(t, got, "person")["person"],
			pick(t, show, "id", "givenName", "familyName", "email", "memberNo"); !reflect.DeepEqual(person, any(want)) {
			t.Errorf("person_bookings' person:\n%v\npersons show's:\n%v", person, want)
		}

		if keys := pick(t, got, "person", "bookings", "waitlistEntries"); len(keys) != len(got.(map[string]any)) {
			t.Errorf("person_bookings has keys beyond person, bookings and waitlistEntries: %v", got)
		}

		// person_passes: Clara's own pass, with the bookings it paid for.
		clara := personIDOf(t, amberly, "clara.amberly@example.org")

		passes := s.document(t, "person_passes", map[string]any{"personId": clara})
		wantSome(t, "person_passes' items", listOf(t, passes, "items"))

		if want := commandDocument(t, r, "passes", "list", "--as", demoOffice, "--person", clara); !reflect.DeepEqual(
			passes, want) {
			t.Errorf("person_passes:\n%v\npasses list --person:\n%v", passes, want)
		}

		// receipts: the demo issued its receipts from 2026-09-02 to 2026-09-15.
		for _, tc := range []struct {
			args map[string]any
			cmd  []string
		}{
			{map[string]any{}, nil},
			{map[string]any{"from": "2026-09-03", "to": "2026-09-12"}, []string{"--from", "2026-09-03", "--to", "2026-09-12"}},
			{map[string]any{"from": "2026-09-15"}, []string{"--from", "2026-09-15"}},
		} {
			got := s.document(t, "receipts", tc.args)
			wantSome(t, "receipts' items", listOf(t, got, "items"))

			want := commandDocument(t, r, append([]string{"receipts", "list", "--as", demoOffice}, tc.cmd...)...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("receipts %v:\n%v\nreceipts list:\n%v", tc.args, got, want)
			}
		}

		// statistics: the whole document, the top participants included.
		for _, tc := range []struct {
			args map[string]any
			cmd  []string
		}{
			{map[string]any{}, nil},
			{map[string]any{"range": "term"}, []string{"--range", "term"}},
			{map[string]any{"from": "2026-09-01", "to": "2026-09-30"}, []string{"--from", "2026-09-01", "--to", "2026-09-30"}},
		} {
			got := s.document(t, "statistics", tc.args)
			wantSome(t, "statistics' topParticipants", listOf(t, got, "topParticipants"))

			want := commandDocument(t, r, append([]string{"stats", "--as", demoOffice}, tc.cmd...)...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("statistics %v:\n%v\nstats:\n%v", tc.args, got, want)
			}
		}

		s.close(t)

		// mail_log: an admin's tool.
		s = mcpSession(t, r, "--as", demoAdmin, "--personal-data")

		for _, tc := range []struct {
			args map[string]any
			cmd  []string
		}{
			{map[string]any{}, nil},
			{map[string]any{"status": "failed"}, []string{"--status", "failed"}},
			{map[string]any{"template": "receipt"}, []string{"--template", "receipt"}},
			{map[string]any{"status": "failed", "template": "invite"}, []string{"--status", "failed", "--template", "invite"}},
		} {
			got := s.document(t, "mail_log", tc.args)
			wantSome(t, "mail_log's items", listOf(t, got, "items"))

			want := commandDocument(t, r, append([]string{"mail", "list", "--as", demoAdmin}, tc.cmd...)...)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("mail_log %v:\n%v\nmail list:\n%v", tc.args, got, want)
			}
		}
	})
}

// TestMCPSearchTermStaysPrivate (review focus 2): the person search's term
// reaches neither the log (the command's stderr) nor an audit event, also
// on a successful call.
func TestMCPSearchTermStaysPrivate(t *testing.T) {
	t.Parallel()

	const secret = "Zebulon"

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		office, _ := accountOf(t, st, demoOffice)
		s := mcpSession(t, r, "--as", demoOffice, "--personal-data")

		for _, term := range []string{secret, "Clara " + secret} {
			if items := listOf(t, s.document(t, "search_persons", map[string]any{"term": term}), "items"); len(items) != 0 {
				t.Errorf("%q matches %v", term, items)
			}
		}

		if stderr := s.close(t); strings.Contains(stderr, secret) {
			t.Errorf("the log has the term: %q", stderr)
		}

		calls := toolCalls(t, st)
		if len(calls) != 2 {
			t.Fatalf("%d tool calls audited, want 2", len(calls))
		}

		for _, e := range calls {
			if b, err := json.Marshal(e); err != nil || bytes.Contains(b, []byte(secret)) {
				t.Errorf("audit event %s (%v) has the term", b, err)
			}

			wantToolCall(t, e, office.ID, "search_persons", "ok")
		}
	})
}

// TestMCPPersonToolsNeedTheFlag (review focus 3): without --personal-data
// a person tool called by name is the SDK's unknown tool: nothing runs,
// and nothing is audited.
func TestMCPPersonToolsNeedTheFlag(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		admin, _ := accountOf(t, st, demoAdmin)
		s := mcpSession(t, r, "--as", demoAdmin)

		for _, tc := range []struct {
			tool string
			args map[string]any
		}{
			{"lesson", map[string]any{"lessonId": "00000000-0000-7000-8000-0000000000cd"}},
			{"search_persons", map[string]any{"term": "amberly"}},
			{"person_bookings", map[string]any{"personId": ppClara}},
			{"person_passes", map[string]any{"personId": ppClara}},
			{"receipts", map[string]any{}},
			{"mail_log", map[string]any{}},
		} {
			res, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err == nil || !strings.Contains(err.Error(), "unknown tool") {
				t.Errorf("%s without --personal-data: result %+v, error %v", tc.tool, res, err)
			}
		}

		if calls := toolCalls(t, st); len(calls) != 0 {
			t.Errorf("%d calls of unlisted tools audited: %+v", len(calls), calls)
		}

		// The session goes on.
		s.document(t, "schedule", nil)

		calls := toolCalls(t, st)
		if len(calls) != 1 {
			t.Fatalf("%d tool calls audited, want 1", len(calls))
		}

		wantToolCall(t, calls[0], admin.ID, "schedule", "ok")
	})
}

// TestMCPInstructorLesson: an instructor gets the participants of the
// lessons they teach, as the office sees them; another instructor's lesson
// is not found, audited as not-found.
func TestMCPInstructorLesson(t *testing.T) {
	t.Parallel()

	const alina = "Alina Starling" // demoInstructor's staff name

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		instructor, _ := accountOf(t, st, demoInstructor)

		var own, other string

		week := commandDocument(t, r, "lessons", "list", "--as", demoOffice, "--from", "2026-09-21", "--to",
			"2026-09-27")
		for _, item := range listOf(t, week, "lessons") {
			l, _ := item.(map[string]any)
			teacher, _ := l["instructor"].(map[string]any)
			accompanist, _ := l["accompanist"].(map[string]any)
			id, _ := l["id"].(string)

			switch {
			case teacher["displayName"] == alina && own == "":
				own = id
			case teacher != nil && teacher["displayName"] != alina && accompanist["displayName"] != alina &&
				other == "":
				other = id
			}
		}

		if own == "" || other == "" {
			t.Fatalf("the week has no lesson of %s (%q) or none of another instructor (%q)", alina, own, other)
		}

		s := mcpSession(t, r, "--as", demoInstructor, "--personal-data")

		mine := s.document(t, "lesson", map[string]any{"lessonId": own})
		wantSome(t, "the instructor's lesson's bookings", listOf(t, mine, "bookings"))

		if text, isError := s.callTool(t, "lesson", map[string]any{"lessonId": other}); !isError ||
			!strings.Contains(text, "not found") {
			t.Errorf("another instructor's lesson: result %q (error %t)", text, isError)
		}

		s.close(t)

		calls := toolCalls(t, st)
		if len(calls) != 2 {
			t.Fatalf("%d tool calls audited, want 2", len(calls))
		}

		wantToolCall(t, calls[0], instructor.ID, "lesson", "ok")
		wantToolCall(t, calls[1], instructor.ID, "lesson", "not-found")

		s = mcpSession(t, r, "--as", demoOffice, "--personal-data")
		if office := s.document(t, "lesson", map[string]any{"lessonId": own}); !reflect.DeepEqual(mine, office) {
			t.Errorf("the instructor's lesson:\n%v\nthe office's:\n%v", mine, office)
		}
	})
}
