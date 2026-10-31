package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/cwbudde/studio-schedule/internal/store/postgres"
)

// The commands run against a real PostgreSQL server, like the store tests
// in internal/store/postgres (whose helper is not importable): the server
// from envTestDatabaseURL, or a container. It starts on first use, so the
// tests that need no database never wait for Docker.

// envTestDatabaseURL names a PostgreSQL server for the tests. The role must
// be allowed to create databases. Without it, the tests start a container.
const envTestDatabaseURL = "STUDIO_TEST_DATABASE_URL"

// testImage is the PostgreSQL image of the test container.
const testImage = "postgres:18-alpine"

// testServer is the PostgreSQL server shared by the tests of this binary.
// Every test gets its own database, copied from a migrated template.
var testServer struct {
	once     sync.Once
	url      string        // connection URL of the maintenance database
	admin    *pgxpool.Pool // creates and drops the test databases
	template string        // migrated template database
	prefix   string        // unique per test binary, so binaries can share a server
	next     atomic.Int64
	err      error // set if the server could not be prepared; tests then fail

	mu       sync.Mutex
	cleanups []func() error
}

func TestMain(m *testing.M) {
	code := m.Run()

	if err := stopTestServer(); err != nil {
		log.Printf("cmd tests: cleanup: %v", err)

		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}

// addCleanup registers fn to run when the test binary ends.
func addCleanup(fn func() error) {
	testServer.mu.Lock()
	defer testServer.mu.Unlock()

	testServer.cleanups = append(testServer.cleanups, fn)
}

func stopTestServer() error {
	testServer.mu.Lock()
	defer testServer.mu.Unlock()

	var errs []error
	for _, fn := range slices.Backward(testServer.cleanups) {
		errs = append(errs, fn())
	}

	return errors.Join(errs...)
}

// startTestServer connects to the server from envTestDatabaseURL or starts a
// container, then creates the migrated template database.
func startTestServer(ctx context.Context) error {
	serverURL := os.Getenv(envTestDatabaseURL)
	if serverURL == "" {
		ctr, err := tcpostgres.Run(ctx, testImage,
			tcpostgres.WithDatabase("postgres"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("test"),
			tcpostgres.BasicWaitStrategies(),
			// Durability is pointless for throwaway data.
			testcontainers.WithTmpfs(map[string]string{"/var/lib/postgresql": "rw"}),
			testcontainers.WithCmd("postgres",
				"-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off"),
		)
		if ctr != nil {
			addCleanup(func() error { return testcontainers.TerminateContainer(ctr) })
		}

		if err != nil {
			return fmt.Errorf("start %s container (is Docker running? or set %s): %w", testImage, envTestDatabaseURL, err)
		}

		serverURL, err = ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return err
		}
	}

	admin, err := pgxpool.New(ctx, withParams(serverURL, map[string]string{"pool_max_conns": "4"}))
	if err != nil {
		return fmt.Errorf("connect to the test server: %w", err)
	}

	addCleanup(func() error { admin.Close(); return nil })

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}

	testServer.url = serverURL
	testServer.admin = admin
	testServer.prefix = "studio_cmd_test_" + hex.EncodeToString(suffix[:])
	testServer.template = testServer.prefix + "_template"

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{testServer.template}.Sanitize()); err != nil {
		return fmt.Errorf("create template database: %w", err)
	}

	addCleanup(func() error { return dropDatabase(context.Background(), testServer.template) })

	m, err := postgres.NewMigrator(withDatabase(serverURL, testServer.template))
	if err != nil {
		return err
	}

	_, err = m.Up(ctx)

	// The template must have no open connections when it is copied.
	if cerr := m.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		return fmt.Errorf("migrate template database: %w", err)
	}

	return nil
}

// newTestDatabase creates an empty database with every migration applied
// for t and returns its URL. The database is dropped when t ends.
func newTestDatabase(t *testing.T) string {
	t.Helper()

	return createTestDatabase(t, true)
}

// newUnmigratedDatabase creates an empty database without any migration
// (not even goose's version table) for t and returns its URL.
func newUnmigratedDatabase(t *testing.T) string {
	t.Helper()

	return createTestDatabase(t, false)
}

func createTestDatabase(t *testing.T, migrated bool) string {
	t.Helper()

	testServer.once.Do(func() { testServer.err = startTestServer(context.Background()) })

	if testServer.err != nil {
		// ADR 0002: tests that need the database fail rather than skip.
		t.Fatalf("no PostgreSQL test server: %v", testServer.err)
	}

	name := fmt.Sprintf("%s_%d", testServer.prefix, testServer.next.Add(1))

	stmt := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if migrated {
		stmt += " TEMPLATE " + pgx.Identifier{testServer.template}.Sanitize()
	}

	if _, err := testServer.admin.Exec(t.Context(), stmt); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), name); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})

	return withDatabase(testServer.url, name)
}

// execSQL runs a statement on the database at databaseURL, e.g. to damage
// a schema for a startup refusal.
func execSQL(t *testing.T, databaseURL, sql string, args ...any) {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	defer func() { _ = conn.Close(context.Background()) }()

	if _, err := conn.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// openTestStore opens the store on the migrated database at databaseURL;
// it is closed when t ends, before the database is dropped.
func openTestStore(t *testing.T, databaseURL string) *postgres.Store {
	t.Helper()

	st, err := postgres.Open(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	t.Cleanup(st.Close)

	return st
}

func dropDatabase(ctx context.Context, name string) error {
	_, err := testServer.admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")

	return err
}

// withDatabase returns the connection URL dsn pointing at database name.
func withDatabase(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		panic(fmt.Sprintf("test server URL: %v", err))
	}

	u.Path = "/" + name

	return u.String()
}

// withParams adds connection parameters to the URL dsn.
func withParams(dsn string, params map[string]string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		panic(fmt.Sprintf("test server URL: %v", err))
	}

	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}

	u.RawQuery = q.Encode()

	return u.String()
}
