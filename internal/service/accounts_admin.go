package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Account administration (ADR 0016): an admin disables and enables
// accounts, changes their roles and deletes them; a member deletes their
// own account. The person of a deleted account stays. The school always
// keeps one admin who can sign in (an enabled, confirmed admin account:
// mayActAs), and an admin does not lock themself out: they
// do not disable, demote or delete their own account here (another admin
// does). Every change is recorded in the audit log. The HTTP layer admits
// only admins to the administration; the access level is checked there,
// the rules that protect the school here.

var (
	// ErrLastAdmin refuses disabling, demoting or deleting the last admin
	// account that can sign in.
	ErrLastAdmin = fmt.Errorf("%w: the last admin account that can sign in", domain.ErrConflict)
	// ErrOwnAccount refuses an admin's change of their own account in the
	// account administration.
	ErrOwnAccount = fmt.Errorf("%w: an admin does not disable, demote or delete their own account",
		domain.ErrConflict)
	// errAccountRole is a role an account cannot have.
	errAccountRole error = &domain.FieldError{Field: "role", Msg: "invalid account role"}
)

// SetAccountDisabled disables (disabled) or enables the account id for the
// admin p and returns it with its person. Disabling ends every session of
// the account and the impersonations it started (endSessions: each running
// impersonation is audited as ended); it signs in no more (mayActAs). An
// unchanged state changes nothing and is not audited. An unknown account is
// domain.ErrNotFound, p's own account ErrOwnAccount and the last admin who
// can sign in ErrLastAdmin.
func (s *AuthService) SetAccountDisabled(
	ctx context.Context, p Principal, id domain.AccountID, disabled bool,
) (AccountEntry, error) {
	if p.AccountID == id {
		return AccountEntry{}, ErrOwnAccount
	}

	var out AccountEntry

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, admins, err := lockAccount(ctx, tx, id)
		if err != nil {
			return err
		}

		if disabled == !acc.DisabledAt.IsZero() {
			out, err = accountEntry(ctx, tx, acc)

			return err
		}

		action := domain.AuditAccountEnabled
		acc.DisabledAt = time.Time{}

		if disabled {
			if lastAdmin(acc, admins) {
				return ErrLastAdmin
			}

			action, acc.DisabledAt = domain.AuditAccountDisabled, s.clock.Now()
		}

		if err := tx.Accounts().Update(ctx, acc); err != nil {
			return err
		}

		if disabled {
			if err := endSessions(ctx, tx, s.ids, s.clock.Now(), id, domain.SessionID{}); err != nil {
				return err
			}
		}

		if err := s.auditAccount(ctx, tx, p, action, acc.ID, domain.AuditDetails{}); err != nil {
			return err
		}

		out, err = accountEntry(ctx, tx, acc)

		return err
	})
	if err != nil {
		return AccountEntry{}, fmt.Errorf("set account %s disabled=%t: %w", id, disabled, err)
	}

	return out, nil
}

// SetAccountRole gives the account id the role for the admin p and returns
// it with its person. A role is read with every request, so the change
// takes effect at once. The same role changes nothing and is not audited.
// A role an account cannot have (guest, unknown) is a validation error on
// role, an unknown account domain.ErrNotFound, p's own account
// ErrOwnAccount and demoting the last admin who can sign in ErrLastAdmin.
func (s *AuthService) SetAccountRole(
	ctx context.Context, p Principal, id domain.AccountID, role domain.Role,
) (AccountEntry, error) {
	if !role.Valid() || role == domain.RoleGuest {
		return AccountEntry{}, errAccountRole
	}

	if p.AccountID == id {
		return AccountEntry{}, ErrOwnAccount
	}

	var out AccountEntry

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, admins, err := lockAccount(ctx, tx, id)
		if err != nil {
			return err
		}

		if acc.Role != role {
			if role != domain.RoleAdmin && lastAdmin(acc, admins) {
				return ErrLastAdmin
			}

			previous := acc.Role
			acc.Role = role

			if err := tx.Accounts().Update(ctx, acc); err != nil {
				return err
			}

			err := s.auditAccount(ctx, tx, p, domain.AuditAccountRoleChanged, acc.ID,
				domain.AuditDetails{PreviousRole: previous, Role: role})
			if err != nil {
				return err
			}
		}

		out, err = accountEntry(ctx, tx, acc)

		return err
	})
	if err != nil {
		return AccountEntry{}, fmt.Errorf("set role of account %s: %w", id, err)
	}

	return out, nil
}

