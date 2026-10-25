package postgres

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/app"
)

// TestReady checks Store.Ready, and that /readyz of an app wired to it as
// the server wires it answers 503 while the schema is not current.
func TestReady(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dbURL := newTestDatabase(t)

	st, err := Open(ctx, withParams(dbURL, map[string]string{"pool_max_conns": "2"}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(st.Close)

	m, err := NewMigrator(dbURL)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	// Every probe a minute later: past the cached result of the one before.
	h, _ := app.New(app.Deps{Store: st, Ready: st.Ready, Clock: &tickClock{}})
	want := func(name string, wantErr error, wantStatus int) {
		t.Helper()

		if err := st.Ready(ctx); !errors.Is(err, wantErr) || (wantErr == nil && err != nil) {
			t.Errorf("%s: Ready = %v, want %v", name, err, wantErr)
		}

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/readyz", nil))

		if rec.Code != wantStatus {
			t.Errorf("%s: /readyz = %d, want %d; body %s", name, rec.Code, wantStatus, rec.Body)
		}
	}

	want("migrated", nil, http.StatusOK)

	if _, err := m.Down(ctx); err != nil {
		t.Fatal(err)
	}

	want("pending migration", ErrSchemaPending, http.StatusServiceUnavailable)

	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}

	want("migrated again", nil, http.StatusOK)

	if _, err := st.pool.Exec(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999, true)"); err != nil {
		t.Fatal(err)
	}

	want("unknown migration", ErrSchemaNewer, http.StatusServiceUnavailable)
}

// TestReadyWithoutMigrations checks that a database without goose's table
// is not ready.
func TestReadyWithoutMigrations(t *testing.T) {
	t.Parallel()

	st, err := Open(t.Context(), newUnmigratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(st.Close)

	if err := st.Ready(t.Context()); !errors.Is(err, ErrSchemaPending) {
		t.Fatalf("Ready = %v, want ErrSchemaPending", err)
	}
}

// tickClock is a clock a minute later on every reading.
type tickClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *tickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(time.Minute)

	return c.t
}
