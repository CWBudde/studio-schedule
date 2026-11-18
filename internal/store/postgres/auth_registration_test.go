package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func TestRegistrationLeavesConcurrentlyConfirmedAccountUnchanged(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	st := newTestStore(t)
	now := time.Date(2026, time.October, 23, 16, 0, 0, 0, time.UTC)
	svc := registrationTestService(t, st, now)
	original := service.Registration{
		GivenName: "Original", FamilyName: "Member", Email: "registration@example.test", Password: "fixture original phrase", Locale: "en",
	}
	if err := svc.Register(ctx, original); err != nil {
		t.Fatal(err)
	}

	// Hold the person row as well so the test can observe either the old
	// registration update or the new locking account read waiting to proceed.
	confirmed, blockerPID := beginRegistrationBlocker(ctx, t, st)
	if _, err := confirmed.Exec(ctx, "UPDATE persons SET given_name = given_name"); err != nil {
		t.Fatal(err)
	}
	if _, err := confirmed.Exec(ctx, "UPDATE accounts SET confirmed_at = $1", now); err != nil {
		t.Fatal(err)
	}

	renew := original
	renew.GivenName, renew.Password, renew.Locale = "Different", "fixture replacement phrase", "de"
	done := make(chan error, 1)
	go func() { done <- svc.Register(ctx, renew) }()
	waitForBlockedTransaction(ctx, t, st, blockerPID)
	if err := confirmed.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	var name, locale string
	var mailCount int
	if err := st.pool.QueryRow(ctx, `SELECT given_name, locale, (SELECT count(*) FROM outbox_mails)
		FROM accounts JOIN persons ON persons.id = accounts.person_id`).Scan(&name, &locale, &mailCount); err != nil {
		t.Fatal(err)
	}
	if name != original.GivenName || locale != original.Locale || mailCount != 1 {
		t.Errorf("confirmed profile changed to name %q, locale %q, with %d queued mails", name, locale, mailCount)
	}
	if _, err := svc.Login(ctx, original.Email, original.Password); err != nil {
		t.Errorf("original confirmed password no longer works: %v", err)
	}
}

func TestRegistrationLeavesConcurrentlyCreatedSchoolAccountUnchanged(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	st := newTestStore(t)
	svc := registrationTestService(t, st, time.Unix(0, 0))
	school, blockerPID := beginRegistrationBlocker(ctx, t, st)
	if _, err := school.Exec(ctx, `WITH school_person AS (
		INSERT INTO persons (id, given_name, family_name)
		VALUES ('018f0000-0000-7000-8000-000000000001', 'Original', 'Member') RETURNING id
	) INSERT INTO accounts (id, person_id, email, role)
	SELECT '018f0000-0000-7000-8000-000000000002', id, 'Registration@example.test', 'member' FROM school_person`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- svc.Register(ctx, service.Registration{
			GivenName: "Different", FamilyName: "Member", Email: "registration@example.test", Password: "fixture replacement phrase", Locale: "de",
		})
	}()
	waitForBlockedTransaction(ctx, t, st, blockerPID)
	if err := school.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Errorf("registration must succeed silently after the school's account was created: %v", err)
	}

	var people, credentials, mails int
	if err := st.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM persons),
		(SELECT count(*) FROM account_credentials), (SELECT count(*) FROM outbox_mails)`).Scan(&people, &credentials, &mails); err != nil {
		t.Fatal(err)
	}
	if people != 1 || credentials != 0 || mails != 0 {
		t.Errorf("registration left %d people, %d credentials and %d mails for a school account", people, credentials, mails)
	}
}

func TestConfirmationRechecksAccountAfterConcurrentDisable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	st := newTestStore(t)
	now := time.Date(2026, time.October, 23, 16, 0, 0, 0, time.UTC)
	svc := registrationTestService(t, st, now)
	original := service.Registration{
		GivenName: "Original", FamilyName: "Member", Email: "registration@example.test", Password: "fixture original phrase", Locale: "en",
	}
	if err := svc.Register(ctx, original); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := st.pool.QueryRow(ctx, "SELECT body_text FROM outbox_mails WHERE template = 'confirm-email'").Scan(&body); err != nil {
		t.Fatal(err)
	}
	_, link, ok := strings.Cut(body, "https://school.example.test/dance/confirm?token=")
	if !ok || len(strings.Fields(link)) == 0 {
		t.Fatal("registration did not queue a mounted confirmation link")
	}
	token := strings.Fields(link)[0]

	disabled, blockerPID := beginRegistrationBlocker(ctx, t, st)
	if _, err := disabled.Exec(ctx, "UPDATE accounts SET disabled_at = $1", now); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.Confirm(ctx, token, original.Password)
		done <- err
	}()
	waitForBlockedTransaction(ctx, t, st, blockerPID)
	if err := disabled.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("confirm after disabling account: got %v, want invalid credentials", err)
	}
	var preserved bool
	if err := st.pool.QueryRow(ctx, `SELECT confirmed_at IS NULL AND disabled_at IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM sessions) AND EXISTS (SELECT 1 FROM account_tokens) FROM accounts`).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if !preserved {
		t.Error("failed confirmation changed the disabled account, issued a session, or consumed the link")
	}
}

func beginRegistrationBlocker(ctx context.Context, t *testing.T, st *Store) (pgx.Tx, int32) {
	t.Helper()
	tx, err := st.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	var pid int32
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}

	return tx, pid
}

func registrationTestService(t *testing.T, st *Store, now time.Time) *service.AuthService {
	t.Helper()
	if err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, seed.DemoSettings())
	}); err != nil {
		t.Fatal(err)
	}
	renderer, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	return service.NewAuthService(st, clock.NewFixed(now), ids.V7{}, hasher, renderer, "https://school.example.test/dance/")
}
