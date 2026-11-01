package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/store/postgres"
)

// The migrations themselves are tested against a real database in
// internal/store/postgres; these tests cover the command wiring.

func TestMigrateNeedsDatabaseURL(t *testing.T) {
	t.Parallel()

	for _, sub := range []string{"up", "down", "status"} {
		t.Run(sub, func(t *testing.T) {
			t.Parallel()

			out, err := execute(nil, "migrate", sub)
			if !errors.Is(err, errNoDatabaseURL) {
				t.Fatalf("migrate %s error = %v, want errNoDatabaseURL\n%s", sub, err, out)
			}

			// An empty flag does not fall back to the environment.
			out, err = execute(map[string]string{"DATABASE_URL": "postgres://db.example.test/studio"},
				"migrate", sub, "--database-url", "")
			if !errors.Is(err, errNoDatabaseURL) {
				t.Fatalf("migrate %s with empty flag: error = %v, want errNoDatabaseURL\n%s", sub, err, out)
			}
		})
	}
}

// TestDatabaseCommandsIgnoreLogSettings checks that bad log settings, which
// only serve uses, do not block the commands that write no log: they get as
// far as asking for the database.
func TestDatabaseCommandsIgnoreLogSettings(t *testing.T) {
	t.Parallel()

	env := map[string]string{"LOG_FORMAT": "xml", "LOG_LEVEL": "loud"}

	for _, args := range [][]string{{"migrate", "up"}, {"migrate", "status"}, {"seed"}, {"init"}} {
		out, err := execute(env, args...)
		if !errors.Is(err, errNoDatabaseURL) {
			t.Errorf("%v error = %v, want errNoDatabaseURL\n%s", args, err, out)
		}
	}
}

func TestMigrateRejectsInvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	out, err := execute(map[string]string{"DATABASE_URL": "mysql://db.example.test/studio"}, "migrate", "status")
	if !errors.Is(err, config.ErrInvalid) {
		t.Fatalf("migrate status error = %v, want config.ErrInvalid\n%s", err, out)
	}
}

func TestMigrateErrorHidesPassword(t *testing.T) {
	t.Parallel()

	const password = "change-me-secret"

	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"env":  {env: map[string]string{"DATABASE_URL": "postgres://app:" + password + "@127.0.0.1:1/studio?connect_timeout=2"}},
		"flag": {args: []string{"--database-url", "host=127.0.0.1 port=1 user=app password=" + password + " connect_timeout=2"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Nothing listens on port 1, so connecting fails.
			out, err := execute(tc.env, append([]string{"migrate", "status"}, tc.args...)...)
			if err == nil {
				t.Fatalf("migrate status succeeded\n%s", out)
			}

			if strings.Contains(err.Error()+out, password) {
				t.Errorf("output leaks the password: %v\n%s", err, out)
			}
		})
	}
}

func TestMigrateHelp(t *testing.T) {
	t.Parallel()

	out := run(t, "migrate", "--help")

	for _, want := range []string{"up", "down", "status", "--database-url", "DATABASE_URL"} {
		if !strings.Contains(out, want) {
			t.Errorf("migrate --help lacks %q:\n%s", want, out)
		}
	}
}

// migrateRow is one line of `migrate status`.
type migrateRow struct {
	version, state, appliedAt, name string
}

// migrateEmbedded returns the file names of the embedded migrations in
// version order, read from the source directory the binary embeds.
func migrateEmbedded(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join("..", "..", "internal", "store", "postgres", "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	var names []string

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}

	if len(names) < 2 {
		t.Fatalf("found %d migrations, the tests need at least 2", len(names))
	}

	return names
}

// migrateRun runs `migrate <sub>` on the database at databaseURL (from the
// environment) and fails t if it fails.
func migrateRun(t *testing.T, databaseURL, sub string) string {
	t.Helper()

	out, err := execute(map[string]string{"DATABASE_URL": databaseURL}, "migrate", sub)
	if err != nil {
		t.Fatalf("migrate %s: %v\n%s", sub, err, out)
	}

	return out
}

