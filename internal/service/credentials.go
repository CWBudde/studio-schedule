package service

import (
	"cmp"
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

// Lifetimes of the credential links (ADR 0006).
const (
	// ResetTokenTTL is how long a password-reset link is valid.
	ResetTokenTTL = time.Hour
	// ChangeEmailTokenTTL is how long the link confirming a new email
	// address is valid.
	ChangeEmailTokenTTL = 24 * time.Hour
)

var (
	// ErrWrongPassword rejects a change of credentials whose current
	// password is wrong.
	ErrWrongPassword = fmt.Errorf("%w: the current password is wrong", domain.ErrValidation)
	// ErrImpersonating rejects changing the credentials of an account an
	// admin is signed in as.
	ErrImpersonating = fmt.Errorf("%w: not while signed in as another account", domain.ErrForbidden)
	// ErrSameEmail rejects changing the email to the one the account has.
	ErrSameEmail = fmt.Errorf("%w: this is the account's email address already", domain.ErrValidation)
	// ErrEmailTaken rejects confirming a new email address that another
	// account got in the meantime.
	ErrEmailTaken = fmt.Errorf("%w: the email address belongs to another account", domain.ErrConflict)
)

// RequestPasswordReset mails a password-reset link to email if it belongs to
// an account that may sign in, or to an invited account whose invitation
// expired. It succeeds either way, so it reveals nothing about which
// accounts exist. A new link replaces older ones. The mail is in the
// account's language, else in locale, else in the school's default.
func (s *AuthService) RequestPasswordReset(ctx context.Context, email, locale string) error {
	token := newToken()

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().GetByEmail(ctx, strings.TrimSpace(email))

		switch {
		case errors.Is(err, domain.ErrNotFound):
			return nil
		case err != nil:
			return err
		}

		if ok, err := mayResetPassword(ctx, tx, acc); err != nil || !ok {
			return err
		}

		return s.sendLink(ctx, tx, acc, token, linkMail{
			purpose: domain.TokenResetPassword, to: acc.Email, ttl: ResetTokenTTL,
			template: mail.TemplateResetPassword, route: "reset-password", locale: locale,
		})
	})
	if err != nil {
		return fmt.Errorf("request password reset: %w", err)
	}

	return nil
}

// ResetPassword sets the password of the account a reset link was sent to.
// It ends every session of the account and does not sign in. The link of
// an invitation confirms the invited account as well: it proves the
// address.
func (s *AuthService) ResetPassword(ctx context.Context, token, password string) error {
	if err := checkPassword("password", password); err != nil {
		return err
	}

	// Check the link before the slow hashing, so invalid links cost nothing.
	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, err := s.resetAccount(ctx, tx, token)

		return err
	})
	if err != nil {
		return err
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, err := s.resetAccount(ctx, tx, token)
		if err != nil {
			return err
		}

		// An unconfirmed account has no password (mayResetPassword); a
		// confirmed one usually has.
		hadPassword := false

		if acc.ConfirmedAt.IsZero() {
			acc.ConfirmedAt = s.clock.Now()
			if err := tx.Accounts().Update(ctx, acc); err != nil {
				return err
			}
		} else {
			_, err := tx.Credentials().Get(ctx, acc.ID)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return err
			}

			hadPassword = err == nil
		}

		return s.setPassword(ctx, tx, acc.ID, hash, domain.SessionID{}, hadPassword)
	})
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}

	return nil
}

// resetAccount returns the account of a valid reset token; it must still be
// allowed to set its password (mayResetPassword).
func (s *AuthService) resetAccount(ctx context.Context, tx store.Tx, token string) (domain.Account, error) {
	_, acc, err := s.linkToken(ctx, tx, token, domain.TokenResetPassword)
	if err != nil {
		return domain.Account{}, err
	}

	switch ok, err := mayResetPassword(ctx, tx, acc); {
	case err != nil:
		return domain.Account{}, err
	case !ok:
		return domain.Account{}, ErrInvalidToken
	}

	return acc, nil
}

