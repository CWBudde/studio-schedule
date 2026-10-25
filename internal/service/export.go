package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The data export (ADR 0016): everything the school stores about one
// person, as one document the office or the person downloads (GDPR Art. 15
// and 20). It holds no secrets: no password hash, no session, link or feed
// token, no receipt PDF. The office exports any person, a tombstone or an
// anonymized one included; a member exports their own person and their
// dependents (ADR 0008).

// ExportVersion is the version of the export format; it changes when a
// field changes its meaning or goes, not when one is added.
const ExportVersion = 1

// PersonExport is the data of one person.
type PersonExport struct {
	ExportedAt time.Time
	Person     domain.Person
	// Guardians and Dependents are the linked persons, ordered by name;
	// Merged the persons merged into this one (ADR 0015).
	Guardians  []domain.Person
	Dependents []domain.Person
	Merged     []domain.Person
	// Account is the person's login; nil without one, or when the export
	// leaves what belongs to the account out (a dependent's own account,
	// exported by a guardian). Then CalendarFeeds, PushSubscriptions and
	// Dismissed are empty too.
	Account       *domain.Account
	CalendarFeeds []domain.CalendarFeed
	// PushSubscriptions are the account's devices with notifications on
	// (ADR 0017).
	PushSubscriptions []domain.PushSubscription
	// Dismissed are the announcements the account dismissed, newest first.
	Dismissed []domain.Announcement
	// Classes are the class memberships.
	Classes []PersonClass
	// Bookings are all bookings of the person, canceled ones included, by
	// lesson start; Waitlist all its waiting-list entries, ended ones
	// included, by the time it joined.
	Bookings []ExportBooking
	Waitlist []ExportWaitlistEntry
	// Passes are the passes the person owns with their redemptions.
	Passes []ExportPass
	// Receipts are the receipts the person (or a person merged into it)
	// paid, newest first, with whether a sale was canceled.
	Receipts []ExportReceipt
}

// ExportBooking is a booking with its lesson.
type ExportBooking struct {
	Booking    domain.Booking
	Lesson     domain.Lesson
	CourseType domain.CourseType
}

// ExportWaitlistEntry is a waiting-list entry with its lesson.
type ExportWaitlistEntry struct {
	Entry      domain.WaitlistEntry
	Lesson     domain.Lesson
	CourseType domain.CourseType
}

// ExportPass is a pass with its type and redemptions.
type ExportPass struct {
	Pass        domain.Pass
	Type        domain.PassType
	Redemptions []domain.PassRedemption
}

// ExportReceipt is a receipt and whether it was canceled.
type ExportReceipt struct {
	Receipt  domain.Receipt
	Canceled bool
}

// Export returns the data of the person id for the office. An unknown
// person is domain.ErrNotFound; a tombstone and an anonymized person are
// exported like any other.
func (s *PersonService) Export(ctx context.Context, id domain.PersonID) (PersonExport, error) {
	var out PersonExport

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		person, err := tx.Persons().Get(ctx, id)
		if err != nil {
			return err
		}

		out, err = exportPerson(ctx, tx, s.clock.Now(), person, true)

		return err
	})
	if err != nil {
		return PersonExport{}, fmt.Errorf("export person %s: %w", id, err)
	}

	return out, nil
}

// Export returns the data of p's own person or of one of p's dependents
// (ADR 0008); anyone else is domain.ErrNotFound. A dependent with an
// account of its own is exported without what belongs to that account: its
// holder exports it.
func (s *FamilyService) Export(ctx context.Context, p Principal, id domain.PersonID) (PersonExport, error) {
	var out PersonExport

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		person, err := familyMember(ctx, tx, p, id)
		if err != nil {
			return err
		}

		out, err = exportPerson(ctx, tx, s.clock.Now(), person, id == p.PersonID)

		return err
	})
	if err != nil {
		return PersonExport{}, fmt.Errorf("export person %s: %w", id, err)
	}

	return out, nil
}

// exportPerson collects the data of person. withAccount includes the
// person's account and what belongs to it; without, a person that has an
// account is exported without it.
func exportPerson(
	ctx context.Context, tx store.Tx, now time.Time, person domain.Person, withAccount bool,
) (PersonExport, error) {
	out := PersonExport{ExportedAt: now, Person: person}

	var err error
	if out.Guardians, err = tx.Persons().ListGuardians(ctx, person.ID); err != nil {
		return out, err
	}

	if out.Dependents, err = tx.Persons().ListDependents(ctx, person.ID); err != nil {
		return out, err
	}

	if out.Merged, err = tx.Persons().ListMerged(ctx, person.ID); err != nil {
		return out, err
	}

	if out.Classes, err = personClasses(ctx, tx, person.ID); err != nil {
		return out, err
	}

	if withAccount {
		if err := exportAccount(ctx, tx, &out); err != nil {
			return out, err
		}
	}

	courseTypes, err := tx.CourseTypes().List(ctx)
	if err != nil {
		return out, err
	}

	types := indexBy(courseTypes, func(c domain.CourseType) domain.CourseTypeID { return c.ID })

	if err := exportLessons(ctx, tx, &out, types); err != nil {
		return out, err
	}

	if err := exportPasses(ctx, tx, &out); err != nil {
		return out, err
	}

	return out, exportReceipts(ctx, tx, &out)
}

