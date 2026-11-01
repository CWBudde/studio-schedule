package service

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Merging (ADR 0015): the office merges a duplicate person (the merged
// person) into another (the survivor). Everything that refers to the merged
// person moves to the survivor: bookings (through the ledger), waiting-list
// entries, class memberships, passes it owns or paid for, guardian links in
// both directions, staff links, its account, the duplicate dismissals that
// name it and earlier tombstones merged into it. Where both have something
// of the same lesson or class, the survivor's stays and the merged person's
// goes: its booking of a lesson that has not started is canceled through
// the ledger (a visit it cost comes back), its waiting-list entry
// withdrawn, its membership ended, and its automatic bookings of that
// class follow the survivor's membership. Bookings of lessons that have
// started are history: both stay, the merged person's moved to the
// survivor as it was (domain.Booking.MergedFrom). The merged person stays
// as a tombstone for the receipts it paid, which are immutable (ADR 0012);
// they are listed under the survivor.

var (
	// ErrMergeTwoAccounts refuses a merge of two persons who both have an
	// account, unless the merged person's is disabled (it then stays with
	// the tombstone): one account is disabled first.
	ErrMergeTwoAccounts = fmt.Errorf("%w: both persons have an account", domain.ErrConflict)
	// ErrMergeCycle refuses a merge after which a guardian of the survivor
	// would also be its dependent, directly or through others.
	ErrMergeCycle = fmt.Errorf("%w: the merge would make the guardian links a cycle", domain.ErrConflict)
	// errMergeSelf is a merge of a person with itself.
	errMergeSelf error = &domain.FieldError{Field: "mergedId", Msg: "the persons must differ"}
)

// MergeTake says which fields the survivor takes from the merged person;
// the others keep the survivor's value. The notes of both are kept.
type MergeTake struct {
	Name, Email, Phone, BirthDate, MemberNo bool
}

// MergeRequest names the persons of a merge and the fields to take.
type MergeRequest struct {
	SurvivorID domain.PersonID
	MergedID   domain.PersonID
	Take       MergeTake
	// Notes are the survivor's notes after the merge, as the office edited
	// them; nil keeps the notes of both (mergeNotes).
	Notes *string
}

// MergeCancelReason says why a merge cancels a booking of the merged
// person.
type MergeCancelReason string

// Reasons of a canceled booking.
const (
	// MergeCancelBooked: the survivor has an active booking of the lesson.
	MergeCancelBooked MergeCancelReason = "booked"
	// MergeCancelMembership: an automatic class booking the survivor's
	// membership of the class would not have (a pause, a later start, or
	// a date the survivor released).
	MergeCancelMembership MergeCancelReason = "membership"
)

// MergeCanceled is a booking of the merged person of a lesson that has not
// started, which a merge cancels.
type MergeCanceled struct {
	Lesson     domain.Lesson
	CourseType domain.CourseType
	Reason     MergeCancelReason
}

// MergeSummary is what a merge does, or would do.
type MergeSummary struct {
	// Survivor is the survivor as the merge leaves it; Merged the tombstone.
	Survivor domain.Person
	Merged   domain.Person
	// BookingsMoved counts the bookings that move, canceled ones included;
	// Canceled are those the merge cancels first, by lesson start; History
	// counts those of lessons that started which both have, kept as they
	// are beside the survivor's.
	BookingsMoved int
	Canceled      []MergeCanceled
	History       int
	// WaitlistMoved and WaitlistWithdrawn count the open waiting-list
	// entries that move or are withdrawn (the survivor's too, where it gets
	// a booking).
	WaitlistMoved     int
	WaitlistWithdrawn int
	// MembershipsMoved counts the memberships that move; MembershipsDropped
	// are those of classes the survivor is a member of already.
	MembershipsMoved   int
	MembershipsDropped []PersonClass
	// PassesMoved and PaidPassesMoved count the passes the merged person
	// owns and those it paid for.
	PassesMoved     int
	PaidPassesMoved int
	// LinksMoved counts the guardian links that move; LinksDropped those
	// the survivor has already, or that would link it to itself.
	LinksMoved   int
	LinksDropped int
	StaffMoved   int
	AccountMoved bool
	// Receipts counts the receipts the merged person paid; they stay on the
	// tombstone and are listed under the survivor.
	Receipts int
}

// Merge merges req.MergedID into req.SurvivorID for the office's principal
// p: with apply it writes and records the merge in the audit log; without,
// it reports what it would do and writes nothing (the same code, rolled
// back). An unknown or merged person is domain.ErrNotFound, the same person
// twice a validation error on mergedId, notes longer than
// domain.MaxNotesLength one on notes, two accounts ErrMergeTwoAccounts and a
// guardian cycle ErrMergeCycle.
func (s *PersonService) Merge(ctx context.Context, p Principal, req MergeRequest, apply bool) (MergeSummary, error) {
	if req.SurvivorID == req.MergedID {
		return MergeSummary{}, errMergeSelf
	}

	var sum MergeSummary

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error
		if sum, err = s.merge(ctx, tx, p, req); err != nil {
			return err
		}

		if !apply {
			return errDryRun
		}

		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return MergeSummary{}, fmt.Errorf("merge %s into %s: %w", req.MergedID, req.SurvivorID, err)
	}

	return sum, nil
}

