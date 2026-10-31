package service_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// stepClock is a clock that tests move forward.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *stepClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

// cheapParams keeps hashing fast in tests.
var cheapParams = password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

const appURL = "https://school.example.org/app/#/"

type authFixture struct {
	t     *testing.T
	st    store.Store
	clock *stepClock
	svc   *service.AuthService

	// The parts of svc, for a second service on the same data (faulty).
	mem      *memory.Store
	seq      *ids.Sequence
	hasher   service.PasswordHasher
	renderer *mail.Renderer
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	hasher, err := password.NewWithParams(cheapParams, nil)
	if err != nil {
		t.Fatal(err)
	}

	return newAuthFixtureWith(t, hasher)
}

func newAuthFixtureWith(t *testing.T, hasher service.PasswordHasher) *authFixture {
	t.Helper()

	renderer, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	st := memory.New()

	err = st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, seed.DemoSettings())
	})
	if err != nil {
		t.Fatal(err)
	}

	clk := &stepClock{t: time.Date(2026, time.October, 23, 16, 0, 0, 0, time.UTC)}
	seq := ids.NewSequence()
	svc := service.NewAuthService(st, clk, seq, hasher, renderer, appURL)

	return &authFixture{t: t, st: st, clock: clk, svc: svc, mem: st, seq: seq, hasher: hasher, renderer: renderer}
}

// faulty returns a FaultStore on the fixture's data and an AuthService on
// it that shares the fixture's clock, IDs, hasher and renderer.
func (f *authFixture) faulty() (*service.FaultStore, *service.AuthService) {
	fs := service.NewFaultStore(f.mem)

	return fs, service.NewAuthService(fs, f.clock, f.seq, f.hasher, f.renderer, appURL)
}

func (f *authFixture) register(email, pw string) {
	f.t.Helper()

	err := f.svc.Register(f.t.Context(), service.Registration{
		GivenName: "Ada", FamilyName: "Lovelace", Email: email, Password: pw, Locale: "en",
	})
	if err != nil {
		f.t.Fatalf("Register(%s): %v", email, err)
	}
}

func (f *authFixture) mails() []domain.OutboxMail {
	f.t.Helper()

	var mails []domain.OutboxMail

	err := f.st.InTx(f.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		mails, err = tx.Outbox().List(ctx, store.OutboxFilter{Limit: 100})

		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}

	return mails
}

var confirmLink = regexp.MustCompile(regexp.QuoteMeta(appURL) + `confirm\?token=([A-Za-z0-9_-]+)`)

// lastToken returns the token of the newest confirmation mail.
func (f *authFixture) lastToken() string {
	f.t.Helper()

	mails := f.mails()
	if len(mails) == 0 {
		f.t.Fatal("no mail in the outbox")
	}

	m := confirmLink.FindStringSubmatch(mails[0].Text)
	if m == nil {
		f.t.Fatalf("no confirmation link in %q", mails[0].Text)
	}

	return m[1]
}

func (f *authFixture) account(email string) domain.Account {
	f.t.Helper()

	var acc domain.Account

	err := f.st.InTx(f.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		acc, err = tx.Accounts().GetByEmail(ctx, email)

		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}

	return acc
}

func wantIs(t *testing.T, err, target error) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Fatalf("want %v, got %v", target, err)
	}
}

