package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestDBCheckClean finds nothing in the demo school, on both stores.
func TestDBCheckClean(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		text := r.exec(t, "db", "check")
		if text.code != 0 || text.stderr != "" {
			t.Fatalf("db check: exit %d, stderr %q", text.code, text.stderr)
		}

		golden(t, "db/clean.txt", text.stdout)

		js := r.exec(t, "db", "check", "--output", "json")
		if js.code != 0 || js.stderr != "" {
			t.Fatalf("db check --output json: exit %d, stderr %q", js.code, js.stderr)
		}

		golden(t, "db/clean.json", js.stdout)
	})
}

// TestDBCheckFindings reports what every check finds in a broken demo
// school, the same on both stores, and exits with status 1.
func TestDBCheckFindings(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		breakSchool(t, storeOf(t, r))

		text := r.exec(t, "db", "check")
		if text.code != 1 {
			t.Errorf("db check: exit %d, want 1", text.code)
		}

		golden(t, "db/findings.txt", text.stdout)

		js := r.exec(t, "db", "check", "--output", "json")
		if js.code != 1 {
			t.Errorf("db check --output json: exit %d, want 1", js.code)
		}

		golden(t, "db/findings.json", js.stdout)

		report := dbReport(t, js)
		if report.OK {
			t.Error("ok with findings")
		}

		want := "4 findings" // one per check
		if text.stderr != "error: "+want+"\n" {
			t.Errorf("stderr %q, want %q", text.stderr, want)
		}

		var problem struct {
			Detail string `json:"detail"`
		}
		if err := json.Unmarshal([]byte(js.stderr), &problem); err != nil || problem.Detail != want {
			t.Errorf("--output json: stderr %q, want a problem document with %q (%v)", js.stderr, want, err)
		}

		ran := map[string]bool{}
		for _, f := range report.Findings {
			ran[f.Check] = true
		}

		if !slices.Equal(report.Checks, []string{"passes", "capacity", "tombstones", "receipt-counters"}) ||
			len(ran) != len(report.Checks) {
			t.Errorf("checks %v found %v, want every check to find something", report.Checks, ran)
		}
	})
}

// TestDBCheckOnly runs the checks of --only, in their own order, however
// they are given.
func TestDBCheckOnly(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		breakSchool(t, storeOf(t, r))

		capacity := r.exec(t, "db", "check", "--only", "capacity")
		if capacity.code != 1 || capacity.stderr != "error: 1 finding\n" {
			t.Errorf("--only capacity: exit %d, stderr %q, want 1 and one finding", capacity.code, capacity.stderr)
		}

		golden(t, "db/only-capacity.txt", capacity.stdout)

		for _, args := range [][]string{
			{"--only", "tombstones", "--only", "passes"},
			{"--only", "tombstones,passes"},
			{"--only", "passes,tombstones,passes"},
		} {
			res := r.exec(t, append([]string{"db", "check", "--output", "json"}, args...)...)
			if res.code != 1 {
				t.Errorf("%v: exit %d, want 1", args, res.code)
			}

			report := dbReport(t, res)
			if !slices.Equal(report.Checks, []string{"passes", "tombstones"}) {
				t.Errorf("%v: checks %v, want passes, tombstones", args, report.Checks)
			}

			var checks []string
			for _, f := range report.Findings {
				checks = append(checks, f.Check)
			}

			if !slices.Equal(slices.Compact(checks), []string{"passes", "tombstones"}) {
				t.Errorf("%v: findings of %v, want those of passes, then of tombstones", args, checks)
			}
		}
	})
}

// TestDBCheckOnlyClean names only the checks that ran when they find
// nothing.
func TestDBCheckOnlyClean(t *testing.T) {
	t.Parallel()

	res := cliRun{st: demoStore(t)}.exec(t, "db", "check", "--only", "receipt-counters,passes")
	if res.code != 0 || res.stdout != "no findings: passes, receipt-counters\n" {
		t.Errorf("exit %d, stdout %q", res.code, res.stdout)
	}
}

