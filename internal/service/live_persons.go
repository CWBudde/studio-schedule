package service

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// A merged person is a tombstone (ADR 0015): it stays for the receipts it
// paid and the audit log, and nothing new may refer to it. Reading it by ID
// still finds it; every write that writes a person, or makes something
// refer to a person the caller names (a pass, a payer, an account, a class
// membership, a staff link, a booking by staff, a guardian link), looks the
// person up with lockLivePersons. The merge turns a person into a tombstone
// under the same person lock, so such a write either sees the tombstone
// (domain.ErrNotFound) or commits before the merge, which then moves what it
// wrote; and no stale copy of a person can revive a tombstone.

// livePerson returns the person id unless it is merged or anonymized: a
// merged person, a tombstone, and an anonymized one (ADR 0016) are
// domain.ErrNotFound for everything but reading them. It takes no lock; a
// write uses lockLivePersons.
func livePerson(ctx context.Context, tx store.Tx, id domain.PersonID) (domain.Person, error) {
	p, err := tx.Persons().Get(ctx, id)
	if err != nil {
		return domain.Person{}, err
	}

	switch {
	case p.Merged():
		return domain.Person{}, fmt.Errorf("person %s is merged: %w", id, domain.ErrNotFound)
	case p.Anonymized():
		return domain.Person{}, fmt.Errorf("person %s is anonymized: %w", id, domain.ErrNotFound)
	}

	return p, nil
}

// lockLivePersons locks the persons ids (PersonRepo.Lock: together, in ID
// order) and returns them, read under the lock, in the order given. An
// unknown person, a tombstone or an anonymized person is
// domain.ErrNotFound. Persons come first
// in the lock order: persons → terms → classes → lessons → passes.
func lockLivePersons(ctx context.Context, tx store.Tx, ids ...domain.PersonID) ([]domain.Person, error) {
	if err := tx.Persons().Lock(ctx, ids...); err != nil {
		return nil, err
	}

	out := make([]domain.Person, len(ids))

	for i, id := range ids {
		p, err := livePerson(ctx, tx, id)
		if err != nil {
			return nil, err
		}

		out[i] = p
	}

	return out, nil
}

// lockLivePerson is lockLivePersons of one person.
func lockLivePerson(ctx context.Context, tx store.Tx, id domain.PersonID) (domain.Person, error) {
	persons, err := lockLivePersons(ctx, tx, id)
	if err != nil {
		return domain.Person{}, err
	}

	return persons[0], nil
}

// lockPersonSet locks the persons collect names, together and in ID order
// (PersonRepo.Lock), and calls collect again under the locks until it names
// no person that is not locked yet, so that what collect read last was read
// under the locks of everyone it named. A person added meanwhile is locked
// in a later call. Persons come first in the lock order.
func lockPersonSet(ctx context.Context, tx store.Tx, collect func() ([]domain.PersonID, error)) error {
	locked := map[domain.PersonID]bool{}

	for {
		ids, err := collect()
		if err != nil {
			return err
		}

		var missing []domain.PersonID

		for _, id := range ids {
			if !locked[id] {
				locked[id] = true

				missing = append(missing, id)
			}
		}

		if len(missing) == 0 {
			return nil
		}

		if err := tx.Persons().Lock(ctx, missing...); err != nil {
			return err
		}
	}
}

// personIDs returns the IDs of persons.
func personIDs(persons []domain.Person) []domain.PersonID {
	return mapSlice(persons, func(p domain.Person) domain.PersonID { return p.ID })
}
