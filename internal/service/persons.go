package service

import (
	"cmp"
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

// MaxPersonMatches is the most persons SearchPersons returns.
const MaxPersonMatches = 50

var (
	// errUnknownGuardian is a guardian to link that does not exist.
	errUnknownGuardian error = &domain.FieldError{Field: "guardianId", Msg: "person does not exist"}
	// errGuardianCycle is a guardian to link who is a dependent of the
	// person, directly or through others.
	errGuardianCycle error = &domain.FieldError{
		Field: "guardianId", Msg: "a dependent of the person cannot be their guardian",
	}

	// ErrGuardianLinked rejects a guardian link that exists.
	ErrGuardianLinked = fmt.Errorf("%w: the guardian is linked already", domain.ErrDuplicate)
	// errPersonGone is a person that a transaction found and that a
	// concurrent one deleted before the write.
	errPersonGone = fmt.Errorf("%w: the person was deleted meanwhile", domain.ErrNotFound)
)

// PersonService lets the office find, see and edit every person and every
// guardian link (ADR 0008), e.g. to link a staff member, add a class member
// or link a second parent. The HTTP layer admits only office and admin
// accounts.
type PersonService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
	w     waitlister
}

// NewPersonService returns a PersonService. Deleting a person sends the
// waiting-list invites of the places it frees with mails.
func NewPersonService(st store.Store, clk clock.Clock, src ids.Source, mails Mails) *PersonService {
	return &PersonService{store: st, clock: clk, ids: src, w: waitlister{ids: src, mails: mails}}
}

// PersonRecord are the fields of a person the office edits: all of them,
// including the email, the office's notes and the member number. Optional
// fields are empty or zero when absent.
type PersonRecord struct {
	GivenName  string
	FamilyName string
	Email      string
	Phone      string
	BirthDate  domain.Date
	Notes      string
	MemberNo   string
}

// PersonDetail is a person with everything that refers to it, as the
// office sees it.
type PersonDetail struct {
	Person domain.Person
	// Guardians and Dependents are ordered by family and given name.
	Guardians  []domain.Person
	Dependents []domain.Person
	// Account is the person's login; nil if it has none.
	Account *domain.Account
	// Staff are the staff members linked to the person, ordered by display
	// name.
	Staff []domain.Staff
	// Classes are the person's class memberships, ordered by term start,
	// weekday and start time.
	Classes []PersonClass
	// Merged are the persons merged into this one (ADR 0015), ordered by
	// family and given name.
	Merged []domain.Person
	// MergedInto is the person this one was merged into; zero unless the
	// person is a tombstone.
	MergedInto domain.Person
}

// PersonClass is a class membership with its class, term and course type.
type PersonClass struct {
	Class      domain.Class
	Term       domain.Term
	CourseType domain.CourseType
	Since      domain.Date
}

// SearchPersons returns the persons whose given name, family name, email or
// member number contains every word of query, ignoring case, ordered by
// family name and given name; at most MaxPersonMatches. An empty query
// matches everyone. Merged persons are left out.
func (s *PersonService) SearchPersons(ctx context.Context, query string) ([]domain.Person, error) {
	var persons []domain.Person

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		persons, err = tx.Persons().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("search persons: %w", err)
	}

	words := strings.Fields(strings.ToLower(query))
	matches := make([]domain.Person, 0, min(len(persons), MaxPersonMatches))

	for _, p := range persons {
		if len(matches) == MaxPersonMatches {
			break
		}

		text := strings.ToLower(p.GivenName + " " + p.FamilyName + " " + p.Email + " " + p.MemberNo)
		if containsAll(text, words) {
			matches = append(matches, p)
		}
	}

	return matches, nil
}

// Person returns the person id with its guardians, dependents, account,
// staff links and class memberships. An unknown person is
// domain.ErrNotFound.
func (s *PersonService) Person(ctx context.Context, id domain.PersonID) (PersonDetail, error) {
	var d PersonDetail

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		if d.Person, err = tx.Persons().Get(ctx, id); err != nil {
			return err
		}

		if d.Guardians, err = tx.Persons().ListGuardians(ctx, id); err != nil {
			return err
		}

		if d.Dependents, err = tx.Persons().ListDependents(ctx, id); err != nil {
			return err
		}

		switch acc, err := tx.Accounts().GetByPerson(ctx, id); {
		case err == nil:
			d.Account = &acc
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}

		if d.Staff, err = tx.Staff().ListByPerson(ctx, id); err != nil {
			return err
		}

		if d.Merged, err = tx.Persons().ListMerged(ctx, id); err != nil {
			return err
		}

		if d.Person.Merged() {
			if d.MergedInto, err = tx.Persons().Get(ctx, d.Person.MergedInto); err != nil {
				return fmt.Errorf("merged into: %w", refErr(err))
			}
		}

		d.Classes, err = personClasses(ctx, tx, id)

		return err
	})
	if err != nil {
		return PersonDetail{}, fmt.Errorf("person %s: %w", id, err)
	}

	return d, nil
}

