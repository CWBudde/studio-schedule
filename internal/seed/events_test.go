package seed_test

import (
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/receiptfmt"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

// demoEvents are the three events of a demo school (ADR 0019), told apart
// by status and price.
type demoEvents struct {
	intensive, taster, draft domain.Event
}

// eventsOf returns the events of d: the published one with a price (the
// intensive), the published free one (the taster) and the draft.
func eventsOf(t *testing.T, what string, d seed.Dataset) demoEvents {
	t.Helper()

	if len(d.Events) != 3 {
		t.Fatalf("%s: %d events, want 3", what, len(d.Events))
	}

	var (
		got   demoEvents
		found int
	)

	for _, e := range d.Events {
		switch {
		case e.Status == domain.EventDraft && got.draft.ID.IsZero():
			got.draft = e
		case e.Status == domain.EventPublished && e.Price > 0 && got.intensive.ID.IsZero():
			got.intensive = e
		case e.Status == domain.EventPublished && e.Price == 0 && got.taster.ID.IsZero():
			got.taster = e
		default:
			t.Fatalf("%s: event %+v: want one draft, one published with a price and one published free", what, e)
		}

		found++
	}

	if found != 3 {
		t.Fatalf("%s: %d events told apart, want 3", what, found)
	}

	return got
}

// family returns the person of the account with email in d, their
// guardians and their dependents.
func family(t *testing.T, what string, d seed.Dataset, email string) (person domain.PersonID, members map[domain.PersonID]bool) {
	t.Helper()

	i := slices.IndexFunc(d.Accounts, func(a domain.Account) bool { return a.Email == email })
	if i < 0 {
		t.Fatalf("%s: no account %s", what, email)
	}

	person = d.Accounts[i].PersonID
	members = map[domain.PersonID]bool{person: true}

	for _, g := range d.Guardians {
		switch person {
		case g.GuardianID:
			members[g.DependentID] = true
		case g.DependentID:
			members[g.GuardianID] = true
		}
	}

	return person, members
}

// TestDemoEvents checks the events of every demo school for every anchor
// (ADR 0019): a published intensive over the first weekend after the
// anchor, booked up but one place, with one fee paid at the desk and its
// receipt; a published free taster on a Sunday in the second week with a
// guest the office added; and a draft without registrations three weeks
// out. Every session is a public workshop lesson of its event, after the
// anchor; nobody of a demo login's family is registered, so the member
// login can register a child for the intensive's last place.
func TestDemoEvents(t *testing.T) {
	t.Parallel()

	for _, school := range seed.Schools() {
		t.Run(string(school), func(t *testing.T) {
			t.Parallel()

			logins, err := seed.SchoolLogins(school)
			if err != nil {
				t.Fatal(err)
			}

			for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
				d, err := seed.DemoSchoolAt(anchor, school)
				if err != nil {
					t.Fatalf("%s %v: %v", school, anchor, err)
				}

				checkEvents(t, string(school)+" "+anchor.String(), anchor, d, logins)
			}
		})
	}
}

// checkEvents checks d's events, sessions and registrations for anchor
// (TestDemoEvents).
func checkEvents(t *testing.T, what string, anchor domain.Date, d seed.Dataset, logins []seed.Login) {
	t.Helper()

	loc, err := time.LoadLocation(d.Settings.Timezone)
	if err != nil {
		t.Fatal(err)
	}

	validAll(t, what, d.Events)
	validAll(t, what, d.EventRegistrations)

	events := eventsOf(t, what, d)
	byID := index(d.Events, func(e domain.Event) domain.EventID { return e.ID })
	accounts := index(d.Accounts, func(a domain.Account) domain.AccountID { return a.ID })
	start := anchor.At(domain.TimeOfDay{}, loc)

	slugs := map[string]bool{}

	for _, e := range d.Events {
		if slugs[e.Slug] {
			t.Errorf("%s: slug %q twice", what, e.Slug)
		}

		slugs[e.Slug] = true

		if a := accounts[e.CreatedBy]; a.Role != domain.RoleOffice || !e.CreatedAt.Before(start) {
			t.Errorf("%s: event %q created by %+v at %v, want the office before the anchor", what, e.Slug, a,
				e.CreatedAt)
		}
	}

	sessions := checkSessions(t, what, anchor, d, loc)

	// The intensive: the first weekend after the anchor, Saturday and
	// Sunday; the taster: the first Sunday ten days on; the draft: the
	// first Sunday three weeks on.
	saturday := firstOn(anchor.AddDays(1), time.Saturday)

	for _, c := range []struct {
		event     domain.Event
		days      []domain.Date
		nSessions int
	}{
		{events.intensive, []domain.Date{saturday, saturday.AddDays(1)}, 3},
		{events.taster, []domain.Date{firstOn(anchor.AddDays(10), time.Sunday)}, 1},
		{events.draft, []domain.Date{firstOn(anchor.AddDays(21), time.Sunday)}, 1},
	} {
		ss := sessions[c.event.ID]
		if len(ss) != c.nSessions {
			t.Fatalf("%s: event %q has %d sessions, want %d", what, c.event.Slug, len(ss), c.nSessions)
		}

		var days []domain.Date
		for _, l := range ss {
			if day := domain.DateOf(l.StartsAt, loc); !slices.Contains(days, day) {
				days = append(days, day)
			}
		}

		if !slices.Equal(days, c.days) {
			t.Errorf("%s: event %q on %v, want %v", what, c.event.Slug, days, c.days)
		}
	}

	regs := map[domain.EventID][]domain.EventRegistration{}
	for _, r := range d.EventRegistrations {
		regs[r.EventID] = append(regs[r.EventID], r)
	}

	checkRegistrations(t, what, start, d, byID)
	checkIntensive(t, what, d, events.intensive, regs[events.intensive.ID], sessions[events.intensive.ID], loc)
	checkTaster(t, what, events.taster, regs[events.taster.ID])

	if n := len(regs[events.draft.ID]); n != 0 {
		t.Errorf("%s: the draft has %d registrations, want none", what, n)
	}

	// Nobody of a demo login's family is registered; the member login has
	// a dependent to register for the intensive's last place.
	registered := map[domain.PersonID]bool{}
	for _, r := range d.EventRegistrations {
		registered[r.PersonID] = true
	}

	for i, l := range logins {
		person, members := family(t, what, d, l.Email)

		for id := range members {
			if registered[id] {
				t.Errorf("%s: %v of the family of the %s login %s is registered for an event", what, id, l.Role,
					l.Email)
			}
		}

		if i > 0 {
			continue
		}

		dependents := 0

		for _, g := range d.Guardians {
			if g.GuardianID == person {
				dependents++
			}
		}

		if l.Role != domain.RoleMember || dependents == 0 {
			t.Errorf("%s: the member login %s (%s) has %d dependents, want one to register", what, l.Email, l.Role,
				dependents)
		}
	}
}

