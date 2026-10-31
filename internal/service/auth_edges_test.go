package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The edges of the account services that the happy paths and the store
// sweeps leave out: a failing password hasher, a concurrent change between
// the slow password check and the transaction that relies on it, a broken
// mail template, and the inputs the services turn away before the store.

// hookHasher is the cheap hasher with failures and hooks to inject: hashErr
// and verifyErr fail the calls, onHash and onVerify run first (to change
// the data while the service hashes, as a concurrent request could).
type hookHasher struct {
	service.PasswordHasher

	mu                 sync.Mutex
	hashErr, verifyErr error
	onHash, onVerify   func()
}

func (h *hookHasher) Hash(pw string) (string, error) {
	h.mu.Lock()
	hook, err := h.onHash, h.hashErr
	h.mu.Unlock()

	if hook != nil {
		hook()
	}

	if err != nil {
		return "", err
	}

	return h.PasswordHasher.Hash(pw)
}

func (h *hookHasher) Verify(encoded, pw string) (bool, error) {
	h.mu.Lock()
	hook, err := h.onVerify, h.verifyErr
	h.mu.Unlock()

	if hook != nil {
		hook()
	}

	if err != nil {
		return false, err
	}

	return h.PasswordHasher.Verify(encoded, pw)
}

func (h *hookHasher) set(change func(h *hookHasher)) {
	h.mu.Lock()
	defer h.mu.Unlock()

	change(h)
}

func newHookFixture(t *testing.T) (*authFixture, *hookHasher) {
	t.Helper()

	cheap, err := password.NewWithParams(cheapParams, nil)
	if err != nil {
		t.Fatal(err)
	}

	h := &hookHasher{PasswordHasher: cheap}

	return newAuthFixtureWith(t, h), h
}

var errHasher = errors.New("hasher out of memory")

// wantInternal checks that err is err of the hasher, not one of the
// errors the client is told about.
func wantInternal(t *testing.T, err, cause error) {
	t.Helper()

	if !errors.Is(err, cause) || errors.Is(err, domain.ErrValidation) ||
		errors.Is(err, service.ErrInvalidCredentials) || errors.Is(err, service.ErrWrongPassword) {
		t.Fatalf("want the internal %v, got %v", cause, err)
	}
}

