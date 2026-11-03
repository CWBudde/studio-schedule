package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Lessons of the demo data the cancel_lesson tests write.
const (
	// clJazz is today's Jazz Dance (2026-09-22, 17:00 school time), after
	// testNow, with three bookings.
	clJazz = bkJazz
	// clAfternoon is a lesson earlier today, after testNow, with two
	// bookings.
	clAfternoon = "00000000-0000-7000-8000-0000000000cc"
	// clStarted is yesterday's Creative Dance, which has started.
	clStarted = "00000000-0000-7000-8000-0000000000c7"
)

// clOptions are the options of a session with cancel_lesson.
var clOptions = Options{AllowWrites: []WriteGroup{WriteLessons}}

// clLesson returns the stored lesson id.
func clLesson(t *testing.T, f *fixture, id string) domain.Lesson {
	t.Helper()

	var l domain.Lesson

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		l, err = tx.Lessons().Get(ctx, bkLessonID(t, id))

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return l
}

// clAdmin returns the admin part of a cancel_lesson document.
func clAdmin(t *testing.T, text string) map[string]any {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}

	admin, ok := doc["admin"].(map[string]any)
	if !ok {
		t.Fatalf("no admin part in %s", text)
	}

	return admin
}

// TestCancelLesson: the office cancels a future lesson with a note. The
// result is the lesson as the schedule shows it to the office
// (cancelLesson's body); the store's lesson is canceled by the office's
// account, everyone booked is mailed, and the call is audited. A second
// call returns the same document, keeps the first note and mails nobody.
func TestCancelLesson(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, clOptions, registry)

	if n := len(bkMails(t, f, mail.TemplateLessonCanceled)); n != 0 {
		t.Fatalf("%d lesson-canceled mails before", n)
	}

	text, isError := callTool(t, cs, "cancel_lesson", map[string]any{"lessonId": clJazz, "note": "The teacher is ill."})
	if isError {
		t.Fatalf("cancel_lesson: error result %q", text)
	}

	golden(t, "cancel_lesson.json", text)

	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}

	// Without --personal-data the document does not name the account that
	// canceled the lesson (TestCancelLessonPersonalData).
	admin := clAdmin(t, text)
	if _, by := admin["canceledBy"]; doc["id"] != clJazz || doc["status"] != "canceled" ||
		admin["cancelNote"] != "The teacher is ill." || admin["activeBookings"] != 3.0 || by {
		t.Errorf("lesson %v", doc)
	}

	// Four mails for three bookings: a booked child's guardians get one
	// each (as for `lessons cancel`).
	l := clLesson(t, f, clJazz)
	if mails := bkMails(t, f, mail.TemplateLessonCanceled); l.Status != domain.LessonCanceled ||
		l.CancelNote != "The teacher is ill." || l.CanceledBy != office.AccountID || !l.CanceledAt.Equal(testNow) ||
		len(mails) != 4 {
		t.Fatalf("canceled lesson %+v, mails %v", l, mails)
	}

	// Again, with another note: the same document, nothing changes, nobody
	// is mailed.
	again, isError := callTool(t, cs, "cancel_lesson", map[string]any{"lessonId": clJazz, "note": "Another note."})
	if isError || again != text {
		t.Errorf("second call: %q (error %t), want %q", again, isError, text)
	}

	if l := clLesson(t, f, clJazz); l.CancelNote != "The teacher is ill." ||
		len(bkMails(t, f, mail.TemplateLessonCanceled)) != 4 {
		t.Errorf("the second call changed the lesson %+v or mailed", l)
	}

	bkWantCalls(t, f, office.AccountID, "cancel_lesson", "ok", "ok")
}

