// Package postgres implements store.Store on PostgreSQL with pgx and
// sqlc-generated queries (internal/sqlc, from queries/ and migrations/).
//
// Transactions run at READ COMMITTED. Lessons().Lock takes a row lock
// (SELECT … FOR UPDATE); everything else relies on the constraints of the
// schema. InTx retries the whole callback after serialization failures and
// deadlocks (SQLSTATE 40001/40P01), so callbacks must not have side effects
// outside the transaction.
//
// Driver errors never leave the package: they are mapped to domain and store
// sentinels by error code (see errors.go).
//
// The schema is managed with goose migrations embedded in the binary (see
// Migrator); Open expects an up-to-date schema.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// Store is a store.Store on a PostgreSQL connection pool. Create one with
// Open and release it with Close.
type Store struct {
	pool  *pgxpool.Pool
	retry retryPolicy

	// sqlDB is pool as a *sql.DB, and migrations the versions of the
	// embedded migrations, both for Ready.
	sqlDB      *sql.DB
	migrations []int64
}

var _ store.Store = (*Store)(nil)

// retryPolicy bounds the retries of InTx: at most attempts runs, waiting
// backoff·n after the n-th failure.
type retryPolicy struct {
	attempts int
	backoff  time.Duration
}

var defaultRetry = retryPolicy{attempts: 5, backoff: 10 * time.Millisecond}

// rollbackTimeout bounds the rollback after a failed or canceled callback.
const rollbackTimeout = 5 * time.Second

// Open connects to the database at databaseURL (a postgres:// URL or a
// key=value DSN; pool_* parameters configure the pool) and checks the
// connection. Errors never contain the password.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := poolConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", mapErr(err))
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("postgres: connect: %w", mapErr(err))
	}

	// Closing sqlDB leaves the pool open.
	sqlDB := stdlib.OpenDBFromPool(pool)

	provider, err := newProvider(sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		pool.Close()

		return nil, err
	}

	return &Store{pool: pool, retry: defaultRetry, sqlDB: sqlDB, migrations: embeddedVersions(provider)}, nil
}

// poolConfig parses databaseURL. Unless the URL sets default_query_exec_mode,
// queries use QueryExecModeCacheDescribe: it caches only the result
// descriptions, not server-side prepared statements, so a running server
// survives migrations without "cached plan must not change result type".
func poolConfig(databaseURL string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// pgconn redacts the password in its parse errors.
		return nil, fmt.Errorf("postgres: invalid database URL: %w", mapErr(err))
	}

	// pgx.ParseConfig removes the parameter after reading it, so look it up
	// in the raw connection parameters.
	raw, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: invalid database URL: %w", mapErr(err))
	}

	if _, ok := raw.RuntimeParams["default_query_exec_mode"]; !ok {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	}

	return cfg, nil
}

// Close closes all connections. Transactions in progress fail.
func (s *Store) Close() {
	_ = s.sqlDB.Close()
	s.pool.Close()
}

// txKey marks the context of a transaction callback.
type txKey struct{}

// InTx implements store.Store.
func (s *Store) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	if ctx.Value(txKey{}) == s {
		return store.ErrNestedTx
	}

	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := s.runTx(ctx, mode, fn)
		if err == nil || !retryable(err) {
			return err
		}

		if attempt >= s.retry.attempts {
			return lostRace(err, attempt)
		}

		if err := sleep(ctx, s.retry.backoff*time.Duration(attempt)); err != nil {
			return err
		}
	}
}

// runTx runs fn once in a new transaction.
func (s *Store) runTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	opts := pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite}
	if mode != store.ReadWrite {
		opts.AccessMode = pgx.ReadOnly
	}

	ptx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", mapErr(err))
	}

	// Rolls back unless committed, also when fn panics. The rollback must
	// run even if ctx is canceled; its error is dropped, so it never hides
	// the error of fn (pgx closes a connection whose rollback failed).
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()

		_ = ptx.Rollback(rctx)
	}()

	t := &tx{q: sqlc.New(ptx)}
	if err := fn(context.WithValue(ctx, txKey{}, s), t); err != nil {
		return err
	}

	// A canceled request must not commit, even if fn ignored the context.
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := ptx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit: %w", mapErr(err))
	}

	for _, f := range t.afterCommit {
		f()
	}

	return nil
}

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// tx implements store.Tx; its repositories share one pgx transaction.
type tx struct {
	q *sqlc.Queries
	// afterCommit runs once the transaction committed (Tx.AfterCommit); a
	// retried run starts with a new tx, so a failed run's functions never
	// run.
	afterCommit []func()
}

func (t *tx) AfterCommit(fn func()) { t.afterCommit = append(t.afterCommit, fn) }

func (t *tx) Settings() store.SettingsRepo      { return settingsRepo{t.q} }
func (t *tx) Persons() store.PersonRepo         { return personRepo{t.q} }
func (t *tx) Accounts() store.AccountRepo       { return accountRepo{t.q} }
func (t *tx) Credentials() store.CredentialRepo { return credentialRepo{t.q} }
func (t *tx) Sessions() store.SessionRepo       { return sessionRepo{t.q} }
func (t *tx) AccountTokens() store.AccountTokenRepo {
	return accountTokenRepo{t.q}
}
func (t *tx) Locations() store.LocationRepo     { return locationRepo{t.q} }
func (t *tx) Rooms() store.RoomRepo             { return roomRepo{t.q} }
func (t *tx) Staff() store.StaffRepo            { return staffRepo{t.q} }
func (t *tx) CourseTypes() store.CourseTypeRepo { return courseTypeRepo{t.q} }
func (t *tx) Terms() store.TermRepo             { return termRepo{t.q} }
func (t *tx) Classes() store.ClassRepo          { return classRepo{t.q} }
func (t *tx) Lessons() store.LessonRepo         { return lessonRepo{t.q} }
func (t *tx) Bookings() store.BookingRepo       { return bookingRepo{t.q} }
func (t *tx) Waitlist() store.WaitlistRepo      { return waitlistRepo{t.q} }
func (t *tx) Notices() store.LessonNoticeRepo   { return noticeRepo{t.q} }
func (t *tx) CalendarFeeds() store.CalendarFeedRepo {
	return calendarFeedRepo{t.q}
}
func (t *tx) PassTypes() store.PassTypeRepo { return passTypeRepo{t.q} }
func (t *tx) Passes() store.PassRepo        { return passRepo{t.q} }
func (t *tx) Redemptions() store.PassRedemptionRepo {
	return redemptionRepo{t.q}
}
func (t *tx) Receipts() store.ReceiptRepo { return receiptRepo{t.q} }
func (t *tx) ReceiptCounters() store.ReceiptCounterRepo {
	return receiptCounterRepo{t.q}
}
func (t *tx) Outbox() store.OutboxRepo { return outboxRepo{t.q} }
func (t *tx) Audit() store.AuditRepo   { return auditRepo{t.q} }
func (t *tx) Announcements() store.AnnouncementRepo {
	return announcementRepo{t.q}
}
func (t *tx) Imports() store.ImportRunRepo { return importRunRepo{t.q} }
func (t *tx) DuplicateDismissals() store.DuplicateDismissalRepo {
	return dismissalRepo{t.q}
}

func (t *tx) PushSubscriptions() store.PushSubscriptionRepo {
	return pushSubscriptionRepo{t.q}
}
func (t *tx) PushOutbox() store.PushOutboxRepo { return pushOutboxRepo{t.q} }
