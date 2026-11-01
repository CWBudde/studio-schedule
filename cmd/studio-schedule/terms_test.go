package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// The demo term and its Saturday Contemporary class (seed's deterministic
// IDs).
const (
	demoTerm         = "00000000-0000-7000-8000-00000000005a"
	demoContemporary = "00000000-0000-7000-8000-000000000074"
)

// servicesOf are the application's services on st at testNow, for a test
// to change the data the way the office would.
func servicesOf(t *testing.T, st store.Store) app.Services {
	t.Helper()

	return app.NewServices(app.Deps{Store: st, Clock: clock.NewFixed(testNow), IDs: idsOf(t), AppURL: "http://localhost/"})
}

// pastDemoStore is a memory store with the demo data of an earlier school
// year: no term includes today (testNow).
func pastDemoStore(t *testing.T) *memory.Store {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemoAt(t.Context(), st, domain.Date{Year: 2025, Month: time.September, Day: 22}); err != nil {
		t.Fatal(err)
	}

	return st
}

// newSchoolStore is a memory store with a school that was just set up:
// the demo settings and a confirmed office account of demoOffice, and no
// terms yet.
func newSchoolStore(t *testing.T) *memory.Store {
	t.Helper()

	st := memory.New()
	seq := ids.NewSequence()
	person := domain.Person{ID: ids.New[domain.PersonID](seq), GivenName: "Olga", FamilyName: "Ledgerby"}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := tx.Settings().Put(ctx, seed.DemoSettings()); err != nil {
			return err
		}

		if err := tx.Persons().Create(ctx, person); err != nil {
			return err
		}

		return tx.Accounts().Create(ctx, domain.Account{
			ID: ids.New[domain.AccountID](seq), PersonID: person.ID, Email: demoOffice, Role: domain.RoleOffice,
			ConfirmedAt: testNow,
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	return st
}

// TestTermsList lists the demo term, marked current, as text and as
// listTerms's body.
func TestTermsList(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		got := r.exec(t, "terms", "list", "--as", demoOffice)
		wantExit(t, got, 0, "")
		golden(t, "terms/list.txt", got.stdout)

		got = r.exec(t, "terms", "list", "--as", demoOffice, "--output", "json")
		wantExit(t, got, 0, "")
		golden(t, "terms/list.json", got.stdout)
	})
}

// TestTermsListRefusals covers the --as refusals, a term that is not
// current, and the group's help.
func TestTermsListRefusals(t *testing.T) {
	t.Parallel()

	r := cliRun{st: demoStore(t)}
	wantExit(t, r.exec(t, "terms", "list", "--as", demoMember), 4, "below office")
	wantExit(t, r.exec(t, "terms", "list", "--as", demoInstructor), 4, "below office")
	wantExit(t, r.exec(t, "terms", "list"), 2, "give --as <email>")

	// The term of an earlier school year is not current.
	got := cliRun{st: pastDemoStore(t)}.exec(t, "terms", "list", "--as", demoOffice)
	if got.code != 0 || got.stdout != demoTerm+"  School Year 2025/26  2025-09-01  2026-07-31  -\n" {
		t.Errorf("past term: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	// A school without terms yet.
	got = cliRun{st: newSchoolStore(t)}.exec(t, "terms", "list", "--as", demoOffice)
	if got.code != 0 || got.stdout != "no terms\n" {
		t.Errorf("no terms: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	if got := (cliRun{}).exec(t, "terms"); got.code != 0 || !strings.Contains(got.stdout, "list") {
		t.Errorf("group help: exit %d, stdout %q", got.code, got.stdout)
	}
}

// retire retires the class id of the demo term as the office would.
func retire(t *testing.T, svc app.Services, id string) {
	t.Helper()

	term, _ := domain.ParseID[domain.TermID](demoTerm)

	classes, err := svc.Timetable.AllClasses(t.Context(), term)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range classes {
		if c.ID.String() != id {
			continue
		}

		_, err := svc.Timetable.UpdateClass(t.Context(), c.ID, service.ClassData{
			CourseTypeID: c.CourseTypeID, RoomID: c.RoomID, InstructorID: c.InstructorID, AccompanistID: c.AccompanistID,
			Level: c.Level, Weekday: c.Weekday, StartTime: c.StartTime, Duration: c.Duration, Capacity: c.Capacity,
			Retired: true,
		})
		if err != nil {
			t.Fatal(err)
		}

		return
	}

	t.Fatalf("no class %s", id)
}

// TestClassesList lists the classes of the current term by default and of
// --term, as text and as listAllClasses's body: a retired one included.
func TestClassesList(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		retire(t, servicesOf(t, storeOf(t, r)), demoContemporary)

		got := r.exec(t, "classes", "list", "--as", demoOffice)
		wantExit(t, got, 0, "")
		golden(t, "classes/list.txt", got.stdout)

		got = r.exec(t, "classes", "list", "--as", demoOffice, "--term", demoTerm, "--output", "json")
		wantExit(t, got, 0, "")
		golden(t, "classes/list.json", got.stdout)

		if !strings.Contains(got.stdout, `"id": "`+demoContemporary+`",`) {
			t.Error("the JSON leaves the retired class out")
		}
	})
}

// TestClassesListRefusals covers an unknown or invalid --term, no current
// term, the --as refusals and a term without classes.
func TestClassesListRefusals(t *testing.T) {
	t.Parallel()

	st := demoStore(t)
	r := cliRun{st: st}

	for _, c := range []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{"--as", demoMember}, 4, "below office"},
		{[]string{"--as", demoInstructor}, 4, "below office"},
		{[]string{"--as", demoOffice, "--term", "spring"}, 2, `--term: "spring" is not a term ID`},
		{[]string{"--as", demoOffice, "--term", demoUnknown}, 3, "--term: term " + demoUnknown + ": not found"},
	} {
		wantExit(t, r.exec(t, append([]string{"classes", "list"}, c.args...)...), c.code, c.stderr)
	}

	// A new term has no classes yet.
	summer, err := servicesOf(t, st).Timetable.CreateTerm(t.Context(), service.TermData{
		Name:     "Summer School 2027",
		StartsOn: domain.Date{Year: 2027, Month: time.August, Day: 2},
		EndsOn:   domain.Date{Year: 2027, Month: time.August, Day: 27},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := r.exec(t, "classes", "list", "--as", demoOffice, "--term", summer.ID.String())
	if got.code != 0 || got.stdout != "Summer School 2027: 2027-08-02 to 2027-08-27\nno classes\n" {
		t.Errorf("new term: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	// Without a current term, --term is needed.
	past := cliRun{st: pastDemoStore(t)}
	wantExit(t, past.exec(t, "classes", "list", "--as", demoOffice), 3,
		"no term includes today; give --term (see terms list)")

	got = past.exec(t, "classes", "list", "--as", demoOffice, "--term", demoTerm)
	if got.code != 0 || !strings.HasPrefix(got.stdout, "School Year 2025/26: 2025-09-01 to 2026-07-31\n") {
		t.Errorf("past term: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	if got := (cliRun{}).exec(t, "classes"); got.code != 0 || !strings.Contains(got.stdout, "list") {
		t.Errorf("group help: exit %d, stdout %q", got.code, got.stdout)
	}
}
