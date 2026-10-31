package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The seed tests run the command against a real database; the flag
// validation that needs none is in main_test.go.

// seedState is what a seeded database holds, as far as these tests check.
type seedState struct {
	settings   domain.Settings
	background domain.Background
	terms      []domain.Term
	locations  int
	persons    int
}

// seedRead reads the seeded data from the database at databaseURL.
func seedRead(t *testing.T, databaseURL string) seedState {
	t.Helper()

	var s seedState

	err := openTestStore(t, databaseURL).InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		if s.settings, err = tx.Settings().Get(ctx); err != nil {
			return fmt.Errorf("settings: %w", err)
		}

		if s.background, err = tx.Settings().Background(ctx); err != nil {
			return fmt.Errorf("background: %w", err)
		}

		if s.terms, err = tx.Terms().List(ctx); err != nil {
			return fmt.Errorf("terms: %w", err)
		}

		locations, err := tx.Locations().List(ctx)
		if err != nil {
			return fmt.Errorf("locations: %w", err)
		}

		persons, err := tx.Persons().List(ctx)
		if err != nil {
			return fmt.Errorf("persons: %w", err)
		}

		s.locations, s.persons = len(locations), len(persons)

		return nil
	})
	if err != nil {
		t.Fatalf("read seeded data: %v", err)
	}

	return s
}

// seedCheckSchool checks that the database holds the demo school sch for
// the anchor date: its settings and background, the term around the anchor
// and its places.
func seedCheckSchool(t *testing.T, got seedState, sch seed.School, anchor domain.Date) {
	t.Helper()

	want, err := seed.DemoSchoolAt(anchor, sch)
	if err != nil {
		t.Fatalf("demo data: %v", err)
	}

	if got.settings.SchoolName != want.Settings.SchoolName || got.settings.Theme != want.Settings.Theme ||
		got.settings.DefaultLocale != want.Settings.DefaultLocale || got.settings.ReceiptPrefix != want.Settings.ReceiptPrefix {
		t.Errorf("settings: name %q, theme %q, locale %q, prefix %q; want %q, %q, %q, %q",
			got.settings.SchoolName, got.settings.Theme, got.settings.DefaultLocale, got.settings.ReceiptPrefix,
			want.Settings.SchoolName, want.Settings.Theme, want.Settings.DefaultLocale, want.Settings.ReceiptPrefix)
	}

	if got.background.IsZero() != want.Background.IsZero() || got.background.ContentType != want.Background.ContentType ||
		len(got.background.Data) != len(want.Background.Data) {
		t.Errorf("background: %q, %d bytes; want %q, %d bytes",
			got.background.ContentType, len(got.background.Data), want.Background.ContentType, len(want.Background.Data))
	}

	if len(got.terms) != len(want.Terms) {
		t.Fatalf("%d terms, want %d", len(got.terms), len(want.Terms))
	}

	for i, term := range got.terms {
		if term != want.Terms[i] {
			t.Errorf("term %d = %+v, want %+v", i, term, want.Terms[i])
		}

		if term.StartsOn.Compare(anchor) > 0 || term.EndsOn.Compare(anchor) < 0 {
			t.Errorf("term %s to %s does not contain the anchor %s", term.StartsOn, term.EndsOn, anchor)
		}
	}

	if got.locations != len(want.Locations) {
		t.Errorf("%d locations, want %d", got.locations, len(want.Locations))
	}

	if got.persons == 0 {
		t.Error("no persons seeded")
	}
}

// seedCheckOutput checks seed's report: the school, the anchor and the
// warning about the published password.
func seedCheckOutput(t *testing.T, out string, sch seed.School, anchor domain.Date) {
	t.Helper()

	for _, want := range []string{
		fmt.Sprintf("demo data (school %s) loaded around %s\n", sch, anchor),
		"warning:",
		fmt.Sprintf("%q", seed.DemoPassword),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("seed output lacks %q:\n%s", want, out)
		}
	}
}

// TestSeedDatabase seeds an empty database with each demo school and anchor
// and checks what it reports and what the database then holds.
func TestSeedDatabase(t *testing.T) {
	t.Parallel()

	anchor2027 := domain.Date{Year: 2027, Month: 9, Day: 14}

	for name, tc := range map[string]struct {
		args   []string
		school seed.School
		anchor domain.Date
	}{
		"defaults":         {school: seed.SchoolProgramme, anchor: seed.FixedDemoAnchor},
		"werkstatt":        {args: []string{"--school", "werkstatt"}, school: seed.SchoolWerkstatt, anchor: seed.FixedDemoAnchor},
		"anchor date":      {args: []string{"--anchor", "2027-09-14"}, school: seed.SchoolProgramme, anchor: anchor2027},
		"werkstatt anchor": {args: []string{"--school", "werkstatt", "--anchor", "2027-09-14"}, school: seed.SchoolWerkstatt, anchor: anchor2027},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			url := newTestDatabase(t)

			out, err := execute(map[string]string{"DATABASE_URL": url}, append([]string{"seed"}, tc.args...)...)
			if err != nil {
				t.Fatalf("seed: %v\n%s", err, out)
			}

			seedCheckOutput(t, out, tc.school, tc.anchor)
			seedCheckSchool(t, seedRead(t, url), tc.school, tc.anchor)
		})
	}
}

