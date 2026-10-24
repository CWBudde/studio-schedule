package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office enrolls persons in classes as members. Membership has no effect
// on bookings until Phase 4. The HTTP layer admits only office and admin
// accounts to these methods.

// ClassMemberEntry is a class member with their person.
type ClassMemberEntry struct {
	domain.ClassMember

	Person domain.Person
}

// ClassMembers returns the members of a class with their persons, ordered
// by family name, given name and ID. An unknown class is
// domain.ErrNotFound.
func (s *TimetableService) ClassMembers(ctx context.Context, class domain.ClassID) ([]ClassMemberEntry, error) {
	var entries []ClassMemberEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Classes().Get(ctx, class); err != nil {
			return err
		}

		members, err := tx.Classes().ListMembers(ctx, class)
		if err != nil {
			return err
		}

		entries = make([]ClassMemberEntry, 0, len(members))
		for _, m := range members {
			p, err := tx.Persons().Get(ctx, m.PersonID)
			if err != nil {
				return fmt.Errorf("person of member %s: %w", m.PersonID, err)
			}

			entries = append(entries, ClassMemberEntry{ClassMember: m, Person: p})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list members of class %s: %w", class, err)
	}

	slices.SortFunc(entries, func(a, b ClassMemberEntry) int {
		return cmp.Or(
			cmp.Compare(a.Person.FamilyName, b.Person.FamilyName),
			cmp.Compare(a.Person.GivenName, b.Person.GivenName),
			cmp.Compare(a.PersonID.String(), b.PersonID.String()),
		)
	})

	return entries, nil
}

// errMemberGone is a class or person that a transaction found and that a
// concurrent one deleted before the membership was added.
var errMemberGone = fmt.Errorf("%w: the class or the person was deleted meanwhile", domain.ErrNotFound)

// AddClassMember enrolls a person in a class. An unknown class is
// domain.ErrNotFound, an unknown person a validation error on personId, and
// a person who is a member already domain.ErrDuplicate. A class or person
// deleted by a concurrent request is domain.ErrNotFound as well.
func (s *TimetableService) AddClassMember(ctx context.Context, m domain.ClassMember) (ClassMemberEntry, error) {
	var e ClassMemberEntry

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Classes().Get(ctx, m.ClassID); err != nil {
			return err
		}

		if err := m.Validate(); err != nil {
			return err
		}

		p, err := tx.Persons().Get(ctx, m.PersonID)
		if errors.Is(err, domain.ErrNotFound) {
			return errUnknownPerson
		} else if err != nil {
			return err
		}

		e = ClassMemberEntry{ClassMember: m, Person: p}

		switch err := tx.Classes().AddMember(ctx, m); {
		case errors.Is(err, domain.ErrDuplicate):
			return fmt.Errorf("%s is a member already: %w", m.PersonID, err)
		case errors.Is(err, domain.ErrConflict):
			// The class and the person were there a moment ago.
			return errMemberGone
		default:
			return err
		}
	})
	if err != nil {
		return ClassMemberEntry{}, fmt.Errorf("add member to class %s: %w", m.ClassID, err)
	}

	return e, nil
}

// RemoveClassMember ends a person's membership in a class. A person who is
// not a member, or an unknown class, is domain.ErrNotFound.
func (s *TimetableService) RemoveClassMember(ctx context.Context, class domain.ClassID, person domain.PersonID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Classes().RemoveMember(ctx, class, person)
	})
	if err != nil {
		return fmt.Errorf("remove member %s from class %s: %w", person, class, err)
	}

	return nil
}