// ChangePassword replaces the password of p's account after checking the
// current one. Every other session of the account ends; p's stays.
func (s *AuthService) ChangePassword(ctx context.Context, p Principal, current, password string) error {
	if !p.ImpersonatorID.IsZero() {
		return ErrImpersonating
	}

	if err := checkPassword("newPassword", password); err != nil {
		return err
	}

	cred, err := s.checkCurrentPassword(ctx, p, current)
	if err != nil {
		return err
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		cur, err := tx.Credentials().Get(ctx, p.AccountID)
		if err != nil {
			return err
		}

		if cur.PasswordHash != cred.PasswordHash {
			return ErrWrongPassword // changed in the meantime
		}

		return s.setPassword(ctx, tx, p.AccountID, hash, p.SessionID, true)
	})
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}

	return nil
}

// checkCurrentPassword verifies the password of p's account and returns its
// credential.
func (s *AuthService) checkCurrentPassword(ctx context.Context, p Principal, password string) (domain.Credential, error) {
	var cred domain.Credential

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		cred, err = tx.Credentials().Get(ctx, p.AccountID)
		if errors.Is(err, domain.ErrNotFound) {
			return ErrWrongPassword // no password set yet
		}

		return err
	})
	if err != nil {
		return domain.Credential{}, err
	}

	if err := s.verify(cred, password); errors.Is(err, ErrInvalidCredentials) {
		return domain.Credential{}, ErrWrongPassword
	} else if err != nil {
		return domain.Credential{}, err
	}

	return cred, nil
}

// setPassword stores a new password hash, ends every session acting for
// account except keep (zero: all of them; endSessions audits the
// impersonations it ends), and invalidates every open link: whoever
// requested one may have known the old password. If the account had a
// password (hadPassword, which the caller knows), it is told by mail (ASVS
// 2.2.3); setting the first one (an invitation, the setup link) is no change
// to report.
func (s *AuthService) setPassword(
	ctx context.Context, tx store.Tx, account domain.AccountID, hash string, keep domain.SessionID, hadPassword bool,
) error {
	cred := domain.Credential{AccountID: account, PasswordHash: hash, ChangedAt: s.clock.Now()}
	if err := tx.Credentials().Put(ctx, cred); err != nil {
		return err
	}

	if err := endSessions(ctx, tx, s.ids, s.clock.Now(), account, keep); err != nil {
		return err
	}

	if err := deleteTokens(ctx, tx, account); err != nil {
		return err
	}

	if !hadPassword {
		return nil
	}

	return s.notifyPasswordChanged(ctx, tx, account)
}

// notifyPasswordChanged mails account, in its language, that its password
// was changed. The mail has no link: it tells whoever did not change it to
// contact the school.
func (s *AuthService) notifyPasswordChanged(ctx context.Context, tx store.Tx, account domain.AccountID) error {
	acc, err := tx.Accounts().Get(ctx, account)
	if err != nil {
		return err
	}

	settings, err := tx.Settings().Get(ctx)
	if err != nil {
		return fmt.Errorf("school settings: %w", err)
	}

	person, err := tx.Persons().Get(ctx, acc.PersonID)
	if err != nil {
		return err
	}

	rendered, err := s.mails.Render(cmp.Or(acc.Locale, settings.DefaultLocale), mail.TemplatePasswordChanged,
		mail.PasswordChangedData{SchoolName: settings.SchoolName, GivenName: person.GivenName})
	if err != nil {
		return fmt.Errorf("render %s mail: %v", mail.TemplatePasswordChanged, err) //nolint:errorlint // see sendLink
	}

	return enqueueMail(ctx, tx, s.ids, s.clock.Now(), acc.Email, mail.TemplatePasswordChanged, mailOpts{}, rendered)
}