// DeleteAccount deletes the account id for the admin p; its person stays
// (AccountRepo.Delete). An unknown account is domain.ErrNotFound, p's own
// account ErrOwnAccount, the last admin who can sign in ErrLastAdmin, and
// an account that records name ErrAccountKept (it is disabled instead).
// Its running impersonations are audited as ended (endSessions).
func (s *AuthService) DeleteAccount(ctx context.Context, p Principal, id domain.AccountID) error {
	if p.AccountID == id {
		return ErrOwnAccount
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, admins, err := lockAccount(ctx, tx, id)
		if err != nil {
			return err
		}

		if lastAdmin(acc, admins) {
			return ErrLastAdmin
		}

		if err := deleteAccount(ctx, tx, s.ids, s.clock.Now(), id); err != nil {
			return err
		}

		return s.auditDeleted(ctx, tx, actorOf(p), p.Via, acc)
	})
	if err != nil {
		return fmt.Errorf("delete account %s: %w", id, err)
	}

	return nil
}

// DeleteOwnAccount deletes p's own account after checking its password, and
// mails a confirmation to its address; the person stays. It is refused
// while an admin is signed in as the account (ErrImpersonating), with a
// wrong password (ErrWrongPassword), for the last admin who can sign in
// (ErrLastAdmin), and for an account that records name (ErrAccountKept).
// The mail is in the account's language, else in locale, else in the
// school's default. The audit log records it without an actor.
func (s *AuthService) DeleteOwnAccount(ctx context.Context, p Principal, password, locale string) error {
	if !p.ImpersonatorID.IsZero() {
		return ErrImpersonating
	}

	cred, err := s.checkCurrentPassword(ctx, p, password)
	if err != nil {
		return err
	}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, admins, err := lockAccount(ctx, tx, p.AccountID)
		if err != nil {
			return err
		}

		// The password checked above cannot be gone: only deleting the
		// account deletes it, and that waits for the lock of its person.
		switch cur, err := tx.Credentials().Get(ctx, acc.ID); {
		case err != nil:
			return err
		case cur.PasswordHash != cred.PasswordHash:
			return ErrWrongPassword // changed in the meantime
		}

		if lastAdmin(acc, admins) {
			return ErrLastAdmin
		}

		if err := s.notifyAccountDeleted(ctx, tx, acc, locale); err != nil {
			return err
		}

		if err := deleteAccount(ctx, tx, s.ids, s.clock.Now(), acc.ID); err != nil {
			return err
		}

		return s.auditDeleted(ctx, tx, domain.AccountID{}, p.Via, acc)
	})
	if err != nil {
		return fmt.Errorf("delete own account: %w", err)
	}

	return nil
}

// lockAccount returns the account id, read under the lock of its person.
// When it is an admin's, it also returns the admins who can sign in
// (mayActAs), read under the person locks of every admin account
// (adminSet): two changes that could together leave no admin who can sign
// in wait for each other, and the second sees the first. A change that
// cannot lessen the admins (of a non-admin account: a promotion, say) does
// not wait for them. Persons come first in the lock order.
func lockAccount(ctx context.Context, tx store.Tx, id domain.AccountID) (domain.Account, []domain.Account, error) {
	var (
		acc    domain.Account
		admins []domain.Account
	)

	err := lockPersonSet(ctx, tx, func() ([]domain.PersonID, error) {
		var err error

		if acc, err = tx.Accounts().Get(ctx, id); err != nil {
			return nil, err
		}

		admins = nil

		if acc.Role != domain.RoleAdmin {
			return []domain.PersonID{acc.PersonID}, nil
		}

		persons, active, err := adminSet(ctx, tx)
		admins = active

		return append(persons, acc.PersonID), err
	})
	if err != nil {
		return domain.Account{}, nil, err
	}

	return acc, admins, nil
}

// adminSet returns the persons of every admin account, enabled or not, to
// lock, and the admins who can sign in (mayActAs): an admin account that is
// disabled or not confirmed yet keeps nobody able to administer the school.
func adminSet(ctx context.Context, tx store.Tx) ([]domain.PersonID, []domain.Account, error) {
	all, err := tx.Accounts().ListByRole(ctx, domain.RoleAdmin)
	if err != nil {
		return nil, nil, err
	}

	active := slices.DeleteFunc(slices.Clone(all), func(a domain.Account) bool { return !mayActAs(a) })

	return mapSlice(all, func(a domain.Account) domain.PersonID { return a.PersonID }), active, nil
}

// lastAdmin reports whether acc is the only admin among admins, the admins
// who can sign in (adminSet): disabling, demoting or deleting it would leave
// none. An account that cannot sign in is never the last admin.
func lastAdmin(acc domain.Account, admins []domain.Account) bool {
	return len(admins) == 1 && admins[0].ID == acc.ID
}

