package servicetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Credentials runs the scenarios of the password rules (ADR 0004,
// amendment) and of the command line's account administration (ADR 0020:
// finding an account, reset links, ending sessions) on fresh stores from
// newStore.
func Credentials(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	for name, fn := range map[string]func(t *testing.T, w *world){
		"PasswordChangedMail": passwordChangedMail,
		"CommonPasswords":     commonPasswords,
		"AccountByRef":        accountByRef,
		"PasswordResetLink":   passwordResetLink,
		"RevokeSessions":      revokeSessions,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, newWorld(t, newStore(t)))
		})
	}
}

// resetLink stores a reset link with the token for acc, as
// RequestPasswordReset would mail it.
func (w *world) resetLink(acc domain.Account, token string) {
	w.t.Helper()

	w.write(func(ctx context.Context, tx store.Tx) error {
		return tx.AccountTokens().Create(ctx, domain.AccountToken{
			Hash: domain.HashToken(token), AccountID: acc.ID, Purpose: domain.TokenResetPassword, Email: acc.Email,
			CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		})
	})
}

// passwordChangedMails returns the password-changed mails of the outbox.
func (w *world) passwordChangedMails() []domain.OutboxMail {
	return read(w, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
		return tx.Outbox().List(ctx, store.OutboxFilter{Template: mail.TemplatePasswordChanged, Limit: 10})
	})
}

// passwordChangedMail: changing and resetting a password mail the account
// in its language, in the same transaction; setting the first password (an
// invitation) mails nothing.
func passwordChangedMail(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.auth()
	f := newAdminFixture(w)

	hash, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	w.setPassword(hash, f.member.ID)

	f.member.Locale = "de"
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, f.member) })

	member := service.Principal{
		AccountID: f.member.ID, PersonID: f.member.PersonID, Email: f.member.Email, Role: domain.RoleMember,
		SessionID: f.session.ID,
	}

	// A refused change mails nothing.
	err = svc.ChangePassword(ctx, member, "wrong", "a new fictional secret")
	if !errors.Is(err, service.ErrWrongPassword) {
		t.Errorf("wrong password: %v", err)
	}

	if mails := w.passwordChangedMails(); len(mails) != 0 {
		t.Errorf("mails after a refused change: %+v", mails)
	}

	if err := svc.ChangePassword(ctx, member, "correct horse", "a new fictional secret"); err != nil {
		t.Fatalf("change: %v", err)
	}

	mails := w.passwordChangedMails()
	if len(mails) != 1 || mails[0].To != "mia@example.org" ||
		mails[0].Subject != "Dein Passwort bei "+w.settings.SchoolName+" wurde geändert" {
		t.Fatalf("mails after the change %+v", mails)
	}

	// A reset mails again, in the school's language without the account's.
	f.member.Locale = ""
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, f.member) })
	w.resetLink(f.member, "mia-reset")

	if err := svc.ResetPassword(ctx, "mia-reset", "yet another fictional secret"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	mails = w.passwordChangedMails()
	if len(mails) != 2 {
		t.Fatalf("mails after the reset %+v", mails)
	}

	for _, m := range mails {
		if !m.ReceiptID.IsZero() || m.To != "mia@example.org" {
			t.Errorf("mail %+v", m)
		}
	}

	// An invited account sets its first password with the same link: no
	// change to report.
	invited := w.account(w.person("Ivo", "Invited", ""), "ivo@example.org")
	w.resetLink(invited, "ivo-invite")

	if err := svc.ResetPassword(ctx, "ivo-invite", "a fictional first secret"); err != nil {
		t.Fatalf("first password: %v", err)
	}

	if mails := w.passwordChangedMails(); len(mails) != 2 {
		t.Errorf("mails after a first password %+v", mails)
	}
}

// commonPasswords: a password among the most common ones, in any case, is a
// field error on the field that carried it and changes nothing.
func commonPasswords(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.auth()
	f := newAdminFixture(w)

	hash, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	w.setPassword(hash, f.member.ID)

	member := service.Principal{
		AccountID: f.member.ID, PersonID: f.member.PersonID, Email: f.member.Email, Role: domain.RoleMember,
		SessionID: f.session.ID,
	}

	wantField := func(what string, err error, field string) {
		t.Helper()

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: %v, want a field error on %s", what, err, field)
		}
	}

	wantField("change", svc.ChangePassword(ctx, member, "correct horse", "Password1"), "newPassword")

	w.resetLink(f.member, "mia-reset")
	wantField("reset", svc.ResetPassword(ctx, "mia-reset", "SUNSHINE"), "password")

	wantField("register", svc.Register(ctx, service.Registration{
		GivenName: "Rae", FamilyName: "Register", Email: "rae@example.org", Password: "qwertyuiop",
	}), "password")

	if mails := w.passwordChangedMails(); len(mails) != 0 {
		t.Errorf("mails %+v", mails)
	}

	// The old password still works: nothing changed.
	if _, err := svc.Login(ctx, "mia@example.org", "correct horse"); err != nil {
		t.Errorf("login with the old password: %v", err)
	}
}