// merge does the work of Merge in its transaction. The lock order is the
// persons (both, then the dependents the cycle check walks), the terms and
// classes of the merged person's memberships, the lessons of its bookings
// and waiting-list entries, and the passes.
func (s *PersonService) merge(ctx context.Context, tx store.Tx, p Principal, req MergeRequest) (MergeSummary, error) {
	var sum MergeSummary

	persons, err := lockLivePersons(ctx, tx, req.SurvivorID, req.MergedID)
	if err != nil {
		return sum, err
	}

	survivor, merged := persons[0], persons[1]

	notes := mergeNotes(survivor.Notes, merged.Notes)
	if req.Notes != nil {
		notes = strings.TrimSpace(*req.Notes)
	}

	if utf8.RuneCountInString(notes) > domain.MaxNotesLength {
		return sum, &domain.FieldError{
			Field: "notes", Msg: fmt.Sprintf("the notes are longer than %d characters", domain.MaxNotesLength),
		}
	}

	account, err := mergeAccount(ctx, tx, survivor.ID, merged.ID)
	if err != nil {
		return sum, err
	}

	if sum.LinksMoved, sum.LinksDropped, err = mergeLinks(ctx, tx, survivor.ID, merged.ID); err != nil {
		return sum, err
	}

	if err := s.mergeLessons(ctx, tx, p, survivor.ID, merged.ID, &sum); err != nil {
		return sum, err
	}

	if err := mergePasses(ctx, tx, survivor.ID, merged.ID, &sum); err != nil {
		return sum, err
	}

	staff, err := tx.Staff().ListByPerson(ctx, merged.ID)
	if err != nil {
		return sum, err
	}

	for _, st := range staff {
		st.PersonID = survivor.ID
		if err := tx.Staff().Update(ctx, st); err != nil {
			return sum, err
		}
	}

	sum.StaffMoved = len(staff)

	if account != nil {
		account.PersonID = survivor.ID
		if err := tx.Accounts().Update(ctx, *account); err != nil {
			return sum, err
		}

		sum.AccountMoved = true
	}

	if err := mergeDismissals(ctx, tx, survivor.ID, merged.ID); err != nil {
		return sum, err
	}

	tombstones, err := tx.Persons().ListMerged(ctx, merged.ID)
	if err != nil {
		return sum, err
	}

	for _, t := range tombstones {
		t.MergedInto = survivor.ID
		if err := tx.Persons().Update(ctx, t); err != nil {
			return sum, err
		}
	}

	receipts, err := tx.Receipts().ListByPayers(ctx, []domain.PersonID{merged.ID})
	if err != nil {
		return sum, err
	}

	sum.Receipts = len(receipts)

	// The tombstone gives up its member number first, so the survivor may
	// take it.
	sum.Survivor, sum.Merged = mergeFields(survivor, merged, req.Take, notes)
	if err := errors.Join(sum.Survivor.Validate(), sum.Merged.Validate()); err != nil {
		return sum, err
	}

	if err := tx.Persons().Update(ctx, sum.Merged); err != nil {
		return sum, err
	}

	if err := tx.Persons().Update(ctx, sum.Survivor); err != nil {
		return sum, err
	}

	e := domain.AuditEvent{
		ID: ids.New[domain.AuditEventID](s.ids), At: s.clock.Now(), ActorID: actorOf(p), Action: domain.AuditPersonMerged,
		TargetKind: domain.AuditTargetPerson, TargetPersonID: survivor.ID,
		Details: domain.AuditDetails{
			MergedPersonID: merged.ID, MergedName: merged.GivenName + " " + merged.FamilyName, Via: p.Via,
		},
	}
	if err := e.Validate(); err != nil {
		return sum, err
	}

	return sum, tx.Audit().Create(ctx, e)
}

