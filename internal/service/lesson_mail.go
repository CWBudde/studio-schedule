package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// recipient is one address mailed about a lesson, with the booked persons
// it gets the mail for (ADR 0007).
type recipient struct {
	email string
	// locale is the language of the receiving account; empty for an
	// address without an account (the school's default).
	locale    string
	givenName string
	// account tells that email is an account's address. Its language and
	// name win over a person's own address that is the same.
	account bool
	// guest tells that the address is a guest's (guestRecipient).
	guest        bool
	participants []string
}

// lessonRecipients returns who is mailed about the active bookings of a
// lesson, in the order of the bookings (ADR 0007). For a booked person
// with a disabled account, nobody. Otherwise their account's address if it
// is confirmed, else their own address, else the addresses of the confirmed
// accounts of their guardians that are not disabled; an unconfirmed or
// disabled guardian account gets nothing, and so does a guardian without an
// account. For a guest, the guest's address, if they gave one, in the
// school's default language (guestRecipient). Each address appears once,
// ignoring case, with every booked person it is mailed for; an address that
// is an account's and also a person's own is mailed as the account's.
func lessonRecipients(ctx context.Context, tx store.Tx, lesson domain.LessonID) ([]recipient, error) {
	bookings, err := tx.Bookings().ListByLesson(ctx, lesson)
	if err != nil {
		return nil, err
	}

	bookings = slices.DeleteFunc(bookings, func(b domain.Booking) bool { return b.Status == domain.BookingCanceled })

	var booked []domain.PersonID

	for _, b := range bookings {
		if !b.PersonID.IsZero() && !slices.Contains(booked, b.PersonID) {
			booked = append(booked, b.PersonID)
		}
	}

	chains, err := recipientsOf(ctx, tx, booked)
	if err != nil {
		return nil, err
	}

	var set recipientSet

	for _, b := range bookings {
		if b.PersonID.IsZero() {
			if r, ok := guestRecipient(b); ok {
				set.add(r, b.GuestName)
			}

			continue
		}

		for _, r := range chains[b.PersonID] {
			set.add(r, r.participants[0])
		}
	}

	return set.list, nil
}

// recipientSet collects recipients, each address once ignoring case, in
// the order they first come, with every participant each gets the mail
// for. An address that comes as an account's and as a person's own is
// mailed as the account's. The zero value is empty.
type recipientSet struct {
	list []recipient
	// index is the position in list by lowercased address.
	index map[string]int
}

// add adds r with participants (r's own are ignored).
func (s *recipientSet) add(r recipient, participants ...string) {
	key := strings.ToLower(r.email)

	i, ok := s.index[key]

	switch {
	case !ok:
		if s.index == nil {
			s.index = map[string]int{}
		}

		r.participants = nil // added below
		s.list = append(s.list, r)
		i = len(s.list) - 1
		s.index[key] = i
	case r.account && !s.list[i].account:
		o := &s.list[i]
		o.email, o.locale, o.givenName, o.account = r.email, r.locale, r.givenName, true
	}

	for _, p := range participants {
		if !slices.Contains(s.list[i].participants, p) {
			s.list[i].participants = append(s.list[i].participants, p)
		}
	}
}

// guestRecipient returns the guest of booking b as the recipient of the
// mails about it, greeted by their name, in the school's default language
// (no account has a language for them); false for a guest without an
// email (ADR 0009, amendment).
func guestRecipient(b domain.Booking) (recipient, bool) {
	if b.GuestEmail == "" {
		return recipient{}, false
	}

	return recipient{email: b.GuestEmail, givenName: b.GuestName, guest: true, participants: []string{b.GuestName}}, true
}

// mailsGuardians reports whether mail about booked person p goes to p's
// guardians: p has neither an account that decides (a confirmed or a
// disabled one) nor an address of its own.
func mailsGuardians(p domain.Person, accounts map[domain.PersonID]domain.Account) bool {
	acc, found := accounts[p.ID]
	decides := found && (!acc.DisabledAt.IsZero() || !acc.ConfirmedAt.IsZero())

	return !decides && p.Email == ""
}