// personClasses returns the class memberships of person with their
// classes, terms and course types, ordered by term start, weekday and start
// time.
func personClasses(ctx context.Context, tx store.Tx, person domain.PersonID) ([]PersonClass, error) {
	memberships, err := tx.Classes().ListMemberships(ctx, person)
	if err != nil {
		return nil, err
	}

	out := make([]PersonClass, 0, len(memberships))
	// The classes of a person mostly share terms and course types.
	terms := map[domain.TermID]domain.Term{}
	courseTypes := map[domain.CourseTypeID]domain.CourseType{}

	for _, m := range memberships {
		pc := PersonClass{Since: m.Since}
		if pc.Class, err = tx.Classes().Get(ctx, m.ClassID); err != nil {
			return nil, err
		}

		if pc.Term, err = cached(ctx, terms, pc.Class.TermID, tx.Terms().Get); err != nil {
			return nil, err
		}

		if pc.CourseType, err = cached(ctx, courseTypes, pc.Class.CourseTypeID, tx.CourseTypes().Get); err != nil {
			return nil, err
		}

		out = append(out, pc)
	}

	slices.SortFunc(out, func(a, b PersonClass) int {
		return cmp.Or(
			a.Term.StartsOn.Compare(b.Term.StartsOn),
			cmp.Compare(a.Class.Weekday, b.Class.Weekday),
			a.Class.StartTime.Compare(b.Class.StartTime),
			cmp.Compare(a.Class.ID.String(), b.Class.ID.String()),
		)
	})

	return out, nil
}

// cached returns the record id from seen, or gets it and adds it to seen.
func cached[ID comparable, T any](
	ctx context.Context, seen map[ID]T, id ID, get func(context.Context, ID) (T, error),
) (T, error) {
	if v, ok := seen[id]; ok {
		return v, nil
	}

	v, err := get(ctx, id)
	if err == nil {
		seen[id] = v
	}

	return v, err
}

// CreatePerson adds a person, e.g. a second parent or a staff member's
// record, and returns it.
func (s *PersonService) CreatePerson(ctx context.Context, r PersonRecord) (domain.Person, error) {
	var person domain.Person

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error
		if person, err = s.apply(ctx, tx, domain.Person{ID: ids.New[domain.PersonID](s.ids)}, r); err != nil {
			return err
		}

		return tx.Persons().Create(ctx, person)
	})
	if err != nil {
		return domain.Person{}, fmt.Errorf("create person: %w", err)
	}

	return person, nil
}

// UpdatePerson replaces every field of the person id and returns it. It
// leaves the person's account alone: its email is the login and changes
// only through the account. An unknown or merged person is
// domain.ErrNotFound, a member number another person has domain.ErrDuplicate.
func (s *PersonService) UpdatePerson(ctx context.Context, id domain.PersonID, r PersonRecord) (domain.Person, error) {
	var person domain.Person

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error
		if person, err = lockLivePerson(ctx, tx, id); err != nil {
			return err
		}

		if person, err = s.apply(ctx, tx, person, r); err != nil {
			return err
		}

		return tx.Persons().Update(ctx, person)
	})
	if err != nil {
		return domain.Person{}, fmt.Errorf("update person %s: %w", id, err)
	}

	return person, nil
}

// apply sets every field of person from r and validates it.
func (s *PersonService) apply(
	ctx context.Context, tx store.Tx, person domain.Person, r PersonRecord,
) (domain.Person, error) {
	person.Email = strings.TrimSpace(r.Email)
	person.Notes = strings.TrimSpace(r.Notes)
	person.MemberNo = strings.TrimSpace(r.MemberNo)

	return applyPersonData(ctx, tx, s.clock, person, PersonData{
		GivenName: r.GivenName, FamilyName: r.FamilyName, Phone: r.Phone, BirthDate: r.BirthDate,
	})
}

