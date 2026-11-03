package main

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// mwNote is the note of the write tests' cancel.
const mwNote = "The teacher is ill."

// mwMails returns the mails of template in the outbox of st as
// "to\nsubject\ntext", sorted: what two stores mailed, without the IDs.
func mwMails(t *testing.T, st store.Store, template string) []string {
	t.Helper()

	mails := accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
		return tx.Outbox().List(ctx, store.OutboxFilter{Template: template, Limit: 1000})
	})

	out := make([]string, 0, len(mails))
	for _, m := range mails {
		out = append(out, m.To+"\n"+m.Subject+"\n"+m.Text)
	}

	slices.Sort(out)

	return out
}

// mwOutbox counts every mail in the outbox of st.
func mwOutbox(t *testing.T, st store.Store) int {
	t.Helper()

	return len(mwMails(t, st, ""))
}

// mwJobRuns returns the latest run of each job of st.
func mwJobRuns(t *testing.T, st store.Store) []domain.JobRun {
	t.Helper()

	return accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.JobRun, error) {
		return tx.JobRuns().Latest(ctx)
	})
}

// mwBookings returns the bookings of the lesson id of st.
func mwBookings(t *testing.T, st store.Store, id string) []domain.Booking {
	t.Helper()

	lesson := lessonOf(t, st, id).ID

	return accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
		return tx.Bookings().ListByLesson(ctx, lesson)
	})
}

// TestMCPWriteParity: cancel_lesson, requeue_mail and run_job return the
// documents of `lessons cancel`, `mail requeue` and `jobs run`. The
// command runs on the demo data, the tool on a fresh copy of it, so both
// change the same records: the documents name only demo records, the
// account of --as and instants of the fixed clock, never an ID the run
// creates, so they compare whole.
func TestMCPWriteParity(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		cmdStore := storeOf(t, r)
		toolRun := freshDemo(t, r)
		toolStore := storeOf(t, toolRun)

		// cancel_lesson: today's Jazz Dance lesson, with its three bookings.
		// The command's document names the account that canceled it, which
		// the tool shows only with --personal-data.
		want := commandDocument(t, r, "lessons", "cancel", "--as", demoOffice, "--yes", "--note", mwNote, demoJazz)

		s := mcpSession(t, toolRun, "--as", demoOffice, "--personal-data", "--allow-writes", "lessons")
		if got := s.document(t, "cancel_lesson", map[string]any{"lessonId": demoJazz, "note": mwNote}); !reflect.DeepEqual(
			got, want) {
			t.Errorf("cancel_lesson:\n%v\nlessons cancel:\n%v", got, want)
		}

		s.close(t)

		for _, st := range []store.Store{cmdStore, toolStore} {
			if l := lessonOf(t, st, demoJazz); l.Status != domain.LessonCanceled || l.CancelNote != mwNote {
				t.Errorf("canceled lesson %+v", l)
			}
		}

		// Four mails for three bookings (a booked child's guardians get one
		// each), the same on both stores, each with the note.
		mailed := mwMails(t, cmdStore, mail.TemplateLessonCanceled)
		if len(mailed) != 4 {
			t.Errorf("lessons cancel mailed %d, want 4", len(mailed))
		}

		if got := mwMails(t, toolStore, mail.TemplateLessonCanceled); !slices.Equal(got, mailed) {
			t.Errorf("cancel_lesson mailed:\n%q\nlessons cancel:\n%q", got, mailed)
		}

		for _, m := range mailed {
			if !strings.Contains(m, mwNote) {
				t.Errorf("the mail %q has not the note", m)
			}
		}

		// requeue_mail: the same failed mail in both stores.
		addLogMails(t, cmdStore, failedReceipt)
		addLogMails(t, toolStore, failedReceipt)

		id := failedReceipt.ID.String()
		want = commandDocument(t, r, "mail", "requeue", "--as", demoAdmin, "--yes", id)

		s = mcpSession(t, toolRun, "--as", demoAdmin, "--personal-data", "--allow-writes", "mail")
		if got := s.document(t, "requeue_mail", map[string]any{"mailId": id}); !reflect.DeepEqual(got, want) {
			t.Errorf("requeue_mail:\n%v\nmail requeue:\n%v", got, want)
		}

		s.close(t)

		if cmdMail, toolMail := logMailOf(t, cmdStore, failedReceipt.ID), logMailOf(t, toolStore,
			failedReceipt.ID); toolMail.Status() != domain.MailPending || !reflect.DeepEqual(toolMail, cmdMail) {
			t.Errorf("requeued by requeue_mail: %+v\nby mail requeue: %+v", toolMail, cmdMail)
		}

		// run_job: generate-lessons, the job that writes the school's data
		// only.
		before := lessonCount(t, cmdStore)
		want = commandDocument(t, r, "jobs", "run", "generate-lessons")

		s = mcpSession(t, toolRun, "--as", demoAdmin, "--allow-writes", "jobs")
		if got := s.document(t, "run_job", map[string]any{"name": "generate-lessons"}); !reflect.DeepEqual(got, want) {
			t.Errorf("run_job:\n%v\njobs run:\n%v", got, want)
		}

		s.close(t)

		if cmdN, toolN := lessonCount(t, cmdStore), lessonCount(t, toolStore); cmdN != toolN {
			t.Errorf("lessons after jobs run %d (before %d), after run_job %d", cmdN, before, toolN)
		}

		for _, st := range []store.Store{cmdStore, toolStore} {
			runs := mwJobRuns(t, st)
			if len(runs) != 1 || runs[0].Job != "generate-lessons" || runs[0].Result != domain.JobOK {
				t.Errorf("job runs %+v, want generate-lessons ok", runs)
			}
		}
	})
}

