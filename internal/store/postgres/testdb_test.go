package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// envTestDatabaseURL names a PostgreSQL server for the tests. The role must
// be allowed to create databases. Without it, the tests start a container.
const envTestDatabaseURL = "STUDIO_TEST_DATABASE_URL"

// testImage is the PostgreSQL image of the test container.
const testImage = "postgres:18-alpine"

// testServer is the PostgreSQL server shared by all tests of this binary.
// Every test gets its own database, copied from a migrated template.
var testServer struct {
	url      string        // connection URL of the maintenance database
	admin    *pgxpool.Pool // creates and drops the test databases
	template string        // migrated template database
	prefix   string        // unique per test binary, so binaries can share a server
	next     atomic.Int64
	err      error // set if the server could not be prepared; tests then fail
}

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	cleanup, err := startTestServer(ctx)
	if err != nil {
		// Tests that need the server fail with this error; ADR 0002 forbids
		// skipping them.
		testServer.err = err
	}

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Printf("postgres tests: cleanup: %v", err)

		if code == 0 {
			code = 1
		}
	}

	return code
}

// startTestServer connects to the server from envTestDatabaseURL or starts a
// container, then creates the template database. The returned cleanup is
// never nil.
func startTestServer(ctx context.Context) (func() error, error) {
	var cleanups []func() error

	cleanup := func() error {
		var errs []error
		for i := len(cleanups) - 1; i >= 0; i-- {
			errs = append(errs, cleanups[i]())
		}

		return errors.Join(errs...)
	}

	serverURL := os.Getenv(envTestDatabaseURL)
	if serverURL == "" {
		ctr, err := tcpostgres.Run(ctx, testImage,
			tcpostgres.WithDatabase("postgres"),
			tcpostgres.WithUsername("postgres"),
			tcpostgres.WithPassword("test"),
			tcpostgres.BasicWaitStrategies(),
			// Durability is pointless for throwaway data; a RAM disk and no
			// fsync make CREATE DATABASE and commits cheap.
			testcontainers.WithTmpfs(map[string]string{"/var/lib/postgresql": "rw"}),
			testcontainers.WithCmd("postgres",
				"-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off",
				"-c", "max_connections=300"),
		)
		if ctr != nil {
			cleanups = append(cleanups, func() error { return testcontainers.TerminateContainer(ctr) })
		}

		if err != nil {
			return cleanup, fmt.Errorf("start %s container (is Docker running? or set %s): %w", testImage, envTestDatabaseURL, err)
		}

		serverURL, err = ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return cleanup, err
		}
	}

	admin, err := pgxpool.New(ctx, withParams(serverURL, map[string]string{"pool_max_conns": "4"}))
	if err != nil {
		return cleanup, fmt.Errorf("connect to the test server: %w", err)
	}

	cleanups = append(cleanups, func() error { admin.Close(); return nil })

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return cleanup, err
	}

	testServer.url = serverURL
	testServer.admin = admin
	testServer.prefix = "studio_test_" + hex.EncodeToString(suffix[:])
	testServer.template = testServer.prefix + "_template"

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{testServer.template}.Sanitize()); err != nil {
		return cleanup, fmt.Errorf("create template database: %w", err)
	}

	cleanups = append(cleanups, func() error { return dropDatabase(context.Background(), testServer.template) })

	m, err := NewMigrator(withDatabase(serverURL, testServer.template))
	if err != nil {
		return cleanup, err
	}

	_, err = m.Up(ctx)

	// The template must have no open connections when it is copied.
	if cerr := m.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		return cleanup, fmt.Errorf("migrate template database: %w", err)
	}

	return cleanup, nil
}

// newTestDatabase creates an empty, migrated database for t and returns its
// URL. The database is dropped when t ends.
func newTestDatabase(t *testing.T) string {
	t.Helper()

	if testServer.err != nil {
		t.Fatalf("no PostgreSQL test server: %v", testServer.err)
	}

	name := fmt.Sprintf("%s_%d", testServer.prefix, testServer.next.Add(1))

	_, err := testServer.admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+
		" TEMPLATE "+pgx.Identifier{testServer.template}.Sanitize())
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}

	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), name); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})

	return withDatabase(testServer.url, name)
}

// newTestStore returns a Store on a fresh database. Its pool is small: the
// contract suite runs many stores in parallel.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	st, err := Open(t.Context(), withParams(newTestDatabase(t), map[string]string{"pool_max_conns": "4"}))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Registered after the database cleanup, so it runs before it.
	t.Cleanup(st.Close)

	return st
}

func dropDatabase(ctx context.Context, name string) error {
	_, err := testServer.admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")

	return err
}

// withDatabase returns the connection string dsn pointing at database name.
func withDatabase(dsn, name string) string {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		u.Path = "/" + name

		return u.String()
	}

	// key=value DSN: a later keyword overrides an earlier one.
	return dsn + " dbname=" + name
}

// withParams adds connection parameters to dsn.
func withParams(dsn string, params map[string]string) string {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}

		u.RawQuery = q.Encode()

		return u.String()
	}

	var b strings.Builder

	b.WriteString(dsn)

	for k, v := range params {
		fmt.Fprintf(&b, " %s=%s", k, v)
	}

	return b.String()
}