// chainOf returns whom mail about booked person p goes to (see
// lessonRecipients), without participants: nobody for a disabled account,
// else the confirmed account, else p's own address, else the confirmed
// accounts of p's guardians (guardianIDs, ordered like PersonRepo.List)
// that are not disabled.
func chainOf(
	p domain.Person, accounts map[domain.PersonID]domain.Account, guardianIDs []domain.PersonID,
	guardians map[domain.PersonID]domain.Person, guardianAccounts map[domain.PersonID]domain.Account,
) []recipient {
	acc, found := accounts[p.ID]

	switch {
	case found && !acc.DisabledAt.IsZero():
		return nil
	case found && !acc.ConfirmedAt.IsZero():
		return []recipient{{email: acc.Email, locale: acc.Locale, givenName: p.GivenName, account: true}}
	case p.Email != "":
		return []recipient{{email: p.Email, givenName: p.GivenName}}
	}

	var out []recipient

	for _, g := range guardianIDs {
		acc, found := guardianAccounts[g]
		if found && acc.DisabledAt.IsZero() && !acc.ConfirmedAt.IsZero() {
			out = append(out, recipient{email: acc.Email, locale: acc.Locale, givenName: guardians[g].GivenName, account: true})
		}
	}

	return out
}

// recipientsOf returns, by person, whom mail about each of the persons goes
// to, by the chain of lessonRecipients, each recipient with the person as
// its one participant: the waiting-list mails (ADR 0010). A person whom the
// chain reaches nobody has none. It costs the same few queries for any
// number of persons.
func recipientsOf(
	ctx context.Context, tx store.Tx, ids []domain.PersonID,
) (map[domain.PersonID][]recipient, error) {
	persons, accounts, err := personsWithAccounts(ctx, tx, ids)
	if err != nil {
		return nil, err
	}

	var viaGuardians []domain.PersonID

	for _, id := range ids {
		p, ok := persons[id]
		if !ok {
			return nil, fmt.Errorf("person %v: %w", id, errBrokenReference)
		}

		if mailsGuardians(p, accounts) {
			viaGuardians = append(viaGuardians, id)
		}
	}

	guardiansOf, guardians, guardianAccounts, err := guardiansWithAccounts(ctx, tx, viaGuardians)
	if err != nil {
		return nil, err
	}

	out := make(map[domain.PersonID][]recipient, len(ids))

	for _, id := range ids {
		p := persons[id]

		rs := chainOf(p, accounts, guardiansOf[id], guardians, guardianAccounts)
		for i := range rs {
			rs[i].participants = []string{fullName(p)}
		}

		out[id] = rs
	}

	return out, nil
}

// personRecipients returns whom mail about person goes to (recipientsOf).
func personRecipients(ctx context.Context, tx store.Tx, person domain.PersonID) ([]recipient, error) {
	out, err := recipientsOf(ctx, tx, []domain.PersonID{person})

	return out[person], err
}

// personsWithAccounts returns the persons with the given IDs and, by
// person, the accounts of those that have one.
func personsWithAccounts(
	ctx context.Context, tx store.Tx, ids []domain.PersonID,
) (map[domain.PersonID]domain.Person, map[domain.PersonID]domain.Account, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}

	persons, err := tx.Persons().ListByIDs(ctx, ids)
	if err != nil {
		return nil, nil, err
	}

	accounts, err := tx.Accounts().ListByPersons(ctx, ids)
	if err != nil {
		return nil, nil, err
	}

	return byID(persons), byPerson(accounts), nil
}

// guardiansWithAccounts returns the guardians of each dependent, ordered
// like PersonRepo.List, the guardians by ID, and their accounts by person.
func guardiansWithAccounts(ctx context.Context, tx store.Tx, dependents []domain.PersonID) (
	map[domain.PersonID][]domain.PersonID, map[domain.PersonID]domain.Person, map[domain.PersonID]domain.Account, error,
) {
	if len(dependents) == 0 {
		return nil, nil, nil, nil
	}

	links, err := tx.Persons().ListGuardianLinks(ctx, dependents)
	if err != nil {
		return nil, nil, nil, err
	}

	ids := make([]domain.PersonID, len(links))
	for i, g := range links {
		ids[i] = g.GuardianID
	}

	ordered, err := tx.Persons().ListByIDs(ctx, ids)
	if err != nil {
		return nil, nil, nil, err
	}

	accounts, err := tx.Accounts().ListByPersons(ctx, ids)
	if err != nil {
		return nil, nil, nil, err
	}

	rank := make(map[domain.PersonID]int, len(ordered))
	for i, p := range ordered {
		rank[p.ID] = i
	}

	slices.SortStableFunc(links, func(a, b domain.Guardian) int {
		return cmp.Compare(rank[a.GuardianID], rank[b.GuardianID])
	})

	of := make(map[domain.PersonID][]domain.PersonID, len(dependents))
	for _, g := range links {
		of[g.DependentID] = append(of[g.DependentID], g.GuardianID)
	}

	return of, byID(ordered), byPerson(accounts), nil
}

