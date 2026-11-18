package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// TestSetupRefusesConcurrentSettingsInsert holds an uncommitted settings row
// like another installer would. Init must leave it intact when the other
// installer commits, even though the row was invisible when Init started.
func TestSetupRefusesConcurrentSettingsInsert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	st := newTestStore(t)
	first, err := st.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = first.Rollback(t.Context()) }()

	var firstPID int32
	if err := first.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&firstPID); err != nil {
		t.Fatal(err)
	}

	_, err = first.Exec(ctx, `INSERT INTO settings (
		school_name, timezone, default_locale, currency, receipt_prefix, release_weekday,
		release_time, release_weeks_ahead, booking_cutoff, cancellation_cutoff, default_visible_days
	) VALUES ('First School', 'UTC', 'en', 'EUR', 'R', 0, '18:00:00', 2, '0 seconds', '2 hours', 14)`)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		setup := service.NewSetupService(st, clock.NewFixed(time.Unix(0, 0)), ids.NewSequence(), "/")
		_, err := setup.Init(ctx, service.SetupData{
			SchoolName: "Second School", Timezone: "UTC", Locale: "en", Currency: "EUR",
			AdminEmail: "second@example.org", AdminGivenName: "Second", AdminFamilyName: "Example",
		})
		done <- err
	}()

	waitForBlockedTransaction(ctx, t, st, firstPID)
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; !errors.Is(err, service.ErrAlreadySetUp) {
		t.Errorf("concurrent setup returned %v, want ErrAlreadySetUp", err)
	}

	var school string
	var people int
	if err := st.pool.QueryRow(ctx, "SELECT school_name, (SELECT count(*) FROM persons) FROM settings").Scan(&school, &people); err != nil {
		t.Fatal(err)
	}

	if school != "First School" || people != 0 {
		t.Errorf("after losing setup: school %q, %d persons; want First School, zero persons", school, people)
	}
}

func waitForBlockedTransaction(ctx context.Context, t *testing.T, st *Store, firstPID int32) {
	t.Helper()

	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()

	for {
		var waiting bool
		err := st.pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE datname = current_database()
				AND $1 = ANY (pg_blocking_pids(pid))
		)`, firstPID).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatal("no transaction waited for the competing write")
		case <-poll.C:
		}
	}
}