func TestAuthRegisterConfirmLogin(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	ctx := t.Context()

	f.register(" ada@example.org ", "correct horse")

	mails := f.mails()
	if len(mails) != 1 || mails[0].To != "ada@example.org" || mails[0].Template != mail.TemplateConfirmEmail {
		t.Fatalf("outbox = %+v, want one confirmation mail to ada@example.org", mails)
	}

	if mails[0].Status() != domain.MailPending {
		t.Fatalf("mail status = %s, want pending", mails[0].Status())
	}

	acc := f.account("ada@example.org")
	if acc.Role != domain.RoleMember || !acc.ConfirmedAt.IsZero() {
		t.Fatalf("account = %+v, want an unconfirmed member", acc)
	}

	_, err := f.svc.Login(ctx, "ada@example.org", "correct horse")
	wantIs(t, err, service.ErrEmailNotConfirmed)

	_, err = f.svc.Login(ctx, "ada@example.org", "wrong horse")
	wantIs(t, err, service.ErrInvalidCredentials)

	_, err = f.svc.Login(ctx, "nobody@example.org", "correct horse")
	wantIs(t, err, service.ErrInvalidCredentials)

	token := f.lastToken()

	_, err = f.svc.Confirm(ctx, token, "wrong horse")
	wantIs(t, err, service.ErrInvalidCredentials)

	signedIn, err := f.svc.Confirm(ctx, token, "correct horse")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	if signedIn.Token == "" || signedIn.Principal.AccountID != acc.ID {
		t.Fatalf("Confirm = %+v, want a session for %s", signedIn, acc.ID)
	}

	if got := f.account("ada@example.org"); !got.ConfirmedAt.Equal(f.clock.Now()) {
		t.Fatalf("ConfirmedAt = %v, want %v", got.ConfirmedAt, f.clock.Now())
	}

	// The link works once.
	_, err = f.svc.Confirm(ctx, token, "correct horse")
	wantIs(t, err, service.ErrInvalidToken)

	login, err := f.svc.Login(ctx, "ADA@example.org", "correct horse")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	p, _, err := f.svc.Authenticate(ctx, login.Token)
	if err != nil || p.AccountID != acc.ID || p.Role != domain.RoleMember {
		t.Fatalf("Authenticate = %+v, %v", p, err)
	}

	// Only the hash of the token is stored.
	err = f.st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		sess, err := tx.Sessions().GetByToken(ctx, domain.HashToken(login.Token))
		if err == nil && strings.Contains(string(sess.TokenHash[:]), login.Token) {
			t.Fatal("session stores the raw token")
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Logout(ctx, login.Token); err != nil {
		t.Fatal(err)
	}

	_, _, err = f.svc.Authenticate(ctx, login.Token)
	wantIs(t, err, service.ErrNoSession)

	// The confirmation session is independent of the logged-out one.
	if _, _, err := f.svc.Authenticate(ctx, signedIn.Token); err != nil {
		t.Fatalf("Authenticate(confirm session): %v", err)
	}

	if err := f.svc.Logout(ctx, "unknown"); err != nil {
		t.Fatalf("Logout(unknown) = %v, want nil", err)
	}
}

func TestAuthConfirmLinkExpires(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	f.register("ada@example.org", "correct horse")
	token := f.lastToken()

	f.clock.advance(service.ConfirmTokenTTL)

	_, err := f.svc.Confirm(t.Context(), token, "correct horse")
	wantIs(t, err, service.ErrInvalidToken)

	_, err = f.svc.Confirm(t.Context(), "not-a-token", "correct horse")
	wantIs(t, err, service.ErrInvalidToken)
}

func TestAuthLatestRegistrationWins(t *testing.T) {
	t.Parallel()

	// Someone registers Ada's address first; Ada registers afterwards.
	f := newAuthFixture(t)
	f.register("ada@example.org", "attacker pass")
	first := f.lastToken()
	f.register("ada@example.org", "ada's own pass")
	second := f.lastToken()

	if len(f.mails()) != 2 {
		t.Fatalf("want two confirmation mails, got %d", len(f.mails()))
	}

	ctx := t.Context()

	_, err := f.svc.Confirm(ctx, first, "attacker pass")
	wantIs(t, err, service.ErrInvalidToken) // replaced by the newer link

	_, err = f.svc.Confirm(ctx, second, "attacker pass")
	wantIs(t, err, service.ErrInvalidCredentials)

	if _, err := f.svc.Confirm(ctx, second, "ada's own pass"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
}

func TestAuthRegisterConfirmedEmailIsSilent(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	f.register("ada@example.org", "correct horse")

	if _, err := f.svc.Confirm(t.Context(), f.lastToken(), "correct horse"); err != nil {
		t.Fatal(err)
	}

	f.register("Ada@Example.org", "other password")

	if n := len(f.mails()); n != 1 {
		t.Fatalf("want no further mail, got %d mails", n)
	}

	if _, err := f.svc.Login(t.Context(), "ada@example.org", "correct horse"); err != nil {
		t.Fatalf("the old password must still work: %v", err)
	}
}

func TestAuthRegisterValidates(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)

	for name, r := range map[string]service.Registration{
		"short password": {GivenName: "Ada", FamilyName: "L", Email: "ada@example.org", Password: "short"},
		"long password":  {GivenName: "Ada", FamilyName: "L", Email: "ada@example.org", Password: strings.Repeat("x", 257)},
		"bad email":      {GivenName: "Ada", FamilyName: "L", Email: "ada", Password: "correct horse"},
		"no name":        {GivenName: " ", FamilyName: "L", Email: "ada@example.org", Password: "correct horse"},
	} {
		err := f.svc.Register(t.Context(), r)
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: want ErrValidation, got %v", name, err)
		}
	}

	if n := len(f.mails()); n != 0 {
		t.Fatalf("invalid registrations sent %d mails", n)
	}
}

// signedInFixture returns a fixture with a confirmed account and a fresh
// session token.
func signedInFixture(t *testing.T) (*authFixture, string) {
	t.Helper()

	f := newAuthFixture(t)
	f.register("ada@example.org", "correct horse")

	s, err := f.svc.Confirm(t.Context(), f.lastToken(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}

	return f, s.Token
}

func TestAuthSlidingExpiry(t *testing.T) {
	t.Parallel()

	f, token := signedInFixture(t)
	ctx := t.Context()

	_, refreshed, err := f.svc.Authenticate(ctx, token)
	if err != nil || refreshed.Token != "" {
		t.Fatalf("fresh session: refreshed=%+v err=%v, want no refresh", refreshed, err)
	}

	f.clock.advance(2 * 24 * time.Hour)

	_, refreshed, err = f.svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}

	if want := f.clock.Now().Add(service.SessionIdleTimeout); !refreshed.ExpiresAt.Equal(want) {
		t.Fatalf("refreshed until %v, want %v", refreshed.ExpiresAt, want)
	}

	// Unused for the idle timeout: the session ends.
	f.clock.advance(service.SessionIdleTimeout)

	_, _, err = f.svc.Authenticate(ctx, token)
	wantIs(t, err, service.ErrNoSession)
}

