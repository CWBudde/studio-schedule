package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office enrolls persons in classes as members, who are booked into the
// class's lessons automatically (ADR 0009; see enrollment.go). The HTTP layer
// admits only office and admin accounts to these methods.

// ClassMemberEntry is a class member with their person.
type ClassMemberEntry struct {
	domain.ClassMember

	Person domain.Person
}

// ClassMembers returns the members of a class with their persons, ordered
// by family name, given name and ID, with a pause only while it lasts: a
// pause that ended before today is left out. An unknown class is
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

		today, err := schoolToday(ctx, tx, s.clock.Now())
		if err != nil {
			return err
		}

		entries = make([]ClassMemberEntry, 0, len(members))
		for _, m := range members {
			if m.PausedUntil.Compare(today) < 0 {
				m.PausedUntil = domain.Date{} // over
			}

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

// AddClassMember enrolls a person in a class and books them into the
// class's lessons from m.Since on that have not started (ADR 0009; see
// enroll), even beyond a lesson's capacity. p is the office account, the
// actor of the booking events. An unknown class is domain.ErrNotFound, an
// unknown person a validation error on personId, a person who is a member
// already domain.ErrDuplicate, a retired class ErrClassRetired, and a class
// whose members fill its capacity ErrClassFull. A class or person deleted by a concurrent request is
// domain.ErrNotFound as well.
func (s *TimetableService) AddClassMember(ctx context.Context, p Principal, m domain.ClassMember) (ClassMemberEntry, error) {
	var e ClassMemberEntry

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		cur, err := tx.Classes().Get(ctx, m.ClassID)
		if err != nil {
			return err
		}

		if err := m.Validate(); err != nil {
			return err
		}

		person, err := tx.Persons().Get(ctx, m.PersonID)
		if errors.Is(err, domain.ErrNotFound) {
			return errUnknownPerson
		} else if err != nil {
			return err
		}

		e = ClassMemberEntry{ClassMember: m, Person: person}

		// Lock order term → class → lesson: the class lock keeps
		// generation, class edits and other membership changes out, and
		// counts the members reliably.
		if _, err := tx.Terms().Lock(ctx, cur.TermID); err != nil {
			return fmt.Errorf("term of class %s: %w", m.ClassID, err)
		}

		c, err := tx.Classes().Lock(ctx, m.ClassID)
		if err != nil {
			return err
		}

		members, err := tx.Classes().ListMembers(ctx, c.ID)
		if err != nil {
			return err
		}

		// Under the class lock the members cannot change, so the store's
		// own duplicate check cannot fire.
		switch {
		case c.Retired:
			return ErrClassRetired
		case slices.ContainsFunc(members, func(o domain.ClassMember) bool { return o.PersonID == m.PersonID }):
			return fmt.Errorf("%s is a member already: %w", m.PersonID, domain.ErrDuplicate)
		case len(members) >= c.Capacity:
			return ErrClassFull
		}

		switch err := tx.Classes().AddMember(ctx, m); {
		case errors.Is(err, domain.ErrConflict):
			// The person was there a moment ago (the class is locked).
			return errMemberGone
		case err != nil:
			return err
		}

		return s.enroll(ctx, tx, actorOf(p), m)
	})
	if err != nil {
		return ClassMemberEntry{}, fmt.Errorf("add member to class %s: %w", m.ClassID, err)
	}

	return e, nil
}

// UpdateClassMember replaces the dates of a membership: since when the
// person is a member, and the end of their pause (zero for none, which
// ends a pause; ADR 0009). It cancels the person's automatic bookings of
// the class's lessons that have not started and that they are no longer
// active on (see withdraw), then books them into those they are active on
// and have no booking for, as when they were added (see enroll): a date
// they released stays released. p is the office account. A person who is
// not a member, or an unknown class, is domain.ErrNotFound; a retired class
// ErrClassRetired.
func (s *TimetableService) UpdateClassMember(ctx context.Context, p Principal, m domain.ClassMember) (ClassMemberEntry, error) {
	var e ClassMemberEntry

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		cur, err := tx.Classes().Get(ctx, m.ClassID)
		if err != nil {
			return err
		}

		if err := m.Validate(); err != nil {
			return err
		}

		// Lock order term → class → lesson, as in AddClassMember.
		if _, err := tx.Terms().Lock(ctx, cur.TermID); err != nil {
			return fmt.Errorf("term of class %s: %w", m.ClassID, err)
		}

		c, err := tx.Classes().Lock(ctx, m.ClassID)
		if err != nil {
			return err
		}

		if c.Retired {
			return ErrClassRetired
		}

		if err := tx.Classes().UpdateMember(ctx, m); err != nil {
			return err
		}

		person, err := tx.Persons().Get(ctx, m.PersonID)
		if err != nil {
			return err
		}

		e = ClassMemberEntry{ClassMember: m, Person: person}

		today, err := schoolToday(ctx, tx, s.clock.Now())
		if err != nil {
			return err
		}

		if e.PausedUntil.Compare(today) < 0 {
			e.PausedUntil = domain.Date{} // over
		}

		if err := s.withdraw(ctx, tx, actorOf(p), m); err != nil {
			return err
		}

		return s.enroll(ctx, tx, actorOf(p), m)
	})
	if err != nil {
		return ClassMemberEntry{}, fmt.Errorf("update member %s of class %s: %w", m.PersonID, m.ClassID, err)
	}

	return e, nil
}

// RemoveClassMember ends a person's membership in a class and cancels
// their active bookings of the class's lessons that have not started,
// whatever their source, without a mail (ADR 0009; see unenroll). p is the
// office account, the actor of the booking events. A person who is not a
// member, or an unknown class, is domain.ErrNotFound.
func (s *TimetableService) RemoveClassMember(
	ctx context.Context, p Principal, class domain.ClassID, person domain.PersonID,
) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		cur, err := tx.Classes().Get(ctx, class)
		if err != nil {
			return err
		}

		// Lock order term → class → lesson, as in AddClassMember.
		if _, err := tx.Terms().Lock(ctx, cur.TermID); err != nil {
			return fmt.Errorf("term of class %s: %w", class, err)
		}

		if _, err := tx.Classes().Lock(ctx, class); err != nil {
			return err
		}

		if err := tx.Classes().RemoveMember(ctx, class, person); err != nil {
			return err
		}

		return s.unenroll(ctx, tx, actorOf(p), class, person)
	})
	if err != nil {
		return fmt.Errorf("remove member %s from class %s: %w", person, class, err)
	}

	return nil
}

// schoolToday returns the school date of now, or the zero date without
// school settings.
func schoolToday(ctx context.Context, tx store.Tx, now time.Time) (domain.Date, error) {
	_, loc, err := loadSchool(ctx, tx)

	switch {
	case errors.Is(err, ErrNoSchool):
		return domain.Date{}, nil
	case err != nil:
		return domain.Date{}, err
	}

	return domain.DateOf(now, loc), nil
}

// actorOf is the real account of p, recorded as the actor of booking
// events: the admin when impersonating.
func actorOf(p Principal) domain.AccountID { return cmp.Or(p.ImpersonatorID, p.AccountID) }