// deleteTokens invalidates every open link of account.
func deleteTokens(ctx context.Context, tx store.Tx, account domain.AccountID) error {
	for _, purpose := range domain.TokenPurposes() {
		if err := tx.AccountTokens().DeleteByAccount(ctx, account, purpose); err != nil {
			return err
		}
	}

	return nil
}

// RequestEmailChange mails a link to newEmail that moves p's account to that
// address, after checking the account's password. The account keeps its
// address until the link is used. If newEmail already has an account, it
// succeeds without a mail, so it reveals nothing about which accounts exist.
func (s *AuthService) RequestEmailChange(ctx context.Context, p Principal, password, newEmail, locale string) error {
	if !p.ImpersonatorID.IsZero() {
		return ErrImpersonating
	}

	newEmail = strings.TrimSpace(newEmail)
	if strings.EqualFold(newEmail, p.Email) {
		return ErrSameEmail
	}

	if _, err := s.checkCurrentPassword(ctx, p, password); err != nil {
		return err
	}

	token := newToken()

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().Get(ctx, p.AccountID)
		if err != nil {
			return err
		}

		moved := acc
		moved.Email = newEmail

		if err := moved.Validate(); err != nil {
			return atField(err, "newEmail")
		}

		switch _, err := tx.Accounts().GetByEmail(ctx, newEmail); {
		case err == nil:
			return nil // taken; say nothing
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}

		return s.sendLink(ctx, tx, acc, token, linkMail{
			purpose: domain.TokenChangeEmail, to: newEmail, ttl: ChangeEmailTokenTTL,
			template: mail.TemplateChangeEmail, route: "confirm-email-change", locale: locale,
		})
	})
	if err != nil {
		return fmt.Errorf("request email change: %w", err)
	}

	return nil
}

// ConfirmEmailChange moves the account of an email-change link to the new
// address and mails a notice to the old one. The link alone suffices (it
// proves the new address; the password was checked when it was requested),
// so it works in any browser. The notice is in the account's language, else
// in locale, else in the school's default.
func (s *AuthService) ConfirmEmailChange(ctx context.Context, token, locale string) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		t, acc, err := s.linkToken(ctx, tx, token, domain.TokenChangeEmail)
		if err != nil {
			return err
		}

		if !mayActAs(acc) {
			return ErrInvalidToken
		}

		switch other, err := tx.Accounts().GetByEmail(ctx, t.Email); {
		case err == nil && other.ID != acc.ID:
			return ErrEmailTaken
		case err != nil && !errors.Is(err, domain.ErrNotFound):
			return err
		}

		old := acc.Email
		acc.Email = t.Email

		// The address comes from the link, not from a field of the request.
		if err := acc.Validate(); err != nil {
			return atField(err, "")
		}

		switch err := tx.Accounts().Update(ctx, acc); {
		case errors.Is(err, domain.ErrDuplicate):
			return fmt.Errorf("%w (%w)", ErrEmailTaken, err) // taken meanwhile
		case err != nil:
			return err
		}

		// Links sent to the old address, or for another new one, end here.
		if err := deleteTokens(ctx, tx, acc.ID); err != nil {
			return err
		}

		return s.notifyEmailChanged(ctx, tx, acc, old, locale)
	})
	if err != nil {
		return fmt.Errorf("confirm email change: %w", err)
	}

	return nil
}

// notifyEmailChanged mails the old address of acc that its email changed.
func (s *AuthService) notifyEmailChanged(
	ctx context.Context, tx store.Tx, acc domain.Account, old, locale string,
) error {
	settings, err := tx.Settings().Get(ctx)
	if err != nil {
		return fmt.Errorf("school settings: %w", err)
	}

	person, err := tx.Persons().Get(ctx, acc.PersonID)
	if err != nil {
		return err
	}

	rendered, err := s.mails.Render(cmp.Or(acc.Locale, locale, settings.DefaultLocale), mail.TemplateEmailChanged, mail.EmailChangedData{
		SchoolName: settings.SchoolName, GivenName: person.GivenName, NewEmail: acc.Email,
	})
	if err != nil {
		return fmt.Errorf("render %s mail: %v", mail.TemplateEmailChanged, err) //nolint:errorlint // see sendLink
	}

	return enqueueMail(ctx, tx, s.ids, s.clock.Now(), old, mail.TemplateEmailChanged, mailOpts{}, rendered)
}

