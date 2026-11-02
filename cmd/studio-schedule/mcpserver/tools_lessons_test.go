package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Lessons of the demo data the lesson tests read.
const (
	// lessonsTestWorkshop is Nina Tapworth's full Tap workshop on
	// 2026-10-02: four bookings and two waiting.
	lessonsTestWorkshop = "00000000-0000-7000-8000-000000000244"
	// lessonsTestAlinas is a lesson of Alina Starling's (demoInstructor)
	// on 2026-09-21, the day before testNow, with three bookings.
	lessonsTestAlinas = "00000000-0000-7000-8000-0000000000c8"
	// lessonsTestUnknown is an ID no lesson or person has.
	lessonsTestUnknown = "00000000-0000-7000-8000-000000000999"
)

// lessonsTestNoBirthDate fails the test if the result text of tool names
// a date of birth.
func lessonsTestNoBirthDate(t *testing.T, tool, text string) {
	t.Helper()

	for _, key := range []string{"birthDate", "dateOfBirth"} {
		if strings.Contains(text, key) {
			t.Errorf("%s: the result has %s: %s", tool, key, text)
		}
	}
}

// lessonsTestList returns the list key of doc, failing unless it has
// want entries.
func lessonsTestList(t *testing.T, doc map[string]any, key string, want int) []any {
	t.Helper()

	list, ok := doc[key].([]any)
	if !ok || len(list) != want {
		t.Fatalf("%s: %v, want %d entries", key, doc[key], want)
	}

	return list
}

// TestLessonOffice: the office gets any lesson with its bookings and its
// waiting list in order, the counts beside them, and no date of birth.
func TestLessonOffice(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	text, isError := callTool(t, cs, "lesson", map[string]any{"lessonId": lessonsTestWorkshop})
	if isError {
		t.Fatalf("lesson: error result %q", text)
	}

	golden(t, "lesson.json", text)
	lessonsTestNoBirthDate(t, "lesson", text)

	doc := document(t, cs, "lesson", map[string]any{"lessonId": lessonsTestWorkshop})
	if doc["lessonId"] != lessonsTestWorkshop || doc["active"] != 4.0 || doc["waiting"] != 2.0 || doc["freePlaces"] != 0.0 {
		t.Errorf("lesson %v", doc)
	}

	lessonsTestList(t, doc, "bookings", 4)

	waitlist := lessonsTestList(t, doc, "waitlist", 2)
	for i, name := range []string{"Vera Rushwick", "Wanda Sparrowhill"} {
		if e := waitlist[i].(map[string]any); e["name"] != name || e["position"] != float64(i+1) {
			t.Errorf("waitlist[%d] = %v, want %s at %d", i, e, name, i+1)
		}
	}
}

// TestLessonInstructor: an instructor gets a lesson they teach, and for
// another instructor's lesson the not-found error result, audited as
// not-found, exactly as for a lesson that does not exist.
func TestLessonInstructor(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoInstructor, Options{PersonalData: true}, registry)

	doc := document(t, cs, "lesson", map[string]any{"lessonId": lessonsTestAlinas})
	if doc["lessonId"] != lessonsTestAlinas {
		t.Errorf("own lesson: %v", doc)
	}

	lessonsTestList(t, doc, "bookings", 3)

	wantError(t, cs, "lesson", map[string]any{"lessonId": lessonsTestWorkshop},
		"participants of lesson "+lessonsTestWorkshop+": not found: lesson "+lessonsTestWorkshop)
	wantError(t, cs, "lesson", map[string]any{"lessonId": lessonsTestUnknown},
		"participants of lesson "+lessonsTestUnknown+": lesson: not found")

	calls := toolCalls(t, f.st)
	if len(calls) != 3 {
		t.Fatalf("%d tool calls audited, want 3", len(calls))
	}

	for i, want := range []string{"ok", "not-found", "not-found"} {
		if d := calls[i].Details; d.Tool != "lesson" || d.Outcome != want {
			t.Errorf("call %d audited as %+v, want lesson %s", i, d, want)
		}
	}
}

// TestLessonInput: lessonId is required and must be an ID; an unknown
// lesson is not found, and an unknown property is refused. The errors
// name the field, never its value.
func TestLessonInput(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	for _, tc := range []struct {
		args any
		text string
	}{
		{nil, "validation failed: lessonId is required"},
		{map[string]any{"lessonId": ""}, "validation failed: lessonId is required"},
		{map[string]any{"lessonId": "Zebulon"}, "validation failed: lessonId is not an ID"},
		{
			map[string]any{"lessonId": lessonsTestUnknown},
			"participants of lesson " + lessonsTestUnknown + ": lesson: not found",
		},
		{
			map[string]any{"lessonId": lessonsTestWorkshop, "personId": lessonsTestUnknown},
			`validation failed: the arguments do not fit the tool's input schema: json: unknown field "personId"`,
		},
		{
			map[string]any{"lessonId": 42},
			"validation failed: the arguments do not fit the tool's input schema: json: cannot unmarshal number " +
				"into Go struct field lessonInput.lessonId of type string",
		},
	} {
		wantError(t, cs, "lesson", tc.args, tc.text)
	}

	if strings.Contains(f.log.String(), "Zebulon") {
		t.Errorf("the log has the argument: %s", f.log.String())
	}
}

// TestLessonStoreFails: a store that fails while the participants are
// read is an unexpected error: "internal error" for the client, the cause
// in the log.
func TestLessonStoreFails(t *testing.T) {
	t.Parallel()

	st := &faultStore{Store: newFixture(t).st}
	f := fixtureOn(st)
	office := f.principal(t, demoOffice)
	cs := f.sessionOf(t, office, func(context.Context) (service.Principal, error) { return office, nil },
		Options{PersonalData: true}, registry)

	st.failing(store.ReadOnly, errors.New("connection reset"))

	wantError(t, cs, "lesson", map[string]any{"lessonId": lessonsTestWorkshop}, "internal error")

	if !strings.Contains(f.log.String(), "connection reset") {
		t.Errorf("log %q", f.log.String())
	}
}