// byID indexes persons by their ID.
func byID(persons []domain.Person) map[domain.PersonID]domain.Person {
	out := make(map[domain.PersonID]domain.Person, len(persons))
	for _, p := range persons {
		out[p.ID] = p
	}

	return out
}

// byPerson indexes accounts by their person.
func byPerson(accounts []domain.Account) map[domain.PersonID]domain.Account {
	out := make(map[domain.PersonID]domain.Account, len(accounts))
	for _, a := range accounts {
		out[a.PersonID] = a
	}

	return out
}

// mailLesson queues the template about lesson l (TemplateLessonCanceled or
// TemplateLessonRestored) for everyone with an active booking.
func (s *LessonService) mailLesson(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, l domain.Lesson, template string,
) error {
	return s.mailRecipients(ctx, tx, settings, loc, l, template, func(base mail.LessonData) any { return base })
}

// mailChange queues TemplateLessonChanged about the move of a lesson from
// old to l for everyone with an active booking.
func (s *LessonService) mailChange(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, old, l domain.Lesson,
) error {
	room, location, err := place(ctx, tx, old.RoomID)
	if err != nil {
		return err
	}

	return s.mailRecipients(ctx, tx, settings, loc, l, mail.TemplateLessonChanged, func(base mail.LessonData) any {
		return mail.LessonChangedData{
			LessonData: base, PreviousStartsAt: old.StartsAt.In(loc), PreviousEndsAt: old.EndsAt.In(loc),
			PreviousRoom: room.Name, PreviousLocation: location.Name,
		}
	})
}

// mailRecipients renders template for each recipient of lesson l, with the
// data that data makes of the recipient's mail.LessonData, and queues it.
// A lesson that does not start in the future mails nobody (ADR 0007).
func (s *LessonService) mailRecipients(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, l domain.Lesson, template string,
	data func(mail.LessonData) any,
) error {
	now := s.clock.Now()
	if !l.StartsAt.After(now) {
		return nil
	}

	recipients, err := lessonRecipients(ctx, tx, l.ID)
	if err != nil || len(recipients) == 0 {
		return err
	}

	ct, err := tx.CourseTypes().Get(ctx, l.CourseTypeID)
	if err != nil {
		return fmt.Errorf("course type %v: %w", l.CourseTypeID, refErr(err))
	}

	room, location, err := place(ctx, tx, l.RoomID)
	if err != nil {
		return err
	}

	return sendMails(ctx, tx, s.ids, s.mails, settings, now, recipients, template, mailOpts{}, func(r recipient) any {
		return data(mail.LessonData{
			SchoolName: settings.SchoolName, GivenName: r.givenName, Participants: r.participants,
			CourseType: ct.Name, StartsAt: l.StartsAt.In(loc), EndsAt: l.EndsAt.In(loc),
			Room: room.Name, Location: location.Name, Note: l.CancelNote,
		})
	})
}

// place returns a room and its location.
func place(ctx context.Context, tx store.Tx, id domain.RoomID) (domain.Room, domain.Location, error) {
	room, err := tx.Rooms().Get(ctx, id)
	if err != nil {
		return domain.Room{}, domain.Location{}, fmt.Errorf("room %v: %w", id, refErr(err))
	}

	location, err := tx.Locations().Get(ctx, room.LocationID)
	if err != nil {
		return domain.Room{}, domain.Location{}, fmt.Errorf("location %v: %w", room.LocationID, refErr(err))
	}

	return room, location, nil
}
