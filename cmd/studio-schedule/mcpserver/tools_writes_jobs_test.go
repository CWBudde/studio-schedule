package mcpserver

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/jobs"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// wjOptions are the flags of the run_job tests.
var wjOptions = Options{AllowWrites: []WriteGroup{WriteJobs}}

// wjFixtureJobs are the jobs of fixture.jobs, in the runner's order: no
// deliver-mail, no deliver-push.
const wjFixtureJobs = "generate-lessons, purge-sessions, waitlist-expiry, announcements, purge-mail, purge-personal-data"

// wjLatest returns the latest recorded run of the job name on f's store,
// as `jobs list` reads it; zero if it has none.
func wjLatest(t *testing.T, f *fixture, name string) domain.JobRun {
	t.Helper()

	runner, err := f.jobs()
	if err != nil {
		t.Fatal(err)
	}

	statuses, err := runner.Statuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range statuses {
		if s.Name == name {
			return s.Last
		}
	}

	t.Fatalf("no job %s", name)

	return domain.JobRun{}
}

// wjOutcomes are the tool and outcome of each audited call of f.
func wjOutcomes(t *testing.T, f *fixture) []string {
	t.Helper()

	var out []string
	for _, c := range toolCalls(t, f.st) {
		out = append(out, c.Details.Tool+" "+c.Details.Outcome)
	}

	return out
}

// TestRunJob: a job runs now and its run is returned as `jobs run --output
// json` prints it, the instants of the fixed clock in UTC; the run is
// recorded, and the call audited as ok.
func TestRunJob(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoAdmin, wjOptions, registry)

	if last := wjLatest(t, f, "generate-lessons"); !last.ID.IsZero() {
		t.Fatalf("generate-lessons ran before the call: %+v", last)
	}

	text, isError := callTool(t, cs, "run_job", map[string]any{"name": "generate-lessons"})
	if isError {
		t.Fatalf("run_job: error result %q", text)
	}

	golden(t, "run_job.json", text)

	doc := document(t, cs, "run_job", map[string]any{"name": "purge-sessions"})
	if doc["job"] != "purge-sessions" || doc["result"] != "ok" || doc["startedAt"] != "2026-09-22T08:00:00Z" ||
		doc["endedAt"] != "2026-09-22T08:00:00Z" {
		t.Errorf("purge-sessions: %v", doc)
	}

	if _, has := doc["error"]; has {
		t.Errorf("an ok run has an error: %v", doc)
	}

	for _, name := range []string{"generate-lessons", "purge-sessions"} {
		if last := wjLatest(t, f, name); last.ID.IsZero() || last.Result != domain.JobOK ||
			!last.StartedAt.Equal(testNow) {
			t.Errorf("the recorded run of %s: %+v", name, last)
		}
	}

	if got, want := wjOutcomes(t, f), []string{"run_job ok", "run_job ok"}; !slices.Equal(got, want) {
		t.Errorf("the calls were audited as %v, want %v", got, want)
	}
}

// TestRunJobUnknown: an unknown job is not found, and the error lists the
// jobs there are; nothing is recorded.
func TestRunJobUnknown(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoAdmin, wjOptions, registry)

	wantError(t, cs, "run_job", map[string]any{"name": "zebras"},
		`not found: no job "zebras"; the jobs are `+wjFixtureJobs)

	// deliver-mail is a job of the server's, not of this configuration's.
	wantError(t, cs, "run_job", map[string]any{"name": "deliver-mail"},
		`not found: no job "deliver-mail"; the jobs are `+wjFixtureJobs)

	if got, want := wjOutcomes(t, f), []string{"run_job not-found", "run_job not-found"}; !slices.Equal(got, want) {
		t.Errorf("the calls were audited as %v, want %v", got, want)
	}
}

