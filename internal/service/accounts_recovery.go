package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
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
		var (
			acc domain.Account
			err error
		)

		if id, perr := domain.ParseID[domain.AccountID](ref); perr == nil {
			acc, err = tx.Accounts().Get(ctx, id)
		} else {
			acc, err = tx.Accounts().GetByEmail(ctx, ref)
		}

		if err != nil {
			return err
		}

		out, err = accountEntry(ctx, tx, acc)

		return err
	})

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return AccountEntry{}, fmt.Errorf("%w: no account has the email or ID %q", domain.ErrNotFound, ref)
	case err != nil:
		return AccountEntry{}, fmt.Errorf("account %q: %w", ref, err)
	}

	return out, nil
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
// ErrNoPasswordReset; an unknown account is domain.ErrNotFound. The link
// is a secret: it is never logged.
func (s *AuthService) IssuePasswordReset(ctx context.Context, id domain.AccountID, mailIt bool) (PasswordReset, error) {
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

		if mailIt {
			err = s.sendLink(ctx, tx, acc, token, m)
			out.ExpiresAt, out.Mailed = s.clock.Now().Add(m.ttl), true
		} else {
			var t domain.AccountToken

			t, err = s.storeLink(ctx, tx, acc, token, m)
			out.Link, out.ExpiresAt = s.appURL+m.route+"?token="+url.QueryEscape(token), t.ExpiresAt
		}

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
// yet. The account may sign in again. An unknown account is
// domain.ErrNotFound.
func (s *AuthService) RevokeSessions(ctx context.Context, id domain.AccountID) (int, error) {
	var n int

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Accounts().Get(ctx, id); err != nil {
			return err
		}

		var err error

		n, err = endSessionsCounted(ctx, tx, s.ids, s.clock.Now(), id, domain.SessionID{})

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("end the sessions of account %s: %w", id, err)
	}

	return n, nil
}

// RevokeAllSessions ends the sessions of every account (RevokeSessions),
// e.g. after a leak, and returns how many had not ended yet, and of how
// many accounts.
func (s *AuthService) RevokeAllSessions(ctx context.Context) (sessions, accounts int, err error) {
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

		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("end every session: %w", err)
	}

	return sessions, accounts, nil
}
