package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The account administration of the command line (ADR 0020): finding an
// account by its email or ID, a password-reset link for an account, and
// ending its sessions. The command line admits only admins to it; the
// access level is checked there.

// ErrNoPasswordReset refuses a reset link for an account that may not set
// its password: a disabled account, or a registration whose email is not
// confirmed (it confirms with its own link).
var ErrNoPasswordReset = fmt.Errorf("%w: the account may not set a password: it is disabled, or it registered "+
	"and has not confirmed its email", domain.ErrConflict)

// AccountByRef returns the account that ref names, by its ID or its email
// (ignoring case and surrounding spaces), with its person. An unknown
// account is domain.ErrNotFound.
func (s *AuthService) AccountByRef(ctx context.Context, ref string) (AccountEntry, error) {
	ref = strings.TrimSpace(ref)

	var out AccountEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		acc, err := accountOfRef(ctx, tx, ref)
		if err != nil {
			return err
		}

		out, err = accountEntry(ctx, tx, acc)

		return err
	})
	if err != nil {
		return AccountEntry{}, refError(ref, err)
	}

	return out, nil
}

// accountOfRef returns the account that ref, trimmed, names by its ID or
// its email.
func accountOfRef(ctx context.Context, tx store.Tx, ref string) (domain.Account, error) {
	if id, err := domain.ParseID[domain.AccountID](ref); err == nil {
		return tx.Accounts().Get(ctx, id)
	}

	return tx.Accounts().GetByEmail(ctx, ref)
}

// refError is err of the account that ref names: an unknown account says
// so.
func refError(ref string, err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: no account has the email or ID %q", domain.ErrNotFound, ref)
	}

	return fmt.Errorf("account %q: %w", ref, err)
}

// PasswordReset is a password-reset link issued for an account. Link is
// empty when the link was mailed.
type PasswordReset struct {
	Account   AccountEntry
	Link      string
	ExpiresAt time.Time
	Mailed    bool
}

// IssuePasswordReset creates a password-reset link for the account id,
// valid for ResetTokenTTL, and replaces its older ones: the way back in for
// an admin who has forgotten their password. With mailIt, the link is
// mailed to the account's address in its language as RequestPasswordReset
// mails it (the deliver-mail job sends it) and not returned; without, it is
// returned and nothing is mailed. It is issued for an account that may sign
// in and for an invited one (mayResetPassword), else it is
// ErrNoPasswordReset; an unknown account is domain.ErrNotFound. It is
// audited as password-reset-issued by p, with whether the link was mailed
// or printed. The link is a secret: it is never logged or audited.
func (s *AuthService) IssuePasswordReset(
	ctx context.Context, p Principal, id domain.AccountID, mailIt bool,
) (PasswordReset, error) {
	token := newToken()

	var out PasswordReset

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().Get(ctx, id)
		if err != nil {
			return err
		}

		switch ok, err := mayResetPassword(ctx, tx, acc); {
		case err != nil:
			return err
		case !ok:
			return ErrNoPasswordReset
		}

		m := linkMail{
			purpose: domain.TokenResetPassword, to: acc.Email, ttl: ResetTokenTTL,
			template: mail.TemplateResetPassword, route: "reset-password",
		}

		delivery := domain.LinkPrinted

		if mailIt {
			err = s.sendLink(ctx, tx, acc, token, m)
			out.ExpiresAt, out.Mailed, delivery = s.clock.Now().Add(m.ttl), true, domain.LinkMailed
		} else {
			out.Link, out.ExpiresAt, err = s.printLink(ctx, tx, acc, token, m)
		}

		if err != nil {
			return err
		}

		err = s.auditAccount(ctx, tx, p, domain.AuditPasswordResetIssued, acc.ID, domain.AuditDetails{Delivery: delivery})
		if err != nil {
			return err
		}

		out.Account, err = accountEntry(ctx, tx, acc)

		return err
	})
	if err != nil {
		return PasswordReset{}, fmt.Errorf("password reset for account %s: %w", id, err)
	}

	return out, nil
}

// RevokeSessions ends every session that acts for the account id, its own
// and the impersonation sessions it started, as disabling it does
// (endSessions: each running impersonation is audited as ended; its web
// push subscriptions go too), and returns how many of them had not ended
// yet. The account may sign in again. It is audited as sessions-revoked
// by p, with the count. An unknown account is domain.ErrNotFound.
func (s *AuthService) RevokeSessions(ctx context.Context, p Principal, id domain.AccountID) (int, error) {
	var n int

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Accounts().Get(ctx, id); err != nil {
			return err
		}

		var err error

		if n, err = endSessionsCounted(ctx, tx, s.ids, s.clock.Now(), id, domain.SessionID{}); err != nil {
			return err
		}

		return s.auditAccount(ctx, tx, p, domain.AuditSessionsRevoked, id, domain.AuditDetails{Sessions: n})
	})
	if err != nil {
		return 0, fmt.Errorf("end the sessions of account %s: %w", id, err)
	}

	return n, nil
}

// RevokeAllSessions ends the sessions of every account (RevokeSessions),
// e.g. after a leak, and returns how many had not ended yet, and of how
// many accounts. It is audited as one sessions-revoked by p without a
// target, with the count.
func (s *AuthService) RevokeAllSessions(ctx context.Context, p Principal) (sessions, accounts int, err error) {
	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		all, err := tx.Accounts().List(ctx)
		if err != nil {
			return err
		}

		sessions, accounts = 0, len(all)

		for _, acc := range all {
			n, err := endSessionsCounted(ctx, tx, s.ids, s.clock.Now(), acc.ID, domain.SessionID{})
			if err != nil {
				return err
			}

			sessions += n
		}

		return s.auditEvent(ctx, tx, domain.AuditEvent{
			ActorID: actorOf(p), Action: domain.AuditSessionsRevoked,
			Details: domain.AuditDetails{Sessions: sessions, Via: p.Via},
		})
	})
	if err != nil {
		return 0, 0, fmt.Errorf("end every session: %w", err)
	}

	return sessions, accounts, nil
}

// printLink stores token as the account's link of m (storeLink) and
// returns the link and its expiry, for a command that prints it.
func (s *AuthService) printLink(
	ctx context.Context, tx store.Tx, acc domain.Account, token string, m linkMail,
) (string, time.Time, error) {
	t, err := s.storeLink(ctx, tx, acc, token, m)
	if err != nil {
		return "", time.Time{}, err
	}

	return s.appURL + m.route + "?token=" + url.QueryEscape(token), t.ExpiresAt, nil
}

// auditEvent stores e with a new ID at the clock's time, after validating
// it.
func (s *AuthService) auditEvent(ctx context.Context, tx store.Tx, e domain.AuditEvent) error {
	e.ID, e.At = ids.New[domain.AuditEventID](s.ids), s.clock.Now()
	if err := e.Validate(); err != nil {
		return err
	}

	return tx.Audit().Create(ctx, e)
}