// TestRunJobFailed: a job that fails ran, so the result is the run with
// its error and the result failed, not an error result; the call is ok.
func TestRunJobFailed(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	runner := jobs.NewRunner(jobs.Config{Store: f.st, Clock: clock.NewFixed(testNow), IDs: f.ids}, jobs.Job{
		Name: "flaky", Every: time.Hour, Run: func(context.Context) error { return errors.New("disk full") },
	})

	opts := wjOptions
	opts.Jobs = func() (*jobs.Runner, error) { return runner, nil }
	cs := f.session(t, demoAdmin, opts, registry)

	doc := document(t, cs, "run_job", map[string]any{"name": "flaky"})
	if doc["job"] != "flaky" || doc["result"] != "failed" || doc["error"] != `job "flaky": disk full` ||
		doc["startedAt"] != "2026-09-22T08:00:00Z" {
		t.Errorf("flaky: %v", doc)
	}

	statuses, err := runner.Statuses(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if last := statuses[0].Last; last.Result != domain.JobFailed || last.Error != `job "flaky": disk full` {
		t.Errorf("the recorded run: %+v", last)
	}

	if got, want := wjOutcomes(t, f), []string{"run_job ok"}; !slices.Equal(got, want) {
		t.Errorf("the calls were audited as %v, want %v", got, want)
	}
}

// wjLockFails is a store whose jobs' locks cannot be taken.
type wjLockFails struct{ store.Store }

func (wjLockFails) TryLockJob(context.Context, string) (func(), bool, error) {
	return nil, false, errors.New("connection reset")
}

// TestRunJobLockFails: a lock that cannot be taken fails the run without
// running the job; the run is recorded and returned as failed, not as an
// error result.
func TestRunJobLockFails(t *testing.T) {
	t.Parallel()

	f := fixtureOn(wjLockFails{newFixture(t).st})
	cs := f.session(t, demoAdmin, wjOptions, registry)

	doc := document(t, cs, "run_job", map[string]any{"name": "generate-lessons"})
	if doc["result"] != "failed" || doc["error"] != `job "generate-lessons": lock: connection reset` {
		t.Errorf("generate-lessons: %v", doc)
	}

	if last := wjLatest(t, f, "generate-lessons"); last.Result != domain.JobFailed {
		t.Errorf("the recorded run: %+v", last)
	}
}

// TestRunJobLocked: while another holder has the job's lock the run is
// skipped, a conflict; once the lock is free the job runs.
func TestRunJobLocked(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoAdmin, wjOptions, registry)

	release, ok, err := f.st.TryLockJob(t.Context(), "generate-lessons")
	if err != nil || !ok {
		t.Fatalf("TryLockJob: %t, %v", ok, err)
	}

	wantError(t, cs, "run_job", map[string]any{"name": "generate-lessons"},
		`conflict: job "generate-lessons" is running elsewhere`)

	if last := wjLatest(t, f, "generate-lessons"); last.Result != domain.JobSkipped {
		t.Errorf("the recorded run while locked: %+v", last)
	}

	release()

	if doc := document(t, cs, "run_job", map[string]any{"name": "generate-lessons"}); doc["result"] != "ok" {
		t.Errorf("after the release: %v", doc)
	}

	if got, want := wjOutcomes(t, f), []string{"run_job conflict", "run_job ok"}; !slices.Equal(got, want) {
		t.Errorf("the calls were audited as %v, want %v", got, want)
	}
}

// TestRunJobNoRunner: jobs that cannot be built (the command's database
// failing) are an unexpected error: "internal error" for the client, the
// cause in the log by the tool's name.
func TestRunJobNoRunner(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	opts := wjOptions
	opts.Jobs = func() (*jobs.Runner, error) { return nil, errors.New("database unreachable") }
	cs := f.session(t, demoAdmin, opts, registry)

	wantError(t, cs, "run_job", map[string]any{"name": "generate-lessons"}, "internal error")

	if log := f.log.String(); !strings.Contains(log, "database unreachable") || !strings.Contains(log, "tool=run_job") {
		t.Errorf("log %q", log)
	}

	if got, want := wjOutcomes(t, f), []string{"run_job error"}; !slices.Equal(got, want) {
		t.Errorf("the calls were audited as %v, want %v", got, want)
	}
}

// wjRunsUnrecorded is a store whose job runs cannot be recorded; the rest,
// the audit included, works.
type wjRunsUnrecorded struct{ store.Store }

func (s wjRunsUnrecorded) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, wjRunsUnrecordedTx{tx})
	})
}

type wjRunsUnrecordedTx struct{ store.Tx }

func (tx wjRunsUnrecordedTx) JobRuns() store.JobRunRepo { return wjRunsUnrecordedRepo{tx.Tx.JobRuns()} }

type wjRunsUnrecordedRepo struct{ store.JobRunRepo }

func (wjRunsUnrecordedRepo) Create(context.Context, domain.JobRun) error {
	return errors.New("disk full")
}

// TestRunJobUnrecorded: a run whose record fails still has its ID (the
// runner only logs the failure), so the tool returns the run as it ended,
// as `jobs run` prints it; the run is not in the store. RunNow returns a
// run without an ID only for an unknown job.
func TestRunJobUnrecorded(t *testing.T) {
	t.Parallel()

	f := fixtureOn(wjRunsUnrecorded{newFixture(t).st})
	cs := f.session(t, demoAdmin, wjOptions, registry)

	if doc := document(t, cs, "run_job", map[string]any{"name": "generate-lessons"}); doc["result"] != "ok" {
		t.Errorf("generate-lessons: %v", doc)
	}

	if last := wjLatest(t, f, "generate-lessons"); !last.ID.IsZero() {
		t.Errorf("the run was recorded: %+v", last)
	}

	if got, want := wjOutcomes(t, f), []string{"run_job ok"}; !slices.Equal(got, want) {
		t.Errorf("the calls were audited as %v, want %v", got, want)
	}
}