// migrateStatusRows runs `migrate status` and parses its table.
func migrateStatusRows(t *testing.T, databaseURL string) []migrateRow {
	t.Helper()

	lines := strings.Split(strings.TrimRight(migrateRun(t, databaseURL, "status"), "\n"), "\n")
	if got := strings.Fields(lines[0]); !slices.Equal(got, []string{"VERSION", "STATE", "APPLIED", "AT", "NAME"}) {
		t.Fatalf("status header = %q", lines[0])
	}

	rows := make([]migrateRow, 0, len(lines)-1)

	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("status line %q: want 4 columns", line)
		}

		rows = append(rows, migrateRow{version: f[0], state: f[1], appliedAt: f[2], name: f[3]})
	}

	return rows
}

// migrateCheckStatus checks that the status lists every embedded migration
// in order, the first applied ones applied (with an RFC 3339 time) and the
// rest pending.
func migrateCheckStatus(t *testing.T, rows []migrateRow, embedded []string, applied int) {
	t.Helper()

	if len(rows) != len(embedded) {
		t.Fatalf("status lists %d migrations, want %d: %v", len(rows), len(embedded), rows)
	}

	for i, r := range rows {
		if r.name != embedded[i] {
			t.Errorf("row %d names %s, want %s", i, r.name, embedded[i])
		}

		if want := strings.TrimLeft(strings.SplitN(embedded[i], "_", 2)[0], "0"); r.version != want {
			t.Errorf("%s: version %s, want %s", r.name, r.version, want)
		}

		switch {
		case i < applied:
			if r.state != "applied" {
				t.Errorf("%s: state %s, want applied", r.name, r.state)
			}

			if _, err := time.Parse(time.RFC3339, r.appliedAt); err != nil {
				t.Errorf("%s: applied at %q is not RFC 3339: %v", r.name, r.appliedAt, err)
			}
		case r.state != "pending" || r.appliedAt != "-":
			t.Errorf("%s: %s at %s, want pending at -", r.name, r.state, r.appliedAt)
		}
	}
}

// migrateResultLine matches a line of migrate up or down: the direction,
// the migration and how long it took.
var migrateResultLine = regexp.MustCompile(`^(up  |down) (\S+\.sql) \(\S+\)$`)

// migrateResults parses the output of migrate up or down into the
// migrations it names, checking that each line is in direction dir.
func migrateResults(t *testing.T, out, dir string) []string {
	t.Helper()

	var names []string

	for line := range strings.Lines(out) {
		m := migrateResultLine.FindStringSubmatch(strings.TrimRight(line, "\n"))
		if m == nil {
			t.Fatalf("unexpected output line %q in\n%s", line, out)
		}

		if got := strings.TrimSpace(m[1]); got != dir {
			t.Errorf("line %q: direction %s, want %s", line, got, dir)
		}

		names = append(names, m[2])
	}

	return names
}

// TestMigrateUpAndStatus applies every migration to a new database, then
// checks that status lists them as applied and that up has nothing left.
func TestMigrateUpAndStatus(t *testing.T) {
	t.Parallel()

	embedded := migrateEmbedded(t)
	url := newUnmigratedDatabase(t)

	migrateCheckStatus(t, migrateStatusRows(t, url), embedded, 0)

	if got := migrateResults(t, migrateRun(t, url, "up"), "up"); !slices.Equal(got, embedded) {
		t.Errorf("migrate up applied %v, want %v", got, embedded)
	}

	migrateCheckStatus(t, migrateStatusRows(t, url), embedded, len(embedded))

	if out := migrateRun(t, url, "up"); out != "schema is up to date\n" {
		t.Errorf("second migrate up printed %q, want the up-to-date note", out)
	}

	// The schema is exactly the binary's, so the server's check passes.
	if out, err := execute(nil, "seed", "--database-url", url); err != nil {
		t.Errorf("seed after migrate up: %v\n%s", err, out)
	}
}

