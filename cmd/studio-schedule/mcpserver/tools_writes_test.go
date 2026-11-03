package mcpserver

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Lessons and persons of the demo data the write tools are called with.
const (
	// wtBookable is a lesson on testNow's day, 14:00 UTC, with two of its
	// twelve places booked, by wtBooked and another person.
	wtBookable = "00000000-0000-7000-8000-0000000000cc"
	// wtBooked has a booking of wtBookable.
	wtBooked = "00000000-0000-7000-8000-00000000000a"
	// wtUnbooked is a member without a booking of wtBookable.
	wtUnbooked = "00000000-0000-7000-8000-000000000005"
	// wtCancelable is a lesson on testNow's day, 17:00 UTC, with four
	// bookings.
	wtCancelable = "00000000-0000-7000-8000-0000000000cf"
)

// wtWrites are the write tools (writeTools) among names, in their order.
func wtWrites(names []string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return !slices.Contains(writeTools, n) })
}

// wtGroupOf is the write group of the registry's tool name.
func wtGroupOf(t *testing.T, name string) WriteGroup {
	t.Helper()

	for _, group := range registry {
		for _, tl := range group {
			if tl.name == name {
				return tl.write
			}
		}
	}

	t.Fatalf("no tool %s", name)

	return ""
}

// TestWriteToolsPerGroup: each --allow-writes group lists exactly its
// tools beside the read tools, which stay the same; without a group no
// write tool is listed.
func TestWriteToolsPerGroup(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	reads := toolNames(t, f.session(t, demoAdmin, Options{PersonalData: true}, registry))
	if got := wtWrites(reads); len(got) != 0 {
		t.Errorf("without a group the session lists %v", got)
	}

	for _, tc := range []struct {
		group WriteGroup
		tools []string
	}{
		{WriteBookings, []string{"book_lesson", "cancel_booking"}},
		{WriteLessons, []string{"cancel_lesson"}},
		{WriteMail, []string{"requeue_mail"}},
		{WriteJobs, []string{"run_job"}},
	} {
		names := toolNames(t, f.session(t, demoAdmin,
			Options{PersonalData: true, AllowWrites: []WriteGroup{tc.group}}, registry))

		if got := wtWrites(names); !slices.Equal(got, tc.tools) {
			t.Errorf("--allow-writes %s lists %v, want %v", tc.group, got, tc.tools)
		}

		others := slices.DeleteFunc(names, func(n string) bool { return slices.Contains(tc.tools, n) })
		if !slices.Equal(others, reads) {
			t.Errorf("--allow-writes %s changed the read tools: %v, without it %v", tc.group, others, reads)
		}
	}

	all := toolNames(t, f.session(t, demoAdmin, Options{PersonalData: true, AllowWrites: WriteGroups()}, registry))
	if got := wtWrites(all); !slices.Equal(got, writeTools) {
		t.Errorf("every group lists %v, want %v", got, writeTools)
	}
}

// wtState is what the write tools change in the store: the lessons they
// cancel, the bookings, the outbox, the job runs and the audit log.
type wtState struct {
	Lessons  []domain.Lesson
	Bookings []domain.Booking
	Outbox   []domain.OutboxMail
	JobRuns  []domain.JobRun
	Audit    int
}