// TestCancelLessonNote: a note of domain.MaxCancelNoteLength characters
// is taken, trimmed; one longer is invalid and changes nothing; no note
// cancels with none.
// TestCancelLessonPersonalData: without --personal-data the result leaves
// out the account that canceled the lesson and a rental's renter, as the
// schedule tool does; with it, both show. The second call finds the rental
// canceled already and returns it as it is.
func TestCancelLessonPersonalData(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	args := map[string]any{"lessonId": bkRental}

	for _, tc := range []struct {
		opts Options
		want bool
	}{
		{clOptions, false},
		{Options{PersonalData: true, AllowWrites: clOptions.AllowWrites}, true},
	} {
		text, isError := callTool(t, f.session(t, demoOffice, tc.opts, registry), "cancel_lesson", args)
		if isError {
			t.Fatalf("cancel_lesson with %+v: error result %q", tc.opts, text)
		}

		var doc map[string]any
		if err := json.Unmarshal([]byte(text), &doc); err != nil {
			t.Fatal(err)
		}

		by, _ := clAdmin(t, text)["canceledBy"].(map[string]any)
		if renter, _ := doc["renter"].(string); doc["status"] != "canceled" || (renter != "") != tc.want ||
			(by != nil) != tc.want || (tc.want && by["id"] != office.AccountID.String()) {
			t.Errorf("with %+v: lesson %v", tc.opts, doc)
		}
	}
}

func TestCancelLessonNote(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, clOptions, registry)

	wantError(t, cs, "cancel_lesson",
		map[string]any{"lessonId": clJazz, "note": strings.Repeat("ä", domain.MaxCancelNoteLength+1)},
		"lesson "+clJazz+": validation failed: the note is too long")

	if l := clLesson(t, f, clJazz); l.Status != domain.LessonScheduled || l.CancelNote != "" ||
		len(bkMails(t, f, mail.TemplateLessonCanceled)) != 0 {
		t.Errorf("the refused cancel changed the lesson %+v or mailed", l)
	}

	longest := strings.Repeat("ä", domain.MaxCancelNoteLength)

	text, isError := callTool(t, cs, "cancel_lesson", map[string]any{"lessonId": clJazz, "note": " " + longest + " "})
	if note, _ := clAdmin(t, text)["cancelNote"].(string); isError || note != longest ||
		clLesson(t, f, clJazz).CancelNote != longest {
		t.Errorf("the longest note: %q (error %t), %d characters", text, isError, utf8.RuneCountInString(note))
	}

	text, isError = callTool(t, cs, "cancel_lesson", map[string]any{"lessonId": clAfternoon})
	if _, ok := clAdmin(t, text)["cancelNote"]; isError || ok || clLesson(t, f, clAfternoon).Status != domain.LessonCanceled {
		t.Errorf("without a note: %q (error %t)", text, isError)
	}

	bkWantCalls(t, f, office.AccountID, "cancel_lesson", "invalid", "ok", "ok")
}

// TestCancelLessonRefused: a lesson that has started is refused and an
// unknown lesson is not found; nothing changes, nobody is mailed.
func TestCancelLessonRefused(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, clOptions, registry)
	started := clLesson(t, f, clStarted)

	wantError(t, cs, "cancel_lesson", map[string]any{"lessonId": clStarted, "note": "Too late."},
		"lesson "+clStarted+": "+service.ErrLessonStarted.Error())
	wantError(t, cs, "cancel_lesson", map[string]any{"lessonId": lessonsTestUnknown},
		"lesson "+lessonsTestUnknown+": lesson: not found")

	if l := clLesson(t, f, clStarted); l != started || len(bkMails(t, f, mail.TemplateLessonCanceled)) != 0 {
		t.Errorf("the started lesson changed to %+v or someone was mailed", l)
	}

	bkWantCalls(t, f, office.AccountID, "cancel_lesson", "invalid", "not-found")
}