// mergeAccount returns the merged person's account to move to the
// survivor, nil if it has none, and refuses the merge if the survivor has
// one as well, unless the merged person's is disabled: that one stays with
// the tombstone.
func mergeAccount(ctx context.Context, tx store.Tx, survivor, merged domain.PersonID) (*domain.Account, error) {
	var accounts [2]*domain.Account

	for i, id := range []domain.PersonID{survivor, merged} {
		switch a, err := tx.Accounts().GetByPerson(ctx, id); {
		case err == nil:
			accounts[i] = &a
		case !errors.Is(err, domain.ErrNotFound):
			return nil, err
		}
	}

	switch {
	case accounts[0] == nil || accounts[1] == nil:
		return accounts[1], nil
	case !accounts[1].DisabledAt.IsZero():
		return nil, nil // it signs in no more; it stays with the tombstone
	default:
		return nil, ErrMergeTwoAccounts
	}
}

// mergeLinks moves the guardian links of merged to survivor, in both
// directions, and returns how many moved and how many it dropped: links the
// survivor has already, and links between the two. A link that would close
// a cycle is ErrMergeCycle.
func mergeLinks(ctx context.Context, tx store.Tx, survivor, merged domain.PersonID) (moved, dropped int, err error) {
	move := func(list func(context.Context, domain.PersonID) ([]domain.Person, error), link func(a, b domain.PersonID) domain.Guardian) error {
		had, err := list(ctx, survivor)
		if err != nil {
			return err
		}

		others, err := list(ctx, merged)
		if err != nil {
			return err
		}

		for _, o := range others {
			if err := tx.Persons().RemoveGuardian(ctx, link(o.ID, merged)); err != nil {
				return err
			}

			if o.ID == survivor || slices.ContainsFunc(had, func(h domain.Person) bool { return h.ID == o.ID }) {
				dropped++

				continue
			}

			if err := tx.Persons().AddGuardian(ctx, link(o.ID, survivor)); err != nil {
				return err
			}

			moved++
		}

		return nil
	}

	asGuardian := func(other, p domain.PersonID) domain.Guardian {
		return domain.Guardian{GuardianID: other, DependentID: p}
	}
	asDependent := func(other, p domain.PersonID) domain.Guardian {
		return domain.Guardian{GuardianID: p, DependentID: other}
	}

	if err := move(tx.Persons().ListGuardians, asGuardian); err != nil {
		return 0, 0, err
	}

	if err := move(tx.Persons().ListDependents, asDependent); err != nil {
		return 0, 0, err
	}

	// The links were a forest before; a cycle now runs through the
	// survivor.
	switch cycle, err := isDependentOf(ctx, tx, survivor, survivor); {
	case err != nil:
		return 0, 0, err
	case cycle:
		return 0, 0, ErrMergeCycle
	}

	return moved, dropped, nil
}

