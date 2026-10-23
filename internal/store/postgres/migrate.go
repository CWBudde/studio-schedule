package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// migrationFiles holds the goose SQL migrations. They are the schema of
// record; sqlc reads them too (see sqlc.yaml).
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

var (
	// ErrNoMigrationToRollBack is returned by Migrator.Down when no
	// migration is applied.
	ErrNoMigrationToRollBack = errors.New("postgres: no migration to roll back")
	// ErrSchemaPending is returned by Migrator.CheckSchema when embedded
	// migrations are not applied.
	ErrSchemaPending = errors.New("postgres: the database schema is not up to date, migrations are pending")
	// ErrSchemaNewer is returned by Migrator.CheckSchema when the database
	// has migrations that the binary does not embed.
	ErrSchemaNewer = errors.New("postgres: the database schema is newer than this binary")
)

// Migrator applies the embedded schema migrations. Concurrent migrators on
// the same database wait for each other (PostgreSQL advisory lock).
type Migrator struct {
	db       *sql.DB
	provider *goose.Provider
}

// MigrationResult describes one applied or rolled back migration.
type MigrationResult struct {
	Version   int64
	Name      string // file name, e.g. "00001_core.sql"
	Direction string // "up" or "down"
	Duration  time.Duration
}

// MigrationStatus describes one known migration.
type MigrationStatus struct {
	Version   int64
	Name      string
	Applied   bool
	AppliedAt time.Time // zero if not applied
}

// NewMigrator returns a migrator for the database at databaseURL. It does not
// connect until it is used. Errors never contain the password.
func NewMigrator(databaseURL string) (*Migrator, error) {
	// Parse like Open, so pool_* parameters are accepted (and dropped)
	// instead of being sent to the server as runtime parameters.
	cfg, err := poolConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	fsys, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}

	db := stdlib.OpenDB(*cfg.ConnConfig)

	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("postgres: migrations: %w", err)
	}

	return &Migrator{db: db, provider: provider}, nil
}

// Close closes the database connections.
func (m *Migrator) Close() error {
	return m.db.Close()
}

// Up applies all pending migrations and returns them in order. It returns no
// results if the schema is up to date.
func (m *Migrator) Up(ctx context.Context) ([]MigrationResult, error) {
	res, err := m.provider.Up(ctx)
	if err != nil {
		return results(res), migrationErr("up", err)
	}

	return results(res), nil
}

// Down rolls back the most recently applied migration. It fails with
// ErrNoMigrationToRollBack if none is applied.
func (m *Migrator) Down(ctx context.Context) (MigrationResult, error) {
	res, err := m.provider.Down(ctx)

	switch {
	case errors.Is(err, goose.ErrNoNextVersion):
		return MigrationResult{}, ErrNoMigrationToRollBack
	case err != nil:
		return MigrationResult{}, migrationErr("down", err)
	}

	return result(res), nil
}

// appliedVersionsQuery lists the applied migrations from goose's version
// table (goose.DefaultTablename).
const appliedVersionsQuery = `SELECT version_id FROM (
		SELECT DISTINCT ON (version_id) version_id, is_applied
		FROM goose_db_version ORDER BY version_id, id DESC
	) v WHERE is_applied AND version_id > 0`

// CheckSchema reports, without writing anything, whether the database has
// exactly the embedded migrations applied. It fails with ErrSchemaPending if
// some are missing (also if the database was never migrated) and with
// ErrSchemaNewer if the database has migrations this binary does not know,
// e.g. after rolling back to an older binary.
func (m *Migrator) CheckSchema(ctx context.Context) error {
	var exists bool

	err := m.db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", goose.DefaultTablename).Scan(&exists)
	if err != nil {
		return fmt.Errorf("postgres: check schema: %w", mapErr(err))
	}

	if !exists {
		return ErrSchemaPending
	}

	// The provider deletes the row of a rolled back migration; the latest
	// row per version decides, in case older tools appended is_applied=false.
	rows, err := m.db.QueryContext(ctx, appliedVersionsQuery)
	if err != nil {
		return fmt.Errorf("postgres: check schema: %w", mapErr(err))
	}

	defer func() { _ = rows.Close() }()

	applied := map[int64]bool{}

	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("postgres: check schema: %w", mapErr(err))
		}

		applied[v] = true
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres: check schema: %w", mapErr(err))
	}

	pending := false

	for _, src := range m.provider.ListSources() {
		if !applied[src.Version] {
			pending = true
		}

		delete(applied, src.Version)
	}

	switch {
	case len(applied) > 0:
		return ErrSchemaNewer
	case pending:
		return ErrSchemaPending
	}

	return nil
}

// Status lists every embedded migration in version order.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	status, err := m.provider.Status(ctx)
	if err != nil {
		return nil, migrationErr("status", err)
	}

	out := make([]MigrationStatus, len(status))
	for i, s := range status {
		out[i] = MigrationStatus{
			Version:   s.Source.Version,
			Name:      path.Base(s.Source.Path),
			Applied:   s.State == goose.StateApplied,
			AppliedAt: s.AppliedAt.UTC(),
		}
	}

	return out, nil
}

// migrationErr hides driver errors like mapErr. goose reports the failed
// migration in a *goose.PartialError, whose message is kept.
func migrationErr(op string, err error) error {
	var partial *goose.PartialError
	if errors.As(err, &partial) && partial.Failed != nil {
		return fmt.Errorf("postgres: migrate %s: %s: %w", op, path.Base(partial.Failed.Source.Path), mapErr(partial.Err))
	}

	return fmt.Errorf("postgres: migrate %s: %w", op, mapErr(err))
}

func results(res []*goose.MigrationResult) []MigrationResult {
	out := make([]MigrationResult, 0, len(res))
	for _, r := range res {
		if r != nil && r.Error == nil {
			out = append(out, result(r))
		}
	}

	return out
}

func result(r *goose.MigrationResult) MigrationResult {
	return MigrationResult{
		Version:   r.Source.Version,
		Name:      path.Base(r.Source.Path),
		Direction: r.Direction,
		Duration:  r.Duration,
	}
}