// TestMigrateDown rolls back the newest migration, one at a time, and
// re-applies exactly the rolled back ones. The database comes from the flag.
func TestMigrateDown(t *testing.T) {
	t.Parallel()

	embedded := migrateEmbedded(t)
	n := len(embedded)
	url := newTestDatabase(t)

	down := func() []string {
		t.Helper()

		out, err := execute(nil, "migrate", "down", "--database-url", url)
		if err != nil {
			t.Fatalf("migrate down: %v\n%s", err, out)
		}

		return migrateResults(t, out, "down")
	}

	if got := down(); !slices.Equal(got, embedded[n-1:]) {
		t.Fatalf("first migrate down rolled back %v, want %v", got, embedded[n-1:])
	}

	migrateCheckStatus(t, migrateStatusRows(t, url), embedded, n-1)

	if got := down(); !slices.Equal(got, embedded[n-2:n-1]) {
		t.Fatalf("second migrate down rolled back %v, want %v", got, embedded[n-2:n-1])
	}

	migrateCheckStatus(t, migrateStatusRows(t, url), embedded, n-2)

	if got := migrateResults(t, migrateRun(t, url, "up"), "up"); !slices.Equal(got, embedded[n-2:]) {
		t.Errorf("migrate up applied %v, want %v", got, embedded[n-2:])
	}

	migrateCheckStatus(t, migrateStatusRows(t, url), embedded, n)
}

// TestMigrateDownWithoutMigrations: on a database without any migration,
// down has nothing to roll back.
func TestMigrateDownWithoutMigrations(t *testing.T) {
	t.Parallel()

	url := newUnmigratedDatabase(t)

	out, err := execute(map[string]string{"DATABASE_URL": url}, "migrate", "down")
	if !errors.Is(err, postgres.ErrNoMigrationToRollBack) {
		t.Fatalf("migrate down error = %v, want ErrNoMigrationToRollBack\n%s", err, out)
	}

	if out != "" {
		t.Errorf("migrate down printed %q", out)
	}

	migrateCheckStatus(t, migrateStatusRows(t, url), migrateEmbedded(t), 0)
}

// migrateFailNewest rolls back the two newest migrations of a new database,
// breaks the newest one (00028 creates the table job_runs, which then
// exists already) and runs migrate up, which must fail. It returns the
// database, the embedded migrations, and the output and error of up.
func migrateFailNewest(t *testing.T) (url string, embedded []string, out string, err error) {
	t.Helper()

	embedded = migrateEmbedded(t)
	if last := embedded[len(embedded)-1]; last != "00028_job_runs.sql" {
		t.Fatalf("the newest migration is %s; migrateFailNewest breaks 00028 on purpose and needs updating", last)
	}

	url = newTestDatabase(t)
	migrateRun(t, url, "down")
	migrateRun(t, url, "down")
	execSQL(t, url, "CREATE TABLE job_runs (id uuid PRIMARY KEY)")

	out, err = execute(map[string]string{"DATABASE_URL": url}, "migrate", "up")
	if err == nil {
		t.Fatalf("migrate up succeeded\n%s", out)
	}

	return url, embedded, out, err
}

// TestMigrateUpFailedMigration: when a migration fails, up names it, keeps
// the ones applied before it, and leaves the failed one pending (its
// transaction is rolled back).
func TestMigrateUpFailedMigration(t *testing.T) {
	t.Parallel()

	url, embedded, out, err := migrateFailNewest(t)
	n := len(embedded)

	if !strings.Contains(err.Error(), embedded[n-1]) {
		t.Errorf("error %q does not name %s", err, embedded[n-1])
	}

	if strings.Contains(out, embedded[n-1]) || strings.Contains(out, "up to date") {
		t.Errorf("migrate up output claims success: %q", out)
	}

	migrateCheckStatus(t, migrateStatusRows(t, url), embedded, n-1)
}