// TestMCPWriteAudit: a write through the tools is the service's write by
// the account of --as through MCP: requeue_mail's mail-requeued event has
// the channel mcp and the OS user, cancel_lesson's lesson is canceled by
// the office's account, and each call is one mcp-tool-called event; a
// refused write is audited with its outcome and changes nothing.
func TestMCPWriteAudit(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		office, _ := accountOf(t, st, demoOffice)
		admin, _ := accountOf(t, st, demoAdmin)

		addMailLog(t, st)

		s := mcpSession(t, r, "--as", demoOffice, "--allow-writes", "lessons")
		s.document(t, "cancel_lesson", map[string]any{"lessonId": demoJazz})
		s.close(t)

		if l := lessonOf(t, st, demoJazz); l.Status != domain.LessonCanceled || l.CanceledBy != office.ID ||
			!l.CanceledAt.Equal(testNow) {
			t.Errorf("canceled lesson %+v, want canceled by %s at %s", l, office.ID, testNow)
		}

		s = mcpSession(t, r, "--as", demoAdmin, "--personal-data", "--allow-writes", "mail")
		s.document(t, "requeue_mail", map[string]any{"mailId": failedReceipt.ID.String()})

		// A mail with a secret link is never queued again: a conflict.
		if text, isError := s.callTool(t, "requeue_mail", map[string]any{"mailId": failedInvite.ID.String()}); !isError ||
			!strings.Contains(text, "secret link") {
			t.Errorf("requeue_mail of the invite: result %q (error %t)", text, isError)
		}

		s.close(t)

		events := requeueEvents(t, st)
		via := domain.AuditVia{Channel: domain.ChannelMCP, OSUser: testOSUser}

		if len(events) != 1 || events[0].ActorID != admin.ID || events[0].Details.MailID != failedReceipt.ID ||
			events[0].Details.Via != via {
			t.Errorf("mail-requeued events %+v, want one by the admin via %+v", events, via)
		}

		if m := logMailOf(t, st, failedInvite.ID); m.Status() != domain.MailFailed {
			t.Errorf("the invite is %s, want failed", m.Status())
		}

		calls := toolCalls(t, st)
		if len(calls) != 3 {
			t.Fatalf("%d tool calls audited, want 3", len(calls))
		}

		wantToolCall(t, calls[0], office.ID, "cancel_lesson", "ok")
		wantToolCall(t, calls[1], admin.ID, "requeue_mail", "ok")
		wantToolCall(t, calls[2], admin.ID, "requeue_mail", "conflict")
	})
}