// linkMail describes a mail with a single-use link.
type linkMail struct {
	purpose  domain.TokenPurpose
	to       string        // the address the link is sent to
	ttl      time.Duration // whole hours
	template string
	route    string // the app route the link opens, e.g. "confirm"
	locale   string // used if the account has none; empty: the school's default
}

// sendLink replaces the account's links of m.purpose with token and writes
// the mail to the outbox.
func (s *AuthService) sendLink(ctx context.Context, tx store.Tx, acc domain.Account, token string, m linkMail) error {
	settings, err := tx.Settings().Get(ctx)
	if err != nil {
		return fmt.Errorf("school settings: %w", err)
	}

	person, err := tx.Persons().Get(ctx, acc.PersonID)
	if err != nil {
		return err
	}

	if err := tx.AccountTokens().DeleteByAccount(ctx, acc.ID, m.purpose); err != nil {
		return err
	}

	now := s.clock.Now()
	t := domain.AccountToken{
		Hash: domain.HashToken(token), AccountID: acc.ID, Purpose: m.purpose,
		Email: m.to, CreatedAt: now, ExpiresAt: now.Add(m.ttl),
	}

	if err := t.Validate(); err != nil {
		return err
	}

	if err := tx.AccountTokens().Create(ctx, t); err != nil {
		return err
	}

	rendered, err := s.mails.Render(cmp.Or(acc.Locale, m.locale, settings.DefaultLocale), m.template, mail.LinkData{
		SchoolName:   settings.SchoolName,
		GivenName:    person.GivenName,
		Link:         s.appURL + m.route + "?token=" + url.QueryEscape(token),
		ExpiresHours: int(m.ttl / time.Hour),
	})
	if err != nil {
		// A misconfiguration (school name, app URL), not the user's input:
		// %v keeps domain.ErrValidation out, so the client gets a 500.
		return fmt.Errorf("render %s mail: %v", m.template, err) //nolint:errorlint // see above
	}

	return enqueueMail(ctx, tx, s.ids, now, m.to, m.template, mailOpts{}, rendered)
}

// linkToken returns a valid, unexpired token of purpose and its account. A
// confirmation or reset token is only valid while the account still has the
// address it was sent to.
func (s *AuthService) linkToken(
	ctx context.Context, tx store.Tx, token string, purpose domain.TokenPurpose,
) (domain.AccountToken, domain.Account, error) {
	t, err := tx.AccountTokens().Get(ctx, domain.HashToken(token))
	if err != nil {
		return domain.AccountToken{}, domain.Account{}, mapTokenErr(err)
	}

	acc, err := tx.Accounts().Get(ctx, t.AccountID)
	if err != nil {
		return domain.AccountToken{}, domain.Account{}, err
	}

	if t.Purpose != purpose || !s.clock.Now().Before(t.ExpiresAt) ||
		(purpose != domain.TokenChangeEmail && t.Email != acc.Email) {
		return domain.AccountToken{}, domain.Account{}, ErrInvalidToken
	}

	return t, acc, nil
}

// atField reports a validation error of an entity about one of its fields
// on the request field that carried the value, e.g. an account's email on
// newEmail; for field "" on the request as a whole, because no field of it
// carried the value. Other errors pass unchanged.
func atField(err error, field string) error {
	var fe *domain.FieldError
	if !errors.As(err, &fe) {
		return err
	}

	if field == "" {
		return fmt.Errorf("%w: %s", domain.ErrValidation, fe.Msg)
	}

	return &domain.FieldError{Field: field, Msg: fe.Msg}
}
