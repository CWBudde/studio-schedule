package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestNewMigratorInvalidURL checks that a URL that does not parse fails
// NewMigrator without the password in the error.
func TestNewMigratorInvalidURL(t *testing.T) {
	t.Parallel()

	const password = "change-me-secret"

	for _, dsn := range []string{
		"postgres://app:" + password + "@db.example.test:no-port/studio",
		"postgres://app:" + password + "@db.example.test/studio?pool_max_conns=0",
	} {
		m, err := NewMigrator(dsn)
		if err == nil {
			_ = m.Close()

			t.Fatalf("NewMigrator(%q) succeeded", dsn)
		}

		if strings.Contains(err.Error(), password) {
			t.Errorf("NewMigrator error leaks the password: %v", err)
		}

		noDriverError(t, err)
	}
}

// TestUpFailsOnExistingTable checks that a migration that fails reports the
// file it failed in, without the driver error, and records nothing as
// applied.
func TestUpFailsOnExistingTable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m, err := NewMigrator(newUnmigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	// The first migration creates this table.
	if _, err := m.db.ExecContext(ctx, `CREATE TABLE settings (id integer)`); err != nil {
		t.Fatal(err)
	}

	res, err := m.Up(ctx)
	if err == nil {
		t.Fatal("Up succeeded")
	}

	if !strings.Contains(err.Error(), "00001_core.sql") {
		t.Errorf("Up = %v, want the failed migration named", err)
	}

	noDriverError(t, err)

	if len(res) != 0 {
		t.Errorf("Up reports %+v as applied", res)
	}

	if err := m.CheckSchema(ctx); !errors.Is(err, ErrSchemaPending) {
		t.Errorf("CheckSchema after the failed Up = %v, want ErrSchemaPending", err)
	}
}

// TestDownCanceled checks that a Down that cannot even start is an error,
// not ErrNoMigrationToRollBack, and rolls nothing back.
func TestDownCanceled(t *testing.T) {
	t.Parallel()

	m, err := NewMigrator(newTestDatabase(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = m.Down(canceled)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrNoMigrationToRollBack) {
		t.Fatalf("Down = %v, want context.Canceled", err)
	}

	noDriverError(t, err)

	if err := m.CheckSchema(t.Context()); err != nil {
		t.Errorf("CheckSchema after the canceled Down = %v, want nil", err)
	}
}

// TestCheckSchemaBrokenVersionTable checks CheckSchema and Ready on a goose
// version table they cannot read: the result is an error, but neither
// "pending" nor "newer", which would misreport the schema.
func TestCheckSchemaBrokenVersionTable(t *testing.T) {
	t.Parallel()

	for name, ddl := range map[string]string{
		// The query of the applied versions fails.
		"no is_applied column": `CREATE TABLE goose_db_version (id integer, version_id bigint)`,
		// A row does not scan into an int64.
		"version too big": `CREATE TABLE goose_db_version (id integer, version_id numeric, is_applied boolean);
			INSERT INTO goose_db_version VALUES (1, 1, true), (2, 99999999999999999999, true)`,
		// The query fails before it returns a row.
		"failing query": `CREATE TABLE goose_rows (id integer);
			INSERT INTO goose_rows VALUES (1), (2), (3);
			CREATE VIEW goose_db_version AS
				SELECT id, (6 / (id - 2))::bigint AS version_id, true AS is_applied FROM goose_rows`,
		// The query fails after it returned rows: the view sorts before it
		// calls the volatile function, which fails on the last row.
		"failing row": `CREATE TABLE goose_rows (id integer, version_id bigint);
			INSERT INTO goose_rows VALUES (1, 1), (2, 2), (3, 3);
			CREATE FUNCTION goose_applied(id integer) RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
			BEGIN
				IF id = 3 THEN
					RAISE EXCEPTION 'unreadable row';
				END IF;

				RETURN true;
			END $$;
			CREATE VIEW goose_db_version AS SELECT id, version_id, goose_applied(id) AS is_applied
				FROM goose_rows ORDER BY version_id, id DESC`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			dbURL := newUnmigratedDatabase(t)

			m, err := NewMigrator(dbURL)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = m.Close() })

			if _, err := m.db.ExecContext(ctx, ddl); err != nil {
				t.Fatal(err)
			}

			st, err := Open(ctx, withParams(dbURL, map[string]string{"pool_max_conns": "2"}))
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(st.Close)

			for check, err := range map[string]error{"CheckSchema": m.CheckSchema(ctx), "Ready": st.Ready(ctx)} {
				if err == nil || errors.Is(err, ErrSchemaPending) || errors.Is(err, ErrSchemaNewer) {
					t.Errorf("%s = %v, want an error reading the version table", check, err)
				}

				noDriverError(t, err)
			}
		})
	}
}