// TestMCPWritesNeedTheirGroup (review focus 5): a write tool called by
// name without its --allow-writes group is the SDK's unknown tool, also
// when other groups are allowed: nothing runs, nothing is audited, and
// the store is unchanged.
func TestMCPWritesNeedTheirGroup(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		addMailLog(t, st)

		// The calls would succeed with their groups: Clara is not booked on
		// the Jazz lesson, and its first booking is active.
		bookings := mwBookings(t, st, demoJazz)
		if len(bookings) == 0 || bookings[0].Status == domain.BookingCanceled {
			t.Fatalf("the Jazz lesson's bookings %+v: the test proves nothing", bookings)
		}

		for _, b := range bookings {
			if b.PersonID.String() == ppClara {
				t.Fatalf("Clara is booked on the Jazz lesson: the test proves nothing")
			}
		}

		calls := map[string]map[string]any{
			"book_lesson":    {"lessonId": demoJazz, "personId": ppClara},
			"cancel_booking": {"lessonId": demoJazz, "personId": bookings[0].PersonID.String()},
			"cancel_lesson":  {"lessonId": demoJazz},
			"requeue_mail":   {"mailId": failedReceipt.ID.String()},
			"run_job":        {"name": "generate-lessons"},
		}

		mails := mwOutbox(t, st)

		for _, tc := range []struct {
			groups  []string
			refused []string
		}{
			{nil, []string{"book_lesson", "cancel_booking", "cancel_lesson", "requeue_mail", "run_job"}},
			{[]string{"--allow-writes", "jobs"}, []string{"book_lesson", "cancel_booking", "cancel_lesson", "requeue_mail"}},
			{[]string{"--allow-writes", "bookings,lessons,mail"}, []string{"run_job"}},
		} {
			s := mcpSession(t, r, append([]string{"--as", demoAdmin, "--personal-data"}, tc.groups...)...)

			for _, name := range tc.refused {
				res, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: calls[name]})
				if err == nil || !strings.Contains(err.Error(), "unknown tool") {
					t.Errorf("%s with %v: result %+v, error %v", name, tc.groups, res, err)
				}
			}

			s.close(t)
		}

		if calls := toolCalls(t, st); len(calls) != 0 {
			t.Errorf("%d calls of unlisted tools audited: %+v", len(calls), calls)
		}

		if l := lessonOf(t, st, demoJazz); l.Status != domain.LessonScheduled {
			t.Errorf("the lesson is %s, want scheduled", l.Status)
		}

		if got := mwBookings(t, st, demoJazz); !reflect.DeepEqual(got, bookings) {
			t.Errorf("the lesson's bookings changed:\n%+v\nwere:\n%+v", got, bookings)
		}

		if n := mwOutbox(t, st); n != mails {
			t.Errorf("%d mails in the outbox, were %d", n, mails)
		}

		if m := logMailOf(t, st, failedReceipt.ID); m.Status() != domain.MailFailed || len(requeueEvents(t, st)) != 0 {
			t.Errorf("the failed mail is %s with %d mail-requeued events", m.Status(), len(requeueEvents(t, st)))
		}

		if runs := mwJobRuns(t, st); len(runs) != 0 {
			t.Errorf("job runs recorded: %+v", runs)
		}
	})
}

// TestMCPWriteTools: the write tools a session lists, per --allow-writes
// group, the role of --as and --personal-data: the booking tools and
// requeue_mail need --personal-data, requeue_mail and run_job an admin.
func TestMCPWriteTools(t *testing.T) {
	t.Parallel()

	officeReads := []string{"free_rooms", "schedule", "statistics"}
	officePersonal := []string{
		"free_rooms", "lesson", "person_bookings", "person_passes", "receipts", "schedule", "search_persons",
		"statistics",
	}
	adminReads := []string{"db_check", "doctor", "free_rooms", "schedule", "statistics"}
	adminPersonal := []string{
		"db_check", "doctor", "free_rooms", "lesson", "mail_log", "person_bookings", "person_passes", "receipts",
		"schedule", "search_persons", "statistics",
	}
	all := "bookings,lessons,mail,jobs"

	with := func(tools []string, writes ...string) []string {
		return slices.Sorted(slices.Values(append(slices.Clone(tools), writes...)))
	}

	stores(t, func(t *testing.T, r cliRun) {
		for _, tc := range []struct {
			args  []string
			tools []string
		}{
			{
				[]string{"--as", demoOffice, "--personal-data", "--allow-writes", "bookings"},
				with(officePersonal, "book_lesson", "cancel_booking"),
			},
			// Without --personal-data: no booking tool.
			{[]string{"--as", demoOffice, "--allow-writes", "bookings"}, officeReads},
			// The office reaches neither requeue_mail nor run_job.
			{[]string{"--as", demoOffice, "--personal-data", "--allow-writes", all}, with(officePersonal,
				"book_lesson", "cancel_booking", "cancel_lesson")},
			{
				[]string{"--as", demoAdmin, "--allow-writes", "lessons,mail,jobs"},
				with(adminReads, "cancel_lesson", "run_job"),
			},
			{
				[]string{"--as", demoAdmin, "--personal-data", "--allow-writes", all},
				with(adminPersonal, "book_lesson", "cancel_booking", "cancel_lesson", "requeue_mail", "run_job"),
			},
			{[]string{"--as", demoAdmin, "--personal-data", "--allow-writes", "mail"}, with(adminPersonal, "requeue_mail")},
			// No group: no write tool.
			{[]string{"--as", demoAdmin, "--personal-data"}, adminPersonal},
			// A member reaches no write tool.
			{[]string{"--as", demoMember, "--personal-data", "--allow-writes", all}, []string{"schedule"}},
		} {
			s := mcpSession(t, r, tc.args...)

			if got := s.toolNames(t); !slices.Equal(got, tc.tools) {
				t.Errorf("%v: tools %v, want %v", tc.args, got, tc.tools)
			}

			s.close(t)
		}
	})
}