// TestSeedAnchorToday: --anchor today builds the term around the current
// date in the demo school's time zone.
func TestSeedAnchorToday(t *testing.T) {
	t.Parallel()

	url := newTestDatabase(t)

	// Taken around the run, so a run across midnight matches either date.
	before, err := demoAnchor(anchorToday, clock.System{})
	if err != nil {
		t.Fatal(err)
	}

	out, err := execute(nil, "seed", "--database-url", url, "--anchor", "today")
	if err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}

	after, err := demoAnchor(anchorToday, clock.System{})
	if err != nil {
		t.Fatal(err)
	}

	today := before
	if !strings.Contains(out, " around "+before.String()+"\n") {
		today = after
	}

	seedCheckOutput(t, out, seed.SchoolProgramme, today)
	seedCheckSchool(t, seedRead(t, url), seed.SchoolProgramme, today)
}

// TestSeedRefusesDatabaseInUse: seed never mixes demo data into a database
// that holds data, and leaves that data as it was.
func TestSeedRefusesDatabaseInUse(t *testing.T) {
	t.Parallel()

	url := newTestDatabase(t)
	env := map[string]string{"DATABASE_URL": url}

	if out, err := execute(env, "seed"); err != nil {
		t.Fatalf("first seed: %v\n%s", err, out)
	}

	before := seedRead(t, url)

	out, err := execute(env, "seed", "--school", "werkstatt", "--anchor", "2027-09-14")
	if !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("second seed error = %v, want a conflict about a store that is not empty\n%s", err, out)
	}

	if strings.Contains(out, "loaded") {
		t.Errorf("second seed claims success:\n%s", out)
	}

	seedCheckSchool(t, seedRead(t, url), seed.SchoolProgramme, seed.FixedDemoAnchor)

	if after := seedRead(t, url); after.persons != before.persons || after.locations != before.locations {
		t.Errorf("after the refused seed: %d persons, %d locations; want %d, %d",
			after.persons, after.locations, before.persons, before.locations)
	}

	// A store with data but no settings counts as in use too.
	url = newTestDatabase(t)
	execSQL(t, url, "INSERT INTO locations (id, name) VALUES ($1, 'Fictional Hall')", "0b9a1c52-6f0e-4c1e-9a43-2f9c3b1d7e01")

	out, err = execute(map[string]string{"DATABASE_URL": url}, "seed")
	if !errors.Is(err, domain.ErrConflict) || !strings.Contains(err.Error(), "locations") {
		t.Fatalf("seed into a database with a location: error = %v, want a conflict naming locations\n%s", err, out)
	}
}

// TestSeedRefusesMismatchedSchema: seed opens the store only if exactly the
// binary's migrations are applied, and writes nothing otherwise.
func TestSeedRefusesMismatchedSchema(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		prepare func(t *testing.T) string
		want    error
	}{
		"never migrated": {
			prepare: newUnmigratedDatabase,
			want:    errSchemaOutdated,
		},
		"newest migration rolled back": {
			prepare: func(t *testing.T) string {
				url := newTestDatabase(t)
				migrateRun(t, url, "down")

				return url
			},
			want: errSchemaOutdated,
		},
		"migration the binary lacks": {
			prepare: func(t *testing.T) string {
				url := newTestDatabase(t)
				execSQL(t, url, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999, true)")

				return url
			},
			want: errSchemaNewer,
		},
		"rolled back by an older tool": {
			// goose's own tools appended is_applied=false rows; the latest
			// row of a version decides.
			prepare: func(t *testing.T) string {
				url := newTestDatabase(t)
				execSQL(t, url, `INSERT INTO goose_db_version (version_id, is_applied)
					SELECT max(version_id), false FROM goose_db_version`)

				return url
			},
			want: errSchemaOutdated,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			url := tc.prepare(t)

			out, err := execute(map[string]string{"DATABASE_URL": url}, "seed")
			if !errors.Is(err, tc.want) {
				t.Fatalf("seed error = %v, want %v\n%s", err, tc.want, out)
			}

			if strings.Contains(out, "loaded") {
				t.Errorf("seed claims success:\n%s", out)
			}
		})
	}
}

// TestSeedRejectsUnparsableDatabaseURL: a URL that passes the syntax check
// but not the driver's fails before connecting.
func TestSeedRejectsUnparsableDatabaseURL(t *testing.T) {
	t.Parallel()

	out, err := execute(nil, "seed", "--database-url", "postgres://127.0.0.1:1/studio?pool_max_conns=many")
	if err == nil || !strings.Contains(err.Error(), "invalid database URL") {
		t.Fatalf("seed error = %v, want an invalid database URL\n%s", err, out)
	}
}
