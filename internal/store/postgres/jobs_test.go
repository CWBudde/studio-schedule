package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// mustLockJob takes the job lock name on st and fails t unless it got it.
func mustLockJob(t *testing.T, st *Store, name string) func() {
	t.Helper()

	release, ok, err := st.TryLockJob(t.Context(), name)
	if err != nil || !ok {
		t.Fatalf("TryLockJob(%q) = %t, %v; want the lock", name, ok, err)
	}

	return release
}

// wantJobLockTaken fails t unless the job lock name is held elsewhere.
func wantJobLockTaken(t *testing.T, st *Store, name string) {
	t.Helper()

	release, ok, err := st.TryLockJob(t.Context(), name)
	if err != nil || ok || release != nil {
		t.Fatalf("TryLockJob(%q) = %t, %v; want it taken", name, ok, err)
	}
}

// eventuallyLockJob takes the job lock name on st once a connection that
// held it has ended: the server releases the locks of a closed connection
// when its backend exits, a moment after the client is gone.
func eventuallyLockJob(t *testing.T, st *Store, name string) func() {
	t.Helper()

	timeout := time.After(10 * time.Second)

	for {
		release, ok, err := st.TryLockJob(t.Context(), name)
		if err != nil {
			t.Fatalf("TryLockJob(%q): %v", name, err)
		}

		if ok {
			return release
		}

		select {
		case <-timeout:
			t.Fatalf("the lock %q was not freed", name)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// wantNoJobConn fails t if st keeps a connection for job locks, or the
// pool does not get it back. The pool destroys a closed connection in the
// background, so its count may take a moment.
func wantNoJobConn(t *testing.T, st *Store) {
	t.Helper()

	st.jobs.mu.Lock()
	conn, held := st.jobs.conn, st.jobs.held
	st.jobs.mu.Unlock()

	if conn != nil || held != nil {
		t.Errorf("job locks keep connection %p holding %v, want none", conn, held)
	}

	timeout := time.After(10 * time.Second)

	for st.pool.Stat().AcquiredConns() != 0 {
		select {
		case <-timeout:
			t.Fatalf("%d pool connections acquired, want none", st.pool.Stat().AcquiredConns())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// terminateJobConn ends the backend of st's job lock connection from the
// admin pool and waits until it has exited.
func terminateJobConn(t *testing.T, st *Store) {
	t.Helper()

	st.jobs.mu.Lock()
	pid := st.jobs.conn.Conn().PgConn().PID()
	st.jobs.mu.Unlock()

	var done bool

	err := testServer.admin.QueryRow(t.Context(), "SELECT pg_terminate_backend($1, 5000)", pid).Scan(&done)
	if err != nil || !done {
		t.Fatalf("terminate backend %d = %t, %v", pid, done, err)
	}
}

// TestJobLocksAcrossProcesses checks the job locks between two stores on
// one database, as between two servers: each sees the other's locks, and a
// release frees the lock for the other.
func TestJobLocksAcrossProcesses(t *testing.T) {
	t.Parallel()

	dbURL := newTestDatabase(t)
	first, second := openTestStore(t, dbURL), openTestStore(t, dbURL)

	release := mustLockJob(t, first, "deliver-mail")
	wantJobLockTaken(t, second, "deliver-mail")
	wantNoJobConn(t, second) // a refused lock leaves no connection behind

	other := mustLockJob(t, second, "generate-lessons")
	wantJobLockTaken(t, first, "generate-lessons")

	release()
	wantNoJobConn(t, first)

	again := mustLockJob(t, second, "deliver-mail")
	wantJobLockTaken(t, first, "deliver-mail")

	other()
	again()
	wantNoJobConn(t, second)
	mustLockJob(t, first, "deliver-mail")()
}

// TestOpenRefusesAPoolOfOne: the job locks hold a connection beside the
// transactions, so a pool of one connection would starve them.
func TestOpenRefusesAPoolOfOne(t *testing.T) {
	t.Parallel()

	const password = "change-me-secret"

	st, err := Open(t.Context(), "postgres://app:"+password+"@127.0.0.1:1/studio?pool_max_conns=1")
	if err == nil {
		st.Close()
		t.Fatal("Open accepted pool_max_conns=1")
	}

	if !strings.Contains(err.Error(), "pool_max_conns is 1") || strings.Contains(err.Error(), password) {
		t.Errorf("Open: %v", err)
	}
}

// TestJobLocksShareOneConnection checks that the locks of one store share a
// connection, so a pool of two still runs transactions while many jobs hold
// their locks, and that the connection returns to the pool with the last
// release.
func TestJobLocksShareOneConnection(t *testing.T) {
	t.Parallel()

	st, err := Open(t.Context(), withParams(newTestDatabase(t), map[string]string{"pool_max_conns": "2"}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(st.Close)

	var releases []func()
	for _, name := range []string{"announcements", "deliver-mail", "deliver-push", "generate-lessons", "purge-mail"} {
		releases = append(releases, mustLockJob(t, st, name))
	}

	if n := st.pool.Stat().AcquiredConns(); n != 1 {
		t.Errorf("%d connections acquired for five locks, want 1", n)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err = st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.JobRuns().Create(ctx, domain.JobRun{
			ID: ids.New[domain.JobRunID](ids.NewSequence()), Job: "deliver-mail", StartedAt: time.Unix(0, 0),
			EndedAt: time.Unix(0, 0), Result: domain.JobOK,
		})
	})
	if err != nil {
		t.Fatalf("a transaction while five locks are held: %v", err)
	}

	for _, release := range releases {
		release()
	}

	wantNoJobConn(t, st)
}

// TestJobLockEndsWithConnection checks that a lock ends with the
// connection that holds it: the other store gets it, the first store's
// release of it does nothing, and the first store locks again on a new
// connection.
func TestJobLockEndsWithConnection(t *testing.T) {
	t.Parallel()

	t.Run("release first", func(t *testing.T) {
		t.Parallel()

		dbURL := newTestDatabase(t)
		first, second := openTestStore(t, dbURL), openTestStore(t, dbURL)

		stale := mustLockJob(t, first, "deliver-mail")
		terminateJobConn(t, first)
		taken := eventuallyLockJob(t, second, "deliver-mail")

		// The unlock fails on the dead connection, which goes.
		stale()
		wantNoJobConn(t, first)
		wantJobLockTaken(t, first, "deliver-mail")

		mustLockJob(t, first, "generate-lessons")()
		taken()
		mustLockJob(t, first, "deliver-mail")()
	})

	t.Run("lock first", func(t *testing.T) {
		t.Parallel()

		dbURL := newTestDatabase(t)
		first, second := openTestStore(t, dbURL), openTestStore(t, dbURL)

		stale := mustLockJob(t, first, "deliver-mail")
		terminateJobConn(t, first)

		// The lock statement fails on the dead connection: the error is
		// reported, and the connection and its locks go.
		release, ok, err := first.TryLockJob(t.Context(), "generate-lessons")
		if err == nil || ok || release != nil {
			t.Fatalf("TryLockJob on a dead connection = %t, %v; want an error", ok, err)
		}

		noDriverError(t, err)
		wantNoJobConn(t, first)

		taken := eventuallyLockJob(t, second, "deliver-mail")

		// The connection the lock was taken on is gone: the release does
		// nothing, also not to a lock of the name taken again in this
		// process.
		again := mustLockJob(t, first, "generate-lessons")
		stale()
		wantJobLockTaken(t, first, "deliver-mail")
		wantJobLockTaken(t, second, "generate-lessons")

		again()
		taken()
		wantNoJobConn(t, first)
	})
}

// TestJobLockCanceled checks that a canceled caller gets no lock and breaks
// none: the connection that holds the other locks stays.
func TestJobLockCanceled(t *testing.T) {
	t.Parallel()

	dbURL := newTestDatabase(t)
	first, second := openTestStore(t, dbURL), openTestStore(t, dbURL)

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	// Without a lock held, the pool refuses the connection.
	if _, ok, err := first.TryLockJob(canceled, "deliver-mail"); !errors.Is(err, context.Canceled) || ok {
		t.Fatalf("TryLockJob with a canceled context = %t, %v; want context.Canceled", ok, err)
	}

	wantNoJobConn(t, first)

	// With one held, the connection is there, but the statement is not run.
	release := mustLockJob(t, first, "generate-lessons")

	if _, ok, err := first.TryLockJob(canceled, "deliver-mail"); !errors.Is(err, context.Canceled) || ok {
		t.Fatalf("TryLockJob with a canceled context = %t, %v; want context.Canceled", ok, err)
	}

	wantJobLockTaken(t, second, "generate-lessons")
	mustLockJob(t, second, "deliver-mail")()

	release()
	wantNoJobConn(t, first)
	mustLockJob(t, second, "generate-lessons")()
}

// TestCloseEndsJobLocks checks that Close does not wait for a lock still
// held: it ends the lock, whose release then does nothing.
func TestCloseEndsJobLocks(t *testing.T) {
	t.Parallel()

	dbURL := newTestDatabase(t)
	other := openTestStore(t, dbURL)

	st, err := Open(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}

	release := mustLockJob(t, st, "deliver-mail")

	closed := make(chan struct{})

	go func() {
		st.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close waits for the held job lock")
	}

	release()
	eventuallyLockJob(t, other, "deliver-mail")()

	if _, ok, err := st.TryLockJob(t.Context(), "deliver-mail"); err == nil || ok {
		t.Fatalf("TryLockJob on a closed store = %t, %v; want an error", ok, err)
	}
}
