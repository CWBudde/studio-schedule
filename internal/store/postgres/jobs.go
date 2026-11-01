package postgres

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- job locks
//
// A job lock (store.Store.TryLockJob, ADR 0020) is a session-level advisory
// lock: it sits beside the transactions, so the job runs its own InTx calls
// while it holds it, and the server releases it when the connection ends,
// also when the process dies.
//
// One dedicated connection holds all of this process's job locks, not one
// connection per lock: with the pool's default size (max(4, NumCPU)) and
// jobs whose tickers coincide, every job could hold a lock connection while
// it waits for another one for its transaction, and the jobs and every
// request would wait forever. The connection comes from the pool on the
// first TryLockJob while no lock is held, and goes back to it when the last
// lock is released, so an idle store keeps no connection and Close does not
// wait for it.
//
// Advisory locks are re-entrant within a session: the server would grant a
// second lock of a held name to the same connection. So jobLocks.held
// refuses a name this process holds before the server is asked. The mutex
// serializes the statements on the connection, which pgx needs anyway.
//
// When a statement fails, the connection is closed: its state is unknown,
// and with the session the server released every lock it held. held is
// cleared, and a release of a lock taken on that connection does nothing
// (it compares the connection it was taken on), so it never unlocks a lock
// taken again on a new connection.

// jobLockClass is the first key of every job lock, in the two-key space of
// advisory locks (pg_try_advisory_lock(int4, int4)); the second key is
// hashtext of the job name. The two-key space does not overlap the one-key
// space, in which the store's other advisory locks
// (pg_advisory_xact_lock(hashtext('announcement-mail'))) and goose's lock
// live. The value is the ADR's number.
const jobLockClass int32 = 20

// jobLockTimeout bounds a lock or unlock statement. pg_try_advisory_lock
// never waits, so only a broken connection takes this long.
const jobLockTimeout = 5 * time.Second

// jobLocks holds this process's job locks on one connection.
type jobLocks struct {
	mu   sync.Mutex
	conn *pgxpool.Conn       // nil while no lock is held
	held map[string]struct{} // the names locked on conn
}

// TryLockJob implements store.Store. It works inside a transaction's
// callback too, but prefer calling it outside one: there the caller holds a
// pool connection while TryLockJob may wait for the lock connection.
func (s *Store) TryLockJob(ctx context.Context, name string) (release func(), ok bool, err error) {
	l := &s.jobs

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, held := l.held[name]; held {
		return nil, false, nil
	}

	if l.conn == nil {
		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("postgres: job lock: %w", mapErr(err))
		}

		l.conn, l.held = conn, map[string]struct{}{}
	}

	if err := ctx.Err(); err != nil {
		l.releaseIdle()

		return nil, false, fmt.Errorf("postgres: job lock: %w", err)
	}

	// The connection holds the other jobs' locks too: a canceled caller
	// must not break it (pgx closes a connection whose statement was
	// canceled), so the statement gets a context of its own.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), jobLockTimeout)
	defer cancel()

	conn := l.conn

	locked, err := sqlc.New(conn).TryLockJob(sctx, sqlc.TryLockJobParams{Class: jobLockClass, Job: name})
	if err != nil {
		l.drop()

		return nil, false, fmt.Errorf("postgres: job lock: %w", mapErr(err))
	}

	if !locked {
		l.releaseIdle()

		return nil, false, nil
	}

	l.held[name] = struct{}{}

	return sync.OnceFunc(func() { l.unlock(conn, name) }), true, nil
}

// unlock releases the lock of name taken on conn. It runs after the caller's
// context may have ended, so it has a context of its own.
func (l *jobLocks) unlock(conn *pgxpool.Conn, name string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn != conn {
		return // the connection ended, and the server released the lock with it
	}

	ctx, cancel := context.WithTimeout(context.Background(), jobLockTimeout)
	defer cancel()

	if err := sqlc.New(conn).UnlockJob(ctx, sqlc.UnlockJobParams{Class: jobLockClass, Job: name}); err != nil {
		l.drop()

		return
	}

	delete(l.held, name)
	l.releaseIdle()
}

// releaseIdle returns the connection to the pool if it holds no lock.
func (l *jobLocks) releaseIdle() {
	if len(l.held) == 0 {
		l.conn.Release()
		l.conn, l.held = nil, nil
	}
}

// drop closes the connection, which ends every lock it holds, and forgets
// them. Release then destroys the closed connection instead of pooling it.
func (l *jobLocks) drop() {
	ctx, cancel := context.WithTimeout(context.Background(), jobLockTimeout)
	defer cancel()

	_ = l.conn.Conn().Close(ctx) // closing a broken connection may fail; it is gone either way
	l.conn.Release()
	l.conn, l.held = nil, nil
}

// close ends every job lock (Store.Close), so a lock still held does not
// keep the pool waiting for its connection.
func (l *jobLocks) close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn != nil {
		l.drop()
	}
}

// ---------------------------------------------------------------- job runs

type jobRunRepo struct{ q *sqlc.Queries }

func (r jobRunRepo) Create(ctx context.Context, run domain.JobRun) error {
	return mapErr(r.q.CreateJobRun(ctx, sqlc.CreateJobRunParams{
		ID:        pgID(run.ID),
		Job:       run.Job,
		StartedAt: pgInstant(run.StartedAt),
		EndedAt:   pgInstant(run.EndedAt),
		Result:    string(run.Result),
		Error:     pgText(run.Error),
	}))
}

func (r jobRunRepo) Latest(ctx context.Context) ([]domain.JobRun, error) {
	rows, err := r.q.LatestJobRuns(ctx)

	return convertRows(rows, err, func(row sqlc.JobRun) domain.JobRun {
		return domain.JobRun{
			ID:        fromPgID[domain.JobRunID](row.ID),
			Job:       row.Job,
			StartedAt: fromPgInstant(row.StartedAt),
			EndedAt:   fromPgInstant(row.EndedAt),
			Result:    domain.JobRunResult(row.Result),
			Error:     row.Error.String,
		}
	})
}

func (r jobRunRepo) DeleteStartedBefore(ctx context.Context, before time.Time) (int, error) {
	n, err := r.q.DeleteJobRunsStartedBefore(ctx, pgAt(before))

	return int(n), mapErr(err)
}