// wtStateOf reads the wtState of f.
func wtStateOf(t *testing.T, f *fixture) wtState {
	t.Helper()

	var s wtState

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		for _, raw := range []string{wtBookable, wtCancelable} {
			id, err := domain.ParseID[domain.LessonID](raw)
			if err != nil {
				return err
			}

			l, err := tx.Lessons().Get(ctx, id)
			if err != nil {
				return err
			}

			bookings, err := tx.Bookings().ListByLesson(ctx, id)
			if err != nil {
				return err
			}

			s.Lessons, s.Bookings = append(s.Lessons, l), append(s.Bookings, bookings...)
		}

		var err error
		if s.Outbox, err = tx.Outbox().List(ctx, store.OutboxFilter{Limit: 1000}); err != nil {
			return err
		}

		if s.JobRuns, err = tx.JobRuns().Latest(ctx); err != nil {
			return err
		}

		events, err := tx.Audit().List(ctx, 1000)
		s.Audit = len(events)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// TestWriteToolsNeedTheirGroup pins Review focus 5: a write tool called
// by name in a session that allows every other group fails as the SDK's
// unknown tool, even with valid arguments; nothing runs, nothing is
// audited, and the store is as it was.
func TestWriteToolsNeedTheirGroup(t *testing.T) {
	t.Parallel()

	f, m := wmFixture(t)
	args := wtArgs(m)
	before := wtStateOf(t, f)

	for _, name := range writeTools {
		group := wtGroupOf(t, name)
		others := slices.DeleteFunc(WriteGroups(), func(g WriteGroup) bool { return g == group })
		cs := f.session(t, demoAdmin, Options{PersonalData: true, AllowWrites: others}, registry)

		if listed := toolNames(t, cs); slices.Contains(listed, name) {
			t.Errorf("without %s the session lists %s", group, name)
		}

		_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args[name]})
		if err == nil || !strings.Contains(err.Error(), `unknown tool "`+name+`"`) {
			t.Errorf("call %s without --allow-writes %s: %v", name, group, err)
		}
	}

	if after := wtStateOf(t, f); !reflect.DeepEqual(after, before) {
		t.Errorf("unlisted write tools changed the store:\n%+v\nbefore:\n%+v", after, before)
	}

	if calls := toolCalls(t, f.st); len(calls) != 0 {
		t.Errorf("calls of unlisted tools were audited: %+v", calls)
	}
}

// TestWriteToolsPersonalData: without --personal-data the booking tools
// and requeue_mail are not listed even with their groups; cancel_lesson
// and run_job are.
func TestWriteToolsPersonalData(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoAdmin, Options{AllowWrites: WriteGroups()}, registry)

	if got, want := wtWrites(toolNames(t, cs)), []string{"cancel_lesson", "run_job"}; !slices.Equal(got, want) {
		t.Errorf("without --personal-data the write tools are %v, want %v", got, want)
	}

	for _, name := range personDataWriteTools {
		_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil || !strings.Contains(err.Error(), `unknown tool "`+name+`"`) {
			t.Errorf("call %s without --personal-data: %v", name, err)
		}
	}
}

// TestWriteToolsRoles: the office lists its write tools with every group,
// but not requeue_mail and run_job, which are the admins'; an instructor
// and a member list no write tool.
func TestWriteToolsRoles(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	for _, tc := range []struct {
		as    string
		tools []string
	}{
		{demoAdmin, writeTools},
		{demoOffice, []string{"book_lesson", "cancel_booking", "cancel_lesson"}},
		{demoInstructor, nil},
		{demoMember, nil},
	} {
		cs := f.session(t, tc.as, Options{PersonalData: true, AllowWrites: WriteGroups()}, registry)

		if got := wtWrites(toolNames(t, cs)); !slices.Equal(got, tc.tools) {
			t.Errorf("%s lists the write tools %v, want %v", tc.as, got, tc.tools)
		}
	}
}

// TestWriteToolsAnnotations: the annotations the client approves a call
// by (ADR 0022), as the plan's table gives them; no write tool is read
// only, and only run_job reaches outside the school's data.
func TestWriteToolsAnnotations(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoAdmin, Options{PersonalData: true, AllowWrites: WriteGroups()}, registry)

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]struct{ destructive, idempotent, openWorld bool }{
		"book_lesson":    {false, true, false},
		"cancel_booking": {true, true, false},
		"cancel_lesson":  {true, true, false},
		"requeue_mail":   {true, false, false},
		"run_job":        {true, false, true},
	}

	var seen []string

	for _, tl := range res.Tools {
		w, ok := want[tl.Name]
		if !ok {
			continue
		}

		seen = append(seen, tl.Name)

		a := tl.Annotations
		if a == nil || a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint != w.destructive ||
			a.IdempotentHint != w.idempotent || a.OpenWorldHint == nil || *a.OpenWorldHint != w.openWorld {
			t.Errorf("%s: annotations %+v, want %+v", tl.Name, a, w)
		}
	}

	if slices.Sort(seen); !slices.Equal(seen, writeTools) {
		t.Errorf("the write tools listed are %v, want %v", seen, writeTools)
	}
}

