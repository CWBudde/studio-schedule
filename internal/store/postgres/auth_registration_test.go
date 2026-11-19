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
	"github.com/cwbudde/studio-schedule/internal/domain"
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

func TestLoginWaitsForConcurrentAccountDisable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	st := newTestStore(t)
	now := time.Date(2026, time.October, 23, 16, 0, 0, 0, time.UTC)
	svc := registrationTestService(t, st, now)
	const email, secret = "disable-race@example.test", "a fictional login password"
	if err := svc.Register(ctx, service.Registration{
		GivenName: "Fixture", FamilyName: "Member", Email: email, Password: secret,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, "UPDATE accounts SET confirmed_at = $1 WHERE email = $2", now, email); err != nil {
		t.Fatal(err)
	}

	tx, pid := beginRegistrationBlocker(ctx, t, st)
	var account string
	if err := tx.QueryRow(ctx, "SELECT id::text FROM accounts WHERE email = $1 FOR UPDATE", email).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE accounts SET disabled_at = $1 WHERE id = $2", now, account); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := svc.Login(ctx, email, secret)
		result <- err
	}()

	waitForBlockedTransaction(ctx, t, st, pid)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("concurrent login: got %v, want %v", err, service.ErrInvalidCredentials)
	}
	var count int
	if err := st.pool.QueryRow(ctx, "SELECT COUNT(*) FROM sessions WHERE account_id = $1", account).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("disabled account has %d sessions, want zero", count)
	}
}

func TestAuthenticatedWritesWaitForAccountRevocation(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"impersonation", "push subscription", "locale change"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			st := newTestStore(t)
			now := time.Date(2026, time.October, 23, 16, 0, 0, 0, time.UTC)
			svc := registrationTestService(t, st, now)
			member := sessionRaceAccount(ctx, t, st, svc, "member@example.test", domain.RoleMember, now)
			admin := sessionRaceAccount(ctx, t, st, svc, "admin@example.test", domain.RoleAdmin, now)

			revocation, pid := beginRegistrationBlocker(ctx, t, st)
			id := member.Principal.AccountID.String()
			var locked bool
			if err := revocation.QueryRow(ctx, "SELECT TRUE FROM accounts WHERE id = $1 FOR UPDATE", id).Scan(&locked); err != nil {
				t.Fatal(err)
			}

			result := make(chan error, 1)
			want := service.ErrAccountInactive
			if name == "push subscription" {
				want = service.ErrNoSession
				if _, err := revocation.Exec(ctx, "DELETE FROM sessions WHERE account_id = $1", id); err != nil {
					t.Fatal(err)
				}
				push := service.NewPushService(st, clock.NewFixed(now), ids.NewSequence(), sessionRacePushSender{})
				go func() {
					result <- push.Subscribe(ctx, member.Principal, service.PushSubscriptionInput{
						Endpoint: "https://push.example.test/fixture",
						P256DH:   "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
						Auth:     "BTBZMqHH6r4Tts7J_aSIgg",
					})
				}()
			} else {
				if _, err := revocation.Exec(ctx, "UPDATE accounts SET disabled_at = $1 WHERE id = $2", now, id); err != nil {
					t.Fatal(err)
				}
				if name == "locale change" {
					want = service.ErrNoSession
					go func() { result <- svc.SetLocale(ctx, member.Principal, "de") }()
				} else {
					go func() {
						_, err := svc.Impersonate(ctx, admin.Principal, member.Principal.AccountID)
						result <- err
					}()
				}
			}

			waitForBlockedTransaction(ctx, t, st, pid)
			if err := revocation.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if got := <-result; !errors.Is(got, want) {
				t.Errorf("write after revocation: got %v, want %v", got, want)
			}
			if name != "push subscription" {
				var disabled bool
				if err := st.pool.QueryRow(ctx, "SELECT disabled_at IS NOT NULL FROM accounts WHERE id = $1", id).Scan(&disabled); err != nil {
					t.Fatal(err)
				}
				if !disabled {
					t.Error("the concurrent write re-enabled the account")
				}
			}
		})
	}
}

func sessionRaceAccount(ctx context.Context, t *testing.T, st *Store, svc *service.AuthService, email string, role domain.Role, now time.Time) service.SignedIn {
	t.Helper()

	const password = "fictional session race phrase"
	if err := svc.Register(ctx, service.Registration{
		GivenName: "Fixture", FamilyName: "Account", Email: email, Password: password,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, "UPDATE accounts SET confirmed_at = $1, role = $2 WHERE email = $3", now, string(role), email); err != nil {
		t.Fatal(err)
	}
	signedIn, err := svc.Login(ctx, email, password)
	if err != nil {
		t.Fatal(err)
	}
	return signedIn
}

type sessionRacePushSender struct{}

func (sessionRacePushSender) PublicKey() string { return "" }
func (sessionRacePushSender) Send(context.Context, domain.PushSubscription, []byte, time.Duration) error {
	return nil
}