// replacePassword stores pw as the password of email directly, as a
// concurrent request would.
func (f *authFixture) replacePassword(email, pw string) {
	f.t.Helper()

	hash, err := f.hasher.Hash(pw)
	if err != nil {
		f.t.Fatal(err)
	}

	acc := f.account(email)

	err = f.st.InTx(f.t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Credentials().Put(ctx, domain.Credential{AccountID: acc.ID, PasswordHash: hash, ChangedAt: f.clock.Now()})
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// sessionsOf counts the sessions of the account of email.
func (f *authFixture) sessionsOf(email string) int {
	f.t.Helper()

	acc := f.account(email)

	var n int

	err := f.st.InTx(f.t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// DeleteByAccount in a transaction that is rolled back counts them.
		ended, err := tx.Sessions().DeleteByAccount(ctx, acc.ID, domain.SessionID{})
		n = len(ended)

		if err != nil {
			return err
		}

		return errRollback
	})
	if !errors.Is(err, errRollback) {
		f.t.Fatal(err)
	}

	return n
}

var errRollback = errors.New("roll back")

func TestAuthHasherFailures(t *testing.T) {
	t.Parallel()

	const pw = "correct horse"

	t.Run("register", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		h.set(func(h *hookHasher) { h.hashErr = errHasher })

		err := f.svc.Register(t.Context(), service.Registration{
			GivenName: "Bea", FamilyName: "Example", Email: "bea@example.org", Password: pw,
		})
		wantInternal(t, err, errHasher)

		if n := len(f.mails()); n != 0 {
			t.Errorf("%d mails, want none", n)
		}
	})

	// An unknown address verifies against a dummy hash, made on first use.
	t.Run("dummy hash", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		h.set(func(h *hookHasher) { h.hashErr = errHasher })

		_, err := f.svc.Login(t.Context(), "nobody@example.org", pw)
		wantInternal(t, err, errHasher)
	})

	t.Run("verify", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)
		h.set(func(h *hookHasher) { h.verifyErr = errHasher })

		_, err := f.svc.Login(t.Context(), "bea@example.org", pw)
		wantInternal(t, err, errHasher)

		// The current password of a change, too.
		wantInternal(t, f.svc.ChangePassword(t.Context(), bea.Principal, pw, "a new secret phrase"), errHasher)
	})

	// A login whose outdated hash cannot be renewed fails without a session.
	t.Run("rehash", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		f.signedUp("bea@example.org", domain.RoleMember)
		before := f.sessionsOf("bea@example.org")

		h.set(func(h *hookHasher) { h.hashErr = errHasher })
		svc := service.NewAuthService(f.st, f.clock, f.seq, alwaysRehash{h}, f.renderer, appURL)

		_, err := svc.Login(t.Context(), "bea@example.org", pw)
		wantInternal(t, err, errHasher)

		if n := f.sessionsOf("bea@example.org"); n != before {
			t.Errorf("%d sessions, want %d", n, before)
		}
	})

	// A reset that cannot hash the new password leaves the link usable.
	t.Run("reset", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		f.signedUp("bea@example.org", domain.RoleMember)

		if err := f.svc.RequestPasswordReset(t.Context(), "bea@example.org", "en"); err != nil {
			t.Fatal(err)
		}

		token, _ := f.link(mail.TemplateResetPassword, "reset-password")

		h.set(func(h *hookHasher) { h.hashErr = errHasher })
		wantInternal(t, f.svc.ResetPassword(t.Context(), token, "a new secret phrase"), errHasher)

		h.set(func(h *hookHasher) { h.hashErr = nil })

		if err := f.svc.ResetPassword(t.Context(), token, "a new secret phrase"); err != nil {
			t.Fatalf("the link after the failure: %v", err)
		}
	})

	t.Run("change", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)

		h.set(func(h *hookHasher) { h.hashErr = errHasher })
		wantInternal(t, f.svc.ChangePassword(t.Context(), bea.Principal, pw, "a new secret phrase"), errHasher)

		h.set(func(h *hookHasher) { h.hashErr = nil })
		f.wantLogin("bea@example.org", pw, true)
	})
}

// TestAuthConcurrentPasswordChange: the slow password check runs outside
// the transaction that relies on it; a password changed in between (by a
// reset in another tab, say) wins, and the operation that checked the old
// one is refused without a trace.
func TestAuthConcurrentPasswordChange(t *testing.T) {
	t.Parallel()

	const pw = "correct horse"

	t.Run("confirm", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		f.register("bea@example.org", pw)
		token := f.lastToken()

		h.set(func(h *hookHasher) {
			h.onVerify = func() {
				h.set(func(h *hookHasher) { h.onVerify = nil })
				f.replacePassword("bea@example.org", "someone else's phrase")
			}
		})

		_, err := f.svc.Confirm(t.Context(), token, pw)
		wantIs(t, err, service.ErrInvalidToken)

		if acc := f.account("bea@example.org"); !acc.ConfirmedAt.IsZero() || f.sessionsOf(acc.Email) != 0 {
			t.Errorf("confirmed %v with %d sessions, want neither", acc.ConfirmedAt, f.sessionsOf(acc.Email))
		}
	})

	t.Run("change", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)

		h.set(func(h *hookHasher) {
			h.onHash = func() {
				h.set(func(h *hookHasher) { h.onHash = nil })
				f.replacePassword("bea@example.org", "the reset phrase")
			}
		})

		wantIs(t, f.svc.ChangePassword(t.Context(), bea.Principal, pw, "a new secret phrase"), service.ErrWrongPassword)
		f.wantLogin("bea@example.org", "the reset phrase", true)
		f.wantLogin("bea@example.org", "a new secret phrase", false)

		if n := f.count(mail.TemplatePasswordChanged); n != 0 {
			t.Errorf("%d password-changed mails, want none", n)
		}
	})

	t.Run("delete own account", func(t *testing.T) {
		t.Parallel()

		f, h := newHookFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)

		h.set(func(h *hookHasher) {
			h.onVerify = func() {
				h.set(func(h *hookHasher) { h.onVerify = nil })
				f.replacePassword("bea@example.org", "the reset phrase")
			}
		})

		wantIs(t, f.svc.DeleteOwnAccount(t.Context(), bea.Principal, pw, "en"), service.ErrWrongPassword)
		f.wantLogin("bea@example.org", "the reset phrase", true)

		if n := f.count(mail.TemplateAccountDeleted); n != 0 {
			t.Errorf("%d account-deleted mails, want none", n)
		}
	})
}

