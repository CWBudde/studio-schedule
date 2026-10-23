package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// ImpersonationMaxAge ends an impersonation this long after it started; it
// is never extended (ADR 0005).
const ImpersonationMaxAge = time.Hour

var (
	// ErrNotImpersonable rejects impersonating oneself, another admin, or
	// impersonating from within an impersonation.
	ErrNotImpersonable = fmt.Errorf("%w: this account cannot be impersonated", domain.ErrForbidden)
	// ErrAccountInactive rejects impersonating an account that may not sign
	// in: disabled, or its email not confirmed.
	ErrAccountInactive = fmt.Errorf("%w: the account is disabled or not confirmed", domain.ErrConflict)
	// ErrNotImpersonating rejects returning from an impersonation that is
	// none.
	ErrNotImpersonating = fmt.Errorf("%w: not signed in as another account", domain.ErrConflict)
)

// Impersonate signs admin in as the account target ("login as"): the admin's
// session ends and a session of target with the admin as impersonator takes
// its place, for at most ImpersonationMaxAge. Only admins may impersonate,
// and not other admins. The start is recorded in the audit log.
func (s *AuthService) Impersonate(ctx context.Context, admin Principal, target domain.AccountID) (SignedIn, error) {
	if admin.Role != domain.RoleAdmin || !admin.ImpersonatorID.IsZero() || admin.AccountID == target {
		return SignedIn{}, ErrNotImpersonable
	}

	token, err := newToken()
	if err != nil {
		return SignedIn{}, err
	}

	var out SignedIn

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().Get(ctx, target)

		switch {
		case err != nil:
			return err
		case acc.Role == domain.RoleAdmin:
			return ErrNotImpersonable
		case !mayActAs(acc):
			return ErrAccountInactive
		}

		if err := s.endSession(ctx, tx, admin.SessionID); err != nil {
			return err
		}

		now := s.clock.Now()

		sess, err := s.createSession(ctx, tx, acc.ID, token, now, admin.AccountID)
		if err != nil {
			return err
		}

		out = SignedIn{Token: token, Principal: principal(acc, sess), ExpiresAt: sess.ExpiresAt}

		return s.audit(ctx, tx, admin.AccountID, domain.AuditImpersonationStarted, acc.ID)
	})
	if err != nil {
		return SignedIn{}, fmt.Errorf("impersonate %s: %w", target, err)
	}

	return out, nil
}

// StopImpersonation ends the impersonation p and signs its admin in again
// with a new session; the admin's old cookie is gone (only its hash was
// stored). The end is recorded in the audit log.
func (s *AuthService) StopImpersonation(ctx context.Context, p Principal) (SignedIn, error) {
	if p.ImpersonatorID.IsZero() {
		return SignedIn{}, ErrNotImpersonating
	}

	token, err := newToken()
	if err != nil {
		return SignedIn{}, err
	}

	var out SignedIn

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		admin, err := tx.Accounts().Get(ctx, p.ImpersonatorID)
		if err != nil {
			return err
		}

		if admin.Role != domain.RoleAdmin || !mayActAs(admin) {
			return ErrNoSession
		}

		if err := s.endSession(ctx, tx, p.SessionID); err != nil {
			return err
		}

		sess, err := s.createSession(ctx, tx, admin.ID, token, s.clock.Now(), domain.AccountID{})
		if err != nil {
			return err
		}

		out = SignedIn{Token: token, Principal: principal(admin, sess), ExpiresAt: sess.ExpiresAt}

		return s.audit(ctx, tx, admin.ID, domain.AuditImpersonationEnded, p.AccountID)
	})
	if err != nil {
		return SignedIn{}, fmt.Errorf("stop impersonation: %w", err)
	}

	return out, nil
}

// endSession deletes the session of the request; if it is gone already, the
// request no longer counts as signed in.
func (s *AuthService) endSession(ctx context.Context, tx store.Tx, id domain.SessionID) error {
	err := tx.Sessions().Delete(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return ErrNoSession
	}

	return err
}

// audit records that actor did action to target.
func (s *AuthService) audit(
	ctx context.Context, tx store.Tx, actor domain.AccountID, action domain.AuditAction, target domain.AccountID,
) error {
	e := domain.AuditEvent{
		ID: ids.New[domain.AuditEventID](s.ids), At: s.clock.Now(), ActorID: actor, Action: action, TargetID: target,
	}
	if err := e.Validate(); err != nil {
		return err
	}

	return tx.Audit().Create(ctx, e)
}

// AccountEntry is an account with its person, as listed for admins.
type AccountEntry struct {
	Account domain.Account
	Person  domain.Person
}

// Accounts returns every account with its person, ordered by email. The
// API allows it to admins only.
func (s *AuthService) Accounts(ctx context.Context) ([]AccountEntry, error) {
	var out []AccountEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		accounts, err := tx.Accounts().List(ctx)
		if err != nil {
			return err
		}

		persons, err := tx.Persons().List(ctx)
		if err != nil {
			return err
		}

		byID := make(map[domain.PersonID]domain.Person, len(persons))
		for _, p := range persons {
			byID[p.ID] = p
		}

		out = make([]AccountEntry, 0, len(accounts))

		for _, a := range accounts {
			p, ok := byID[a.PersonID]
			if !ok {
				return fmt.Errorf("person %s of account %s: %w", a.PersonID, a.ID, domain.ErrNotFound)
			}

			out = append(out, AccountEntry{Account: a, Person: p})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}

	return out, nil
}
