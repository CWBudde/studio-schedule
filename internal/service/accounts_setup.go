package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The setup link of the command line (ADR 0020, amendment): the way back in
// for a school whose admins cannot sign in, such as a first admin whose
// init link expired before they confirmed. Without an admin, --as finds
// nobody to act as, so the operator issues it.

// Setup link refusals, conflicts (exit status 4 on the command line).
var (
	// ErrAdminExists refuses a setup link while an admin can sign in: that
	// admin issues a password-reset link instead, so the setup link never
	// gets round an admin.
	ErrAdminExists = fmt.Errorf("%w: an admin can sign in, and issues a password-reset link instead "+
		"(accounts reset-password)", domain.ErrConflict)
	// ErrAccountConfirmed refuses a setup link for an account that has
	// confirmed its email: it has a password of its own.
	ErrAccountConfirmed = fmt.Errorf("%w: the account is confirmed; it signs in with its password, or "+
		"\"forgot password\" mails it a link", domain.ErrConflict)
)

// IssueSetupLink returns a new setup link for the account that ref names
// by its email or ID (AccountByRef), as init returns the first admin's: it
// sets the password, and so confirms the account, and is valid for
// InviteTokenTTL. It replaces the account's pending links. It works only
// while no admin can sign in (adminSet, the counting of the last-admin
// guard, under the same person locks), else it is ErrAdminExists, before
// anything else is checked, and changes nothing. The account must not have
// confirmed (ErrAccountConfirmed) and must be an invited one, not disabled
// and without a password of its own (mayResetPassword, else
// ErrNoPasswordReset); an unknown account is domain.ErrNotFound.
//
// No account acts: an operator runs it on the shell. It is audited as
// setup-link-issued without an actor, with via, the command line and the
// OS user, and the link as printed; never the link itself, a secret.
func (s *AuthService) IssueSetupLink(ctx context.Context, ref string, via domain.AuditVia) (PasswordReset, error) {
	ref = strings.TrimSpace(ref)
	token := newToken()

	var out PasswordReset

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var active []domain.Account

		err := lockPersonSet(ctx, tx, func() ([]domain.PersonID, error) {
			persons, admins, err := adminSet(ctx, tx)
			active = admins

			return persons, err
		})

		switch {
		case err != nil:
			return err
		case len(active) > 0:
			return ErrAdminExists
		}

		acc, err := accountOfRef(ctx, tx, ref)
		if err != nil {
			return refError(ref, err)
		}

		if !acc.ConfirmedAt.IsZero() {
			return ErrAccountConfirmed
		}

		switch ok, err := mayResetPassword(ctx, tx, acc); {
		case err != nil:
			return err
		case !ok:
			return ErrNoPasswordReset
		}

		m := linkMail{purpose: domain.TokenResetPassword, to: acc.Email, ttl: InviteTokenTTL, route: "reset-password"}
		if out.Link, out.ExpiresAt, err = s.printLink(ctx, tx, acc, token, m); err != nil {
			return err
		}

		err = s.auditEvent(ctx, tx, domain.AuditEvent{
			Action: domain.AuditSetupLinkIssued, TargetKind: domain.AuditTargetAccount, TargetID: acc.ID,
			Details: domain.AuditDetails{Delivery: domain.LinkPrinted, Via: via},
		})
		if err != nil {
			return err
		}

		out.Account, err = accountEntry(ctx, tx, acc)

		return err
	})
	if err != nil {
		return PasswordReset{}, fmt.Errorf("setup link: %w", err)
	}

	return out, nil
}
