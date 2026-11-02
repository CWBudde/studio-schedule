package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestScheduleGolden: the office's week by default, without person data,
// and a member's week with --personal-data, with whom of the family the
// lessons have booked.
func TestScheduleGolden(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	text, _ := callTool(t, f.session(t, demoOffice, Options{}, registry), "schedule", nil)
	golden(t, "schedule.json", text)

	text, _ = callTool(t, f.session(t, demoMember, Options{PersonalData: true}, registry), "schedule",
		map[string]any{"from": "2026-09-21", "to": "2026-09-27"})
	golden(t, "schedule-member-personal-data.json", text)
}

// lessonKeys counts the lessons of a schedule document that have each
// key of keys.
func lessonKeys(t *testing.T, doc map[string]any, keys ...string) map[string]int {
	t.Helper()

	counts := map[string]int{}

	lessons, ok := doc["lessons"].([]any)
	if !ok {
		t.Fatalf("no lessons in %v", doc)
	}

	for _, l := range lessons {
		for _, k := range keys {
			if _, ok := l.(map[string]any)[k]; ok {
				counts[k]++
			}
		}
	}

	return counts
}

// TestSchedulePersonalData: without --personal-data, no lesson names whom
// of the family it has booked or waitlisted, nor a rental's renter; the
// instructors stay, as the public schedule shows them.
func TestSchedulePersonalData(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	week := map[string]any{"from": "2026-09-21", "to": "2026-09-27"}
	keys := []string{"bookedFor", "waitlist", "renter", "instructor"}

	for _, tc := range []struct {
		as   string
		with string // a key that only --personal-data shows
	}{
		{demoMember, "bookedFor"},
		{demoOffice, "renter"},
	} {
		with := lessonKeys(t, document(t, f.session(t, tc.as, Options{PersonalData: true}, registry), "schedule", week),
			keys...)
		without := lessonKeys(t, document(t, f.session(t, tc.as, Options{}, registry), "schedule", week), keys...)

		if with[tc.with] == 0 || with["instructor"] == 0 {
			t.Errorf("%s with --personal-data: %v", tc.as, with)
		}

		if without["bookedFor"]+without["waitlist"]+without["renter"] != 0 || without["instructor"] != with["instructor"] {
			t.Errorf("%s without --personal-data: %v", tc.as, without)
		}
	}
}

// TestScheduleInput: from is today in the school's time zone by default,
// to a week from from; to is inclusive; the arguments are checked, by
// field and without their values, and the range by the service.
func TestScheduleInput(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoMember, Options{}, registry)

	for _, tc := range []struct {
		args     map[string]any
		from, to string // to is exclusive in the body
	}{
		{map[string]any{}, "2026-09-22", "2026-09-29"},
		{map[string]any{"from": "2026-10-01"}, "2026-10-01", "2026-10-08"},
		{map[string]any{"to": "2026-09-22"}, "2026-09-22", "2026-09-23"},
		{map[string]any{"from": "2026-09-01", "to": "2026-10-12"}, "2026-09-01", "2026-10-13"},
		{map[string]any{"location": "01890a5d-ac96-774b-bcce-b302099a8057"}, "2026-09-22", "2026-09-29"},
	} {
		doc := document(t, cs, "schedule", tc.args)
		if doc["from"] != tc.from || doc["to"] != tc.to {
			t.Errorf("%v: from %v, to %v; want %s, %s", tc.args, doc["from"], doc["to"], tc.from, tc.to)
		}
	}

	for _, tc := range []struct {
		args map[string]any
		text string
	}{
		{map[string]any{"from": "22.09.2026"}, "validation failed: from is not a date (YYYY-MM-DD)"},
		{map[string]any{"from": "2026-09-22", "to": "tomorrow"}, "validation failed: to is not a date (YYYY-MM-DD)"},
		{map[string]any{"location": "Zebulon"}, "validation failed: location is not an ID"},
		{map[string]any{"courseType": "1"}, "validation failed: courseType is not an ID"},
		{
			map[string]any{"level": "x", "instructor": "y"},
			"validation failed: level is not an ID\nvalidation failed: instructor is not an ID",
		},
		{map[string]any{"from": "2026-09-22", "to": "2026-09-21"}, "validation failed: to must be after from"},
		{
			map[string]any{"from": "2026-09-01", "to": "2026-10-13"},
			"validation failed: the range must not exceed 42 days",
		},
	} {
		text, isError := callTool(t, cs, "schedule", tc.args)
		if !isError || !strings.HasPrefix(text, tc.text) {
			t.Errorf("%v: result %q (error %t), want %q", tc.args, text, isError, tc.text)
		}
	}
}

// TestScheduleSchool: without school settings the schedule is not found;
// settings that fail to load, or a time zone the host lacks, are an
// unexpected error.
func TestScheduleSchool(t *testing.T) {
	t.Parallel()

	t.Run("no settings", func(t *testing.T) {
		t.Parallel()

		f := fixtureOn(noSchool{newFixture(t).st})
		cs := f.session(t, demoMember, Options{}, registry)

		wantError(t, cs, "schedule", nil, "schedule: not found: no school settings")
	})

	t.Run("settings fail", func(t *testing.T) {
		t.Parallel()

		st := &faultStore{Store: newFixture(t).st}
		f := fixtureOn(st)
		admin := f.principal(t, demoAdmin)
		cs := f.sessionOf(t, admin, func(context.Context) (service.Principal, error) { return admin, nil }, Options{},
			registry)

		st.failing(store.ReadOnly, errors.New("connection reset"))

		wantError(t, cs, "schedule", nil, "internal error")

		if !strings.Contains(f.log.String(), "connection reset") {
			t.Errorf("log %q", f.log.String())
		}
	})

	t.Run("unknown time zone", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t)

		err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			s, err := tx.Settings().Get(ctx)
			if err != nil {
				return err
			}

			s.Timezone = "Atlantis/Poseidonia"

			return tx.Settings().Put(ctx, s)
		})
		if err != nil {
			t.Fatal(err)
		}

		cs := f.session(t, demoMember, Options{}, registry)
		wantError(t, cs, "schedule", nil, "internal error")

		if !strings.Contains(f.log.String(), "the school's time zone") {
			t.Errorf("log %q", f.log.String())
		}
	})
}