// mergeLessons moves the bookings, waiting-list entries and class
// memberships of merged to survivor, under the locks of the terms and
// classes of the memberships and of every lesson concerned. An active
// booking of merged of a lesson that has not started is canceled first,
// timely and without releasing the date, where the survivor has an active
// booking of the lesson, and where it is an automatic class booking that
// the survivor's membership of the class would not have (a pause, a later
// start, a date it released). Of a lesson that has started, both bookings
// stay: merged's moves as history (domain.Booking.MergedFrom).
func (s *PersonService) mergeLessons(
	ctx context.Context, tx store.Tx, p Principal, survivor, merged domain.PersonID, sum *MergeSummary,
) error {
	memberships, err := lockMemberships(ctx, tx, merged)
	if err != nil {
		return err
	}

	lessons, bookings, entries, err := lockBookedLessons(ctx, tx, store.BookingQuery{
		Persons: []domain.PersonID{merged},
	})
	if err != nil {
		return err
	}

	var c charges

	if len(lessons) > 0 {
		if c, err = chargesIn(ctx, tx); err != nil {
			return err
		}
	}

	now := s.clock.Now()
	lessonIDs := slices.Collect(maps.Keys(lessons))

	theirs, err := tx.Bookings().ListByLessonsAndPersons(ctx, lessonIDs, []domain.PersonID{survivor})
	if err != nil {
		return err
	}

	// The lessons the survivor has an active booking of, after the merge,
	// and the dates it released.
	booked, released := map[domain.LessonID]bool{}, map[domain.LessonID]bool{}

	for _, b := range theirs {
		switch {
		case b.Status != domain.BookingCanceled:
			booked[b.LessonID] = true
		case b.Released:
			released[b.LessonID] = true
		}
	}

	// The survivor's memberships of the classes merged is a member of too
	// (locked above); merged's automatic bookings of their lessons follow
	// them.
	own, err := tx.Classes().ListMemberships(ctx, survivor)
	if err != nil {
		return err
	}

	survivorIn := map[domain.ClassID]domain.ClassMember{}

	for _, m := range own {
		if slices.ContainsFunc(memberships, func(o domain.ClassMember) bool { return o.ClassID == m.ClassID }) {
			survivorIn[m.ClassID] = m
		}
	}

	// unwanted reports why an active booking b of merged of a lesson l that
	// has not started is canceled, if it is.
	unwanted := func(l domain.Lesson, b domain.Booking) (MergeCancelReason, bool) {
		if booked[l.ID] {
			return MergeCancelBooked, true
		}

		m, member := survivorIn[l.ClassID]
		if b.Source == domain.BookingSourceMember && member && (!m.ActiveOn(l.SlotDate) || released[l.ID]) {
			return MergeCancelMembership, true
		}

		return "", false
	}

	byLesson := map[domain.LessonID][]domain.Booking{}
	for _, b := range bookings {
		byLesson[b.LessonID] = append(byLesson[b.LessonID], b)
	}

	courseTypes := map[domain.CourseTypeID]domain.CourseType{}

	for _, id := range sortedLessonIDs(slices.Collect(maps.Keys(byLesson))) {
		l := lessons[id]
		started := !l.StartsAt.After(now)
		changes := make([]bookingChange, 0, len(byLesson[id]))

		for _, b := range byLesson[id] {
			history := false

			if b.Status != domain.BookingCanceled {
				reason, cancel := unwanted(l, b)

				switch {
				case started && booked[id]:
					// History: the merged person's booking stays as it
					// was, beside the survivor's.
					history = true
					sum.History++
				case started || !cancel:
					booked[id] = true
				default:
					// A timely cancellation gives a visit it cost back.
					b.Late, b.Released = false, false
					if b, err = cancelBooking(ctx, tx, s.w, c, actorOf(p), now, l, b); err != nil {
						return err
					}

					ct, err := cached(ctx, courseTypes, l.CourseTypeID, tx.CourseTypes().Get)
					if err != nil {
						return fmt.Errorf("course type: %w", refErr(err))
					}

					sum.Canceled = append(sum.Canceled, MergeCanceled{Lesson: l, CourseType: ct, Reason: reason})
				}
			}

			moved := b
			moved.PersonID = survivor

			if history && moved.MergedFrom.IsZero() {
				moved.MergedFrom = merged
			}

			changes = append(changes, bookingChange{before: b, after: moved})
		}

		if err := updateBookings(ctx, tx, c, now, l, changes...); err != nil {
			return err
		}

		sum.BookingsMoved += len(changes)
	}

	slices.SortFunc(sum.Canceled, func(a, b MergeCanceled) int {
		return cmp.Or(a.Lesson.StartsAt.Compare(b.Lesson.StartsAt), bytes.Compare(a.Lesson.ID[:], b.Lesson.ID[:]))
	})

	if err := s.mergeWaitlist(ctx, tx, now, lessons, booked, entries, survivor, sum); err != nil {
		return err
	}

	return mergeMemberships(ctx, tx, memberships, survivor, merged, sum)
}

// lockClasses locks the terms, then the classes, of the memberships, each
// in ID order.
func lockClasses(ctx context.Context, tx store.Tx, memberships []domain.ClassMember) error {
	classIDs := make([]domain.ClassID, len(memberships))
	for i, m := range memberships {
		classIDs[i] = m.ClassID
	}

	classes, err := tx.Classes().ListByIDs(ctx, classIDs)
	if err != nil {
		return err
	}

	var terms []domain.TermID

	for _, c := range classes {
		if !slices.Contains(terms, c.TermID) {
			terms = append(terms, c.TermID)
		}
	}

	slices.SortFunc(terms, func(a, b domain.TermID) int { return bytes.Compare(a[:], b[:]) })

	for _, t := range terms {
		if _, err := tx.Terms().Lock(ctx, t); err != nil {
			return err
		}
	}

	for _, c := range classes { // ListByIDs orders by ID
		if _, err := tx.Classes().Lock(ctx, c.ID); err != nil {
			return err
		}
	}

	return nil
}

