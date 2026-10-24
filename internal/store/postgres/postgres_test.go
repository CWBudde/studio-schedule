package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/storetest"
)

func TestContract(t *testing.T) {
	t.Parallel()

	storetest.Run(t, func(t *testing.T) store.Store { return newTestStore(t) })
}

// TestLoadDemo loads the demo data, member bookings included, under the
// constraints of the schema (`studio-schedule seed`).
func TestLoadDemo(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	want := seed.Demo()

	var bookings, events int

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		for _, l := range want.Lessons {
			bs, err := tx.Bookings().ListByLesson(ctx, l.ID)
			if err != nil {
				return err
			}

			bookings += len(bs)

			for _, b := range bs {
				es, err := tx.Bookings().ListEvents(ctx, b.ID)
				if err != nil {
					return err
				}

				events += len(es)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if bookings != len(want.Bookings) || events != len(want.BookingEvents) || bookings == 0 {
		t.Fatalf("%d bookings and %d events, want %d and %d", bookings, events, len(want.Bookings), len(want.BookingEvents))
	}
}

func TestMigrationsRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dbURL := newTestDatabase(t) // already migrated up

	m, err := NewMigrator(dbURL)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	wantStatus := func(applied bool) {
		t.Helper()

		status, err := m.Status(ctx)
		if err != nil {
			t.Fatalf("status: %v", err)
		}

		if len(status) == 0 {
			t.Fatal("status lists no migrations")
		}

		for _, s := range status {
			if s.Applied != applied || s.Name == "" || s.Version == 0 || s.AppliedAt.IsZero() == applied {
				t.Errorf("status %+v, want applied=%v", s, applied)
			}
		}
	}
	tables := func() int {
		t.Helper()

		var n int

		err := m.db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name <> 'goose_db_version'`).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}

		return n
	}
	wantCheck := func(want error) {
		t.Helper()

		if err := m.CheckSchema(ctx); !errors.Is(err, want) {
			t.Fatalf("CheckSchema = %v, want %v", err, want)
		}
	}

	wantStatus(true)
	wantCheck(nil)

	if n := tables(); n != 20 {
		t.Fatalf("%d tables after up, want 20", n)
	}

	if res, err := m.Up(ctx); err != nil || len(res) != 0 {
		t.Fatalf("second up = %+v, %v; want nothing to do", res, err)
	}

	for {
		res, err := m.Down(ctx)
		if errors.Is(err, ErrNoMigrationToRollBack) {
			break
		}

		if err != nil {
			t.Fatalf("down: %v", err)
		}

		if res.Direction != "down" || res.Name == "" {
			t.Errorf("down result %+v", res)
		}
	}

	wantStatus(false)
	wantCheck(ErrSchemaPending)

	if n := tables(); n != 0 {
		t.Fatalf("%d tables left after down, want 0", n)
	}

	res, err := m.Up(ctx)
	if err != nil || len(res) == 0 || res[0].Direction != "up" {
		t.Fatalf("up after down = %+v, %v", res, err)
	}

	wantStatus(true)

	// The store works on the re-created schema.
	st, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}

	defer st.Close()

	p := domain.Person{ID: ids.New[domain.PersonID](ids.NewSequence()), GivenName: "Rita", FamilyName: "Roundtrip"}
	if err := st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Persons().Create(ctx, p)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMapErr(t *testing.T) {
	t.Parallel()

	pg := func(code, constraint string) error {
		return &pgconn.PgError{Code: code, ConstraintName: constraint, Message: "message from the server"}
	}

	for name, tc := range map[string]struct {
		err  error
		want error
	}{
		"unique":             {pg(pgerrcode.UniqueViolation, constraintLessonClassSlot), domain.ErrConflict},
		"class member":       {pg(pgerrcode.UniqueViolation, constraintClassMember), domain.ErrDuplicate},
		"account email":      {pg(pgerrcode.UniqueViolation, constraintAccountEmail), domain.ErrDuplicate},
		"account person":     {pg(pgerrcode.UniqueViolation, constraintAccountPerson), domain.ErrDuplicate},
		"guardian link":      {pg(pgerrcode.UniqueViolation, constraintGuardianLink), domain.ErrDuplicate},
		"duplicate name":     {pg(pgerrcode.UniqueViolation, constraintRoomName), domain.ErrDuplicate},
		"foreign key":        {pg(pgerrcode.ForeignKeyViolation, "rooms_location_id_fkey"), domain.ErrConflict},
		"restrict":           {pg(pgerrcode.RestrictViolation, "rooms_location_id_fkey"), domain.ErrConflict},
		"check":              {pg(pgerrcode.CheckViolation, "rooms_capacity_check"), domain.ErrValidation},
		"not null":           {pg(pgerrcode.NotNullViolation, ""), domain.ErrValidation},
		"out of range":       {pg(pgerrcode.NumericValueOutOfRange, ""), domain.ErrValidation},
		"read-only":          {pg(pgerrcode.ReadOnlySQLTransaction, ""), store.ErrReadOnly},
		"serialization":      {pg(pgerrcode.SerializationFailure, ""), nil},
		"deadlock":           {pg(pgerrcode.DeadlockDetected, ""), nil},
		"no rows":            {pgx.ErrNoRows, domain.ErrNotFound},
		"other server error": {pg(pgerrcode.UndefinedTable, ""), nil},
		"other error":        {errors.New("network down"), nil},
		"context canceled":   {context.Canceled, context.Canceled},
		"deadline":           {context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := mapErr(tc.err)
			if got == nil {
				t.Fatal("mapErr returned nil")
			}

			if tc.want != nil && !errors.Is(got, tc.want) {
				t.Errorf("mapErr(%v) = %v, want %v", tc.err, got, tc.want)
			}

			// Nothing above the store may see a driver error.
			var pgErr *pgconn.PgError
			if errors.As(got, &pgErr) || errors.Is(got, pgx.ErrNoRows) {
				t.Errorf("mapErr(%v) = %v exposes the driver error", tc.err, got)
			}
		})
	}

	// A retryable failure is not a business conflict: a callback that
	// handled ErrConflict would swallow it and prevent the retry. InTx
	// reports ErrConflict only when it gives up.
	for _, code := range []string{pgerrcode.SerializationFailure, pgerrcode.DeadlockDetected} {
		if got := mapErr(pg(code, "")); !retryable(got) || errors.Is(got, domain.ErrConflict) {
			t.Errorf("mapErr(%s) = %v, want a retryable error that is not ErrConflict", code, got)
		}
	}

	if mapErr(nil) != nil {
		t.Error("mapErr(nil) != nil")
	}

	if got := mapErr(pg(pgerrcode.UniqueViolation, constraintAccountEmail)).Error(); !strings.Contains(got, "email") {
		t.Errorf("unique violation of %s = %q, want a message about the email", constraintAccountEmail, got)
	}
}

// TestConstraintNamesExist guards the constraint names that errors.go
// relies on against renames in the migrations.
func TestConstraintNamesExist(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)

	for name := range conflicts {
		var n int

		err := st.pool.QueryRow(t.Context(), `SELECT
			(SELECT count(*) FROM pg_constraint WHERE conname = $1) +
			(SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1)`, name).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}

		if n == 0 {
			t.Errorf("constraint or index %q does not exist", name)
		}
	}
}

func TestCheckViolationIsValidation(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	seq := ids.NewSequence()
	p := domain.Person{ID: ids.New[domain.PersonID](seq), GivenName: "Selma", FamilyName: "Self"}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := tx.Persons().Create(ctx, p); err != nil {
			return err
		}

		// Guardian.Validate rejects this; the CHECK constraint is the backstop.
		return tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: p.ID, DependentID: p.ID})
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("self guardian: got %v, want ErrValidation", err)
	}
}

// TestCheckSchemaIsReadOnly checks that CheckSchema reports a database
// without migrations as pending without creating goose's version table, and
// a database with unknown migrations as newer than the binary.
func TestCheckSchemaIsReadOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	check := func(dbURL string) (*Migrator, error) {
		t.Helper()

		m, err := NewMigrator(dbURL)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = m.Close() })

		return m, m.CheckSchema(ctx)
	}

	m, err := check(newUnmigratedDatabase(t))
	if !errors.Is(err, ErrSchemaPending) {
		t.Fatalf("unmigrated: CheckSchema = %v, want ErrSchemaPending", err)
	}

	var table *string
	if err := m.db.QueryRowContext(ctx, "SELECT to_regclass('goose_db_version')::text").Scan(&table); err != nil {
		t.Fatal(err)
	}

	if table != nil {
		t.Errorf("CheckSchema created %s", *table)
	}

	m, err = check(newTestDatabase(t))
	if err != nil {
		t.Fatalf("migrated: CheckSchema = %v, want nil", err)
	}

	if _, err := m.db.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (99999, true)"); err != nil {
		t.Fatal(err)
	}

	if err := m.CheckSchema(ctx); !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("unknown migration: CheckSchema = %v, want ErrSchemaNewer", err)
	}
}

// TestPoolParametersAreAccepted checks that the migrator, like Open,
// accepts pool_* parameters instead of sending them to the server.
func TestPoolParametersAreAccepted(t *testing.T) {
	t.Parallel()

	dbURL := newTestDatabase(t)

	switch {
	case !strings.Contains(dbURL, "://"):
		dbURL += " pool_max_conns=4"
	case strings.Contains(dbURL, "?"):
		dbURL += "&pool_max_conns=4"
	default:
		dbURL += "?pool_max_conns=4"
	}

	m, err := NewMigrator(dbURL)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	if _, err := m.Status(t.Context()); err != nil {
		t.Fatalf("migrator status: %v", err)
	}

	st, err := Open(t.Context(), dbURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	st.Close()
}

func TestRetry(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	st.retry.backoff = time.Millisecond

	failing := func(code string, failures int, calls *int) func(context.Context, store.Tx) error {
		return func(context.Context, store.Tx) error {
			*calls++
			if *calls <= failures {
				return &pgconn.PgError{Code: code, Message: "fake"}
			}

			return nil
		}
	}

	t.Run("RetriesUntilSuccess", func(t *testing.T) {
		for _, code := range []string{pgerrcode.SerializationFailure, pgerrcode.DeadlockDetected} {
			calls := 0
			if err := st.InTx(t.Context(), store.ReadWrite, failing(code, 2, &calls)); err != nil {
				t.Fatalf("%s: %v", code, err)
			}

			if calls != 3 {
				t.Errorf("%s: %d calls, want 3", code, calls)
			}
		}
	})

	t.Run("GivesUpAfterMaxAttempts", func(t *testing.T) {
		calls := 0

		err := st.InTx(t.Context(), store.ReadWrite, failing(pgerrcode.SerializationFailure, 100, &calls))
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("got %v, want ErrConflict", err)
		}

		// The callback returned a raw driver error; it must not leave the
		// package, and the result must not look retryable to an outer caller.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) || retryable(err) {
			t.Errorf("got %v (%T), want a mapped, final error", err, err)
		}

		if calls != st.retry.attempts {
			t.Errorf("%d calls, want %d", calls, st.retry.attempts)
		}
	})

	t.Run("OtherErrorsAreNotRetried", func(t *testing.T) {
		calls := 0

		err := st.InTx(t.Context(), store.ReadWrite, failing(pgerrcode.UniqueViolation, 100, &calls))
		if err == nil || calls != 1 {
			t.Fatalf("got %v after %d calls, want an error after 1", err, calls)
		}
	})

	t.Run("StopsWhenCanceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0

		err := st.InTx(ctx, store.ReadWrite, func(context.Context, store.Tx) error {
			calls++
			cancel()

			return &pgconn.PgError{Code: pgerrcode.SerializationFailure}
		})
		if !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("got %v after %d calls, want context.Canceled after 1", err, calls)
		}
	})
}

// TestUniqueNamesMigration upgrades a database whose location, room and
// course type names differ only in case (00006, 00007): the first keeps its
// name, the others get their ID appended.
func TestUniqueNamesMigration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m, err := NewMigrator(newTestDatabase(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	for _, want := range []string{
		"00013_staff_bookings.sql", "00012_waitlist.sql", "00011_class_member_bookings.sql", "00010_booking_rules.sql",
		"00009_lesson_admin.sql",
		"00008_lesson_slots.sql", "00007_course_type_unique_names.sql",
		"00006_catalog_unique_names.sql",
	} {
		if res, err := m.Down(ctx); err != nil || res.Name != want {
			t.Fatalf("down = %+v, %v; want %s rolled back", res, err, want)
		}
	}

	const (
		loc1, loc2   = "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000002"
		room1, room2 = "00000000-0000-7000-8000-000000000011", "00000000-0000-7000-8000-000000000012"
		room3        = "00000000-0000-7000-8000-000000000013"
		ct1, ct2     = "00000000-0000-7000-8000-000000000021", "00000000-0000-7000-8000-000000000022"
	)

	if _, err := m.db.ExecContext(ctx,
		`INSERT INTO locations (id, name) VALUES ($1, 'Hall'), ($2, 'HALL')`, loc1, loc2); err != nil {
		t.Fatal(err)
	}

	if _, err := m.db.ExecContext(ctx, `INSERT INTO rooms (id, location_id, name, capacity)
		VALUES ($1, $4, 'Studio', 5), ($2, $4, 'studio', 5), ($3, $5, 'Studio', 5)`,
		room1, room2, room3, loc1, loc2); err != nil {
		t.Fatal(err)
	}

	if _, err := m.db.ExecContext(ctx,
		`INSERT INTO course_types (id, name) VALUES ($1, 'Tap'), ($2, 'TAP')`, ct1, ct2); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up: %v", err)
	}

	for id, want := range map[string]string{
		loc1: "Hall", loc2: "HALL (" + loc2 + ")",
		room1: "Studio", room2: "studio (" + room2 + ")", room3: "Studio",
		ct1: "Tap", ct2: "TAP (" + ct2 + ")",
	} {
		var name string

		err := m.db.QueryRowContext(ctx, `SELECT name FROM locations WHERE id = $1
			UNION ALL SELECT name FROM rooms WHERE id = $1
			UNION ALL SELECT name FROM course_types WHERE id = $1`, id).Scan(&name)
		if err != nil || name != want {
			t.Errorf("name of %s = %q, %v; want %q", id, name, err, want)
		}
	}
}

// TestLessonSlotsMigration upgrades a database with generated and one-off
// lessons (00008, 00009): a class lesson gets the school date of its start as
// its slot, a one-off none, and neither is detached.
func TestLessonSlotsMigration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	m, err := NewMigrator(newTestDatabase(t))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = m.Close() })

	for _, want := range []string{
		"00013_staff_bookings.sql", "00012_waitlist.sql", "00011_class_member_bookings.sql", "00010_booking_rules.sql",
		"00009_lesson_admin.sql",
		"00008_lesson_slots.sql",
	} {
		if res, err := m.Down(ctx); err != nil || res.Name != want {
			t.Fatalf("down = %+v, %v; want %s rolled back", res, err, want)
		}
	}

	const (
		loc, room, ct, staff = "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000002",
			"00000000-0000-7000-8000-000000000003", "00000000-0000-7000-8000-000000000004"
		term, class  = "00000000-0000-7000-8000-000000000005", "00000000-0000-7000-8000-000000000006"
		late, oneOff = "00000000-0000-7000-8000-000000000007", "00000000-0000-7000-8000-000000000008"
	)

	for _, stmt := range []string{
		`INSERT INTO settings (school_name, timezone, default_locale, currency, receipt_prefix, release_weekday,
			release_time, release_weeks_ahead, booking_cutoff, cancellation_cutoff, default_visible_days)
			VALUES ('Example School', 'America/New_York', 'en', 'EUR', 'R', 0, '18:00', 2, '0', '0', 14)`,
		`INSERT INTO locations (id, name) VALUES ('` + loc + `', 'Hall')`,
		`INSERT INTO rooms (id, location_id, name, capacity) VALUES ('` + room + `', '` + loc + `', 'Studio', 5)`,
		`INSERT INTO course_types (id, name) VALUES ('` + ct + `', 'Tap')`,
		`INSERT INTO staff (id, display_name, kind, active) VALUES ('` + staff + `', 'Ada', 'instructor', true)`,
		`INSERT INTO terms (id, name, starts_on, ends_on) VALUES ('` + term + `', 'Spring', '2026-03-01', '2026-06-30')`,
		`INSERT INTO classes (id, term_id, course_type_id, room_id, instructor_id, level, weekday, start_time,
			duration, capacity) VALUES ('` + class + `', '` + term + `', '` + ct + `', '` + room + `', '` + staff +
			`', 'open', 1, '21:30', '1 hour', 5)`,
		// 21:30 in New York on Monday 2 March is already Tuesday in UTC.
		`INSERT INTO lessons (id, class_id, course_type_id, room_id, instructor_id, starts_at, ends_at, status,
			visibility, kind, capacity) VALUES ('` + late + `', '` + class + `', '` + ct + `', '` + room + `', '` +
			staff + `', '2026-03-03 02:30Z', '2026-03-03 03:30Z', 'scheduled', 'public', 'regular', 5)`,
		`INSERT INTO lessons (id, course_type_id, room_id, instructor_id, starts_at, ends_at, status,
			visibility, kind, capacity) VALUES ('` + oneOff + `', '` + ct + `', '` + room + `', '` + staff +
			`', '2026-03-03 02:30Z', '2026-03-03 03:30Z', 'scheduled', 'public', 'workshop', 5)`,
	} {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up: %v", err)
	}

	for id, want := range map[string]string{late: "2026-03-02", oneOff: ""} {
		var slot sql.NullString

		err := m.db.QueryRowContext(ctx, `SELECT slot_date::text FROM lessons WHERE id = $1`, id).Scan(&slot)
		if err != nil || slot.String != want {
			t.Errorf("slot of %s = %q, %v; want %q", id, slot.String, err, want)
		}
	}

	// 00009 starts every lesson undetached and without cancellation details.
	var attached int
	if err := m.db.QueryRowContext(ctx, `SELECT count(*) FROM lessons
		WHERE NOT detached AND cancel_note IS NULL AND canceled_at IS NULL AND canceled_by IS NULL`,
	).Scan(&attached); err != nil || attached != 2 {
		t.Errorf("undetached lessons = %d, %v; want 2", attached, err)
	}
}

// TestRetryAfterDeadlock provokes a real deadlock: two transactions lock two
// lessons in opposite order. PostgreSQL aborts one of them, InTx retries it,
// and both succeed.
func TestRetryAfterDeadlock(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	seq := ids.NewSequence()
	l1, l2 := createLesson(t, st, seq, 0), createLesson(t, st, seq, time.Hour)

	var (
		arrived  sync.WaitGroup // both transactions hold their first lock
		attempts atomic.Int32
		wg       sync.WaitGroup
		errs     [2]error
	)

	arrived.Add(2)

	for i, order := range [2][2]domain.LessonID{{l1, l2}, {l2, l1}} {
		first := true

		wg.Go(func() {
			errs[i] = st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
				attempts.Add(1)

				if _, err := tx.Lessons().Lock(ctx, order[0]); err != nil {
					return err
				}

				if first {
					first = false

					arrived.Done()
					arrived.Wait()
				}

				_, err := tx.Lessons().Lock(ctx, order[1])

				return err
			})
		})
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("transaction %d: %v", i, err)
		}
	}

	if n := attempts.Load(); n != 3 {
		t.Errorf("%d attempts, want 3 (one retry after the deadlock)", n)
	}
}