// brokenTemplate renders every template but one.
type brokenTemplate struct {
	*mail.Renderer
	template string
}

func (r brokenTemplate) Render(locale, name string, data any) (mail.Rendered, error) {
	if name == r.template {
		return mail.Rendered{}, &domain.FieldError{Field: "schoolName", Msg: "template broken"}
	}

	return r.Renderer.Render(locale, name, data)
}

// TestAuthBrokenNoticeTemplate: a notice that cannot be rendered is a
// misconfiguration, an internal error (not the user's validation error),
// and the change it reports does not happen without it.
func TestAuthBrokenNoticeTemplate(t *testing.T) {
	t.Parallel()

	const pw = "correct horse"

	broken := func(f *authFixture, template string) *service.AuthService {
		return service.NewAuthService(f.st, f.clock, f.seq, f.hasher, brokenTemplate{f.renderer, template}, appURL)
	}

	wantMisconfigured := func(t *testing.T, err error) {
		t.Helper()

		if err == nil || errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "template broken") {
			t.Fatalf("want an internal error, got %v", err)
		}
	}

	t.Run("password changed", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)

		err := broken(f, mail.TemplatePasswordChanged).ChangePassword(t.Context(), bea.Principal, pw, "a new secret phrase")
		wantMisconfigured(t, err)
		f.wantLogin("bea@example.org", pw, true)
	})

	t.Run("email changed", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)

		if err := f.svc.RequestEmailChange(t.Context(), bea.Principal, pw, "bea.new@example.org", "en"); err != nil {
			t.Fatal(err)
		}

		token, _ := f.link(mail.TemplateChangeEmail, "confirm-email-change")

		wantMisconfigured(t, broken(f, mail.TemplateEmailChanged).ConfirmEmailChange(t.Context(), token, "en"))

		if acc := f.account("bea@example.org"); acc.ID != bea.Principal.AccountID {
			t.Errorf("the account moved to %q", acc.Email)
		}
	})

	t.Run("account deleted", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)

		wantMisconfigured(t, broken(f, mail.TemplateAccountDeleted).DeleteOwnAccount(t.Context(), bea.Principal, pw, "en"))
		f.wantLogin("bea@example.org", pw, true)
	})
}