// firstOn returns the first day on or after day that is a weekday w.
func firstOn(day domain.Date, w time.Weekday) domain.Date {
	for day.Weekday() != w {
		day = day.AddDays(1)
	}

	return day
}

// checkSessions checks that every lesson of an event is a valid, scheduled,
// public workshop of no class with the event's course type and capacity,
// in a room large enough, after the anchor and in the term, and returns
// the sessions by event in the order of d.Lessons.
func checkSessions(
	t *testing.T, what string, anchor domain.Date, d seed.Dataset, loc *time.Location,
) map[domain.EventID][]domain.Lesson {
	t.Helper()

	byID := index(d.Events, func(e domain.Event) domain.EventID { return e.ID })
	rooms := index(d.Rooms, func(r domain.Room) domain.RoomID { return r.ID })
	term := d.Terms[0]
	sessions := map[domain.EventID][]domain.Lesson{}

	for _, l := range d.Lessons {
		if l.EventID.IsZero() {
			continue
		}

		if err := l.Validate(); err != nil {
			t.Errorf("%s: session %+v: %v", what, l, err)
		}

		e, ok := byID[l.EventID]
		if !ok {
			t.Errorf("%s: session %v of an unknown event", what, l.ID)

			continue
		}

		sessions[e.ID] = append(sessions[e.ID], l)

		if l.Kind != domain.LessonWorkshop || !l.ClassID.IsZero() || l.Status != domain.LessonScheduled ||
			l.Visibility != domain.VisibilityPublic || l.CourseTypeID != e.CourseTypeID || l.Capacity != e.Capacity {
			t.Errorf("%s: session %+v of %q: want a scheduled public workshop of no class with the event's "+
				"course type and capacity %d", what, l, e.Slug, e.Capacity)
		}

		if room, ok := rooms[l.RoomID]; !ok || l.Capacity > room.Capacity {
			t.Errorf("%s: session %v of %q: unknown room or capacity %d above the room's", what, l.ID, e.Slug,
				l.Capacity)
		}

		day := domain.DateOf(l.StartsAt, loc)
		if day.Compare(anchor) <= 0 || day.Compare(term.EndsOn) > 0 {
			t.Errorf("%s: session %v of %q on %v, want after the anchor, in the term", what, l.ID, e.Slug, day)
		}
	}

	for id, ss := range sessions {
		if !slices.IsSortedFunc(ss, func(a, b domain.Lesson) int { return a.StartsAt.Compare(b.StartsAt) }) {
			t.Errorf("%s: the sessions of %q are not in order", what, byID[id].Slug)
		}
	}

	return sessions
}