// TestStatistics: the office's figures; without --personal-data the top
// participants are left out, the key absent, with it they are the
// getStatistics body's. The range as `stats` reads its flags.
func TestStatistics(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{}, registry)

	text, _ := callTool(t, cs, "statistics", nil)
	golden(t, "statistics.json", text)

	persons := f.session(t, demoOffice, Options{PersonalData: true}, registry)

	text, _ = callTool(t, persons, "statistics", nil)
	golden(t, "statistics-personal-data.json", text)

	for _, args := range []map[string]any{{}, {"range": "term"}} {
		if top, _ := document(t, persons, "statistics", args)["topParticipants"].([]any); len(top) == 0 {
			t.Errorf("%v with --personal-data: no top participants", args)
		}
	}

	for _, tc := range []struct {
		args            map[string]any
		rng, from, to   string
		hasTerm, failed bool
	}{
		{args: map[string]any{}, rng: "last4Weeks", from: "2026-08-31", to: "2026-09-22"},
		{args: map[string]any{"range": "last12Weeks"}, rng: "last12Weeks", from: "2026-07-06", to: "2026-09-22"},
		{args: map[string]any{"range": "term"}, rng: "term", from: "2026-09-01", to: "2026-09-22", hasTerm: true},
		{args: map[string]any{"from": "2026-09-01", "to": "2026-09-30"}, rng: "custom", from: "2026-09-01", to: "2026-09-30"},
		{
			args: map[string]any{"range": "custom", "from": "2026-09-14", "to": "2026-09-20"}, rng: "custom",
			from: "2026-09-14", to: "2026-09-20",
		},
	} {
		doc := document(t, cs, "statistics", tc.args)

		_, hasTerm := doc["term"]
		if _, has := doc["topParticipants"]; has || doc["range"] != tc.rng || doc["from"] != tc.from ||
			doc["to"] != tc.to || hasTerm != tc.hasTerm {
			t.Errorf("%v: range %v from %v to %v, term %t, top participants %t", tc.args, doc["range"], doc["from"],
				doc["to"], hasTerm, has)
		}
	}

	for _, tc := range []struct {
		args map[string]any
		text string
	}{
		{
			map[string]any{"range": "term", "from": "2026-09-01", "to": "2026-09-30"},
			"validation failed: from and to give a custom range: leave range out or make it custom",
		},
		{map[string]any{"from": "1.9.2026", "to": "2026-09-30"}, "validation failed: from is not a date (YYYY-MM-DD)"},
		{map[string]any{"from": "2026-09-01", "to": "30.9.2026"}, "validation failed: to is not a date (YYYY-MM-DD)"},
		{map[string]any{"from": "2026-09-01"}, "statistics: validation failed: a custom range needs valid from and to dates"},
		{map[string]any{"range": "decade"}, `statistics: validation failed: unknown range "decade"`},
	} {
		wantError(t, cs, "statistics", tc.args, tc.text)
	}
}

// TestChecks: the reports of doctor and db check; a failed check is a
// report, not an error result; an unknown check is invalid.
func TestChecks(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoAdmin, Options{}, registry)

	text, _ := callTool(t, cs, "doctor", nil)
	golden(t, "doctor.json", text)

	text, _ = callTool(t, cs, "db_check", nil)
	golden(t, "db_check.json", text)

	only := document(t, cs, "db_check", map[string]any{"only": []string{"capacity"}})
	if checks, _ := json.Marshal(only["checks"]); string(checks) != `["capacity"]` {
		t.Errorf("--only capacity ran %s", checks)
	}

	text, isError := callTool(t, cs, "db_check", map[string]any{"only": []string{"capacity", "spelling"}})
	if !isError || !strings.HasPrefix(text, `validation failed: unknown check "spelling"`) {
		t.Errorf("unknown check: %q (error %t)", text, isError)
	}

	// A failing check: the doctor's database does not open.
	failing := Options{Checks: brokenChecks{checksEnv{f}}}
	doc := document(t, f.session(t, demoAdmin, failing, registry), "doctor", nil)

	if doc["ok"] != false {
		t.Errorf("doctor on a database that does not open: %v", doc)
	}

	calls := toolCalls(t, f.st)
	if last := calls[len(calls)-1]; last.Details.Tool != "doctor" || last.Details.Outcome != "ok" {
		t.Errorf("the failed report was audited as %+v", last.Details)
	}
}

// brokenChecks is a checks environment whose database does not open.
type brokenChecks struct{ checksEnv }

func (brokenChecks) Open(context.Context, config.Config) (app.Services, func(), error) {
	return app.Services{}, nil, errors.New("connection refused")
}

// noSchool is a store without school settings, but with the demo's
// accounts, which the audit events need.
type noSchool struct{ store.Store }

func (s noSchool) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error { return fn(ctx, noSchoolTx{tx}) })
}

type noSchoolTx struct{ store.Tx }

func (tx noSchoolTx) Settings() store.SettingsRepo { return noSettings{tx.Tx.Settings()} }

type noSettings struct{ store.SettingsRepo }

func (noSettings) Get(context.Context) (domain.Settings, error) {
	return domain.Settings{}, domain.ErrNotFound
}