func TestPoolConfigExecMode(t *testing.T) {
	t.Parallel()

	for dsn, want := range map[string]pgx.QueryExecMode{
		"postgres://app@db.example.test/studio":                                          pgx.QueryExecModeCacheDescribe,
		"postgres://app@db.example.test/studio?default_query_exec_mode=exec":             pgx.QueryExecModeExec,
		"host=db.example.test dbname=studio":                                             pgx.QueryExecModeCacheDescribe,
		"host=db.example.test dbname=studio default_query_exec_mode=simple_protocol":     pgx.QueryExecModeSimpleProtocol,
		"host=db.example.test dbname=studio default_query_exec_mode=cache_statement":     pgx.QueryExecModeCacheStatement,
		"postgres://app@db.example.test/studio?pool_max_conns=2&sslmode=disable":         pgx.QueryExecModeCacheDescribe,
		"postgresql://app@db.example.test/studio?default_query_exec_mode=cache_describe": pgx.QueryExecModeCacheDescribe,
	} {
		cfg, err := poolConfig(dsn)
		if err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}

		if got := cfg.ConnConfig.DefaultQueryExecMode; got != want {
			t.Errorf("%s: exec mode %v, want %v", dsn, got, want)
		}
	}
}

func TestOpenErrorHidesPassword(t *testing.T) {
	t.Parallel()

	const password = "change-me-secret"

	for _, dsn := range []string{
		"postgres://app:" + password + "@127.0.0.1:1/studio?connect_timeout=2",
		"host=127.0.0.1 port=1 user=app password=" + password + " dbname=studio connect_timeout=2",
		"postgres://app:" + password + "@127.0.0.1:1/studio?default_query_exec_mode=nonsense",
	} {
		st, err := Open(t.Context(), dsn)
		if err == nil {
			st.Close()
			t.Fatalf("Open(%q) succeeded", dsn)
		}

		if strings.Contains(err.Error(), password) {
			t.Errorf("Open error leaks the password: %v", err)
		}
	}
}