// TestMigrateUpListsMigrationsAppliedBeforeFailure pins that when a migration
// fails, up still lists the migrations it applied (and committed) before it:
// goose's Provider.Up returns them only in the *goose.PartialError, which
// postgres.Migrator.Up reads.
func TestMigrateUpListsMigrationsAppliedBeforeFailure(t *testing.T) {
	t.Parallel()

	_, embedded, out, _ := migrateFailNewest(t)
	n := len(embedded)

	if got := migrateResults(t, out, "up"); !slices.Equal(got, embedded[n-2:n-1]) {
		t.Errorf("migrate up reported %v as applied, want %v", got, embedded[n-2:n-1])
	}
}

// TestMigrateNewerSchema pins what the commands do on a database that has a
// migration this binary does not embed (e.g. after going back to an older
// binary): status lists only the embedded ones, up finds nothing to do,
// down refuses to roll back what it does not know, and the server's check
// (here through seed) refuses the schema.
func TestMigrateNewerSchema(t *testing.T) {
	t.Parallel()

	embedded := migrateEmbedded(t)
	url := newTestDatabase(t)
	execSQL(t, url, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999, true)")

	migrateCheckStatus(t, migrateStatusRows(t, url), embedded, len(embedded))

	if out := migrateRun(t, url, "up"); out != "schema is up to date\n" {
		t.Errorf("migrate up printed %q, want the up-to-date note", out)
	}

	out, err := execute(map[string]string{"DATABASE_URL": url}, "migrate", "down")
	if err == nil || !strings.Contains(err.Error(), "migrate down") {
		t.Errorf("migrate down error = %v, want a refusal\n%s", err, out)
	}

	// Nothing was rolled back.
	migrateCheckStatus(t, migrateStatusRows(t, url), embedded, len(embedded))

	if out, err := execute(map[string]string{"DATABASE_URL": url}, "seed"); !errors.Is(err, errSchemaNewer) {
		t.Errorf("seed error = %v, want errSchemaNewer\n%s", err, out)
	}
}

// TestMigrateUpRefusesGap pins that up does not apply a migration missing
// below the newest applied one (an out-of-order gap): it names it and
// applies nothing, although the server's check calls the schema outdated
// and points to migrate up.
func TestMigrateUpRefusesGap(t *testing.T) {
	t.Parallel()

	embedded := migrateEmbedded(t)
	url := newTestDatabase(t)
	execSQL(t, url, "DELETE FROM goose_db_version WHERE version_id = 5")

	out, err := execute(map[string]string{"DATABASE_URL": url}, "migrate", "up")
	if err == nil || !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "version 5") {
		t.Errorf("migrate up error = %v, want one naming the missing version 5\n%s", err, out)
	}

	if out != "" {
		t.Errorf("migrate up printed %q", out)
	}

	rows := migrateStatusRows(t, url)
	if len(rows) != len(embedded) || rows[4].version != "5" || rows[4].state != "pending" {
		t.Fatalf("status = %v, want version 5 pending", rows)
	}

	if out, err := execute(map[string]string{"DATABASE_URL": url}, "seed"); !errors.Is(err, errSchemaOutdated) {
		t.Errorf("seed error = %v, want errSchemaOutdated\n%s", err, out)
	}
}

// TestMigrateRejectsUnparsableDatabaseURL: a URL that passes the syntax
// check but not the driver's fails before connecting.
func TestMigrateRejectsUnparsableDatabaseURL(t *testing.T) {
	t.Parallel()

	for _, sub := range []string{"up", "down", "status"} {
		out, err := execute(nil, "migrate", sub, "--database-url", "postgres://127.0.0.1:1/studio?pool_max_conns=many")
		if err == nil || !strings.Contains(err.Error(), "invalid database URL") {
			t.Errorf("migrate %s error = %v, want an invalid database URL\n%s", sub, err, out)
		}
	}
}