// lockMemberships returns the class memberships of person, read under the
// locks of their terms and classes (lockClasses). A membership added
// meanwhile to a class not locked yet adds its class.
func lockMemberships(ctx context.Context, tx store.Tx, person domain.PersonID) ([]domain.ClassMember, error) {
	var locked []domain.ClassMember

	for {
		memberships, err := tx.Classes().ListMemberships(ctx, person)
		if err != nil {
			return nil, err
		}

		missing := slices.DeleteFunc(slices.Clone(memberships), func(m domain.ClassMember) bool {
			return slices.ContainsFunc(locked, func(l domain.ClassMember) bool { return l.ClassID == m.ClassID })
		})
		if len(missing) == 0 {
			return memberships, nil
		}

		if err := lockClasses(ctx, tx, missing); err != nil {
			return nil, err
		}

		locked = append(locked, missing...)
	}
}

// lockBookedLessons locks the lessons of the bookings q finds and of the
// open waiting-list entries of q's persons, in ID order, and returns them
// with those bookings and entries read under the locks. A booking or an
// entry added meanwhile for a lesson not locked yet adds its lesson. The
// merge takes every booking of the merged person, anonymizing the active
// bookings of lessons that have not started.
func lockBookedLessons(
	ctx context.Context, tx store.Tx, q store.BookingQuery,
) (map[domain.LessonID]domain.Lesson, []domain.Booking, []domain.WaitlistEntry, error) {
	lessons := map[domain.LessonID]domain.Lesson{}

	for {
		bookings, err := tx.Bookings().ListByPersons(ctx, q)
		if err != nil {
			return nil, nil, nil, err
		}

		entries, err := tx.Waitlist().ListOpenByPersons(ctx, q.Persons)
		if err != nil {
			return nil, nil, nil, err
		}

		var missing []domain.LessonID

		for _, id := range slices.Concat(
			mapSlice(bookings, func(b domain.Booking) domain.LessonID { return b.LessonID }),
			mapSlice(entries, func(e domain.WaitlistEntry) domain.LessonID { return e.LessonID }),
		) {
			if _, ok := lessons[id]; !ok && !slices.Contains(missing, id) {
				missing = append(missing, id)
			}
		}

		if len(missing) == 0 {
			return lessons, bookings, entries, nil
		}

		for _, id := range sortedLessonIDs(missing) {
			l, err := tx.Lessons().Lock(ctx, id)
			if err != nil {
				return nil, nil, nil, err
			}

			lessons[id] = l
		}
	}
}

// mergeWaitlist moves the open waiting-list entries of the merged person to
// the survivor, except where the survivor has an active booking or an open
// entry of the lesson: there they are withdrawn, and so is an open entry of
// the survivor of a lesson it now has a booking of.
func (s *PersonService) mergeWaitlist(
	ctx context.Context, tx store.Tx, now time.Time, lessons map[domain.LessonID]domain.Lesson,
	booked map[domain.LessonID]bool, entries []domain.WaitlistEntry, survivor domain.PersonID, sum *MergeSummary,
) error {
	own, err := tx.Waitlist().ListOpenByPersons(ctx, []domain.PersonID{survivor})
	if err != nil {
		return err
	}

	waiting := map[domain.LessonID]bool{}

	for _, e := range own {
		if _, locked := lessons[e.LessonID]; !locked {
			continue
		}

		if booked[e.LessonID] {
			if err := s.w.end(ctx, tx, now, lessons[e.LessonID], e, domain.WaitlistWithdrawn); err != nil {
				return err
			}

			sum.WaitlistWithdrawn++

			continue
		}

		waiting[e.LessonID] = true
	}

	slices.SortFunc(entries, func(a, b domain.WaitlistEntry) int { return bytes.Compare(a.LessonID[:], b.LessonID[:]) })

	for _, e := range entries {
		if booked[e.LessonID] || waiting[e.LessonID] {
			if err := s.w.end(ctx, tx, now, lessons[e.LessonID], e, domain.WaitlistWithdrawn); err != nil {
				return err
			}

			sum.WaitlistWithdrawn++

			continue
		}

		e.PersonID = survivor
		if err := s.w.update(ctx, tx, e); err != nil {
			return err
		}

		sum.WaitlistMoved++
	}

	return nil
}

