package service

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// AuditLogLimit is how many of the latest audit events AuditLog returns.
const AuditLogLimit = 200

// AuditEntry is an audit event with its actor and target account, each with
// its person, or its target person, as listed for admins. Target is zero
// unless the event targets an account, TargetPerson unless it targets a
// person (then with the ID only if the person can no longer be found).
type AuditEntry struct {
	Event        domain.AuditEvent
	Actor        AccountEntry
	Target       AccountEntry
	TargetPerson domain.Person
}

// AuditLog returns the latest AuditLogLimit audit events, newest first, with
// the accounts they name. The API allows it to admins only (ADR 0005).
//
// The named accounts, usually a handful, and their persons are looked up in
// one query each. The database keeps an account an audit event names, and
// its person, so one that cannot be found is an inconsistency; it does not
// hide the log: an account without its person comes back without names, an
// account that cannot be found with only its ID.
func (s *AuthService) AuditLog(ctx context.Context) ([]AuditEntry, error) {
	var out []AuditEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		events, err := tx.Audit().List(ctx, AuditLogLimit)
		if err != nil || len(events) == 0 {
			return err
		}

		ids := make([]domain.AccountID, 0, 2*len(events))

		var personIDs []domain.PersonID

		for _, e := range events {
			if !e.ActorID.IsZero() { // zero: an account its holder deleted (ADR 0016)
				ids = append(ids, e.ActorID)
			}

			if !e.TargetID.IsZero() {
				ids = append(ids, e.TargetID)
			}

			if !e.TargetPersonID.IsZero() {
				personIDs = append(personIDs, e.TargetPersonID)
			}
		}

		accounts, err := auditAccounts(ctx, tx, ids)
		if err != nil {
			return err
		}

		var personByID map[domain.PersonID]domain.Person

		if len(personIDs) > 0 {
			persons, err := tx.Persons().ListByIDs(ctx, personIDs)
			if err != nil {
				return err
			}

			personByID = byID(persons)
		}

		entry := func(id domain.AccountID) AccountEntry {
			if a, ok := accounts[id]; ok {
				return a
			}

			return AccountEntry{Account: domain.Account{ID: id}}
		}

		out = make([]AuditEntry, 0, len(events))
		for _, e := range events {
			ae := AuditEntry{Event: e, Actor: entry(e.ActorID)}

			switch e.TargetKind {
			case domain.AuditTargetAccount:
				ae.Target = entry(e.TargetID)
			case domain.AuditTargetPerson:
				ae.TargetPerson = personByID[e.TargetPersonID]
				ae.TargetPerson.ID = e.TargetPersonID
			}

			out = append(out, ae)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}

	return out, nil
}

// auditAccounts returns the accounts with the given IDs that exist, each
// with its person if that exists.
func auditAccounts(ctx context.Context, tx store.Tx, ids []domain.AccountID) (map[domain.AccountID]AccountEntry, error) {
	accounts, err := tx.Accounts().ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	personIDs := make([]domain.PersonID, 0, len(accounts))
	for _, a := range accounts {
		personIDs = append(personIDs, a.PersonID)
	}

	persons, err := tx.Persons().ListByIDs(ctx, personIDs)
	if err != nil {
		return nil, err
	}

	personByID := make(map[domain.PersonID]domain.Person, len(persons))
	for _, p := range persons {
		personByID[p.ID] = p
	}

	out := make(map[domain.AccountID]AccountEntry, len(accounts))
	for _, a := range accounts {
		out[a.ID] = AccountEntry{Account: a, Person: personByID[a.PersonID]}
	}

	return out, nil
}
