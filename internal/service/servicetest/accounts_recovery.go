package servicetest

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The scenarios of the command line's account administration (ADR 0020),
// run with Credentials.

// accountByRef: AccountByRef finds an account by its ID or its email in
// any case, with its person, and an unknown one is not found.
func accountByRef(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()

	for _, ref := range []string{f.member.ID.String(), "  MIA@example.org "} {
		a, err := svc.AccountByRef(ctx, ref)
		if err != nil || a.Account.ID != f.member.ID || a.Person.GivenName != "Mia" {
			t.Errorf("AccountByRef(%q) = %+v, %v", ref, a, err)
		}
	}

	for _, ref := range []string{"nobody@example.org", newID[domain.AccountID](w).String()} {
		if _, err := svc.AccountByRef(ctx, ref); !errors.Is(err, domain.ErrNotFound) ||
			!strings.Contains(err.Error(), "no account has the email or ID") {
			t.Errorf("AccountByRef(%q): %v, want not found", ref, err)
		}
	}
}

// tokenOf returns the token of a reset link.
func tokenOf(t *testing.T, link string) string {
	t.Helper()

	u, err := url.Parse(link)
	if err != nil || !strings.HasPrefix(link, "https://school.example.org/reset-password?token=") {
		t.Fatalf("link %q: %v", link, err)
	}

	return u.Query().Get("token")
}

// passwordResetLink: IssuePasswordReset returns a link that sets the
// password, valid for an hour, and mails nothing; a newer link replaces it.
// With mailIt it mails the link instead of returning it. An invited account
// gets one too; a disabled account, an unconfirmed registration and an
// unknown account do not.
func passwordResetLink(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()

	first, err := svc.IssuePasswordReset(ctx, f.admin.AccountID, false)
	if err != nil {
		t.Fatal(err)
	}

	second, err := svc.IssuePasswordReset(ctx, f.admin.AccountID, false)
	if err != nil {
		t.Fatal(err)
	}

	if second.Mailed || !second.ExpiresAt.Equal(now.Add(service.ResetTokenTTL)) ||
		second.Account.Account.ID != f.admin.AccountID || second.Account.Person.GivenName != "Ada" {
		t.Errorf("reset %+v, want a link of Ada's account for an hour", second)
	}

	err = svc.ResetPassword(ctx, tokenOf(t, first.Link), "a new fictional secret")
	if !errors.Is(err, service.ErrInvalidToken) {
		t.Errorf("the replaced link: %v, want it invalid", err)
	}

	if err := svc.ResetPassword(ctx, tokenOf(t, second.Link), "a new fictional secret"); err != nil {
		t.Errorf("the link: %v", err)
	}

	resetMails := func() []domain.OutboxMail {
		return read(w, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
			return tx.Outbox().List(ctx, store.OutboxFilter{Template: mail.TemplateResetPassword, Limit: 10})
		})
	}

	if n := len(resetMails()); n != 0 {
		t.Errorf("%d reset mails, want none without mailIt", n)
	}

	mailed, err := svc.IssuePasswordReset(ctx, f.member.ID, true)
	if err != nil || !mailed.Mailed || mailed.Link != "" || !mailed.ExpiresAt.Equal(now.Add(service.ResetTokenTTL)) {
		t.Errorf("mailed reset %+v, %v", mailed, err)
	}

	if m := resetMails(); len(m) != 1 || m[0].To != f.member.Email ||
		!strings.Contains(m[0].Text, "https://school.example.org/reset-password?token=") {
		t.Errorf("reset mails %+v, want one with the link to Mia", m)
	}

	invited := w.account(w.person("Ivo", "Invited", ""), "ivo@example.org")
	invited.ConfirmedAt = time.Time{}
	registered := w.account(w.person("Rea", "Registered", ""), "rea@example.org")
	registered.ConfirmedAt = time.Time{}
	w.write(func(ctx context.Context, tx store.Tx) error {
		if err := tx.Accounts().Update(ctx, invited); err != nil {
			return err
		}

		if err := tx.Accounts().Update(ctx, registered); err != nil {
			return err
		}

		return tx.Credentials().Put(ctx, domain.Credential{AccountID: registered.ID, PasswordHash: "x", ChangedAt: now})
	})

	if _, err := svc.IssuePasswordReset(ctx, invited.ID, false); err != nil {
		t.Errorf("invited: %v", err)
	}

	if _, err := svc.SetAccountDisabled(ctx, f.admin, f.member.ID, true); err != nil {
		t.Fatal(err)
	}

	for what, c := range map[string]struct {
		id   domain.AccountID
		want error
	}{
		"disabled":   {f.member.ID, service.ErrNoPasswordReset},
		"registered": {registered.ID, service.ErrNoPasswordReset},
		"unknown":    {newID[domain.AccountID](w), domain.ErrNotFound},
	} {
		if _, err := svc.IssuePasswordReset(ctx, c.id, false); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", what, err, c.want)
		}
	}
}

// revokeSessions: RevokeSessions ends the sessions of one account, its own
// and the impersonations of it, counts the running ones and audits a
// running impersonation as ended; RevokeAllSessions ends every account's.
func revokeSessions(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()

	session := func(acc, impersonator domain.AccountID, token string, expires time.Time) domain.Session {
		s := domain.Session{
			ID: newID[domain.SessionID](w), AccountID: acc, TokenHash: domain.HashToken(token),
			CreatedAt: now.Add(-2 * time.Hour), LastSeenAt: now.Add(-2 * time.Hour), ExpiresAt: expires,
			ImpersonatorID: impersonator,
		}
		w.write(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, s) })

		return s
	}
	live := func(s domain.Session) bool {
		return read(w, func(ctx context.Context, tx store.Tx) (bool, error) {
			_, err := tx.Sessions().GetByToken(ctx, s.TokenHash)
			if errors.Is(err, domain.ErrNotFound) {
				return false, nil
			}

			return err == nil, err
		})
	}

	session(f.member.ID, domain.AccountID{}, "expired", now.Add(-time.Hour))
	session(f.member.ID, f.admin.AccountID, "impersonation", now.Add(time.Hour))

	other := session(f.other.AccountID, domain.AccountID{}, "other", now.Add(time.Hour))

	n, err := svc.RevokeSessions(ctx, f.member.ID)
	if err != nil || n != 2 {
		t.Errorf("RevokeSessions = %d, %v; want the session and the impersonation", n, err)
	}

	if live(f.session) || !live(other) {
		t.Error("the member's session lives, or another account's ended")
	}

	if e := w.audit()[0]; e.Action != domain.AuditImpersonationEnded || e.ActorID != f.admin.AccountID ||
		e.TargetID != f.member.ID {
		t.Errorf("newest audit event %+v, want the impersonation ended", e)
	}

	if _, err := svc.RevokeSessions(ctx, newID[domain.AccountID](w)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown account: %v", err)
	}

	sessions, accounts, err := svc.RevokeAllSessions(ctx)
	if err != nil || sessions != 1 || accounts < 3 || live(other) {
		t.Errorf("RevokeAllSessions = %d of %d, %v; want the other admin's session", sessions, accounts, err)
	}

	if sessions, _, err := svc.RevokeAllSessions(ctx); err != nil || sessions != 0 {
		t.Errorf("RevokeAllSessions again = %d, %v; want none", sessions, err)
	}
}
