package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The demo lessons the tests work on (seed's deterministic IDs).
const (
	// demoJazz is today's Jazz Dance (beginner) at 17:00 in Hall 1, with
	// three active bookings; it has not started at testNow.
	demoJazz = "00000000-0000-7000-8000-0000000000c9"
	// demoStarted is yesterday's Creative Dance, which has started.
	demoStarted = "00000000-0000-7000-8000-0000000000c3"
	// demoUnknown is a lesson ID no lesson has.
	demoUnknown = "00000000-0000-7000-8000-00000000ffff"
)

// wantExit fails unless res exited with code and its stderr contains
// stderr.
func wantExit(t *testing.T, res result, code int, stderr string) {
	t.Helper()

	if res.code != code || !strings.Contains(res.stderr, stderr) {
		t.Errorf("exit %d, stderr %q; want exit %d with %q", res.code, res.stderr, code, stderr)
	}
}

// TestLessonsList lists the demo lessons as the office: today by default,
// a day, a range, the empty day, and the getSchedule body as JSON.
func TestLessonsList(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "lessons", "list", "--as", demoOffice)
		wantExit(t, got, 0, "")
		golden(t, "lessons/today.txt", got.stdout)

		// The same day by --date, as getSchedule's body.
		got = r.exec(t, "lessons", "list", "--as", demoOffice, "--date", "2026-09-22", "--output", "json")
		wantExit(t, got, 0, "")
		golden(t, "lessons/today.json", got.stdout)

		var body struct {
			Timezone string `json:"timezone"`
			From     string `json:"from"`
			To       string `json:"to"`
			Lessons  []struct {
				ID string `json:"id"`
			} `json:"lessons"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &body); err != nil || body.Timezone != "Europe/Berlin" ||
			body.From != "2026-09-22" || body.To != "2026-09-23" || len(body.Lessons) != 4 {
			t.Errorf("JSON body %+v, %v", body, err)
		}

		// A week, both ends inclusive: workshops and a room rental.
		got = r.exec(t, "lessons", "list", "--as", demoOffice, "--from", "2026-09-21", "--to", "2026-09-27")
		wantExit(t, got, 0, "")
		golden(t, "lessons/week.txt", got.stdout)

		got = r.exec(t, "lessons", "list", "--as", demoOffice, "--date", "2026-08-15")
		if got.code != 0 || got.stdout != "no lessons on 2026-08-15\n" {
			t.Errorf("empty day: exit %d, stdout %q", got.code, got.stdout)
		}

		got = r.exec(t, "lessons", "list", "--as", demoOffice, "--from", "2026-08-10", "--to", "2026-08-16")
		if got.code != 0 || got.stdout != "no lessons from 2026-08-10 to 2026-08-16\n" {
			t.Errorf("empty week: exit %d, stdout %q", got.code, got.stdout)
		}
	})
}

// TestLessonsListRefusals covers the usage errors and the --as refusals of
// lessons list.
func TestLessonsListRefusals(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}

	for _, c := range []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"no account", nil, 2, "give --as <email>"},
		{"member", []string{"--as", demoMember}, 4, "below office"},
		{"instructor", []string{"--as", demoInstructor}, 4, "below office"},
		{"unknown account", []string{"--as", "nobody@example.org"}, 3, "--as nobody@example.org"},
		{"bad date", []string{"--as", demoOffice, "--date", "22.09.2026"}, 2, `--date: "22.09.2026" is not a date`},
		{"bad from", []string{"--as", demoOffice, "--from", "x", "--to", "2026-09-22"}, 2, `--from: "x" is not a date`},
		{"bad to", []string{"--as", demoOffice, "--from", "2026-09-22", "--to", "x"}, 2, `--to: "x" is not a date`},
		{"date and from", []string{
			"--as", demoOffice, "--date", "2026-09-22", "--from", "2026-09-22", "--to", "2026-09-23",
		}, 2, "none of the others can be"},
		{"from alone", []string{"--as", demoOffice, "--from", "2026-09-22"}, 2, "must all be set"},
		{
			"to before from",
			[]string{"--as", demoOffice, "--from", "2026-09-22", "--to", "2026-09-21"},
			2,
			"--to: 2026-09-21 lies before --from 2026-09-22",
		},
		{
			"range too long",
			[]string{"--as", demoOffice, "--from", "2026-09-01", "--to", "2026-10-13"},
			2,
			"must not exceed 42 days",
		},
	} {
		got := r.exec(t, append([]string{"lessons", "list"}, c.args...)...)
		if got.code != c.code || !strings.Contains(got.stderr, c.stderr) || got.stdout != "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit %d with %q", c.name, got.code, got.stdout,
				got.stderr, c.code, c.stderr)
		}
	}

	// 42 days, both ends included, are the most a list covers.
	got := r.exec(t, "lessons", "list", "--as", demoOffice, "--from", "2026-09-01", "--to", "2026-10-12")
	if got.code != 0 {
		t.Errorf("42 days: exit %d, stderr %q", got.code, got.stderr)
	}
}

// lessonCount counts the lessons of st that start in the demo term.
func lessonCount(t *testing.T, st store.Store) int {
	t.Helper()

	var n int

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		lessons, err := tx.Lessons().ListRange(ctx, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2027, 8, 1, 0, 0, 0, 0, time.UTC))
		n = len(lessons)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return n
}

// TestLessonsGenerate generates the lessons of the demo classes through
// the end of the year: it says how many it created, and a second run
// creates none.
func TestLessonsGenerate(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		before := lessonCount(t, st)

		got := r.exec(t, "lessons", "generate", "--as", demoOffice, "--until", "2026-12-31")
		wantExit(t, got, 0, "")
		golden(t, "lessons/generate.txt", got.stdout)

		created := lessonCount(t, st) - before
		if want := "created " + count(created, "lesson") + " through 2026-12-31\n"; created == 0 ||
			got.stdout != want {
			t.Errorf("created %d lessons, printed %q", created, got.stdout)
		}

		got = r.exec(t, "lessons", "generate", "--as", demoOffice, "--until", "2026-12-31", "--output", "json")
		if got.code != 0 || got.stdout != "{\n  \"created\": 0,\n  \"until\": \"2026-12-31\"\n}\n" ||
			lessonCount(t, st) != before+created {
			t.Errorf("again: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
		}
	})
}

// TestLessonsGenerateRefusals covers the usage errors and the --as refusal
// of lessons generate: nothing is created.
func TestLessonsGenerateRefusals(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	r := cliRun{st: st}
	before := lessonCount(t, st)

	for _, c := range []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{"--as", demoOffice}, 2, `required flag(s) "until" not set`},
		{[]string{"--as", demoOffice, "--until", "soon"}, 2, `--until: "soon" is not a date`},
		{[]string{"--as", demoOffice, "--until", "2026-09-21"}, 2, "--until: the last date must not lie before today"},
		{[]string{"--as", demoMember, "--until", "2026-12-31"}, 4, "below office"},
	} {
		wantExit(t, r.exec(t, append([]string{"lessons", "generate"}, c.args...)...), c.code, c.stderr)
	}

	// Today is the earliest date: its lessons exist already.
	got := r.exec(t, "lessons", "generate", "--as", demoOffice, "--until", "2026-09-22")
	if got.code != 0 || got.stdout != "created 0 lessons through 2026-09-22\n" || lessonCount(t, st) != before {
		t.Errorf("today: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}
}

// lessonOf returns the lesson id of st.
func lessonOf(t *testing.T, st store.Store, id string) domain.Lesson {
	t.Helper()

	lid, err := domain.ParseID[domain.LessonID](id)
	if err != nil {
		t.Fatal(err)
	}

	var l domain.Lesson

	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		l, err = tx.Lessons().Get(ctx, lid)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return l
}

// mailsOf counts the mails of template in the outbox of st.
func mailsOf(t *testing.T, st store.Store, template string) int {
	t.Helper()

	var n int

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		mails, err := tx.Outbox().List(ctx, store.OutboxFilter{Template: template, Limit: 1000})
		n = len(mails)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return n
}

// TestLessonsCancelRestore cancels today's Jazz Dance lesson as the office
// and restores it: each asks first and refuses without --yes off a
// terminal, mails everyone booked, and a second run changes nothing and
// mails nobody.
func TestLessonsCancelRestore(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		canceled := func() int { return mailsOf(t, st, mail.TemplateLessonCanceled) }
		restored := func() int { return mailsOf(t, st, mail.TemplateLessonRestored) }

		// Off a terminal without --yes: refused, nothing changed.
		got := r.exec(t, "lessons", "cancel", "--as", demoOffice, demoJazz)
		wantExit(t, got, 2, "confirm with --yes when stdin is not a terminal; nothing was changed")

		if lessonOf(t, st, demoJazz).Status != domain.LessonScheduled || canceled() != 0 {
			t.Fatal("the refused cancel changed the lesson or mailed")
		}

		// On a terminal the question names the lesson; "n" changes nothing.
		term := r.with("NO_COLOR", "1")
		term.terminal, term.stdin = true, "n\n"

		got = term.exec(t, "lessons", "cancel", "--as", demoOffice, demoJazz)
		wantExit(t, got, 2, "Cancel Jazz Dance (beginner) on 2026-09-22 at 17:00 in Hall 1 and mail those "+
			"booked on it (3 active bookings)? [y/N] error: not confirmed; nothing was changed")

		if lessonOf(t, st, demoJazz).Status != domain.LessonScheduled || canceled() != 0 {
			t.Fatal("the declined cancel changed the lesson or mailed")
		}

		// "y" on a terminal cancels, with the note; the table has its header.
		term.stdin = "y\n"
		got = term.exec(t, "lessons", "cancel", "--as", demoOffice, "--note", "The teacher is ill.", demoJazz)
		wantExit(t, got, 0, "")
		golden(t, "lessons/cancel.txt", got.stdout)

		// Four mails for three bookings: a booked child's guardians get one
		// each (lessonRecipients).
		l := lessonOf(t, st, demoJazz)
		if l.Status != domain.LessonCanceled || l.CancelNote != "The teacher is ill." ||
			l.CanceledBy != accountID(t, st, demoOffice) || canceled() != 4 {
			t.Fatalf("canceled lesson %+v, %d mails", l, canceled())
		}

		// Again: nothing changes, nobody is mailed, nothing is asked.
		got = r.exec(t, "lessons", "cancel", "--as", demoOffice, "--note", "Another note.", demoJazz)
		wantExit(t, got, 0, "")
		golden(t, "lessons/cancel-again.txt", got.stdout)

		if l := lessonOf(t, st, demoJazz); l.CancelNote != "The teacher is ill." || canceled() != 4 {
			t.Errorf("the second cancel changed the lesson %+v or mailed (%d)", l, canceled())
		}

		// Restore: refused without --yes, then done with it, as restoreLesson's body.
		got = r.exec(t, "lessons", "restore", "--as", demoOffice, demoJazz)
		wantExit(t, got, 2, "confirm with --yes")

		if lessonOf(t, st, demoJazz).Status != domain.LessonCanceled || restored() != 0 {
			t.Fatal("the refused restore changed the lesson or mailed")
		}

		got = r.exec(t, "lessons", "restore", "--as", demoOffice, "--yes", "--output", "json", demoJazz)
		wantExit(t, got, 0, "")
		golden(t, "lessons/restore.json", got.stdout)

		if l := lessonOf(t, st, demoJazz); l.Status != domain.LessonScheduled || !l.Detached || restored() != 4 {
			t.Errorf("restored lesson %+v, %d mails", l, restored())
		}

		got = r.exec(t, "lessons", "restore", "--as", demoOffice, demoJazz)
		wantExit(t, got, 0, "")
		golden(t, "lessons/restore-again.txt", got.stdout)

		// The cancel as cancelLesson's body.
		got = r.exec(t, "lessons", "cancel", "--as", demoOffice, "--yes", "--output", "json", demoJazz)
		wantExit(t, got, 0, "")
		golden(t, "lessons/cancel.json", got.stdout)
	})
}

// TestLessonsRestoreRoomTaken restores a lesson whose room another lesson
// took meanwhile: a conflict, unless --confirm-room-overlap.
func TestLessonsRestoreRoomTaken(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	r := cliRun{st: st}

	if got := r.exec(t, "lessons", "cancel", "--as", demoOffice, "--yes", demoJazz); got.code != 0 {
		t.Fatalf("cancel: exit %d, stderr %q", got.code, got.stderr)
	}

	jazz := lessonOf(t, st, demoJazz)
	_, err := servicesOf(t, st).Lessons.CreateLesson(t.Context(), service.NewLesson{
		CourseTypeID: jazz.CourseTypeID,
		LessonData: service.LessonData{
			Date: domain.Date{Year: 2026, Month: time.September, Day: 22}, StartTime: domain.TimeOfDay{Hour: 17},
			Duration: time.Hour, RoomID: jazz.RoomID, InstructorID: jazz.InstructorID, Capacity: 10,
			Visibility: domain.VisibilityPublic, Kind: domain.LessonWorkshop,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	wantExit(t, r.exec(t, "lessons", "restore", "--as", demoOffice, "--yes", demoJazz), 4, "the room is taken by lesson")

	if lessonOf(t, st, demoJazz).Status != domain.LessonCanceled {
		t.Fatal("the refused restore changed the lesson")
	}

	got := r.exec(t, "lessons", "restore", "--as", demoOffice, "--yes", "--confirm-room-overlap", demoJazz)
	if got.code != 0 || !strings.Contains(got.stdout, "restored; mails to those booked on it (3 active bookings) are queued") {
		t.Errorf("restore with overlap: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}
}

// TestLessonsCancelRefusals covers what cancel and restore refuse: nothing
// changes.
func TestLessonsCancelRefusals(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	r := cliRun{st: st}

	for _, c := range []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"no lesson", []string{"cancel", "--as", demoOffice, "--yes"}, 2, "accepts 1 arg(s)"},
		{"bad ID", []string{"cancel", "--as", demoOffice, "--yes", "c9"}, 2, `<lesson-id>: "c9" is not a lesson ID`},
		{"unknown lesson", []string{"cancel", "--as", demoOffice, "--yes", demoUnknown}, 3, "not found"},
		{"started", []string{"cancel", "--as", demoOffice, "--yes", demoStarted}, 2, "the lesson has started"},
		{"note too long", []string{
			"cancel", "--as", demoOffice, "--yes", "--note", strings.Repeat("x", 501), demoJazz,
		}, 2, "--note: the note is too long"},
		{"member", []string{"cancel", "--as", demoMember, "--yes", demoJazz}, 4, "below office"},
		{"instructor", []string{"cancel", "--as", demoInstructor, "--yes", demoJazz}, 4, "below office"},
		{"restore member", []string{"restore", "--as", demoMember, "--yes", demoJazz}, 4, "below office"},
		{"restore bad ID", []string{"restore", "--as", demoOffice, "--yes", "x"}, 2, "is not a lesson ID"},
	} {
		got := r.exec(t, append([]string{"lessons"}, c.args...)...)
		if got.code != c.code || !strings.Contains(got.stderr, c.stderr) {
			t.Errorf("%s: exit %d, stderr %q; want exit %d with %q", c.name, got.code, got.stderr, c.code, c.stderr)
		}
	}

	if lessonOf(t, st, demoJazz).Status != domain.LessonScheduled || mailsOf(t, st, mail.TemplateLessonCanceled) != 0 {
		t.Error("a refused cancel changed the lesson or mailed")
	}

	// As a problem document, the field error names the flag.
	got := r.exec(t, "lessons", "cancel", "--as", demoOffice, "--yes", "--output", "json", "--note",
		strings.Repeat("x", 501), demoJazz)
	if got.code != 2 || !strings.Contains(got.stderr, `"location": "--note"`) || got.stdout != "" {
		t.Errorf("JSON field error: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}
}

// TestLessonsGroup prints the group's help without a subcommand and
// refuses an unknown one.
func TestLessonsGroup(t *testing.T) {
	t.Parallel()

	r := cliRun{}

	if got := r.exec(t, "lessons"); got.code != 0 || !strings.Contains(got.stdout, "cancel") {
		t.Errorf("help: exit %d, stdout %q", got.code, got.stdout)
	}

	wantExit(t, r.exec(t, "lessons", "move"), 2, `unknown command "move"`)
}