func TestAuthMaxSessionAge(t *testing.T) {
	t.Parallel()

	f, token := signedInFixture(t)
	ctx := t.Context()
	start := f.clock.Now()

	// Used every ten days, the session still ends after SessionMaxAge.
	for f.clock.Now().Add(10 * 24 * time.Hour).Before(start.Add(service.SessionMaxAge)) {
		f.clock.advance(10 * 24 * time.Hour)

		_, refreshed, err := f.svc.Authenticate(ctx, token)
		if err != nil {
			t.Fatalf("at %v: %v", f.clock.Now().Sub(start), err)
		}

		if refreshed.ExpiresAt.After(start.Add(service.SessionMaxAge)) {
			t.Fatalf("extended beyond the maximum age: %v", refreshed.ExpiresAt)
		}
	}

	f.clock.advance(10 * 24 * time.Hour)

	_, _, err := f.svc.Authenticate(ctx, token)
	wantIs(t, err, service.ErrNoSession)
}

func TestAuthDisabledAccount(t *testing.T) {
	t.Parallel()

	f, token := signedInFixture(t)
	ctx := t.Context()

	acc := f.account("ada@example.org")
	acc.DisabledAt = f.clock.Now()

	err := f.st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Accounts().Update(ctx, acc)
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = f.svc.Authenticate(ctx, token)
	wantIs(t, err, service.ErrNoSession)

	_, err = f.svc.Login(ctx, "ada@example.org", "correct horse")
	wantIs(t, err, service.ErrInvalidCredentials)
}

func TestAuthRehashOnLogin(t *testing.T) {
	t.Parallel()

	weak, err := password.NewWithParams(cheapParams, nil)
	if err != nil {
		t.Fatal(err)
	}

	stronger := cheapParams
	stronger.Time = 2

	strong, err := password.NewWithParams(stronger, nil)
	if err != nil {
		t.Fatal(err)
	}

	f := newAuthFixtureWith(t, weak)
	f.register("ada@example.org", "correct horse")

	if _, err := f.svc.Confirm(t.Context(), f.lastToken(), "correct horse"); err != nil {
		t.Fatal(err)
	}

	// The same store with a hasher that uses stronger parameters.
	renderer, _ := mail.NewRenderer()
	svc := service.NewAuthService(f.st, f.clock, ids.NewSequence(), strong, renderer, appURL)

	if _, err := svc.Login(t.Context(), "ada@example.org", "correct horse"); err != nil {
		t.Fatal(err)
	}

	acc := f.account("ada@example.org")

	err = f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		c, err := tx.Credentials().Get(ctx, acc.ID)
		if err == nil && strong.NeedsRehash(c.PasswordHash) {
			t.Fatalf("hash %q was not upgraded", c.PasswordHash)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthPurgeExpired(t *testing.T) {
	t.Parallel()

	f, _ := signedInFixture(t)
	f.register("bea@example.org", "correct horse") // a pending link

	f.clock.advance(service.SessionIdleTimeout)

	n, err := f.svc.PurgeExpired(t.Context())
	if err != nil || n != 2 {
		t.Fatalf("PurgeExpired = %d, %v; want the session and the link", n, err)
	}
}

func TestAuthRegisterLeavesSchoolAccountsAlone(t *testing.T) {
	t.Parallel()

	// An unconfirmed account without a password was created by the school,
	// not by a registration.
	f := newAuthFixture(t)

	p := domain.Person{ID: ids.New[domain.PersonID](ids.NewSequence()), GivenName: "Olga", FamilyName: "Office"}
	acc := domain.Account{ID: ids.New[domain.AccountID](ids.NewSequence()), PersonID: p.ID, Email: "olga@example.org", Role: domain.RoleOffice}

	err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Persons().Create(ctx, p), tx.Accounts().Create(ctx, acc))
	})
	if err != nil {
		t.Fatal(err)
	}

	f.register("olga@example.org", "attacker pass")

	if n := len(f.mails()); n != 0 {
		t.Fatalf("want no mail, got %d", n)
	}

	err = f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		got, err := tx.Persons().Get(ctx, p.ID)
		if err == nil && got != p {
			t.Errorf("person changed to %+v", got)
		}

		if _, err := tx.Credentials().Get(ctx, acc.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("a password was set: %v", err)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthRegisterMisconfigurationIsInternal(t *testing.T) {
	t.Parallel()

	hasher, _ := password.NewWithParams(cheapParams, nil)
	renderer, _ := mail.NewRenderer()
	f := newAuthFixture(t)
	svc := service.NewAuthService(f.st, f.clock, ids.NewSequence(), hasher, renderer, "") // no app URL

	err := svc.Register(t.Context(), service.Registration{
		GivenName: "Ada", FamilyName: "L", Email: "ada@example.org", Password: "correct horse",
	})
	if err == nil || errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want an internal error, got %v", err)
	}
}
