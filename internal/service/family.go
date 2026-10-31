package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// ErrDependentInUse reports that a dependent cannot be removed: the caller
// is its last guardian and something other than what makes it managed
// still refers to it, such as bookings or class memberships.
var ErrDependentInUse = fmt.Errorf("%w: the dependent is still in use, e.g. by bookings or class memberships",
	domain.ErrInUse)

// ErrDependentManaged rejects a guardian's change to a managed dependent:
// one with its own account, linked to a staff member, or a guardian
// itself, whose data its owner or the office maintains (ADR 0008).
var ErrDependentManaged = fmt.Errorf("%w: the dependent's data is managed by its own account or the office",
	domain.ErrConflict)

// ErrFutureBirthDate reports a birth date after today in the school time
// zone.
var ErrFutureBirthDate error = &domain.FieldError{Field: "birthDate", Msg: "birth date must not be in the future"}

// PersonData are the fields a guardian edits, for themself or a dependent.
// Optional fields are empty or zero when absent.
type PersonData struct {
	GivenName  string
	FamilyName string
	Phone      string
	BirthDate  domain.Date
}

// Dependent is a person a guardian manages, with its other guardians.
type Dependent struct {
	Person domain.Person
	// Guardians are the dependent's guardians except the caller.
	Guardians []domain.Person
	// Managed is set for a managed dependent: one with its own account,
	// linked to a staff member, or a guardian of someone itself. The
	// guardian may not edit it, and removing it only ends the guardianship.
	Managed bool
}

// Family is the signed-in person and the dependents they manage.
type Family struct {
	Me         domain.Person
	Dependents []Dependent
}

// FamilyService lets signed-in people manage their own person data and their
// dependents: children without their own login, whom they may book for
// (ADR 0008). A guardian sees and edits only their dependents. Linking
// another guardian to a dependent is left to the office.
type FamilyService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
	w     waitlister
}

// NewFamilyService returns a FamilyService. Removing a dependent sends the
// waiting-list invites of the places it frees with mails.
func NewFamilyService(st store.Store, clk clock.Clock, src ids.Source, mails Mails) *FamilyService {
	return &FamilyService{store: st, clock: clk, ids: src, w: waitlister{ids: src, mails: mails}}
}

// Family returns p's person and dependents, ordered by family and given
// name.
func (s *FamilyService) Family(ctx context.Context, p Principal) (Family, error) {
	var f Family

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		if f.Me, err = tx.Persons().Get(ctx, p.PersonID); err != nil {
			return err
		}

		dependents, err := tx.Persons().ListDependents(ctx, p.PersonID)
		if err != nil {
			return err
		}

		for _, d := range dependents {
			guardians, err := tx.Persons().ListGuardians(ctx, d.ID)
			if err != nil {
				return err
			}

			managed, err := isManaged(ctx, tx, d.ID)
			if err != nil {
				return err
			}

			others := slices.DeleteFunc(guardians, func(g domain.Person) bool { return g.ID == p.PersonID })
			f.Dependents = append(f.Dependents, Dependent{Person: d, Guardians: others, Managed: managed})
		}

		return nil
	})
	if err != nil {
		return Family{}, fmt.Errorf("family: %w", err)
	}

	return f, nil
}

// UpdatePerson changes p's own person data and returns the person.
func (s *FamilyService) UpdatePerson(ctx context.Context, p Principal, d PersonData) (domain.Person, error) {
	var person domain.Person

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error
		if person, err = lockLivePerson(ctx, tx, p.PersonID); err != nil {
			return err
		}

		if person, err = s.apply(ctx, tx, person, d); err != nil {
			return err
		}

		return tx.Persons().Update(ctx, person)
	})
	if err != nil {
		return domain.Person{}, fmt.Errorf("update person: %w", err)
	}

	return person, nil
}

// AddDependent creates a person with p as its guardian and returns it.
func (s *FamilyService) AddDependent(ctx context.Context, p Principal, d PersonData) (domain.Person, error) {
	var person domain.Person

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// The guardian is live and stays so (ADR 0015).
		if _, err := lockLivePerson(ctx, tx, p.PersonID); err != nil {
			return err
		}

		var err error
		if person, err = s.apply(ctx, tx, domain.Person{ID: ids.New[domain.PersonID](s.ids)}, d); err != nil {
			return err
		}

		if err := tx.Persons().Create(ctx, person); err != nil {
			return err
		}

		return tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: p.PersonID, DependentID: person.ID})
	})
	if err != nil {
		return domain.Person{}, fmt.Errorf("add dependent: %w", err)
	}

	return person, nil
}

// UpdateDependent changes the person data of one of p's dependents and
// returns the person. A managed dependent is ErrDependentManaged, anyone
// else's person domain.ErrNotFound.
func (s *FamilyService) UpdateDependent(
	ctx context.Context, p Principal, id domain.PersonID, d PersonData,
) (domain.Person, error) {
	var person domain.Person

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := dependentOf(ctx, tx, p.PersonID, id); err != nil {
			return err
		}

		// Under the lock, nothing can make the dependent managed (or merge
		// it, ADR 0015) meanwhile; the person is read under it.
		var err error
		if person, err = lockLivePerson(ctx, tx, id); err != nil {
			return err
		}

		switch managed, err := isManaged(ctx, tx, id); {
		case err != nil:
			return err
		case managed:
			return ErrDependentManaged
		}

		if person, err = s.apply(ctx, tx, person, d); err != nil {
			return err
		}

		return tx.Persons().Update(ctx, person)
	})
	if err != nil {
		return domain.Person{}, fmt.Errorf("update dependent: %w", err)
	}

	return person, nil
}

