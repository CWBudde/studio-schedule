package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Anonymizing (ADR 0016): the office erases a person's personal data and
// keeps the person as the anonymous holder of its history. The person gets
// the fixed name "Deleted person" and loses everything else that is
// personal (email, phone, birth date, notes, member number). What ties it
// to the school from now on goes: guardian links in both directions, class
// memberships, open waiting-list entries, its bookings of lessons that have
// not started (canceled through the ledger, on time, so a visit they cost
// comes back), its staff links and its account with the account's
// sessions, links and calendar feeds. Its past bookings, its passes and the
// receipts it paid stay: they are the school's records, and receipts are
// immutable (ADR 0012). Like a tombstone of a merge, an anonymized person is
// left out of lists and searches and refused by every write
// (lockLivePersons).

var (
	// ErrAlreadyAnonymized refuses anonymizing a person a second time.
	ErrAlreadyAnonymized = fmt.Errorf("%w: the person is anonymized already", domain.ErrConflict)
	// ErrSoleGuardian refuses anonymizing the only guardian of a dependent:
	// the office links another guardian or removes the link first.
	ErrSoleGuardian = fmt.Errorf("%w: the person is the only guardian of a dependent", domain.ErrConflict)
	// ErrAccountKept refuses deleting an account that receipts, the audit
	// log, announcements or duplicate dismissals name (AccountRepo.Delete):
	// it is disabled instead.
	ErrAccountKept = fmt.Errorf("%w: receipts, the audit log, announcements or duplicate dismissals name "+
		"the account; disable it instead", domain.ErrInUse)
	// ErrAdminPerson refuses the office anonymizing the person of an admin
	// account, which would delete that account: deleting an admin's account
	// is an admin's decision (like creating one, ADR 0008).
	ErrAdminPerson = fmt.Errorf("%w: only an admin anonymizes the person of an admin account", domain.ErrForbidden)
)

// AnonymizeSummary is what anonymizing a person did.
type AnonymizeSummary struct {
	Person domain.Person
	// BookingsCanceled counts the bookings of lessons that had not started,
	// WaitlistWithdrawn the open waiting-list entries.
	BookingsCanceled  int
	WaitlistWithdrawn int
	// MembershipsEnded counts the class memberships, LinksRemoved the
	// guardian links in both directions, StaffUnlinked the staff records
	// that named the person.
	MembershipsEnded int
	LinksRemoved     int
	StaffUnlinked    int
	// AccountDeleted reports whether the person had an account.
	AccountDeleted bool
}

// Anonymize anonymizes the person id for the office's principal p and
// records it in the audit log. An unknown or merged person is
// domain.ErrNotFound, an anonymized one ErrAlreadyAnonymized, the only
// guardian of a dependent ErrSoleGuardian, a person whose account must be
// kept ErrAccountKept, p's own person ErrOwnAccount, the person of an admin
// account ErrAdminPerson unless p is an admin, and that of the last admin
// who can sign in ErrLastAdmin.
func (s *PersonService) Anonymize(ctx context.Context, p Principal, id domain.PersonID) (AnonymizeSummary, error) {
	var sum AnonymizeSummary

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error

		sum, err = s.anonymize(ctx, tx, p, id)

		return err
	})
	if err != nil {
		return AnonymizeSummary{}, fmt.Errorf("anonymize person %s: %w", id, err)
	}

	return sum, nil
}

