package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// InviteTokenTTL is how long the link of an invitation is valid. After
// that, "forgot password" sends the invited account a new link.
const InviteTokenTTL = 72 * time.Hour

var (
	// ErrPersonHasAccount rejects creating an account for a person who has
	// one.
	ErrPersonHasAccount = fmt.Errorf("%w: the person has an account already", domain.ErrDuplicate)
	// ErrEmailInUse rejects creating an account with an email address
	// another account has. The office is trusted, so unlike the public
	// flows (ADR 0006) it is told.
	ErrEmailInUse = fmt.Errorf("%w: the email address belongs to another account", domain.ErrDuplicate)
	// ErrAdminOnly rejects an office account creating an admin account.
	ErrAdminOnly = fmt.Errorf("%w: only an admin may create admin accounts", domain.ErrForbidden)
)

// NewAccount is the input of AuthService.CreateAccount.
type NewAccount struct {
	Email string
	Role  domain.Role
}

// CreateAccount creates an account for an existing person, e.g. a dependent
// who grows up (ADR 0008), and mails it an invitation. The account starts
// unconfirmed and without a password; the invitation's link is a
// password-reset link valid for InviteTokenTTL, and setting the password
// with it confirms the account. Only an admin caller may create an admin.
// An unknown person is domain.ErrNotFound; a person with an account is
// ErrPersonHasAccount and a taken email ErrEmailInUse. If a concurrent
// request creates such an account first, that is domain.ErrDuplicate.
func (s *AuthService) CreateAccount(
	ctx context.Context, caller Principal, person domain.PersonID, a NewAccount,
) (domain.Account, error) {
	if a.Role == domain.RoleAdmin && caller.Role != domain.RoleAdmin {
		return domain.Account{}, ErrAdminOnly
	}

	acc := domain.Account{
		ID: ids.New[domain.AccountID](s.ids), PersonID: person, Email: strings.TrimSpace(a.Email), Role: a.Role,
	}

	token := newToken()

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// The account makes a dependent managed: under the lock, a
		// guardian's edit or removal of the person waits for it.
		if _, err := lockLivePerson(ctx, tx, person); err != nil {
			return err
		}

		if err := acc.Validate(); err != nil {
			return err
		}

		switch _, err := tx.Accounts().GetByPerson(ctx, person); {
		case err == nil:
			return ErrPersonHasAccount
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}

		switch _, err := tx.Accounts().GetByEmail(ctx, acc.Email); {
		case err == nil:
			return ErrEmailInUse
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}

		switch err := tx.Accounts().Create(ctx, acc); {
		case errors.Is(err, domain.ErrDuplicate):
			// A concurrent request created an account for the address
			// after the check above; the person is locked.
			return fmt.Errorf("the email address got an account meanwhile: %w", err)
		case errors.Is(err, domain.ErrConflict):
			// The person was there a moment ago.
			return errPersonGone
		case err != nil:
			return err
		}

		return s.sendLink(ctx, tx, acc, token, linkMail{
			purpose: domain.TokenResetPassword, to: acc.Email, ttl: InviteTokenTTL,
			template: mail.TemplateInvite, route: "reset-password",
		})
	})
	if err != nil {
		return domain.Account{}, fmt.Errorf("create account for person %s: %w", person, err)
	}

	return acc, nil
}

// mayResetPassword reports whether acc may set its password with a reset
// link: it may sign in, or it is an invited account, created by the school
// without a password and not yet confirmed. An unconfirmed registration has
// a password and confirms with its own link instead.
func mayResetPassword(ctx context.Context, tx store.Tx, acc domain.Account) (bool, error) {
	switch {
	case mayActAs(acc):
		return true, nil
	case !acc.DisabledAt.IsZero() || !acc.ConfirmedAt.IsZero():
		return false, nil
	}

	_, err := tx.Credentials().Get(ctx, acc.ID)

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return true, nil
	case err != nil:
		return false, err
	}

	return false, nil
}