// DeletePerson deletes the person id, e.g. one created by mistake. While
// anything refers to it (an account, a staff link, bookings, class
// memberships, passes it owns or paid for, or a guardian link in either
// direction) that is
// domain.ErrInUse and nothing changes. The person leaves the waiting lists
// first, like a removed dependent (FamilyService.RemoveDependent). An
// unknown person is domain.ErrNotFound.
func (s *PersonService) DeletePerson(ctx context.Context, id domain.PersonID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// A tombstone stays for its receipts.
		if _, err := lockLivePerson(ctx, tx, id); err != nil {
			return err
		}

		if err := s.w.withdrawPerson(ctx, tx, s.clock.Now(), id); err != nil {
			return err
		}

		err := tx.Persons().Delete(ctx, id)
		if errors.Is(err, domain.ErrConflict) {
			return fmt.Errorf("%w: the person is still referred to (%w)", domain.ErrInUse, err)
		}

		return err
	})
	if err != nil {
		return fmt.Errorf("delete person %s: %w", id, err)
	}

	return nil
}

// AddGuardian links guardian to dependent, e.g. a second parent, and
// returns the guardian. An unknown dependent is domain.ErrNotFound; an
// unknown guardian, the dependent itself, or one of its dependents
// (directly or through others, which would make the links a cycle) a
// validation error on guardianId; and an existing link ErrGuardianLinked.
func (s *PersonService) AddGuardian(ctx context.Context, dependent, guardian domain.PersonID) (domain.Person, error) {
	var g domain.Person

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// The lock keeps a concurrent link from forming a cycle with this
		// one, and the persons from changing what makes them managed.
		lockErr := tx.Persons().Lock(ctx, dependent, guardian)
		if lockErr != nil && !errors.Is(lockErr, domain.ErrNotFound) {
			return lockErr
		}

		if _, err := livePerson(ctx, tx, dependent); err != nil {
			return err
		}

		link := domain.Guardian{GuardianID: guardian, DependentID: dependent}
		if err := link.Validate(); err != nil {
			return err
		}

		var err error

		g, err = livePerson(ctx, tx, guardian)
		if errors.Is(err, domain.ErrNotFound) {
			return errUnknownGuardian
		} else if err != nil {
			return err
		}

		if lockErr != nil {
			return lockErr
		}

		switch cycle, err := isDependentOf(ctx, tx, guardian, dependent); {
		case err != nil:
			return err
		case cycle:
			return errGuardianCycle
		}

		switch err := tx.Persons().AddGuardian(ctx, link); {
		case errors.Is(err, domain.ErrDuplicate):
			return fmt.Errorf("%w (%w)", ErrGuardianLinked, err)
		case errors.Is(err, domain.ErrConflict):
			// Both persons were there a moment ago.
			return errPersonGone
		default:
			return err
		}
	})
	if err != nil {
		return domain.Person{}, fmt.Errorf("add guardian of %s: %w", dependent, err)
	}

	return g, nil
}

// RemoveGuardian ends guardian's guardianship of dependent. Unlike a
// guardian removing a dependent, it keeps the person even without a
// guardian left: the office decides what happens to it. A link that does
// not exist is domain.ErrNotFound.
func (s *PersonService) RemoveGuardian(ctx context.Context, dependent, guardian domain.PersonID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Persons().RemoveGuardian(ctx, domain.Guardian{GuardianID: guardian, DependentID: dependent})
	})
	if err != nil {
		return fmt.Errorf("remove guardian %s of %s: %w", guardian, dependent, err)
	}

	return nil
}

// isDependentOf reports whether person is a dependent of ancestor, directly
// or through other dependents. The caller has locked ancestor; it locks
// every other dependent before it reads that one's dependents. A link that
// closes a cycle with a concurrent one runs from a person on the other
// one's path to its guardian, so one of the two waits for the other's lock
// and then sees its link. It visits every person once, so it ends even on
// a cycle.
func isDependentOf(ctx context.Context, tx store.Tx, person, ancestor domain.PersonID) (bool, error) {
	seen := map[domain.PersonID]bool{ancestor: true}

	for queue := []domain.PersonID{ancestor}; len(queue) > 0; queue = queue[1:] {
		dependents, err := tx.Persons().ListDependents(ctx, queue[0])
		if err != nil {
			return false, err
		}

		for _, d := range dependents {
			if d.ID == person {
				return true, nil
			}

			if seen[d.ID] {
				continue
			}

			seen[d.ID] = true

			switch err := tx.Persons().Lock(ctx, d.ID); {
			case errors.Is(err, domain.ErrNotFound): // deleted meanwhile
			case err != nil:
				return false, err
			default:
				queue = append(queue, d.ID)
			}
		}
	}

	return false, nil
}

func containsAll(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}

	return true
}