// createLesson creates a one-off lesson (and what it references) at t0+offset.
func createLesson(t *testing.T, st *Store, seq *ids.Sequence, offset time.Duration) domain.LessonID {
	t.Helper()

	start := time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC).Add(offset)
	// Location and course type names are unique; each lesson gets its own.
	locID, ctID := ids.New[domain.LocationID](seq), ids.New[domain.CourseTypeID](seq)
	loc := domain.Location{ID: locID, Name: "Studio " + locID.String()}
	room := domain.Room{ID: ids.New[domain.RoomID](seq), LocationID: loc.ID, Name: "Room A", Capacity: 10}
	staff := domain.Staff{ID: ids.New[domain.StaffID](seq), DisplayName: "Instructor", Kind: domain.StaffInstructor}
	ct := domain.CourseType{ID: ctID, Name: "Ballet " + ctID.String()}
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](seq), CourseTypeID: ct.ID, RoomID: room.ID, InstructorID: staff.ID,
		StartsAt: start, EndsAt: start.Add(time.Hour), Status: domain.LessonScheduled,
		Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 5,
	}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Locations().Create(ctx, loc),
			tx.Rooms().Create(ctx, room),
			tx.Staff().Create(ctx, staff),
			tx.CourseTypes().Create(ctx, ct),
			tx.Lessons().Create(ctx, l),
		)
	})
	if err != nil {
		t.Fatal(err)
	}

	return l.ID
}