// RemoveDependent ends p's guardianship of id. If no guardian is left, the
// person is deleted as well, unless it is a managed dependent, which stays.
// Then only bookings, class memberships and passes can still refer to it; while
// they do, that fails with ErrDependentInUse and nothing changes. A deleted
// person leaves the waiting lists first: their open entries are withdrawn,
// and the places their invites held go to the next entries (ADR 0010).
// Anyone else's person is domain.ErrNotFound.
func (s *FamilyService) RemoveDependent(ctx context.Context, p Principal, id domain.PersonID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := dependentOf(ctx, tx, p.PersonID, id); err != nil {
			return err
		}

		// Under the lock, nothing can make the dependent managed, or refer
		// to it as a guardian, meanwhile.
		if err := tx.Persons().Lock(ctx, id); err != nil {
			return err
		}

		if err := tx.Persons().RemoveGuardian(ctx, domain.Guardian{GuardianID: p.PersonID, DependentID: id}); err != nil {
			return err
		}

		others, err := tx.Persons().ListGuardians(ctx, id)
		if err != nil || len(others) > 0 {
			return err
		}

		if managed, err := isManaged(ctx, tx, id); err != nil || managed {
			return err
		}

		if err := s.w.withdrawPerson(ctx, tx, s.clock.Now(), id); err != nil {
			return err
		}

		// Accounts, staff links and guardian links are ruled out under the
		// lock; whatever still refers to the person keeps it.
		if err := tx.Persons().Delete(ctx, id); errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("%w (%w)", ErrDependentInUse, err)
		} else if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("remove dependent: %w", err)
	}

	return nil
}

// apply sets the editable fields of person from d and validates it. The
// email and the office's notes stay as they are.
func (s *FamilyService) apply(
	ctx context.Context, tx store.Tx, person domain.Person, d PersonData,
) (domain.Person, error) {
	return applyPersonData(ctx, tx, s.clock, person, d)
}

// applyPersonData sets the fields of person in d, trimmed, and validates
// the person. A birth date must not lie after today in the school time
// zone. The guardians' FamilyService and the office's PersonService both
// edit persons with it.
func applyPersonData(
	ctx context.Context, tx store.Tx, clk clock.Clock, person domain.Person, d PersonData,
) (domain.Person, error) {
	person.GivenName = strings.TrimSpace(d.GivenName)
	person.FamilyName = strings.TrimSpace(d.FamilyName)
	person.Phone = strings.TrimSpace(d.Phone)
	person.BirthDate = d.BirthDate

	if err := person.Validate(); err != nil {
		return domain.Person{}, err
	}

	if err := checkBirthDate(ctx, tx, clk, d.BirthDate); err != nil {
		return domain.Person{}, err
	}

	return person, nil
}

// checkBirthDate fails with ErrFutureBirthDate for a birth date after today
// in the school time zone. A zero date is fine.
func checkBirthDate(ctx context.Context, tx store.Tx, clk clock.Clock, d domain.Date) error {
	if d.IsZero() {
		return nil
	}

	_, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return err
	}

	if d.Compare(domain.DateOf(clk.Now(), loc)) > 0 {
		return ErrFutureBirthDate
	}

	return nil
}

// isManaged reports whether person is a managed dependent: it has its own
// account, is linked to a staff member, or is a guardian of someone itself,
// so its owner or the office maintains its data, not a guardian, and a
// guardian never deletes it (ADR 0008).
func isManaged(ctx context.Context, tx store.Tx, person domain.PersonID) (bool, error) {
	switch _, err := tx.Accounts().GetByPerson(ctx, person); {
	case err == nil:
		return true, nil
	case !errors.Is(err, domain.ErrNotFound):
		return false, err
	}

	switch staff, err := tx.Staff().ListByPerson(ctx, person); {
	case err != nil:
		return false, err
	case len(staff) > 0:
		return true, nil
	}

	dependents, err := tx.Persons().ListDependents(ctx, person)

	return len(dependents) > 0, err
}

// dependentOf returns the person dependent if guardian is one of its
// guardians, and domain.ErrNotFound otherwise, so that nobody learns about
// persons outside their family. Booking for others (Phase 4) checks the same.
func dependentOf(ctx context.Context, tx store.Tx, guardian, dependent domain.PersonID) (domain.Person, error) {
	guardians, err := tx.Persons().ListGuardians(ctx, dependent)
	if err != nil {
		return domain.Person{}, err
	}

	if !slices.ContainsFunc(guardians, func(g domain.Person) bool { return g.ID == guardian }) {
		return domain.Person{}, fmt.Errorf("%w: person %v is not a dependent", domain.ErrNotFound, dependent)
	}

	return tx.Persons().Get(ctx, dependent)
}
