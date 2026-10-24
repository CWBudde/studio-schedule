package service

import (
	"cmp"
	"context"
	"errors"
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
	account      bool
	participants []string
}

// lessonRecipients returns who is mailed about the active bookings of a
// lesson, in the order of the bookings (ADR 0007). For a booked person
// with a disabled account, nobody. Otherwise their account's address if it
// is confirmed, else their own address, else the addresses of the confirmed
// accounts of their guardians that are not disabled; an unconfirmed or
// disabled guardian account gets nothing, and so does a guardian without an
// account. For a guest, the guest's address. Each address appears once,
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
		if !b.PersonID.IsZero() {
			booked = append(booked, b.PersonID)
		}
	}

	persons, accounts, err := personsWithAccounts(ctx, tx, booked)
	if err != nil {
		return nil, err
	}

	// Booked persons without an account that decides and without an
	// address of their own are mailed through their guardians.
	var viaGuardians []domain.PersonID

	for _, id := range booked {
		p, ok := persons[id]
		if !ok {
			return nil, fmt.Errorf("person %v: %w", id, errBrokenReference)
		}

		acc, found := accounts[id]
		if decides := found && (!acc.DisabledAt.IsZero() || !acc.ConfirmedAt.IsZero()); !decides && p.Email == "" {
			viaGuardians = append(viaGuardians, id)
		}
	}

	guardiansOf, guardians, guardianAccounts, err := guardiansWithAccounts(ctx, tx, viaGuardians)
	if err != nil {
		return nil, err
	}

	var out []recipient

	add := func(r recipient, participant string) {
		i := slices.IndexFunc(out, func(o recipient) bool { return strings.EqualFold(o.email, r.email) })

		switch {
		case i < 0:
			out = append(out, r)
			i = len(out) - 1
		case r.account && !out[i].account:
			out[i].email, out[i].locale, out[i].givenName, out[i].account = r.email, r.locale, r.givenName, true
		}

		if !slices.Contains(out[i].participants, participant) {
			out[i].participants = append(out[i].participants, participant)
		}
	}

	for _, b := range bookings {
		if b.PersonID.IsZero() {
			add(recipient{email: b.GuestEmail, givenName: b.GuestName}, b.GuestName)

			continue
		}

		p := persons[b.PersonID]
		name := fullName(p)
		acc, found := accounts[p.ID]

		switch {
		case found && !acc.DisabledAt.IsZero():
			continue
		case found && !acc.ConfirmedAt.IsZero():
			add(recipient{email: acc.Email, locale: acc.Locale, givenName: p.GivenName, account: true}, name)

			continue
		case p.Email != "":
			add(recipient{email: p.Email, givenName: p.GivenName}, name)

			continue
		}

		for _, g := range guardiansOf[p.ID] {
			acc, found := guardianAccounts[g]
			if found && acc.DisabledAt.IsZero() && !acc.ConfirmedAt.IsZero() {
				add(recipient{email: acc.Email, locale: acc.Locale, givenName: guardians[g].GivenName, account: true}, name)
			}
		}
	}

	return out, nil
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

	for _, r := range recipients {
		d := data(mail.LessonData{
			SchoolName: settings.SchoolName, GivenName: r.givenName, Participants: r.participants,
			CourseType: ct.Name, StartsAt: l.StartsAt.In(loc), EndsAt: l.EndsAt.In(loc),
			Room: room.Name, Location: location.Name, Note: l.CancelNote,
		})

		rendered, err := s.mails.Render(cmp.Or(r.locale, settings.DefaultLocale), template, d)
		if err != nil {
			// Broken data or templates, not the office's input: %v keeps
			// domain.ErrValidation out, so the client gets a 500.
			return fmt.Errorf("render %s mail: %v", template, err) //nolint:errorlint // see above
		}

		err = enqueueMail(ctx, tx, s.ids, now, r.email, template, rendered)

		switch {
		case errors.Is(err, domain.ErrValidation):
			// A stored address that is no address is broken data too, not
			// the office's input: a 500 like above.
			return fmt.Errorf("queue %s mail: %v", template, err) //nolint:errorlint // see above
		case err != nil:
			return err
		}
	}

	return nil
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