// TestCancelLessonInput: lessonId is required and must be an ID, and an
// unknown property is refused. The errors name the field, never its
// value, which is neither in the log nor in the audit events.
func TestCancelLessonInput(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	office := f.principal(t, demoOffice)
	cs := f.session(t, demoOffice, clOptions, registry)

	for _, tc := range []struct {
		args any
		text string
	}{
		{nil, "validation failed: lessonId is required"},
		{map[string]any{"note": "The teacher is ill."}, "validation failed: lessonId is required"},
		{map[string]any{"lessonId": "Zebulon"}, "validation failed: lessonId is not an ID"},
		{
			map[string]any{"lessonId": clJazz, "personId": bkClara},
			`validation failed: the arguments do not fit the tool's input schema: json: unknown field "personId"`,
		},
	} {
		wantError(t, cs, "cancel_lesson", tc.args, tc.text)
	}

	if l := clLesson(t, f, clJazz); l.Status != domain.LessonScheduled {
		t.Errorf("an invalid call canceled the lesson: %+v", l)
	}

	if strings.Contains(f.log.String(), "Zebulon") {
		t.Errorf("the log has the argument: %s", f.log.String())
	}

	if s := fmt.Sprintf("%+v", toolCalls(t, f.st)); strings.Contains(s, "Zebulon") {
		t.Errorf("the audit events have the argument: %s", s)
	}

	bkWantCalls(t, f, office.AccountID, "cancel_lesson", "invalid", "invalid", "invalid", "invalid")
}

// TestCancelLessonSchema: the listed input schema requires lessonId only,
// and the note's description states domain.MaxCancelNoteLength, which its
// struct tag spells out.
func TestCancelLessonSchema(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	res, err := f.session(t, demoOffice, clOptions, registry).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(res.Tools, func(tl *mcp.Tool) bool { return tl.Name == "cancel_lesson" })
	if i < 0 {
		t.Fatalf("cancel_lesson is not listed among %d tools", len(res.Tools))
	}

	raw, err := json.Marshal(res.Tools[i].InputSchema)
	if err != nil {
		t.Fatal(err)
	}

	var schema struct {
		Required   []string                                `json:"required"`
		Properties map[string]struct{ Description string } `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}

	limit := "at most " + strconv.Itoa(domain.MaxCancelNoteLength) + " characters"
	if note := schema.Properties["note"].Description; !strings.Contains(note, limit) {
		t.Errorf("the note's description %q does not say %q", note, limit)
	}

	if !slices.Equal(schema.Required, []string{"lessonId"}) {
		t.Errorf("required %v, want lessonId", schema.Required)
	}
}

// TestCancelLessonStoreFails: a store that fails while the lesson is
// canceled is an unexpected error: "internal error" for the client, the
// cause in the log, the call audited as an error, the lesson unchanged.
func TestCancelLessonStoreFails(t *testing.T) {
	t.Parallel()

	st := &bkFaultOnce{Store: newFixture(t).st}
	f := fixtureOn(st)
	office := f.principal(t, demoOffice)
	cs := f.sessionOf(t, office, func(context.Context) (service.Principal, error) { return office, nil },
		clOptions, registry)

	st.failNext(errors.New("connection reset"))

	wantError(t, cs, "cancel_lesson", map[string]any{"lessonId": clJazz}, "internal error")

	if log := f.log.String(); !strings.Contains(log, "connection reset") || !strings.Contains(log, "tool=cancel_lesson") {
		t.Errorf("log %q", log)
	}

	if l := clLesson(t, f, clJazz); l.Status != domain.LessonScheduled ||
		len(bkMails(t, f, mail.TemplateLessonCanceled)) != 0 {
		t.Errorf("the failed cancel changed the lesson %+v or mailed", l)
	}

	bkWantCalls(t, f, office.AccountID, "cancel_lesson", "error")
}

// TestCancelLessonInstructor: an instructor does not reach cancel_lesson
// (office), even with the lessons group.
func TestCancelLessonInstructor(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	if names := toolNames(t, f.session(t, demoInstructor, clOptions, registry)); slices.Contains(names, "cancel_lesson") {
		t.Errorf("an instructor lists %v", names)
	}

	if names := toolNames(t, f.session(t, demoOffice, clOptions, registry)); !slices.Contains(names, "cancel_lesson") {
		t.Errorf("the office lists %v", names)
	}
}
