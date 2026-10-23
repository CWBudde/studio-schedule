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
// is its last guardian and bookings or class memberships still refer to it.
var ErrDependentInUse = fmt.Errorf("%w: the dependent has bookings or class memberships", domain.ErrConflict)

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
}

// NewFamilyService returns a FamilyService.
func NewFamilyService(st store.Store, clk clock.Clock, src ids.Source) *FamilyService {
	return &FamilyService{store: st, clock: clk, ids: src}
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

			others := slices.DeleteFunc(guardians, func(g domain.Person) bool { return g.ID == p.PersonID })
			f.Dependents = append(f.Dependents, Dependent{Person: d, Guardians: others})
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
		if person, err = tx.Persons().Get(ctx, p.PersonID); err != nil {
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
		var err error
		if person, err = s.apply(ctx, tx, domain.Person{ID: ids.New[domain.PersonID](s.ids)}, d); err != nil {
			return err
		}

		if err := tx.Persons().Create(ctx, person); err != nil {
			return err
		}

		link := domain.Guardian{GuardianID: p.PersonID, DependentID: person.ID}
		if err := link.Validate(); err != nil {
			return err
		}

		return tx.Persons().AddGuardian(ctx, link)
	})
	if err != nil {
		return domain.Person{}, fmt.Errorf("add dependent: %w", err)
	}

	return person, nil
}

// UpdateDependent changes the person data of one of p's dependents and
// returns the person. Anyone else's person is domain.ErrNotFound.
func (s *FamilyService) UpdateDependent(
	ctx context.Context, p Principal, id domain.PersonID, d PersonData,
) (domain.Person, error) {
	var person domain.Person

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error
		if person, err = dependentOf(ctx, tx, p.PersonID, id); err != nil {
			return err
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
// person is deleted as well; while bookings or class memberships refer to it,
// that fails with ErrDependentInUse and nothing changes. Anyone else's
// person is domain.ErrNotFound.
func (s *FamilyService) RemoveDependent(ctx context.Context, p Principal, id domain.PersonID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := dependentOf(ctx, tx, p.PersonID, id); err != nil {
			return err
		}

		if err := tx.Persons().RemoveGuardian(ctx, domain.Guardian{GuardianID: p.PersonID, DependentID: id}); err != nil {
			return err
		}

		others, err := tx.Persons().ListGuardians(ctx, id)
		if err != nil || len(others) > 0 {
			return err
		}

		err = tx.Persons().Delete(ctx, id)
		if errors.Is(err, domain.ErrConflict) {
			return ErrDependentInUse
		}

		return err
	})
	if err != nil {
		return fmt.Errorf("remove dependent: %w", err)
	}

	return nil
}

// apply sets the editable fields of person from d and validates it. The
// email and the office's notes stay as they are. A birth date must not lie
// after today in the school time zone.
func (s *FamilyService) apply(
	ctx context.Context, tx store.Tx, person domain.Person, d PersonData,
) (domain.Person, error) {
	person.GivenName = strings.TrimSpace(d.GivenName)
	person.FamilyName = strings.TrimSpace(d.FamilyName)
	person.Phone = strings.TrimSpace(d.Phone)
	person.BirthDate = d.BirthDate

	if err := person.Validate(); err != nil {
		return domain.Person{}, err
	}

	if !d.BirthDate.IsZero() {
		_, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return domain.Person{}, err
		}

		if d.BirthDate.Compare(domain.DateOf(s.clock.Now(), loc)) > 0 {
			return domain.Person{}, fmt.Errorf("%w: birth date must not be in the future", domain.ErrValidation)
		}
	}

	return person, nil
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