// endSessions deletes the sessions that act for the account id except keep
// (zero: all of them), its own and the impersonation sessions it started
// (SessionRepo.DeleteByAccount), and records each impersonation among them
// that was still running as impersonation-ended by the admin who started
// it, as Logout does, with no channel (a zero Via): its actor is the
// impersonator, who did not act through the command line, whoever ended
// the sessions. An impersonation past its end had ended already. The
// account's web push subscriptions go too (ADR 0017): a device someone
// else signed out of, by a new password or by the office, gets no more
// notifications; a session that is kept saves its device again.
func endSessions(
	ctx context.Context, tx store.Tx, src ids.Source, now time.Time, id domain.AccountID, keep domain.SessionID,
) error {
	ended, err := tx.Sessions().DeleteByAccount(ctx, id, keep)
	if err != nil {
		return err
	}

	if _, err := tx.PushSubscriptions().DeleteByAccount(ctx, id); err != nil {
		return err
	}

	for _, sess := range ended {
		if sess.ImpersonatorID.IsZero() || !now.Before(sessionEnd(sess)) {
			continue
		}

		e := domain.AuditEvent{
			ID: ids.New[domain.AuditEventID](src), At: now, ActorID: sess.ImpersonatorID,
			Action: domain.AuditImpersonationEnded, TargetKind: domain.AuditTargetAccount, TargetID: sess.AccountID,
		}
		if err := e.Validate(); err != nil {
			return err
		}

		if err := tx.Audit().Create(ctx, e); err != nil {
			return err
		}
	}

	return nil
}

// deleteAccount ends the sessions of the account id (endSessions) and
// deletes it (AccountRepo.Delete); one that records name is ErrAccountKept.
func deleteAccount(ctx context.Context, tx store.Tx, src ids.Source, now time.Time, id domain.AccountID) error {
	if err := endSessions(ctx, tx, src, now, id, domain.SessionID{}); err != nil {
		return err
	}

	err := tx.Accounts().Delete(ctx, id)
	if errors.Is(err, domain.ErrConflict) {
		return fmt.Errorf("%w (%w)", ErrAccountKept, err)
	}

	return err
}

// accountEntry returns acc with its person.
func accountEntry(ctx context.Context, tx store.Tx, acc domain.Account) (AccountEntry, error) {
	person, err := tx.Persons().Get(ctx, acc.PersonID)
	if err != nil {
		return AccountEntry{}, err
	}

	return AccountEntry{Account: acc, Person: person}, nil
}

// auditAccount records that p changed the account target, through p's
// channel.
func (s *AuthService) auditAccount(
	ctx context.Context, tx store.Tx, p Principal, action domain.AuditAction, target domain.AccountID,
	details domain.AuditDetails,
) error {
	details.Via = p.Via

	e := domain.AuditEvent{
		ID: ids.New[domain.AuditEventID](s.ids), At: s.clock.Now(), ActorID: actorOf(p), Action: action,
		TargetKind: domain.AuditTargetAccount, TargetID: target, Details: details,
	}
	if err := e.Validate(); err != nil {
		return err
	}

	return tx.Audit().Create(ctx, e)
}

// auditDeleted records that actor (zero: its holder) deleted the account
// acc through the channel via, with the account's person as the target.
func (s *AuthService) auditDeleted(
	ctx context.Context, tx store.Tx, actor domain.AccountID, via domain.AuditVia, acc domain.Account,
) error {
	e := domain.AuditEvent{
		ID: ids.New[domain.AuditEventID](s.ids), At: s.clock.Now(), ActorID: actor, Action: domain.AuditAccountDeleted,
		TargetKind: domain.AuditTargetPerson, TargetPersonID: acc.PersonID, Details: domain.AuditDetails{Via: via},
	}
	if err := e.Validate(); err != nil {
		return err
	}

	return tx.Audit().Create(ctx, e)
}

// notifyAccountDeleted mails the address of acc that its holder deleted it.
// It is not a mail with a secret link: it informs.
func (s *AuthService) notifyAccountDeleted(ctx context.Context, tx store.Tx, acc domain.Account, locale string) error {
	settings, err := tx.Settings().Get(ctx)
	if err != nil {
		return fmt.Errorf("school settings: %w", err)
	}

	person, err := tx.Persons().Get(ctx, acc.PersonID)
	if err != nil {
		return err
	}

	rendered, err := s.mails.Render(cmp.Or(acc.Locale, locale, settings.DefaultLocale), mail.TemplateAccountDeleted,
		mail.AccountDeletedData{SchoolName: settings.SchoolName, GivenName: person.GivenName})
	if err != nil {
		return fmt.Errorf("render %s mail: %v", mail.TemplateAccountDeleted, err) //nolint:errorlint // see sendLink
	}

	return enqueueMail(ctx, tx, s.ids, s.clock.Now(), acc.Email, mail.TemplateAccountDeleted, mailOpts{}, rendered)
}