// TestDBCheckUnknown refuses a check that does not exist with status 2,
// before it reads anything, naming --only.
func TestDBCheckUnknown(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}

	text := r.exec(t, "db", "check", "--only", "passes,bogus")
	if text.code != 2 || text.stdout != "" {
		t.Errorf("exit %d, stdout %q, want 2 and nothing", text.code, text.stdout)
	}

	if !strings.HasPrefix(text.stderr, `error: --only: unknown check "bogus"; the checks are passes, capacity,`) {
		t.Errorf("stderr %q does not name --only", text.stderr)
	}

	js := r.exec(t, "db", "check", "--only", "bogus", "--output", "json")
	if js.code != 2 || js.stdout != "" {
		t.Errorf("--output json: exit %d, stdout %q, want 2 and nothing", js.code, js.stdout)
	}

	var problem struct {
		Status int `json:"status"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(js.stderr), &problem); err != nil {
		t.Fatalf("stderr is no problem document (%v): %q", err, js.stderr)
	}

	if len(problem.Errors) != 1 || problem.Errors[0].Location != "--only" ||
		!strings.HasPrefix(problem.Errors[0].Message, `unknown check "bogus"`) {
		t.Errorf("problem %+v, want the error at --only", problem)
	}

	// The database is not opened for an unknown check: an unreachable one
	// is not reported.
	unreachable := cliRun{open: failingOpener(errors.New("connection refused"))}.
		with("DATABASE_URL", "postgres://db.example.test/studio")
	if got := unreachable.exec(t, "db", "check", "--only", "bogus"); got.code != 2 ||
		!strings.HasPrefix(got.stderr, `error: --only: unknown check "bogus"`) {
		t.Errorf("unreachable database: exit %d, stderr %q, want 2 naming --only", got.code, got.stderr)
	}
}

// TestDBCheckRefused refuses --as (an operator command) and a missing
// database URL with status 2, and fails with status 1 when the database
// cannot be opened or read.
func TestDBCheckRefused(t *testing.T) {
	t.Parallel()

	dbURL := []string{"DATABASE_URL", "postgres://db.example.test/studio"}

	for _, c := range []struct {
		name   string
		run    cliRun
		args   []string
		code   int
		stderr string
	}{
		{"as", cliRun{st: demoStore(t)}, []string{"--as", demoAdmin}, 2, "error: --as: studio-schedule db check acts as no account\n"},
		{"no database", cliRun{}, nil, 2, "error: " + errNoDatabaseURL.Error() + "\n"},
		{
			"invalid database url flag",
			cliRun{st: demoStore(t)},
			[]string{"--database-url", ""},
			2,
			"error: " + errNoDatabaseURL.Error() + "\n",
		},
		{
			"unreachable", cliRun{open: failingOpener(errors.New("connection refused"))}.with(dbURL...), nil, 1,
			"error: connection refused\n",
		},
		{
			"outdated schema", cliRun{open: failingOpener(errSchemaOutdated)}.with(dbURL...), nil, 1,
			"error: " + errSchemaOutdated.Error() + "\n",
		},
		// The layer reads the school's time zone before the check.
		{"unreadable", cliRun{st: unreadableStore{demoStore(t)}}, nil, 1, "error: public settings: the disk is unreadable\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			res := c.run.exec(t, append([]string{"db", "check"}, c.args...)...)
			if res.code != c.code || res.stderr != c.stderr || res.stdout != "" {
				t.Errorf("exit %d, stdout %q, stderr %q; want %d and %q", res.code, res.stdout, res.stderr, c.code, c.stderr)
			}
		})
	}
}

// TestDBCheckReadOnly changes nothing: it reads in read-only transactions
// and leaves the data as it was, findings or not.
func TestDBCheckReadOnly(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	breakSchool(t, st)

	before, err := st.Snapshot("test")
	if err != nil {
		t.Fatal(err)
	}

	rec := &txRecorder{Store: st}
	r := cliRun{st: rec}

	for _, args := range [][]string{nil, {"--output", "json"}, {"--only", "capacity"}} {
		if res := r.exec(t, append([]string{"db", "check"}, args...)...); res.code != 1 {
			t.Errorf("%v: exit %d, want 1", args, res.code)
		}
	}

	after, err := st.Snapshot("test")
	if err != nil {
		t.Fatal(err)
	}

	if !maps.EqualFunc(tablesOf(t, before), tablesOf(t, after), slices.Equal) {
		t.Error("db check changed the data")
	}

	// Each run: the layer's read of the school's time zone, then the check.
	if want := slices.Repeat([]store.TxMode{store.ReadOnly}, 6); !slices.Equal(rec.modes(), want) {
		t.Errorf("transactions %v, want %v", rec.modes(), want)
	}
}

// TestDBCheckTerminal shows the table's header on a terminal; a script
// reads the findings alone (the golden files).
func TestDBCheckTerminal(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	breakSchool(t, st)

	res := cliRun{st: st, terminal: true}.exec(t, "db", "check", "--only", "capacity")
	if res.code != 1 {
		t.Errorf("exit %d, want 1", res.code)
	}

	lines := strings.Split(strings.TrimSuffix(res.stdout, "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "\x1b[1mCheck\x1b[0m") || !strings.Contains(lines[0], "Message") ||
		!strings.HasPrefix(lines[1], "capacity") {
		t.Errorf("stdout %q, want the header and one finding", res.stdout)
	}
}

// tablesOf returns the rows of the tables of a memory store's snapshot,
// each table's rows sorted: the snapshot writes them in map order.
func tablesOf(t *testing.T, snapshot []byte) map[string][]string {
	t.Helper()

	var s struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := json.Unmarshal(snapshot, &s); err != nil {
		t.Fatal(err)
	}

	tables := map[string][]string{}

	for name, raw := range s.Tables {
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			rows = []json.RawMessage{raw} // the settings, a single row
		}

		for _, row := range rows {
			tables[name] = append(tables[name], string(row))
		}

		slices.Sort(tables[name])
	}

	return tables
}

// dbReport parses what db check --output json printed.
func dbReport(t *testing.T, res result) dbCheckReport {
	t.Helper()

	var report dbCheckReport
	if err := json.Unmarshal([]byte(res.stdout), &report); err != nil {
		t.Fatalf("db check printed no report (%v): stdout %q, stderr %q", err, res.stdout, res.stderr)
	}

	return report
}

// brokenReceipt is the ID of the receipt breakSchool adds.
const brokenReceipt = "0000000f-0000-7000-8000-000000000001"

// breakSchool damages the demo data of st so that every check finds
// something, the way a bug or a manual change in the database could: a
// visit pass with a visit fewer than its redemptions, a lesson with fewer
// places than active bookings, a person merged although bookings still
// refer to it, and a receipt numbered beyond its year's counter. It picks
// the records from the demo data set, whose IDs are the same in every
// store.
func breakSchool(t *testing.T, st store.Store) {
	t.Helper()

	d := seed.Demo()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		pass := firstOf(t, d.Passes, func(p domain.Pass) bool {
			return p.Kind == domain.PassVisits && p.VisitsRemaining > 0
		})
		pass.VisitsRemaining--

		if err := tx.Passes().Update(ctx, pass); err != nil {
			return err
		}

		lesson, active := overbookable(t, d)
		lesson.Capacity = active - 1

		if err := tx.Lessons().Update(ctx, lesson); err != nil {
			return err
		}

		merged := mergeable(t, d)
		merged.MergedInto = firstOf(t, d.Persons, func(p domain.Person) bool { return p.ID != merged.ID }).ID

		if err := tx.Persons().Update(ctx, merged); err != nil {
			return err
		}

		receipt := firstOf(t, d.Receipts, func(r domain.Receipt) bool { return r.Kind == domain.ReceiptSale })

		id, err := domain.ParseID[domain.ReceiptID](brokenReceipt)
		if err != nil {
			return err
		}

		receipt.ID, receipt.Sequence = id, 90
		receipt.Number = domain.ReceiptNumber(d.Settings.ReceiptPrefix, receipt.Year, receipt.Sequence)

		return tx.Receipts().Create(ctx, receipt)
	})
	if err != nil {
		t.Fatalf("break the demo school: %v", err)
	}
}

// firstOf returns the first of items that keep accepts.
func firstOf[T any](t *testing.T, items []T, keep func(T) bool) T {
	t.Helper()

	i := slices.IndexFunc(items, keep)
	if i < 0 {
		t.Fatal("the demo data has no such record")
	}

	return items[i]
}

// overbookable returns the first lesson of d with at least two active
// bookings and none booked beyond its capacity, and its active bookings.
func overbookable(t *testing.T, d seed.Dataset) (domain.Lesson, int) {
	t.Helper()

	overridden := map[domain.BookingID]bool{}
	for _, e := range d.BookingEvents {
		overridden[e.BookingID] = overridden[e.BookingID] || e.Override
	}

	active, override := map[domain.LessonID]int{}, map[domain.LessonID]bool{}

	for _, b := range d.Bookings {
		if b.Status != domain.BookingCanceled {
			active[b.LessonID]++
			override[b.LessonID] = override[b.LessonID] || overridden[b.ID]
		}
	}

	l := firstOf(t, d.Lessons, func(l domain.Lesson) bool { return active[l.ID] >= 2 && !override[l.ID] })

	return l, active[l.ID]
}

// mergeable returns the person of d with bookings whom the fewest records
// refer to, so the golden files stay short.
func mergeable(t *testing.T, d seed.Dataset) domain.Person {
	t.Helper()

	refs, booked := map[domain.PersonID]int{}, map[domain.PersonID]bool{}

	for _, b := range d.Bookings {
		refs[b.PersonID]++
		booked[b.PersonID] = true
	}

	for _, m := range d.ClassMembers {
		refs[m.PersonID]++
	}

	for _, g := range d.Guardians {
		refs[g.GuardianID]++
		refs[g.DependentID]++
	}

	for _, a := range d.Accounts {
		refs[a.PersonID]++
	}

	for _, s := range d.Staff {
		refs[s.PersonID]++
	}

	for _, p := range d.Passes {
		refs[p.PersonID]++
		refs[p.PayerID]++
	}

	for _, e := range d.WaitlistEntries {
		refs[e.PersonID]++
	}

	var (
		best  domain.Person
		found bool
	)

	for _, p := range d.Persons {
		if booked[p.ID] && (!found || refs[p.ID] < refs[best.ID]) {
			best, found = p, true
		}
	}

	if !found {
		t.Fatal("the demo data has no booked person")
	}

	return best
}

// txRecorder records the modes of the transactions on a store.
type txRecorder struct {
	store.Store

	mu  sync.Mutex
	got []store.TxMode
}

func (r *txRecorder) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	r.mu.Lock()
	r.got = append(r.got, mode)
	r.mu.Unlock()

	return r.Store.InTx(ctx, mode, fn)
}

func (r *txRecorder) modes() []store.TxMode {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.got)
}