// TestRunJobInput: name is required; an unknown property is refused.
func TestRunJobInput(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoAdmin, wjOptions, registry)

	for _, tc := range []struct {
		args any
		text string
	}{
		{nil, "validation failed: name is required"},
		{map[string]any{}, "validation failed: name is required"},
		{map[string]any{"name": ""}, "validation failed: name is required"},
		{
			map[string]any{"name": "generate-lessons", "force": true},
			`validation failed: the arguments do not fit the tool's input schema: json: unknown field "force"`,
		},
	} {
		wantError(t, cs, "run_job", tc.args, tc.text)
	}

	if last := wjLatest(t, f, "generate-lessons"); !last.ID.IsZero() {
		t.Errorf("an invalid call ran the job: %+v", last)
	}

	for _, c := range toolCalls(t, f.st) {
		if c.Details.Tool != "run_job" || c.Details.Outcome != "invalid" {
			t.Errorf("an invalid call was audited as %+v", c.Details)
		}
	}
}

// wjMailer and wjPusher stand in for a mail server and the VAPID keys, so
// app.Jobs builds deliver-mail and deliver-push too.
type (
	wjMailer struct{}
	wjPusher struct{}
)

func (wjMailer) Send(context.Context, mail.Message) error { return nil }

func (wjPusher) PublicKey() string { return "" }

func (wjPusher) Send(context.Context, domain.PushSubscription, []byte, time.Duration) error {
	return nil
}

// TestJobEffects: jobEffects has exactly the jobs app.Jobs builds with
// every sender, so a new job cannot be left out. run_job's description
// names every job that mails or deletes, and the tool is destructive,
// open to the world and not idempotent.
func TestJobEffects(t *testing.T) {
	t.Parallel()

	runner := app.Jobs(app.Deps{
		Store: memory.New(), Clock: clock.NewFixed(testNow), IDs: ids.NewSequence(), Mailer: wjMailer{},
		Pusher: wjPusher{},
	})

	var names []string
	for _, info := range runner.Jobs() {
		names = append(names, info.Name)
	}

	slices.Sort(names)

	if keys := slices.Sorted(maps.Keys(jobEffects)); !slices.Equal(keys, names) {
		t.Errorf("jobEffects has %v, app.Jobs builds %v", keys, names)
	}

	if !slices.Contains(names, "deliver-mail") || !slices.Contains(names, "deliver-push") {
		t.Errorf("the senders' jobs are missing: %v", names)
	}

	f := newFixture(t)
	cs := f.session(t, demoAdmin, wjOptions, registry)

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(res.Tools, func(tl *mcp.Tool) bool { return tl.Name == "run_job" })
	if i < 0 {
		t.Fatal("run_job is not listed")
	}

	tl := res.Tools[i]

	for name, effect := range jobEffects {
		if effect != jobWrites && !strings.Contains(tl.Description, name) {
			t.Errorf("the description does not name %s, which %s: %s", name, effect, tl.Description)
		}
	}

	for _, want := range []string{
		"announcements, deliver-mail, deliver-push and waitlist-expiry " + string(jobMails),
		"purge-mail, purge-personal-data and purge-sessions " + string(jobDeletes),
	} {
		if !strings.Contains(tl.Description, want) {
			t.Errorf("the description lacks %q: %s", want, tl.Description)
		}
	}

	a := tl.Annotations
	if a == nil || a.DestructiveHint == nil || !*a.DestructiveHint || a.OpenWorldHint == nil || !*a.OpenWorldHint ||
		a.IdempotentHint || a.ReadOnlyHint {
		t.Errorf("run_job's annotations: %+v", a)
	}
}

// TestNewJobs: New needs Options.Jobs when the jobs group is allowed, and
// only then.
func TestNewJobs(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	admin := f.principal(t, demoAdmin)

	_, err := New(f.svc, f.actorOf(demoAdmin), admin, clock.NewFixed(testNow),
		Options{Checks: checksEnv{f}, AllowWrites: WriteGroups()}, "test")
	if err == nil || err.Error() != "mcpserver: Options.Jobs is nil, and the jobs group is allowed" {
		t.Errorf("New with the jobs group and no Jobs: %v", err)
	}

	others := slices.DeleteFunc(WriteGroups(), func(g WriteGroup) bool { return g == WriteJobs })

	if _, err := New(f.svc, f.actorOf(demoAdmin), admin, clock.NewFixed(testNow),
		Options{Checks: checksEnv{f}, AllowWrites: others}, "test"); err != nil {
		t.Errorf("New without the jobs group and no Jobs: %v", err)
	}
}