// wtArgs are the arguments of a happy call of every tool an admin lists
// with every flag, on the demo data with the failed mail of m.
func wtArgs(m wmMails) map[string]map[string]any {
	return map[string]map[string]any{
		// Reads.
		"schedule":        {},
		"free_rooms":      {"date": "2026-09-22", "start": "18:00", "durationMinutes": 60},
		"lesson":          {"lessonId": wtBookable},
		"search_persons":  {"term": "Clara"},
		"person_bookings": {"personId": wtBooked},
		"person_passes":   {"personId": wtBooked},
		"statistics":      {},
		"receipts":        {},
		"mail_log":        {},
		"doctor":          {},
		"db_check":        {},
		// Writes.
		"book_lesson":    {"lessonId": wtBookable, "personId": wtUnbooked},
		"cancel_booking": {"lessonId": wtBookable, "personId": wtBooked},
		"cancel_lesson":  {"lessonId": wtCancelable, "note": "The studio's heating is broken."},
		"requeue_mail":   {"mailId": m.failed.String()},
		"run_job":        {"name": "generate-lessons"},
	}
}

// wtAnnouncements returns every announcement of f's store.
func wtAnnouncements(t *testing.T, f *fixture) []domain.Announcement {
	t.Helper()

	var out []domain.Announcement

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.Announcements().List(ctx)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// TestWriteToolsNoAnnouncements: no tool writes an announcement (ADR
// 0022, amendment of 2026-11-03). A session with every flag calls every
// tool it lists, each on its happy path, and the store's announcements
// are as they were; the table of arguments must cover every listed tool,
// so a new tool is called here too.
func TestWriteToolsNoAnnouncements(t *testing.T) {
	t.Parallel()

	f, m := wmFixture(t)
	args := wtArgs(m)
	cs := f.session(t, demoAdmin, Options{PersonalData: true, AllowWrites: WriteGroups()}, registry)

	before := wtAnnouncements(t, f)
	if len(before) == 0 {
		t.Fatal("the demo data has no announcements to compare")
	}

	listed := toolNames(t, cs)
	if keys := slices.Sorted(maps.Keys(args)); !slices.Equal(keys, listed) {
		t.Fatalf("the arguments cover %v, the session lists %v", keys, listed)
	}

	for _, name := range listed {
		if text, isError := callTool(t, cs, name, args[name]); isError {
			t.Errorf("call %s %v: error result %q", name, args[name], text)
		}
	}

	if after := wtAnnouncements(t, f); !reflect.DeepEqual(after, before) {
		t.Errorf("the announcements changed:\n%+v\nbefore:\n%+v", after, before)
	}

	// The writes happened: the comparison is not of a session that wrote
	// nothing.
	s := wtStateOf(t, f)
	if s.Lessons[1].Status != domain.LessonCanceled {
		t.Errorf("cancel_lesson left the lesson %s", s.Lessons[1].Status)
	}

	statuses := map[string]domain.BookingStatus{}
	for _, b := range s.Bookings {
		if b.LessonID.String() == wtBookable {
			statuses[b.PersonID.String()] = b.Status
		}
	}

	if statuses[wtUnbooked] != domain.BookingBooked || statuses[wtBooked] != domain.BookingCanceled {
		t.Errorf("the bookings of the lesson: %v", statuses)
	}

	if got := wmMail(t, f, m.failed); got.Status() != domain.MailPending {
		t.Errorf("requeue_mail left the mail %s", got.Status())
	}

	if !slices.ContainsFunc(s.JobRuns, func(r domain.JobRun) bool { return r.Job == "generate-lessons" }) {
		t.Errorf("run_job recorded no run: %+v", s.JobRuns)
	}

	calls := toolCalls(t, f.st)
	if len(calls) != len(listed) {
		t.Errorf("%d calls audited, want %d", len(calls), len(listed))
	}

	for _, c := range calls {
		if c.Details.Outcome != "ok" {
			t.Errorf("%s audited as %s", c.Details.Tool, c.Details.Outcome)
		}
	}
}