// mergeMemberships moves the class memberships of merged to survivor; a
// class the survivor is a member of already keeps the survivor's.
func mergeMemberships(
	ctx context.Context, tx store.Tx, memberships []domain.ClassMember, survivor, merged domain.PersonID,
	sum *MergeSummary,
) error {
	own, err := personClasses(ctx, tx, survivor)
	if err != nil {
		return err
	}

	dropped, err := personClasses(ctx, tx, merged)
	if err != nil {
		return err
	}

	for _, m := range memberships {
		if err := tx.Classes().RemoveMember(ctx, m.ClassID, merged); err != nil {
			return err
		}

		if slices.ContainsFunc(own, func(pc PersonClass) bool { return pc.Class.ID == m.ClassID }) {
			continue
		}

		m.PersonID = survivor
		if err := tx.Classes().AddMember(ctx, m); err != nil {
			return err
		}

		sum.MembershipsMoved++
	}

	sum.MembershipsDropped = slices.DeleteFunc(dropped, func(pc PersonClass) bool {
		return !slices.ContainsFunc(own, func(o PersonClass) bool { return o.Class.ID == pc.Class.ID })
	})

	return nil
}

// mergePasses moves the passes merged owns and those it paid for to
// survivor, under the lock of the passes of everyone concerned.
func mergePasses(ctx context.Context, tx store.Tx, survivor, merged domain.PersonID, sum *MergeSummary) error {
	paid, err := tx.Passes().ListByPayers(ctx, []domain.PersonID{merged})
	if err != nil {
		return err
	}

	owners := []domain.PersonID{survivor, merged}
	for _, p := range paid {
		if !slices.Contains(owners, p.PersonID) {
			owners = append(owners, p.PersonID)
		}
	}

	passes, err := tx.Passes().LockByPersons(ctx, owners...)
	if err != nil {
		return fmt.Errorf("lock passes: %w", err)
	}

	for _, p := range passes {
		if p.PersonID != merged && p.PayerID != merged {
			continue
		}

		if p.PersonID == merged {
			p.PersonID = survivor
			sum.PassesMoved++
		}

		if p.PayerID == merged {
			p.PayerID = survivor
			sum.PaidPassesMoved++
		}

		if err := tx.Passes().Update(ctx, p); err != nil {
			return err
		}
	}

	return nil
}

// mergeDismissals moves the duplicate dismissals that name merged to
// survivor; one of the pair of the two goes, and so does one of a pair the
// survivor has a dismissal of already (DuplicateDismissalRepo.Create keeps
// the survivor's).
func mergeDismissals(ctx context.Context, tx store.Tx, survivor, merged domain.PersonID) error {
	dismissals, err := tx.DuplicateDismissals().ListByPerson(ctx, merged)
	if err != nil {
		return err
	}

	for _, d := range dismissals {
		if err := tx.DuplicateDismissals().Delete(ctx, d.PersonA, d.PersonB); err != nil {
			return err
		}

		other := d.PersonA
		if other == merged {
			other = d.PersonB
		}

		if other == survivor {
			continue
		}

		moved := domain.NewDuplicateDismissal(survivor, other, d.DismissedBy, d.DismissedAt)
		if err := tx.DuplicateDismissals().Create(ctx, moved); err != nil {
			return err
		}
	}

	return nil
}

// mergeNotes are the notes of both: the survivor's, then the merged
// person's unless the survivor's contain them already.
func mergeNotes(survivor, merged string) string {
	if merged == "" || strings.Contains(survivor, merged) {
		return survivor
	}

	return strings.TrimSpace(survivor + "\n\n" + merged)
}

// mergeFields returns the survivor with the fields take names from merged
// and notes, and merged as a tombstone of it.
func mergeFields(survivor, merged domain.Person, take MergeTake, notes string) (domain.Person, domain.Person) {
	if take.Name {
		survivor.GivenName, survivor.FamilyName = merged.GivenName, merged.FamilyName
	}

	if take.Email {
		survivor.Email = merged.Email
	}

	if take.Phone {
		survivor.Phone = merged.Phone
	}

	if take.BirthDate {
		survivor.BirthDate = merged.BirthDate
	}

	if take.MemberNo {
		survivor.MemberNo = merged.MemberNo
	}

	survivor.Notes = notes
	merged.MergedInto, merged.MemberNo = survivor.ID, ""

	return survivor, merged
}

func sortedLessonIDs(ids []domain.LessonID) []domain.LessonID {
	slices.SortFunc(ids, func(a, b domain.LessonID) int { return bytes.Compare(a[:], b[:]) })

	return ids
}

func mapSlice[T, U any](s []T, f func(T) U) []U {
	out := make([]U, len(s))
	for i, v := range s {
		out[i] = f(v)
	}

	return out
}