// anonymize does the work of Anonymize. The lock order is the persons
// (lockAnonymizeTarget), the terms and classes of its memberships, the
// lessons of its bookings that have not started and of its open
// waiting-list entries, and the passes (persons → term → class → lesson →
// passes).
func (s *PersonService) anonymize(
	ctx context.Context, tx store.Tx, p Principal, id domain.PersonID,
) (AnonymizeSummary, error) {
	var sum AnonymizeSummary

	t, err := lockAnonymizeTarget(ctx, tx, id)
	if err != nil {
		return sum, err
	}

	switch {
	case t.person.Merged():
		return sum, fmt.Errorf("person %s is merged: %w", id, domain.ErrNotFound)
	case t.person.Anonymized():
		return sum, ErrAlreadyAnonymized
	case id == p.PersonID:
		return sum, ErrOwnAccount
	case t.account != nil && t.account.Role == domain.RoleAdmin && p.Role != domain.RoleAdmin:
		return sum, ErrAdminPerson
	case t.account != nil && lastAdmin(*t.account, t.admins):
		return sum, ErrLastAdmin
	}

	if err := soleGuardianCheck(ctx, tx, id, t.dependents); err != nil {
		return sum, err
	}

	now := s.clock.Now()

	memberships, err := lockMemberships(ctx, tx, id)
	if err != nil {
		return sum, err
	}

	if err := s.cancelAhead(ctx, tx, p, now, id, &sum); err != nil {
		return sum, err
	}

	for _, m := range memberships {
		if err := tx.Classes().RemoveMember(ctx, m.ClassID, id); err != nil {
			return sum, err
		}
	}

	sum.MembershipsEnded = len(memberships)

	for _, g := range t.guardians {
		if err := tx.Persons().RemoveGuardian(ctx, domain.Guardian{GuardianID: g.ID, DependentID: id}); err != nil {
			return sum, err
		}
	}

	for _, d := range t.dependents {
		if err := tx.Persons().RemoveGuardian(ctx, domain.Guardian{GuardianID: id, DependentID: d.ID}); err != nil {
			return sum, err
		}
	}

	sum.LinksRemoved = len(t.guardians) + len(t.dependents)

	if sum.StaffUnlinked, err = unlinkStaff(ctx, tx, id); err != nil {
		return sum, err
	}

	if t.account != nil {
		if err := deleteAccount(ctx, tx, s.ids, now, t.account.ID); err != nil {
			return sum, err
		}

		sum.AccountDeleted = true
	}

	dismissals, err := tx.DuplicateDismissals().ListByPerson(ctx, id)
	if err != nil {
		return sum, err
	}

	for _, d := range dismissals {
		if err := tx.DuplicateDismissals().Delete(ctx, d.PersonA, d.PersonB); err != nil {
			return sum, err
		}
	}

	sum.Person = t.person.Anonymize(now)
	if err := sum.Person.Validate(); err != nil {
		return sum, err
	}

	if err := tx.Persons().Update(ctx, sum.Person); err != nil {
		return sum, err
	}

	e := domain.AuditEvent{
		ID: ids.New[domain.AuditEventID](s.ids), At: now, ActorID: actorOf(p), Action: domain.AuditPersonAnonymized,
		TargetKind: domain.AuditTargetPerson, TargetPersonID: id, Details: domain.AuditDetails{Via: p.Via},
	}
	if err := e.Validate(); err != nil {
		return sum, err
	}

	return sum, tx.Audit().Create(ctx, e)
}

// anonymizeTarget is what anonymizing a person reads under the locks of the
// persons it involves.
type anonymizeTarget struct {
	person                domain.Person
	guardians, dependents []domain.Person
	// account is the person's account, nil without one; admins are the
	// admins who can sign in (lockAccount), read only when account is an
	// admin's.
	account *domain.Account
	admins  []domain.Account
}