// checkRegistrations checks what holds for every registration: an open
// one for a known event, made after the event and before the anchor
// (start), at most one per person and event, a person's by their own
// account or a guardian's, a guest's by the office.
func checkRegistrations(
	t *testing.T, what string, start time.Time, d seed.Dataset, events map[domain.EventID]domain.Event,
) {
	t.Helper()

	persons := index(d.Persons, func(p domain.Person) domain.PersonID { return p.ID })
	accounts := index(d.Accounts, func(a domain.Account) domain.AccountID { return a.ID })
	seen := map[[2]string]bool{}

	for _, r := range d.EventRegistrations {
		e, ok := events[r.EventID]
		if !ok {
			t.Errorf("%s: registration %v for an unknown event", what, r.ID)

			continue
		}

		if r.Status != domain.EventRegistered || r.Price != e.Price || r.VATRate != e.VATRate ||
			!r.CreatedAt.After(e.CreatedAt) || !r.CreatedAt.Before(start) {
			t.Errorf("%s: registration %+v for %q: want registered at the event's price, after the event was "+
				"created and before the anchor", what, r, e.Slug)
		}

		by := accounts[r.RegisteredBy]

		if r.PersonID.IsZero() {
			if by.Role != domain.RoleOffice {
				t.Errorf("%s: guest registration %+v by %+v, want the office", what, r, by)
			}

			continue
		}

		if _, ok := persons[r.PersonID]; !ok {
			t.Errorf("%s: registration %v of an unknown person", what, r.ID)
		}

		key := [2]string{e.ID.String(), r.PersonID.String()}
		if seen[key] {
			t.Errorf("%s: %v registered twice for %q", what, r.PersonID, e.Slug)
		}

		seen[key] = true

		if by.ID.IsZero() || (by.PersonID != r.PersonID &&
			!slices.Contains(d.Guardians, domain.Guardian{GuardianID: by.PersonID, DependentID: r.PersonID})) {
			t.Errorf("%s: registration %+v by %+v, want the person's own account or a guardian's", what, r, by)
		}
	}
}

// checkIntensive checks the intensive's registrations: one place free,
// one fee paid with its receipt (the last receipt issued, for the price,
// paid by the person, with the line ReceiptService.issueEventFee writes),
// the others unpaid.
func checkIntensive(
	t *testing.T, what string, d seed.Dataset, e domain.Event, regs []domain.EventRegistration,
	sessions []domain.Lesson, loc *time.Location,
) {
	t.Helper()

	if len(regs) != e.Capacity-1 || e.Capacity != 12 {
		t.Errorf("%s: %d registrations for %q of %d places, want 11 of 12", what, len(regs), e.Slug, e.Capacity)
	}

	receipts := index(d.Receipts, func(r domain.Receipt) domain.ReceiptID { return r.ID })
	persons := index(d.Persons, func(p domain.Person) domain.PersonID { return p.ID })
	paid := 0

	for _, r := range regs {
		if r.Payment != domain.EventPaid {
			if r.Payment != domain.EventUnpaid || !r.ReceiptID.IsZero() {
				t.Errorf("%s: registration %+v for %q: want unpaid without a receipt", what, r, e.Slug)
			}

			continue
		}

		paid++

		rc, ok := receipts[r.ReceiptID]
		p := persons[r.PersonID]
		line := receiptfmt.EventLine(rc.Locale, e.Title, p.GivenName+" "+p.FamilyName,
			domain.DateOf(sessions[0].StartsAt, loc), domain.DateOf(sessions[len(sessions)-1].StartsAt, loc))

		if !ok || rc.Kind != domain.ReceiptSale || rc.Gross != e.Price || rc.PayerID != r.PersonID ||
			!rc.IssuedAt.Equal(r.UpdatedAt) || !rc.IssuedAt.After(r.CreatedAt) ||
			len(rc.Lines) != 1 || rc.Lines[0].Description != line || rc.Lines[0].VATRate != e.VATRate {
			t.Errorf("%s: paid registration %+v for %q, receipt %+v: want a sale of the price to the person "+
				"with the line %q", what, r, e.Slug, rc, line)
		}

		if last := d.Receipts[len(d.Receipts)-1]; last.ID != rc.ID {
			t.Errorf("%s: the fee's receipt %s is not the last issued (%s)", what, rc.Number, last.Number)
		}
	}

	if paid != 1 {
		t.Errorf("%s: %d paid registrations for %q, want 1", what, paid, e.Slug)
	}
}

// checkTaster checks the free taster's registrations: two persons and one
// guest the office added, all paid from the start without a receipt.
func checkTaster(t *testing.T, what string, e domain.Event, regs []domain.EventRegistration) {
	t.Helper()

	if e.Price != 0 || len(regs) != 3 {
		t.Errorf("%s: %q at %v with %d registrations, want free with 3", what, e.Slug, e.Price, len(regs))
	}

	guests := 0

	for _, r := range regs {
		if r.Payment != domain.EventPaid || !r.ReceiptID.IsZero() {
			t.Errorf("%s: registration %+v for %q: want paid without a receipt", what, r, e.Slug)
		}

		if !r.PersonID.IsZero() {
			continue
		}

		guests++

		if r.GuestName == "" || r.GuestEmail == "" || !exampleEmail(r.GuestEmail) {
			t.Errorf("%s: guest registration %+v: want a name and an email on an example domain", what, r)
		}
	}

	if guests != 1 {
		t.Errorf("%s: %d guests registered for %q, want 1", what, guests, e.Slug)
	}
}