func TestAuthRefusesEarly(t *testing.T) {
	t.Parallel()

	const pw = "correct horse"

	tooLong := strings.Repeat("x", service.MaxPasswordLength+1)

	t.Run("too long a password matches nothing", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		f.register("bea@example.org", pw)
		token := f.lastToken()

		_, err := f.svc.Confirm(t.Context(), token, tooLong)
		wantIs(t, err, service.ErrInvalidCredentials)

		_, err = f.svc.Login(t.Context(), "bea@example.org", tooLong)
		wantIs(t, err, service.ErrInvalidCredentials)

		// The link still confirms with the right password.
		if _, err := f.svc.Confirm(t.Context(), token, pw); err != nil {
			t.Fatal(err)
		}
	})

	// A disabled account's link does not confirm it or sign it in.
	t.Run("confirm a disabled account", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		f.register("bea@example.org", pw)
		f.update("bea@example.org", func(a *domain.Account) { a.DisabledAt = f.clock.Now() })

		_, err := f.svc.Confirm(t.Context(), f.lastToken(), pw)
		wantIs(t, err, service.ErrInvalidCredentials)

		if acc := f.account("bea@example.org"); !acc.ConfirmedAt.IsZero() || f.sessionsOf(acc.Email) != 0 {
			t.Errorf("confirmed %v, %d sessions", acc.ConfirmedAt, f.sessionsOf(acc.Email))
		}
	})

	// A second registration of a pending address with a blank name is a
	// field error and leaves the first one, its link included.
	t.Run("renewal without a name", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		f.register("bea@example.org", pw)
		token := f.lastToken()

		err := f.svc.Register(t.Context(), service.Registration{
			GivenName: " ", FamilyName: "Example", Email: "bea@example.org", Password: "another phrase",
		})
		wantField(t, err, "givenName")

		s, err := f.svc.Confirm(t.Context(), token, pw)
		if err != nil {
			t.Fatalf("the first link: %v", err)
		}

		me, err := f.svc.Me(t.Context(), s.Principal)
		if err != nil || me.Person.GivenName != "Ada" {
			t.Errorf("person %+v, %v; want the first registration's", me.Person, err)
		}
	})

	t.Run("no session token", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)

		_, _, err := f.svc.Authenticate(t.Context(), "")
		wantIs(t, err, service.ErrNoSession)
	})

	// An account without a password (an invited one) has no current
	// password to check.
	t.Run("no current password", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		acc := f.invited("cleo@example.org")
		p := service.Principal{AccountID: acc.ID, PersonID: acc.PersonID, Email: acc.Email, Role: acc.Role}

		wantIs(t, f.svc.ChangePassword(t.Context(), p, "", "a new secret phrase"), service.ErrWrongPassword)
		wantIs(t, f.svc.DeleteOwnAccount(t.Context(), p, "", "en"), service.ErrWrongPassword)
	})

	// An admin whose session ended (signed out in another tab) does not
	// start an impersonation with it.
	t.Run("impersonate from an ended session", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		admin := f.signedUp("ada@example.org", domain.RoleAdmin)
		bea := f.signedUp("bea@example.org", domain.RoleMember)

		if err := f.svc.Logout(t.Context(), admin.Token); err != nil {
			t.Fatal(err)
		}

		_, err := f.svc.Impersonate(t.Context(), admin.Principal, bea.Principal.AccountID)
		wantIs(t, err, service.ErrNoSession)

		if n := f.sessionsOf("bea@example.org"); n != 1 {
			t.Errorf("%d sessions of Bea, want her own only", n)
		}

		if log := f.auditLog(); len(log) != 0 {
			t.Errorf("audit %+v, want nothing", log)
		}
	})
}

// TestAuthRaces simulates what a concurrent request changes between the
// reads of an operation and its write.
func TestAuthRaces(t *testing.T) {
	t.Parallel()

	const pw = "correct horse"

	// The session is gone (logged out) between reading and extending it.
	t.Run("logged out while extending", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)
		f.clock.advance(48 * time.Hour)

		fs, svc := f.faulty()
		fs.FailWith("Sessions.GetByToken", 2, domain.ErrNotFound)

		_, _, err := svc.Authenticate(t.Context(), bea.Token)
		wantIs(t, err, service.ErrNoSession)
	})

	// Another account took the new address after the link was checked:
	// the database's unique email refuses the update.
	t.Run("email taken while confirming", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		bea := f.signedUp("bea@example.org", domain.RoleMember)

		if err := f.svc.RequestEmailChange(t.Context(), bea.Principal, pw, "bea.new@example.org", "en"); err != nil {
			t.Fatal(err)
		}

		token, _ := f.link(mail.TemplateChangeEmail, "confirm-email-change")
		mails := len(f.mails())

		fs, svc := f.faulty()
		fs.FailWith("Accounts.Update", 1, domain.ErrDuplicate)

		err := svc.ConfirmEmailChange(t.Context(), token, "en")
		if !errors.Is(err, service.ErrEmailTaken) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("want ErrEmailTaken, got %v", err)
		}

		if acc := f.account("bea@example.org"); acc.ID != bea.Principal.AccountID || len(f.mails()) != mails {
			t.Errorf("account %q, %d mails; want it unchanged without a notice", acc.Email, len(f.mails()))
		}
	})
}