// lockAnonymizeTarget locks, together and in ID order, the person id, its
// guardians and its dependents (whose links anonymizing removes, and whose
// other guardians soleGuardianCheck counts), and, when the person's account
// is an admin's, the persons of every admin (as lockAccount does), and
// returns what it read under those locks. An unknown person is
// domain.ErrNotFound.
func lockAnonymizeTarget(ctx context.Context, tx store.Tx, id domain.PersonID) (anonymizeTarget, error) {
	var t anonymizeTarget

	err := lockPersonSet(ctx, tx, func() ([]domain.PersonID, error) {
		t = anonymizeTarget{}

		var err error

		if t.guardians, err = tx.Persons().ListGuardians(ctx, id); err != nil {
			return nil, err
		}

		if t.dependents, err = tx.Persons().ListDependents(ctx, id); err != nil {
			return nil, err
		}

		persons := slices.Concat([]domain.PersonID{id}, personIDs(t.guardians), personIDs(t.dependents))

		acc, err := tx.Accounts().GetByPerson(ctx, id)

		switch {
		case errors.Is(err, domain.ErrNotFound):
			return persons, nil
		case err != nil:
			return nil, err
		}

		t.account = &acc

		if acc.Role != domain.RoleAdmin {
			return persons, nil
		}

		admins, active, err := adminSet(ctx, tx)
		t.admins = active

		return append(persons, admins...), err
	})
	if err != nil {
		return anonymizeTarget{}, err
	}

	if t.person, err = tx.Persons().Get(ctx, id); err != nil {
		return anonymizeTarget{}, err
	}

	return t, nil
}

// soleGuardianCheck refuses anonymizing the person id while one of its
// dependents, whom the caller has locked, has no other guardian
// (ErrSoleGuardian).
func soleGuardianCheck(ctx context.Context, tx store.Tx, id domain.PersonID, dependents []domain.Person) error {
	if len(dependents) == 0 {
		return nil
	}

	dependentIDs := personIDs(dependents)

	links, err := tx.Persons().ListGuardianLinks(ctx, dependentIDs)
	if err != nil {
		return err
	}

	for _, d := range dependentIDs {
		others := slices.ContainsFunc(links, func(g domain.Guardian) bool {
			return g.DependentID == d && g.GuardianID != id
		})
		if !others {
			return ErrSoleGuardian
		}
	}

	return nil
}

// cancelAhead cancels the person's active bookings of lessons that have not
// started, on time and without releasing the date, and withdraws its open
// waiting-list entries, under the locks of those lessons (lockBookedLessons).
func (s *PersonService) cancelAhead(
	ctx context.Context, tx store.Tx, p Principal, now time.Time, id domain.PersonID, sum *AnonymizeSummary,
) error {
	lessons, bookings, entries, err := lockBookedLessons(ctx, tx, store.BookingQuery{
		Persons: []domain.PersonID{id}, Active: true, StartsFrom: now,
	})
	if err != nil {
		return err
	}

	return s.cancelLocked(ctx, tx, p, now, lessons, bookings, entries, sum)
}

// cancelLocked cancels the bookings and withdraws the entries of
// cancelAhead, whose lessons are locked.
func (s *PersonService) cancelLocked(
	ctx context.Context, tx store.Tx, p Principal, now time.Time, lessons map[domain.LessonID]domain.Lesson,
	bookings []domain.Booking, entries []domain.WaitlistEntry, sum *AnonymizeSummary,
) error {
	if len(bookings) > 0 {
		c, err := chargesIn(ctx, tx)
		if err != nil {
			return err
		}

		for _, b := range bookings {
			l := lessons[b.LessonID]
			if !now.Before(l.StartsAt) {
				continue // started: history
			}

			b.Late, b.Released = false, false
			if _, err := cancelBooking(ctx, tx, s.w, c, actorOf(p), now, l, b); err != nil {
				return err
			}

			sum.BookingsCanceled++
		}
	}

	for _, e := range entries {
		if err := s.w.end(ctx, tx, now, lessons[e.LessonID], e, domain.WaitlistWithdrawn); err != nil {
			return err
		}

		sum.WaitlistWithdrawn++
	}

	return nil
}

// unlinkStaff removes the person from the staff records that name it and
// returns how many; the records stay, with their display names, which the
// office maintains.
func unlinkStaff(ctx context.Context, tx store.Tx, id domain.PersonID) (int, error) {
	staff, err := tx.Staff().ListByPerson(ctx, id)
	if err != nil {
		return 0, err
	}

	for _, st := range staff {
		st.PersonID = domain.PersonID{}
		if err := tx.Staff().Update(ctx, st); err != nil {
			return 0, err
		}
	}

	return len(staff), nil
}
