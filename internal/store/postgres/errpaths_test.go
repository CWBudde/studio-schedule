package postgres

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// noDriverError fails t if err exposes a driver error, like TestMapErr.
func noDriverError(t *testing.T, err error) {
	t.Helper()

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) || errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("%v exposes the driver error", err)
	}
}

// TestOutOfRangeValuesFailWrites checks that every write converting a number
// for an integer column (or a weekday for a smallint) refuses a value the
// column cannot hold with domain.ErrValidation, before it writes anything.
func TestOutOfRangeValuesFailWrites(t *testing.T) {
	t.Parallel()

	const big = math.MaxInt32 + 1

	st := newTestStore(t)
	seq := ids.NewSequence()

	for name, write := range map[string]func(context.Context, store.Tx) error{
		"announcement create": func(ctx context.Context, tx store.Tx) error {
			return tx.Announcements().Create(ctx, domain.Announcement{ID: ids.New[domain.AnnouncementID](seq), EmailRecipients: big})
		},
		"announcement mail state": func(ctx context.Context, tx store.Tx) error {
			return tx.Announcements().SetMailState(ctx, domain.Announcement{ID: ids.New[domain.AnnouncementID](seq), EmailRecipients: big})
		},
		"announcement mailing": func(ctx context.Context, tx store.Tx) error {
			return tx.Announcements().CreateMailing(ctx, domain.AnnouncementMailing{ID: ids.New[domain.AnnouncementMailingID](seq), Recipients: big})
		},
		"room create": func(ctx context.Context, tx store.Tx) error {
			return tx.Rooms().Create(ctx, domain.Room{ID: ids.New[domain.RoomID](seq), Capacity: big})
		},
		"room update": func(ctx context.Context, tx store.Tx) error {
			return tx.Rooms().Update(ctx, domain.Room{ID: ids.New[domain.RoomID](seq), Capacity: -big - 1})
		},
		"import run": func(ctx context.Context, tx store.Tx) error {
			return tx.Imports().Create(ctx, domain.ImportRun{
				ID: ids.New[domain.ImportRunID](seq), Counts: domain.ImportCounts{Rows: 1, LinksAdded: big},
			})
		},
		"lesson create": func(ctx context.Context, tx store.Tx) error {
			return tx.Lessons().Create(ctx, domain.Lesson{ID: ids.New[domain.LessonID](seq), Capacity: big})
		},
		"lesson update": func(ctx context.Context, tx store.Tx) error {
			return tx.Lessons().Update(ctx, domain.Lesson{ID: ids.New[domain.LessonID](seq), Capacity: big})
		},
		"lesson notice": func(ctx context.Context, tx store.Tx) error {
			return tx.Notices().Create(ctx, domain.LessonNotice{ID: ids.New[domain.LessonNoticeID](seq), Recipients: big})
		},
		"outbox create": func(ctx context.Context, tx store.Tx) error {
			return tx.Outbox().Create(ctx, domain.OutboxMail{ID: ids.New[domain.OutboxMailID](seq), Attempts: big})
		},
		"outbox update": func(ctx context.Context, tx store.Tx) error {
			return tx.Outbox().Update(ctx, domain.OutboxMail{ID: ids.New[domain.OutboxMailID](seq), Attempts: big})
		},
		"pass type visits": func(ctx context.Context, tx store.Tx) error {
			return tx.PassTypes().Create(ctx, domain.PassType{ID: ids.New[domain.PassTypeID](seq), Visits: big})
		},
		"pass type validity": func(ctx context.Context, tx store.Tx) error {
			return tx.PassTypes().Create(ctx, domain.PassType{ID: ids.New[domain.PassTypeID](seq), ValidityDays: big})
		},
		"pass type VAT rate": func(ctx context.Context, tx store.Tx) error {
			return tx.PassTypes().Update(ctx, domain.PassType{ID: ids.New[domain.PassTypeID](seq), VATRate: big})
		},
		"pass visits total": func(ctx context.Context, tx store.Tx) error {
			return tx.Passes().Create(ctx, domain.Pass{ID: ids.New[domain.PassID](seq), Kind: domain.PassVisits, VisitsTotal: big})
		},
		"pass visits remaining": func(ctx context.Context, tx store.Tx) error {
			return tx.Passes().Update(ctx, domain.Pass{
				ID: ids.New[domain.PassID](seq), Kind: domain.PassVisits, VisitsTotal: 10, VisitsRemaining: big,
			})
		},
		"latest redemptions": func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Redemptions().ListByPasses(ctx, []domain.PassID{ids.New[domain.PassID](seq)}, big)

			return err
		},
		"settings weekday": func(ctx context.Context, tx store.Tx) error {
			return tx.Settings().Put(ctx, domain.Settings{ReleaseWeekday: time.Saturday + 1})
		},
		"settings weeks ahead": func(ctx context.Context, tx store.Tx) error {
			return tx.Settings().Put(ctx, domain.Settings{ReleaseWeeksAhead: big})
		},
		"settings visible days": func(ctx context.Context, tx store.Tx) error {
			return tx.Settings().Put(ctx, domain.Settings{ReleaseWeeksAhead: 2, DefaultVisibleDays: big})
		},
		"account create": func(ctx context.Context, tx store.Tx) error {
			return tx.Accounts().Create(ctx, domain.Account{ID: ids.New[domain.AccountID](seq), VisibleDays: big})
		},
		"account update": func(ctx context.Context, tx store.Tx) error {
			return tx.Accounts().Update(ctx, domain.Account{ID: ids.New[domain.AccountID](seq), VisibleDays: big})
		},
		"push create": func(ctx context.Context, tx store.Tx) error {
			return tx.PushOutbox().Create(ctx, domain.PushMessage{ID: ids.New[domain.PushMessageID](seq), Attempts: big})
		},
		"push update": func(ctx context.Context, tx store.Tx) error {
			return tx.PushOutbox().Update(ctx, domain.PushMessage{ID: ids.New[domain.PushMessageID](seq), Attempts: big})
		},
		"receipt year": func(ctx context.Context, tx store.Tx) error {
			return tx.Receipts().Create(ctx, domain.Receipt{ID: ids.New[domain.ReceiptID](seq), Year: big})
		},
		"receipt sequence": func(ctx context.Context, tx store.Tx) error {
			return tx.Receipts().Create(ctx, domain.Receipt{ID: ids.New[domain.ReceiptID](seq), Year: 2026, Sequence: big})
		},
		"receipt list year": func(ctx context.Context, tx store.Tx) error {
			_, _, err := tx.Receipts().List(ctx, store.ReceiptFilter{Year: big, Limit: 10})

			return err
		},
		"receipt counter year": func(ctx context.Context, tx store.Tx) error {
			_, err := tx.ReceiptCounters().Next(ctx, big)

			return err
		},
		"class weekday": func(ctx context.Context, tx store.Tx) error {
			return tx.Classes().Create(ctx, domain.Class{ID: ids.New[domain.ClassID](seq), Weekday: -1})
		},
		"class capacity": func(ctx context.Context, tx store.Tx) error {
			return tx.Classes().Create(ctx, domain.Class{ID: ids.New[domain.ClassID](seq), Weekday: time.Monday, Capacity: big})
		},
		"class update": func(ctx context.Context, tx store.Tx) error {
			return tx.Classes().Update(ctx, domain.Class{ID: ids.New[domain.ClassID](seq), Weekday: time.Saturday + 1})
		},
		"event capacity": func(ctx context.Context, tx store.Tx) error {
			return tx.Events().Create(ctx, domain.Event{ID: ids.New[domain.EventID](seq), Capacity: big})
		},
		"event VAT rate": func(ctx context.Context, tx store.Tx) error {
			return tx.Events().Update(ctx, domain.Event{ID: ids.New[domain.EventID](seq), Capacity: 1, VATRate: big})
		},
		"event registration create": func(ctx context.Context, tx store.Tx) error {
			return tx.EventRegistrations().Create(ctx, domain.EventRegistration{
				ID: ids.New[domain.EventRegistrationID](seq), VATRate: big,
			})
		},
		"event registration update": func(ctx context.Context, tx store.Tx) error {
			return tx.EventRegistrations().Update(ctx, domain.EventRegistration{
				ID: ids.New[domain.EventRegistrationID](seq), VATRate: -big - 1,
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := st.InTx(t.Context(), store.ReadWrite, write)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("got %v, want ErrValidation", err)
			}

			noDriverError(t, err)
		})
	}

	// The conversions fail before any statement: even a transaction that
	// ignored the errors would have written nothing.
	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		_ = tx.Rooms().Create(ctx, domain.Room{ID: ids.New[domain.RoomID](seq), Capacity: big})
		_ = tx.Settings().Put(ctx, domain.Settings{ReleaseWeekday: time.Saturday + 1})

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var rows int

	err = st.pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM settings) + (SELECT count(*) FROM rooms) + (SELECT count(*) FROM lessons) +
		(SELECT count(*) FROM classes) + (SELECT count(*) FROM pass_types) + (SELECT count(*) FROM passes) +
		(SELECT count(*) FROM announcements) + (SELECT count(*) FROM announcement_mailings) +
		(SELECT count(*) FROM lesson_notices) + (SELECT count(*) FROM outbox_mails) +
		(SELECT count(*) FROM push_outbox) + (SELECT count(*) FROM receipts) +
		(SELECT count(*) FROM receipt_counters) + (SELECT count(*) FROM import_runs) +
		(SELECT count(*) FROM accounts) + (SELECT count(*) FROM events) +
		(SELECT count(*) FROM event_registrations)`).Scan(&rows)
	if err != nil {
		t.Fatal(err)
	}

	if rows != 0 {
		t.Errorf("%d rows written by failed writes, want none", rows)
	}
}

// demoReceipt loads the demo data into st and returns a new receipt like the
// first demo receipt (same payer and author), not yet stored.
func demoReceipt(t *testing.T, st *Store) domain.Receipt {
	t.Helper()

	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	rc := seed.Demo().Receipts[0]
	rc.ID = ids.New[domain.ReceiptID](ids.NewSequence())
	rc.ID[0] = 0xee // apart from the IDs of the demo's own sequence
	rc.Number += "-X"
	rc.Sequence += 1000
	rc.Lines = append([]domain.ReceiptLine(nil), rc.Lines...)

	return rc
}

// TestReceiptLineFailureRollsBack checks that a receipt whose line cannot be
// written fails as a whole: its row, inserted before the lines, is gone.
func TestReceiptLineFailureRollsBack(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	base := demoReceipt(t, st)

	for name, tc := range map[string]struct {
		line func(*domain.ReceiptLine)
		want error
	}{
		// Converted before the INSERT of the line, after that of the receipt.
		"position overflow": {func(l *domain.ReceiptLine) { l.Position = math.MaxInt32 + 1 }, domain.ErrValidation},
		"quantity overflow": {func(l *domain.ReceiptLine) { l.Quantity = math.MaxInt32 + 1 }, domain.ErrValidation},
		"VAT overflow":      {func(l *domain.ReceiptLine) { l.VATRate = math.MaxInt32 + 1 }, domain.ErrValidation},
		// Refused by the CHECK constraint of receipt_lines.
		"zero quantity": {func(l *domain.ReceiptLine) { l.Quantity, l.Net, l.VAT, l.Gross = 0, 0, 0, 0 }, domain.ErrValidation},
	} {
		t.Run(name, func(t *testing.T) {
			rc := base
			rc.Lines = append([]domain.ReceiptLine(nil), base.Lines...)
			tc.line(&rc.Lines[len(rc.Lines)-1])

			err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
				return tx.Receipts().Create(ctx, rc)
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Create = %v, want %v", err, tc.want)
			}

			noDriverError(t, err)

			err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Receipts().Get(ctx, rc.ID)

				return err
			})
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("receipt after the failed create: %v, want ErrNotFound", err)
			}
		})
	}
}

// TestEmptyReceiptPDF checks that PutPDF refuses an empty PDF.
func TestEmptyReceiptPDF(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Receipts().PutPDF(ctx, ids.New[domain.ReceiptID](ids.NewSequence()), nil)
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("PutPDF(nil) = %v, want ErrValidation", err)
	}
}

// TestCanceledQueryInTx checks that a query whose context is canceled inside
// a transaction fails with context.Canceled and no driver error, and that
// the transaction stays usable with its own context.
func TestCanceledQueryInTx(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	seq := ids.NewSequence()
	lessons := []domain.LessonID{ids.New[domain.LessonID](seq)}
	now := time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		mode  store.TxMode
		query func(context.Context, store.Tx) error
	}{
		"announcements scheduled for mail": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Announcements().CountScheduledForMail(ctx, now, now.Add(time.Hour), domain.AnnouncementID{})

			return err
		}},
		"announcement mail lock": {store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			return tx.Announcements().LockMail(ctx)
		}},
		"audit list": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Audit().List(ctx, 10)

			return err
		}},
		"import list": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Imports().List(ctx, 10)

			return err
		}},
		"active bookings": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Bookings().CountActive(ctx, lessons[0])

			return err
		}},
		"active bookings by lessons": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Bookings().CountActiveByLessons(ctx, lessons)

			return err
		}},
		"booking counts by lessons": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Bookings().CountsByLessons(ctx, lessons)

			return err
		}},
		"late cancellations": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Bookings().CountLateCanceledByLessons(ctx, lessons)

			return err
		}},
		"active persons": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Bookings().CountActivePersons(ctx, lessons)

			return err
		}},
		"booking counts by persons": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Bookings().CountsByPersons(ctx, lessons)

			return err
		}},
		"unsent mails": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Outbox().CountUnsent(ctx)

			return err
		}},
		"redemptions by passes": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Redemptions().CountByPasses(ctx, []domain.PassID{ids.New[domain.PassID](seq)})

			return err
		}},
		"settings usage": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Settings().Usage(ctx)

			return err
		}},
		"receipt list": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, _, err := tx.Receipts().List(ctx, store.ReceiptFilter{Limit: 10})

			return err
		}},
		"receipts by IDs": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Receipts().ListByIDs(ctx, []domain.ReceiptID{ids.New[domain.ReceiptID](seq)})

			return err
		}},
		"receipt PDF": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Receipts().PDF(ctx, ids.New[domain.ReceiptID](seq))

			return err
		}},
		"receipt counter": {store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.ReceiptCounters().Next(ctx, 2026)

			return err
		}},
		"event registration counts": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.EventRegistrations().CountsByEvents(ctx, []domain.EventID{ids.New[domain.EventID](seq)}, now)

			return err
		}},
		"event image removal": {store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			return tx.Events().PutImage(ctx, ids.New[domain.EventID](seq), domain.Background{})
		}},
		"waiting list by lessons": {store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Waitlist().CountByLessons(ctx, lessons)

			return err
		}},
	} {
		t.Run(name, func(t *testing.T) {
			var queryErr error

			err := st.InTx(t.Context(), tc.mode, func(ctx context.Context, tx store.Tx) error {
				canceled, cancel := context.WithCancel(ctx)
				cancel()

				queryErr = tc.query(canceled, tx)

				// The connection is not poisoned: the transaction goes on.
				if err := tc.query(ctx, tx); !errors.Is(err, domain.ErrNotFound) {
					return err
				}

				return nil
			})
			if err != nil {
				t.Fatalf("transaction after the canceled query: %v", err)
			}

			if !errors.Is(queryErr, context.Canceled) {
				t.Fatalf("canceled query = %v, want context.Canceled", queryErr)
			}

			noDriverError(t, queryErr)
		})
	}
}

// TestReceiptLinesQueryFails checks the receipt reads whose first query
// succeeds and whose second one, for the lines, fails: the table of the
// lines is renamed away.
func TestReceiptLinesQueryFails(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	rc := seed.Demo().Receipts[0]

	if _, err := st.pool.Exec(t.Context(), `ALTER TABLE receipt_lines RENAME TO receipt_lines_gone`); err != nil {
		t.Fatal(err)
	}

	for name, read := range map[string]func(context.Context, store.Tx) error{
		"get": func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Receipts().Get(ctx, rc.ID)

			return err
		},
		"list by IDs": func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Receipts().ListByIDs(ctx, []domain.ReceiptID{rc.ID})

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := st.InTx(t.Context(), store.ReadOnly, read)
			if err == nil {
				t.Fatal("read succeeded without the lines")
			}

			if errors.Is(err, domain.ErrNotFound) {
				t.Errorf("got %v, want an error other than ErrNotFound", err)
			}

			noDriverError(t, err)
		})
	}
}

// TestCorruptJSON checks that List fails, naming the record, when a JSON
// column holds what the domain type cannot take, instead of returning a
// partly decoded record.
func TestCorruptJSON(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	seq := ids.NewSequence()
	at := time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC)

	p := domain.Person{ID: ids.New[domain.PersonID](seq), GivenName: "Ada", FamilyName: "Admin"}
	a := domain.Account{ID: ids.New[domain.AccountID](seq), PersonID: p.ID, Email: "ada@example.org", Role: domain.RoleAdmin}
	ev := domain.AuditEvent{
		ID: ids.New[domain.AuditEventID](seq), At: at, ActorID: a.ID, Action: domain.AuditImpersonationStarted,
		TargetKind: domain.AuditTargetAccount, TargetID: a.ID, Details: domain.AuditDetails{Recipient: "ada@example.org"},
	}
	run := domain.ImportRun{
		ID: ids.New[domain.ImportRunID](seq), FileName: "members.csv", ActorID: a.ID, StartedAt: at, FinishedAt: at,
		Counts: domain.ImportCounts{Rows: 1, Failed: 1}, Errors: []domain.ImportRowError{{Line: 2, Code: "invalid"}},
	}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Persons().Create(ctx, p),
			tx.Accounts().Create(ctx, a),
			tx.Audit().Create(ctx, ev),
			tx.Imports().Create(ctx, run),
		)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Valid JSON of the shape the CHECK constraints allow, but not of the
	// domain types.
	for _, stmt := range []string{
		`UPDATE audit_events SET details = '{"mailId": "not-an-id"}'`,
		`UPDATE import_runs SET errors = '[{"line": "two"}]'`,
	} {
		if _, err := st.pool.Exec(t.Context(), stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	for name, tc := range map[string]struct {
		list func(context.Context, store.Tx) error
		id   string
	}{
		"audit events": {func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Audit().List(ctx, 10)

			return err
		}, ev.ID.String()},
		"import runs": {func(ctx context.Context, tx store.Tx) error {
			_, err := tx.Imports().List(ctx, 10)

			return err
		}, run.ID.String()},
	} {
		t.Run(name, func(t *testing.T) {
			err := st.InTx(t.Context(), store.ReadOnly, tc.list)
			if err == nil {
				t.Fatal("List succeeded")
			}

			if !strings.Contains(err.Error(), tc.id) {
				t.Errorf("error %q does not name the record %s", err, tc.id)
			}

			noDriverError(t, err)
		})
	}
}

// TestInTxAfterClose checks that InTx on a closed store fails at BEGIN
// without running the callback.
func TestInTxAfterClose(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	st.Close() // the cleanup closes it again: Close is idempotent

	called := false

	err := st.InTx(t.Context(), store.ReadOnly, func(context.Context, store.Tx) error {
		called = true

		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "begin") {
		t.Fatalf("InTx on a closed store = %v, want a begin error", err)
	}

	if called {
		t.Error("the callback ran")
	}

	noDriverError(t, err)
}

// TestCommitFailure checks a transaction whose COMMIT fails because its
// server process was terminated: InTx reports the failure (no retry turns it
// into a success), the write is gone and the after-commit hooks never run.
func TestCommitFailure(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	dbURL := newTestDatabase(t)

	cfg, err := pgconn.ParseConfig(dbURL)
	if err != nil {
		t.Fatal(err)
	}

	st, err := Open(ctx, withParams(dbURL, map[string]string{"pool_max_conns": "2"}))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(st.Close)

	st.retry.backoff = time.Millisecond

	p := domain.Person{ID: ids.New[domain.PersonID](ids.NewSequence()), GivenName: "Tara", FamilyName: "Terminated"}
	calls, hooks := 0, 0

	err = st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		calls++

		tx.AfterCommit(func() { hooks++ })

		if err := tx.Persons().Create(ctx, p); err != nil {
			return err
		}

		// Waits up to 5 s for each process to exit.
		var terminated int

		err := testServer.admin.QueryRow(ctx, `SELECT count(*) FILTER (WHERE pg_terminate_backend(pid, 5000))
			FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, cfg.Database).Scan(&terminated)
		if err != nil {
			return err
		}

		if terminated == 0 {
			t.Error("no server process terminated")
		}

		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("InTx = %v, want a commit error", err)
	}

	if errors.Is(err, domain.ErrConflict) || retryable(err) {
		t.Errorf("InTx = %v, want a failure that is not a lost race", err)
	}

	noDriverError(t, err)

	if calls != 1 || hooks != 0 {
		t.Errorf("callback ran %d times and after-commit hooks %d times, want 1 and 0", calls, hooks)
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM persons WHERE id = $1`, p.ID[:]).Scan(&n); err != nil {
		t.Fatal(err)
	}

	if n != 0 {
		t.Error("the person of the failed commit was stored")
	}
}

func TestRetryableErrorMessages(t *testing.T) {
	t.Parallel()

	for code, want := range map[string]string{
		pgerrcode.DeadlockDetected:     "deadlock detected",
		pgerrcode.SerializationFailure: "serialization failure",
	} {
		err := &retryableError{code: code}
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("retryableError(%s) = %q, want %q", code, got, want)
		}

		// InTx gives up with the mapped error: the reason must survive.
		lost := lostRace(fmtWrap(err), 5)
		if !errors.Is(lost, domain.ErrConflict) || !strings.Contains(lost.Error(), want) ||
			!strings.Contains(lost.Error(), "5 attempts") {
			t.Errorf("lostRace(%s) = %v, want ErrConflict naming %q after 5 attempts", code, lost, want)
		}

		if errors.Is(lost, err) {
			t.Errorf("lostRace(%s) wraps the retryable error", code)
		}
	}
}

// fmtWrap wraps err the way a repository or service might pass it on.
func fmtWrap(err error) error { return errors.Join(errors.New("booking"), err) }