// exportAccount adds the person's account, its calendar feeds, its push
// subscriptions and the announcements it dismissed.
func exportAccount(ctx context.Context, tx store.Tx, out *PersonExport) error {
	acc, err := tx.Accounts().GetByPerson(ctx, out.Person.ID)

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return nil
	case err != nil:
		return err
	}

	out.Account = &acc

	if out.CalendarFeeds, err = tx.CalendarFeeds().ListByAccount(ctx, acc.ID); err != nil {
		return err
	}

	if out.PushSubscriptions, err = tx.PushSubscriptions().ListByAccount(ctx, acc.ID); err != nil {
		return err
	}

	announcements, err := tx.Announcements().List(ctx)
	if err != nil || len(announcements) == 0 {
		return err
	}

	dismissed, err := tx.Announcements().ListDismissed(ctx, acc.ID,
		mapSlice(announcements, func(a domain.Announcement) domain.AnnouncementID { return a.ID }))
	if err != nil {
		return err
	}

	for _, a := range announcements {
		if slices.Contains(dismissed, a.ID) {
			out.Dismissed = append(out.Dismissed, a)
		}
	}

	return nil
}

// exportLessons adds the bookings and waiting-list entries with their
// lessons.
func exportLessons(
	ctx context.Context, tx store.Tx, out *PersonExport, types map[domain.CourseTypeID]domain.CourseType,
) error {
	persons := []domain.PersonID{out.Person.ID}

	bookings, err := tx.Bookings().ListByPersons(ctx, store.BookingQuery{Persons: persons})
	if err != nil {
		return err
	}

	entries, err := tx.Waitlist().ListByPersons(ctx, persons)
	if err != nil {
		return err
	}

	lessons, err := tx.Lessons().ListByIDs(ctx, slices.Concat(
		mapSlice(bookings, func(b domain.Booking) domain.LessonID { return b.LessonID }),
		mapSlice(entries, func(e domain.WaitlistEntry) domain.LessonID { return e.LessonID }),
	))
	if err != nil {
		return err
	}

	byLesson := indexBy(lessons, func(l domain.Lesson) domain.LessonID { return l.ID })

	for _, b := range bookings {
		l := byLesson[b.LessonID]
		out.Bookings = append(out.Bookings, ExportBooking{Booking: b, Lesson: l, CourseType: types[l.CourseTypeID]})
	}

	for _, e := range entries {
		l := byLesson[e.LessonID]
		out.Waitlist = append(out.Waitlist, ExportWaitlistEntry{Entry: e, Lesson: l, CourseType: types[l.CourseTypeID]})
	}

	return nil
}

// exportPasses adds the passes the person owns with their redemptions.
func exportPasses(ctx context.Context, tx store.Tx, out *PersonExport) error {
	passes, err := tx.Passes().ListByPersons(ctx, []domain.PersonID{out.Person.ID})
	if err != nil || len(passes) == 0 {
		return err
	}

	types, err := passTypesByID(ctx, tx)
	if err != nil {
		return err
	}

	redemptions, err := tx.Redemptions().ListByPasses(ctx,
		mapSlice(passes, func(p domain.Pass) domain.PassID { return p.ID }), 0)
	if err != nil {
		return err
	}

	for _, p := range passes {
		ep := ExportPass{Pass: p, Type: types[p.PassTypeID]}

		for _, r := range redemptions {
			if r.PassID == p.ID {
				ep.Redemptions = append(ep.Redemptions, r)
			}
		}

		out.Passes = append(out.Passes, ep)
	}

	return nil
}

// exportReceipts adds the receipts the person and the persons merged into
// it paid.
func exportReceipts(ctx context.Context, tx store.Tx, out *PersonExport) error {
	payers := append([]domain.PersonID{out.Person.ID},
		mapSlice(out.Merged, func(p domain.Person) domain.PersonID { return p.ID })...)

	receipts, err := tx.Receipts().ListByPayers(ctx, payers)
	if err != nil || len(receipts) == 0 {
		return err
	}

	cancellations, err := tx.Receipts().ListCancellations(ctx,
		mapSlice(receipts, func(r domain.Receipt) domain.ReceiptID { return r.ID }))
	if err != nil {
		return err
	}

	for _, r := range receipts {
		canceled := slices.ContainsFunc(cancellations, func(c domain.Receipt) bool { return c.CancelsReceiptID == r.ID })
		out.Receipts = append(out.Receipts, ExportReceipt{Receipt: r, Canceled: canceled})
	}

	return nil
}

// indexBy returns the values by their keys.
func indexBy[K comparable, V any](values []V, key func(V) K) map[K]V {
	out := make(map[K]V, len(values))
	for _, v := range values {
		out[key(v)] = v
	}

	return out
}
