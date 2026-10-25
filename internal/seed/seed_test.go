package seed_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func TestDemoSettingsValid(t *testing.T) {
	t.Parallel()

	if err := seed.DemoSettings().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDemo(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	got, err := service.NewSettingsService(st).Public(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if got != seed.DemoSettings() {
		t.Fatalf("got %+v, want demo settings", got)
	}
}

func TestDemoDeterministic(t *testing.T) {
	t.Parallel()

	a, b := seed.Demo(), seed.Demo()

	if a.Settings != b.Settings {
		t.Fatal("settings differ")
	}

	sameSlice(t, "locations", a.Locations, b.Locations)
	sameSlice(t, "rooms", a.Rooms, b.Rooms)
	sameSlice(t, "staff", a.Staff, b.Staff)
	sameSlice(t, "course types", a.CourseTypes, b.CourseTypes)
	sameSlice(t, "terms", a.Terms, b.Terms)
	sameSlice(t, "classes", a.Classes, b.Classes)
	sameSlice(t, "persons", a.Persons, b.Persons)
	sameSlice(t, "guardians", a.Guardians, b.Guardians)
	sameSlice(t, "accounts", a.Accounts, b.Accounts)
	sameSlice(t, "class members", a.ClassMembers, b.ClassMembers)
	sameSlice(t, "lessons", a.Lessons, b.Lessons)
	sameSlice(t, "bookings", a.Bookings, b.Bookings)
	sameSlice(t, "waitlist entries", a.WaitlistEntries, b.WaitlistEntries)
	sameSlice(t, "booking events", a.BookingEvents, b.BookingEvents)

	anchor := domain.Date{Year: 2031, Month: time.March, Day: 9}
	c, d := seed.DemoAt(anchor), seed.DemoAt(anchor)

	sameSlice(t, "terms at "+anchor.String(), c.Terms, d.Terms)
	sameSlice(t, "class members at "+anchor.String(), c.ClassMembers, d.ClassMembers)
	sameSlice(t, "lessons at "+anchor.String(), c.Lessons, d.Lessons)
}

// TestDemoFixedAnchor checks that the server's demo data (Demo, LoadDemo)
// is the fixed school year 2026/27.
func TestDemoFixedAnchor(t *testing.T) {
	t.Parallel()

	d := seed.Demo()
	want := domain.Term{
		ID:       d.Terms[0].ID,
		Name:     "School Year 2026/27",
		StartsOn: domain.Date{Year: 2026, Month: time.September, Day: 1},
		EndsOn:   domain.Date{Year: 2027, Month: time.July, Day: 31},
	}

	if len(d.Terms) != 1 || d.Terms[0] != want {
		t.Fatalf("terms = %+v, want %+v", d.Terms, want)
	}

	for _, m := range d.ClassMembers {
		if m.Since != want.StartsOn {
			t.Fatalf("class member %+v: since %v, want the term start", m, m.Since)
		}
	}

	at := seed.DemoAt(seed.FixedDemoAnchor)
	sameSlice(t, "terms", at.Terms, d.Terms)
	sameSlice(t, "class members", at.ClassMembers, d.ClassMembers)
	sameSlice(t, "lessons", at.Lessons, d.Lessons)
}

// demoAnchors are visit dates the web demo may be built for: summer break,
// the end of a school year, a leap day, a year boundary and both DST
// changes in Europe/Berlin.
var demoAnchors = []domain.Date{
	{Year: 2026, Month: time.October, Day: 23},
	{Year: 2027, Month: time.July, Day: 31},
	{Year: 2027, Month: time.August, Day: 1},
	{Year: 2027, Month: time.August, Day: 31},
	{Year: 2028, Month: time.February, Day: 29},
	{Year: 2028, Month: time.March, Day: 26},
	{Year: 2029, Month: time.October, Day: 28},
	{Year: 2030, Month: time.December, Day: 31},
	{Year: 2031, Month: time.January, Day: 1},
}

// TestDemoAtTerm checks that the web demo's term always contains the visit
// date, with weeks of timetable before and many months after it.
func TestDemoAtTerm(t *testing.T) {
	t.Parallel()

	for _, anchor := range demoAnchors {
		d := seed.DemoAt(anchor)
		if len(d.Terms) != 1 {
			t.Fatalf("%v: %d terms, want 1", anchor, len(d.Terms))
		}

		term := d.Terms[0]
		if err := term.Validate(); err != nil {
			t.Errorf("%v: invalid term %+v: %v", anchor, term, err)
		}

		if term.StartsOn.Compare(anchor.AddDays(-21)) > 0 || term.EndsOn.Compare(anchor.AddDays(180)) < 0 {
			t.Errorf("%v: term %v..%v does not span three weeks before to half a year after the anchor",
				anchor, term.StartsOn, term.EndsOn)
		}

		if !strings.HasPrefix(term.Name, "School Year ") {
			t.Errorf("%v: term name %q", anchor, term.Name)
		}

		for _, m := range d.ClassMembers {
			if m.Since != term.StartsOn {
				t.Fatalf("%v: class member %+v: since %v, want the term start", anchor, m, m.Since)
			}
		}
	}
}

// TestDemoWorkshops checks the one-off workshops: valid, without a class,
// free of clashes with the weekly classes, and placed so that the demo shows
// one beyond the release window (published early). One is for members and
// one hidden (staff only); the others are public.
func TestDemoWorkshops(t *testing.T) {
	t.Parallel()

	settings := seed.DemoSettings()

	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		t.Fatal(err)
	}

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)
		d.Lessons = slices.DeleteFunc(d.Lessons, func(l domain.Lesson) bool { return !l.ClassID.IsZero() })

		if n := len(d.Lessons); n < 5 || n > 6 {
			t.Fatalf("%v: %d one-off lessons, want 5..6 workshops", anchor, n)
		}

		validAll(t, d.Lessons)

		rooms := index(d.Rooms, func(r domain.Room) domain.RoomID { return r.ID })
		staff := index(d.Staff, func(s domain.Staff) domain.StaffID { return s.ID })
		courseTypes := index(d.CourseTypes, func(c domain.CourseType) domain.CourseTypeID { return c.ID })
		term := d.Terms[0]

		// The furthest a release window reaches on the anchor day: just
		// before midnight, after that day's release.
		window := service.ComputeReleaseWindow(settings, loc, anchor.AddDays(1).At(domain.TimeOfDay{}, loc).Add(-time.Minute))

		var inWeek, beyond int

		visibilities := map[domain.LessonVisibility]int{}

		for _, l := range d.Lessons {
			if !l.ClassID.IsZero() || l.Kind != domain.LessonWorkshop || l.Status != domain.LessonScheduled {
				t.Errorf("%v: lesson %+v is not a scheduled one-off workshop", anchor, l)
			}

			visibilities[l.Visibility]++

			if _, ok := courseTypes[l.CourseTypeID]; !ok {
				t.Errorf("%v: lesson %v: unknown course type", anchor, l.ID)
			}

			if room, ok := rooms[l.RoomID]; !ok || l.Capacity > room.Capacity {
				t.Errorf("%v: lesson %v: unknown room or capacity %d above the room's", anchor, l.ID, l.Capacity)
			}

			if s, ok := staff[l.InstructorID]; !ok || s.Kind != domain.StaffInstructor || !s.Active {
				t.Errorf("%v: lesson %v: instructor %+v is not an active instructor", anchor, l.ID, s)
			}

			if !l.AccompanistID.IsZero() {
				if s, ok := staff[l.AccompanistID]; !ok || s.Kind != domain.StaffAccompanist || !s.Active {
					t.Errorf("%v: lesson %v: accompanist %+v is not an active accompanist", anchor, l.ID, s)
				}
			}

			day := domain.DateOf(l.StartsAt, loc)
			if day.Compare(term.StartsOn) < 0 || day.Compare(term.EndsOn) > 0 {
				t.Errorf("%v: lesson %v on %v lies outside the term", anchor, l.ID, day)
			}

			// What guests see: public workshops.
			public := l.Visibility == domain.VisibilityPublic
			if public && day.Compare(anchor) > 0 && day.Compare(anchor.AddDays(7)) <= 0 {
				inWeek++
			}

			if public && !l.StartsAt.Before(window.ReleasedUntil) {
				beyond++
			}

			for _, c := range d.Classes {
				if c.Weekday != day.Weekday() {
					continue
				}

				start := day.At(c.StartTime, loc)
				if !l.StartsAt.Before(start.Add(c.Duration)) || !start.Before(l.EndsAt) {
					continue
				}

				if c.RoomID == l.RoomID || c.InstructorID == l.InstructorID ||
					(!l.AccompanistID.IsZero() && c.AccompanistID == l.AccompanistID) {
					t.Errorf("%v: lesson %v clashes with class %v", anchor, l.ID, c.ID)
				}
			}
		}

		if visibilities[domain.VisibilityMembers] != 1 || visibilities[domain.VisibilityHidden] != 1 {
			t.Errorf("%v: visibilities %v, want one workshop for members and one hidden", anchor, visibilities)
		}

		if inWeek == 0 {
			t.Errorf("%v: no public workshop in the week after the anchor", anchor)
		}

		if beyond == 0 {
			t.Errorf("%v: no workshop beyond the release window (until %v)", anchor, window.ReleasedUntil)
		}
	}
}

// TestDemoHistory checks the lessons of the weekly classes: exactly one per
// class slot from a week before the anchor up to and including it, as the
// generator creates them.
func TestDemoHistory(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation(seed.DemoSettings().Timezone)
	if err != nil {
		t.Fatal(err)
	}

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)
		classes := index(d.Classes, func(c domain.Class) domain.ClassID { return c.ID })

		type slot struct {
			class domain.ClassID
			date  domain.Date
		}

		got := map[slot]bool{}

		for _, l := range d.Lessons {
			if l.ClassID.IsZero() {
				continue
			}

			c := classes[l.ClassID]
			start := l.SlotDate.At(c.StartTime, loc)

			switch {
			case got[slot{c.ID, l.SlotDate}]:
				t.Errorf("%v: two lessons of class %v on %v", anchor, c.ID, l.SlotDate)
			case l.SlotDate.Compare(anchor.AddDays(-7)) < 0 || l.SlotDate.Compare(anchor) > 0:
				t.Errorf("%v: lesson on %v outside the week before the anchor", anchor, l.SlotDate)
			case l.SlotDate.Weekday() != c.Weekday || !l.StartsAt.Equal(start) || !l.EndsAt.Equal(start.Add(c.Duration)) ||
				l.RoomID != c.RoomID || l.InstructorID != c.InstructorID || l.Capacity != c.Capacity:
				t.Errorf("%v: lesson %+v does not follow class %+v", anchor, l, c)
			}

			got[slot{c.ID, l.SlotDate}] = true
		}

		// Eight days hold every weekday once, the anchor's weekday twice.
		want := 0
		for _, c := range d.Classes {
			want++
			if c.Weekday == anchor.Weekday() {
				want++
			}
		}

		if len(got) != want {
			t.Errorf("%v: %d class lessons, want %d", anchor, len(got), want)
		}
	}
}

func TestDemoValid(t *testing.T) {
	t.Parallel()

	d := seed.Demo()

	validAll(t, d.Lessons)

	if d.Settings != seed.DemoSettings() {
		t.Fatalf("settings %+v differ from DemoSettings", d.Settings)
	}

	validAll(t, d.Locations)
	validAll(t, d.Rooms)
	validAll(t, d.Staff)
	validAll(t, d.CourseTypes)
	validAll(t, d.Terms)
	validAll(t, d.Classes)
	validAll(t, d.Persons)
	validAll(t, d.Guardians)
	validAll(t, d.Accounts)
	validAll(t, d.ClassMembers)
	validAll(t, d.Bookings)
	validAll(t, d.BookingEvents)
	validAll(t, d.PassTypes)
	validAll(t, d.Passes)
	validAll(t, d.Redemptions)
}

// TestDemoMemberBookings checks that the class lessons of the demo have
// the automatic bookings of exactly the members of their class, as
// generation books them (ADR 0009), and the workshops none.
func TestDemoMemberBookings(t *testing.T) {
	t.Parallel()

	d := seed.Demo()
	// The members' own bookings of the full workshop: TestDemoFullWorkshop;
	// the staff's: TestDemoStaffBookings; the drop-ins paid with a pass:
	// TestDemoPasses.
	full := fullWorkshop(t, d)
	d.Bookings = slices.DeleteFunc(d.Bookings, func(b domain.Booking) bool {
		return b.LessonID == full.ID || b.Source == domain.BookingSourceStaff ||
			slices.ContainsFunc(d.Redemptions, func(r domain.PassRedemption) bool { return r.BookingID == b.ID })
	})
	d.BookingEvents = slices.DeleteFunc(d.BookingEvents, func(e domain.BookingEvent) bool {
		return !slices.ContainsFunc(d.Bookings, func(b domain.Booking) bool { return b.ID == e.BookingID })
	})

	booked := map[domain.LessonID][]domain.PersonID{}

	for _, b := range d.Bookings {
		if b.Source != domain.BookingSourceMember || b.Status != domain.BookingBooked {
			t.Errorf("booking %+v, want a booked member booking", b)
		}

		booked[b.LessonID] = append(booked[b.LessonID], b.PersonID)
	}

	events := map[domain.BookingID]int{}
	for _, e := range d.BookingEvents {
		events[e.BookingID]++

		if e.Status != domain.BookingBooked || !e.ActorAccountID.IsZero() || e.Override {
			t.Errorf("event %+v, want a booked event of the system", e)
		}
	}

	for _, b := range d.Bookings {
		if events[b.ID] != 1 {
			t.Errorf("booking %v has %d events, want one", b.ID, events[b.ID])
		}
	}

	if len(d.Bookings) == 0 {
		t.Fatal("the demo has no member bookings")
	}

	for _, l := range d.Lessons {
		var want []domain.PersonID

		for _, m := range d.ClassMembers {
			if !l.ClassID.IsZero() && m.ClassID == l.ClassID && m.ActiveOn(l.SlotDate) {
				want = append(want, m.PersonID)
			}
		}

		sameSet(t, "members booked into lesson "+l.ID.String(), booked[l.ID], want)
	}
}

// fullWorkshop returns the demo's only lesson with a waiting list.
func fullWorkshop(t *testing.T, d seed.Dataset) domain.Lesson {
	t.Helper()

	if len(d.WaitlistEntries) == 0 {
		t.Fatal("the demo has no waiting list")
	}

	i := slices.IndexFunc(d.Lessons, func(l domain.Lesson) bool { return l.ID == d.WaitlistEntries[0].LessonID })
	if i < 0 {
		t.Fatal("the waiting list's lesson is missing")
	}

	return d.Lessons[i]
}

// TestDemoFullWorkshop checks the full workshop of every demo (ADR 0010):
// a public workshop after the anchor whose places members booked
// themselves, and two persons with logins on its waiting list, one who
// stays on it after an invite, nobody of the demo logins' families.
func TestDemoFullWorkshop(t *testing.T) {
	t.Parallel()

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)
		l := fullWorkshop(t, d)
		validAll(t, d.WaitlistEntries)

		if !l.ClassID.IsZero() || l.Kind != domain.LessonWorkshop || l.Visibility != domain.VisibilityPublic ||
			!l.StartsAt.After(anchor.AddDays(1).At(domain.TimeOfDay{}, time.UTC)) {
			t.Errorf("%v: full workshop %+v, want a public workshop after the anchor", anchor, l)
		}

		accounts := index(d.Accounts, func(a domain.Account) domain.PersonID { return a.PersonID })
		booked := map[domain.PersonID]bool{}

		for _, b := range d.Bookings {
			if b.LessonID != l.ID {
				continue
			}

			booked[b.PersonID] = true

			if b.Source != domain.BookingSourceSelf || b.Status != domain.BookingBooked || !b.BookedAt.Before(l.StartsAt) {
				t.Errorf("%v: booking %+v, want a member's own booking", anchor, b)
			}

			i := slices.IndexFunc(d.BookingEvents, func(e domain.BookingEvent) bool { return e.BookingID == b.ID })
			if i < 0 || d.BookingEvents[i].ActorAccountID != accounts[b.PersonID].ID {
				t.Errorf("%v: booking %v has no event by its member's account", anchor, b.ID)
			}
		}

		if len(booked) != l.Capacity {
			t.Errorf("%v: %d persons booked, want the capacity %d", anchor, len(booked), l.Capacity)
		}

		logins := map[string]bool{}
		for _, login := range seed.DemoLogins() {
			logins[login.Email] = true
		}

		var stays int

		for i, e := range d.WaitlistEntries {
			acc, ok := accounts[e.PersonID]

			switch {
			case e.LessonID != l.ID || e.Status != domain.WaitlistWaiting || booked[e.PersonID]:
				t.Errorf("%v: entry %+v, want a waiting entry of someone not booked", anchor, e)
			case !ok || e.JoinedBy != acc.ID || logins[acc.Email]:
				t.Errorf("%v: entry %+v, want one joined by its own account, not a demo login", anchor, e)
			case !e.JoinedAt.Before(anchor.At(domain.TimeOfDay{}, time.UTC)):
				t.Errorf("%v: entry %+v joined after the anchor", anchor, e)
			case i > 0 && !d.WaitlistEntries[i-1].JoinedAt.Before(e.JoinedAt):
				t.Errorf("%v: entries out of order", anchor)
			}

			if e.StayOnList {
				stays++
			}
		}

		if len(d.WaitlistEntries) != 2 || stays != 1 {
			t.Errorf("%v: entries %+v, want two, one staying on the list", anchor, d.WaitlistEntries)
		}
	}
}

func TestDemoCounts(t *testing.T) {
	t.Parallel()

	d := seed.Demo()
	families := map[domain.PersonID]bool{}

	for _, g := range d.Guardians {
		families[g.GuardianID] = true
	}

	for _, c := range []struct {
		name     string
		n        int
		min, max int
	}{
		{"locations", len(d.Locations), 2, 2},
		{"rooms", len(d.Rooms), 4, 6},
		{"staff", len(d.Staff), 6, 10},
		{"course types", len(d.CourseTypes), 6, 10},
		{"terms", len(d.Terms), 1, 1},
		{"classes", len(d.Classes), 20, 30},
		{"persons", len(d.Persons), 30, 50},
		{"guardians with children", len(families), 5, 20},
		{"accounts", len(d.Accounts), 15, 30},
		{"class members", len(d.ClassMembers), 40, 120},
	} {
		if c.n < c.min || c.n > c.max {
			t.Errorf("%s: got %d, want %d..%d", c.name, c.n, c.min, c.max)
		}
	}

	roles := map[domain.Role]int{}
	for _, a := range d.Accounts {
		roles[a.Role]++
	}

	// Two instructor accounts: Alina (the demo login) and the accompanist
	// Felix, who signs in by email.
	for r, want := range map[domain.Role]int{domain.RoleInstructor: 2, domain.RoleOffice: 1, domain.RoleAdmin: 1} {
		if roles[r] != want {
			t.Errorf("want %d %s accounts, got %d", want, r, roles[r])
		}
	}

	if roles[domain.RoleMember] < 10 {
		t.Errorf("want at least 10 member accounts, got %d", roles[domain.RoleMember])
	}
}

// TestDemoStaffBookings checks the participants that Alina added to her
// ballet workshop, accompanied by Felix, in every demo (ADR 0009,
// amendment): a guest with an email, one without, and a trial with a note,
// by her account; and that both have instructor accounts linked to their
// staff records, Alina being the demo's instructor login.
func TestDemoStaffBookings(t *testing.T) {
	t.Parallel()

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)
		staff := index(d.Staff, func(s domain.Staff) domain.StaffID { return s.ID })
		accounts := index(d.Accounts, func(a domain.Account) domain.PersonID { return a.PersonID })
		lessons := index(d.Lessons, func(l domain.Lesson) domain.LessonID { return l.ID })

		var guests, guestsWithEmail, trials int

		for _, b := range d.Bookings {
			if b.Source != domain.BookingSourceStaff {
				continue
			}

			l := lessons[b.LessonID]
			instructor, accompanist := staff[l.InstructorID], staff[l.AccompanistID]

			if l.Kind != domain.LessonWorkshop || instructor.DisplayName != "Alina Starling" ||
				accompanist.DisplayName != "Felix Keysworth" || !b.BookedAt.Before(l.StartsAt) {
				t.Errorf("%v: staff booking %+v in lesson %+v", anchor, b, l)
			}

			i := slices.IndexFunc(d.BookingEvents, func(e domain.BookingEvent) bool { return e.BookingID == b.ID })
			if i < 0 || d.BookingEvents[i].ActorAccountID != accounts[instructor.PersonID].ID {
				t.Errorf("%v: booking %v has no event by Alina's account", anchor, b.ID)
			}

			for _, p := range []domain.PersonID{instructor.PersonID, accompanist.PersonID} {
				if accounts[p].Role != domain.RoleInstructor {
					t.Errorf("%v: staff person %v has account %+v, want an instructor", anchor, p, accounts[p])
				}
			}

			switch {
			case b.PersonID.IsZero() && b.GuestEmail != "":
				guestsWithEmail++
			case b.PersonID.IsZero():
				guests++
			case b.Trial && b.TrialNote != "":
				trials++
			}
		}

		if guests != 1 || guestsWithEmail != 1 || trials != 1 {
			t.Errorf("%v: %d guests without and %d with an email, %d trials; want one each", anchor, guests,
				guestsWithEmail, trials)
		}
	}

	if logins := seed.DemoLogins(); !slices.ContainsFunc(logins, func(l seed.Login) bool {
		return l.Role == domain.RoleInstructor && l.Email == "alina.starling@example.net"
	}) {
		t.Errorf("demo logins %+v, want Alina as the instructor", logins)
	}
}

// TestDemoReferences checks the rules the stores enforce across rows, plus
// the plausibility of the timetable.
func TestDemoReferences(t *testing.T) {
	t.Parallel()

	d := seed.Demo()

	persons := index(d.Persons, func(p domain.Person) domain.PersonID { return p.ID })
	locations := index(d.Locations, func(l domain.Location) domain.LocationID { return l.ID })
	rooms := index(d.Rooms, func(r domain.Room) domain.RoomID { return r.ID })
	staff := index(d.Staff, func(s domain.Staff) domain.StaffID { return s.ID })
	courseTypes := index(d.CourseTypes, func(c domain.CourseType) domain.CourseTypeID { return c.ID })
	terms := index(d.Terms, func(t domain.Term) domain.TermID { return t.ID })
	classes := index(d.Classes, func(c domain.Class) domain.ClassID { return c.ID })

	for _, r := range d.Rooms {
		if _, ok := locations[r.LocationID]; !ok {
			t.Errorf("room %q: unknown location", r.Name)
		}
	}

	for _, s := range d.Staff {
		if _, ok := persons[s.PersonID]; !s.PersonID.IsZero() && !ok {
			t.Errorf("staff %q: unknown person", s.DisplayName)
		}
	}

	emails := map[string]bool{}
	accountHolders := map[domain.PersonID]bool{}

	for _, a := range d.Accounts {
		if _, ok := persons[a.PersonID]; !ok {
			t.Errorf("account %q: unknown person", a.Email)
		}

		email := strings.ToLower(a.Email)
		if emails[email] {
			t.Errorf("account %q: duplicate email", a.Email)
		}

		if accountHolders[a.PersonID] {
			t.Errorf("account %q: person already has an account", a.Email)
		}

		emails[email] = true
		accountHolders[a.PersonID] = true
	}

	for _, p := range d.Persons {
		if !exampleEmail(p.Email) {
			t.Errorf("person %s %s: email %q is not on an example domain", p.GivenName, p.FamilyName, p.Email)
		}
	}

	for _, a := range d.Accounts {
		if !exampleEmail(a.Email) {
			t.Errorf("account email %q is not on an example domain", a.Email)
		}
	}

	links := map[domain.Guardian]bool{}

	for _, g := range d.Guardians {
		if _, ok := persons[g.GuardianID]; !ok {
			t.Errorf("guardian link %+v: unknown guardian", g)
		}

		if _, ok := persons[g.DependentID]; !ok {
			t.Errorf("guardian link %+v: unknown dependent", g)
		}

		if links[g] {
			t.Errorf("guardian link %+v: duplicate", g)
		}

		links[g] = true
	}

	for _, c := range d.Classes {
		if _, ok := terms[c.TermID]; !ok {
			t.Errorf("class %v: unknown term", c.ID)
		}

		if _, ok := courseTypes[c.CourseTypeID]; !ok {
			t.Errorf("class %v: unknown course type", c.ID)
		}

		if room, ok := rooms[c.RoomID]; !ok {
			t.Errorf("class %v: unknown room", c.ID)
		} else if c.Capacity > room.Capacity {
			t.Errorf("class %v: capacity %d exceeds room capacity %d", c.ID, c.Capacity, room.Capacity)
		}

		if s, ok := staff[c.InstructorID]; !ok || s.Kind != domain.StaffInstructor || !s.Active {
			t.Errorf("class %v: instructor %+v is not an active instructor", c.ID, s)
		}

		if !c.AccompanistID.IsZero() {
			if s, ok := staff[c.AccompanistID]; !ok || s.Kind != domain.StaffAccompanist || !s.Active {
				t.Errorf("class %v: accompanist %+v is not an active accompanist", c.ID, s)
			}
		}

		if c.Weekday == time.Sunday {
			t.Errorf("class %v: the demo school is closed on Sundays", c.ID)
		}

		if c.Duration < 45*time.Minute || c.Duration > 90*time.Minute {
			t.Errorf("class %v: duration %v outside 45..90 minutes", c.ID, c.Duration)
		}
	}

	members := map[domain.ClassMember]bool{}
	perClass := map[domain.ClassID]int{}

	for _, m := range d.ClassMembers {
		if _, ok := classes[m.ClassID]; !ok {
			t.Errorf("class member %+v: unknown class", m)
		}

		if _, ok := persons[m.PersonID]; !ok {
			t.Errorf("class member %+v: unknown person", m)
		}

		key := domain.ClassMember{ClassID: m.ClassID, PersonID: m.PersonID}
		if members[key] {
			t.Errorf("class member %+v: duplicate", m)
		}

		members[key] = true
		perClass[m.ClassID]++
	}

	for id, n := range perClass {
		if n > classes[id].Capacity {
			t.Errorf("class %v: %d members exceed capacity %d", id, n, classes[id].Capacity)
		}
	}
}

// TestDemoTimetableConflicts checks that no room, instructor, accompanist or
// member is in two classes at once.
func TestDemoTimetableConflicts(t *testing.T) {
	t.Parallel()

	d := seed.Demo()

	overlap := func(a, b domain.Class) bool {
		if a.TermID != b.TermID || a.Weekday != b.Weekday {
			return false
		}

		aStart, bStart := a.StartTime.SinceMidnight(), b.StartTime.SinceMidnight()

		return aStart < bStart+b.Duration && bStart < aStart+a.Duration
	}

	members := map[domain.ClassID]map[domain.PersonID]bool{}
	for _, m := range d.ClassMembers {
		if members[m.ClassID] == nil {
			members[m.ClassID] = map[domain.PersonID]bool{}
		}

		members[m.ClassID][m.PersonID] = true
	}

	for i, a := range d.Classes {
		for _, b := range d.Classes[i+1:] {
			if !overlap(a, b) {
				continue
			}

			if a.RoomID == b.RoomID {
				t.Errorf("classes %v and %v overlap in the same room", a.ID, b.ID)
			}

			if a.InstructorID == b.InstructorID {
				t.Errorf("classes %v and %v overlap for the same instructor", a.ID, b.ID)
			}

			if !a.AccompanistID.IsZero() && a.AccompanistID == b.AccompanistID {
				t.Errorf("classes %v and %v overlap for the same accompanist", a.ID, b.ID)
			}

			for p := range members[a.ID] {
				if members[b.ID][p] {
					t.Errorf("classes %v and %v overlap for member %v", a.ID, b.ID, p)
				}
			}
		}
	}
}

// TestDemoChildren checks that children (dependents) are minors with a
// guardian, only share classes with children, and that only one teenager
// holds an account (a managed dependent).
func TestDemoChildren(t *testing.T) {
	t.Parallel()

	d := seed.Demo()
	termStart := d.Terms[0].StartsOn

	children := map[domain.PersonID]bool{}
	guardians := map[domain.PersonID]bool{}

	for _, g := range d.Guardians {
		children[g.DependentID] = true
		guardians[g.GuardianID] = true
	}

	for _, p := range d.Persons {
		if !children[p.ID] {
			continue
		}

		if guardians[p.ID] {
			t.Errorf("child %s is also a guardian", p.GivenName)
		}

		adult := domain.Date{Year: p.BirthDate.Year + 18, Month: p.BirthDate.Month, Day: p.BirthDate.Day}
		if p.BirthDate.IsZero() || adult.Compare(termStart) <= 0 {
			t.Errorf("child %s %s: birth date %v is not a minor's", p.GivenName, p.FamilyName, p.BirthDate)
		}

		// Only a teenager has an email and an account of their own (a
		// managed dependent, ADR 0008).
		teen := domain.Date{Year: p.BirthDate.Year + 14, Month: p.BirthDate.Month, Day: p.BirthDate.Day}
		if p.Email != "" && teen.Compare(termStart) > 0 {
			t.Errorf("child %s %s under 14 has an email", p.GivenName, p.FamilyName)
		}
	}

	if len(children) == 0 {
		t.Fatal("no children in the demo data")
	}

	managed := 0

	for _, a := range d.Accounts {
		if !children[a.PersonID] {
			continue
		}

		managed++

		p := d.Persons[slices.IndexFunc(d.Persons, func(p domain.Person) bool { return p.ID == a.PersonID })]
		if a.Role != domain.RoleMember || p.Email != a.Email {
			t.Errorf("child account %+v, want a member account with the child's email", a)
		}
	}

	if managed != 1 {
		t.Errorf("%d children with an account, want one managed dependent", managed)
	}

	// A class is either a children's or an adults' class.
	kind := map[domain.ClassID]bool{}
	seen := map[domain.ClassID]bool{}

	for _, m := range d.ClassMembers {
		child := children[m.PersonID]
		if seen[m.ClassID] && kind[m.ClassID] != child {
			t.Errorf("class %v mixes children and adults", m.ClassID)
		}

		seen[m.ClassID] = true
		kind[m.ClassID] = child
	}
}

func TestLoadDemoRoundTrip(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	want := seed.Demo()

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		if settings != want.Settings {
			t.Errorf("settings: got %+v, want %+v", settings, want.Settings)
		}

		persons, err := tx.Persons().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "persons", persons, want.Persons)

		accounts, err := tx.Accounts().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "accounts", accounts, want.Accounts)

		for _, c := range want.Credentials {
			got, err := tx.Credentials().Get(ctx, c.AccountID)
			if err != nil {
				return err
			}

			if got != c {
				t.Errorf("credential: got %+v, want %+v", got, c)
			}
		}

		for _, a := range want.Accounts {
			got, err := tx.Accounts().GetByEmail(ctx, strings.ToUpper(a.Email))
			if err != nil {
				return err
			}

			if got != a {
				t.Errorf("GetByEmail(%q): got %+v, want %+v", a.Email, got, a)
			}
		}

		var guardians []domain.Guardian

		for _, p := range persons {
			deps, err := tx.Persons().ListDependents(ctx, p.ID)
			if err != nil {
				return err
			}

			for _, dep := range deps {
				guardians = append(guardians, domain.Guardian{GuardianID: p.ID, DependentID: dep.ID})
			}
		}

		sameSet(t, "guardians", guardians, want.Guardians)

		locations, err := tx.Locations().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "locations", locations, want.Locations)

		rooms, err := tx.Rooms().List(ctx, domain.LocationID{})
		if err != nil {
			return err
		}

		sameSet(t, "rooms", rooms, want.Rooms)

		staff, err := tx.Staff().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "staff", staff, want.Staff)

		courseTypes, err := tx.CourseTypes().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "course types", courseTypes, want.CourseTypes)

		terms, err := tx.Terms().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "terms", terms, want.Terms)

		var (
			classes []domain.Class
			members []domain.ClassMember
		)

		for _, term := range terms {
			cs, err := tx.Classes().ListByTerm(ctx, term.ID)
			if err != nil {
				return err
			}

			classes = append(classes, cs...)

			for _, c := range cs {
				ms, err := tx.Classes().ListMembers(ctx, c.ID)
				if err != nil {
					return err
				}

				members = append(members, ms...)
			}
		}

		sameSet(t, "classes", classes, want.Classes)
		sameSet(t, "class members", members, want.ClassMembers)

		from, to := allTime()

		lessons, err := tx.Lessons().ListRange(ctx, from, to)
		if err != nil {
			return err
		}

		if len(want.Lessons) == 0 {
			t.Error("the demo has no lessons")
		}

		sameSet(t, "lessons", lessons, want.Lessons)

		var (
			bookings []domain.Booking
			events   []domain.BookingEvent
		)

		for _, l := range lessons {
			bs, err := tx.Bookings().ListByLesson(ctx, l.ID)
			if err != nil {
				return err
			}

			bookings = append(bookings, bs...)

			for _, b := range bs {
				es, err := tx.Bookings().ListEvents(ctx, b.ID)
				if err != nil {
					return err
				}

				events = append(events, es...)
			}
		}

		sameSet(t, "bookings", bookings, want.Bookings)
		sameSet(t, "booking events", events, want.BookingEvents)

		var entries []domain.WaitlistEntry

		for _, l := range lessons {
			es, err := tx.Waitlist().ListByLesson(ctx, l.ID)
			if err != nil {
				return err
			}

			entries = append(entries, es...)
		}

		sameSet(t, "waitlist entries", entries, want.WaitlistEntries)

		passTypes, err := tx.PassTypes().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "pass types", passTypes, want.PassTypes)

		personIDs := make([]domain.PersonID, len(persons))
		for i, p := range persons {
			personIDs[i] = p.ID
		}

		passes, err := tx.Passes().ListByPersons(ctx, personIDs)
		if err != nil {
			return err
		}

		sameSet(t, "passes", passes, want.Passes)

		passIDs := make([]domain.PassID, len(passes))
		for i, p := range passes {
			passIDs[i] = p.ID
		}

		redemptions, err := tx.Redemptions().ListByPasses(ctx, passIDs, 0)
		if err != nil {
			return err
		}

		sameSet(t, "redemptions", redemptions, want.Redemptions)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestLoadDemoAt checks that the web demo's data, built for the visit date,
// loads into the memory store with its workshops.
func TestLoadDemoAt(t *testing.T) {
	t.Parallel()

	anchor := domain.Date{Year: 2027, Month: time.August, Day: 14}

	st := memory.New()
	if err := seed.LoadDemoAt(t.Context(), st, anchor); err != nil {
		t.Fatal(err)
	}

	want := seed.DemoAt(anchor)

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		terms, err := tx.Terms().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "terms", terms, want.Terms)

		from, to := allTime()

		lessons, err := tx.Lessons().ListRange(ctx, from, to)
		if err != nil {
			return err
		}

		sameSet(t, "lessons", lessons, want.Lessons)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := seed.LoadDemoAt(t.Context(), st, anchor); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second load: want %v, got %v", domain.ErrConflict, err)
	}
}

// allTime is a lesson range that covers every demo anchor.
func allTime() (from, to time.Time) {
	return time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
}

func TestLoadDemoTwiceConflicts(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	if err := seed.LoadDemo(t.Context(), st); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second load: want %v, got %v", domain.ErrConflict, err)
	}
}

// TestLoadDemoRefusesNonEmptyStore checks that demo data is never mixed into
// a store that already holds a school, even one with different IDs.
func TestLoadDemoRefusesNonEmptyStore(t *testing.T) {
	t.Parallel()

	st := memory.New()
	settings := seed.DemoSettings()
	settings.SchoolName = "Other Example School"

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, settings)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := seed.LoadDemo(t.Context(), st); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("load into non-empty store: want %v, got %v", domain.ErrConflict, err)
	}

	var got domain.Settings

	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		got, err = tx.Settings().Get(ctx)

		return err
	})
	if err != nil || got != settings {
		t.Fatalf("settings changed: %+v, %v", got, err)
	}
}

func TestLoadDemoRefusesStoreWithPersons(t *testing.T) {
	t.Parallel()

	st := memory.New()
	p := domain.Person{ID: domain.PersonID{1}, GivenName: "Existing", FamilyName: "Example"}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Persons().Create(ctx, p)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := seed.LoadDemo(t.Context(), st); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("load into store with persons: want %v, got %v", domain.ErrConflict, err)
	}
}

func TestLoadDemoRefusesStoreWithLocations(t *testing.T) {
	t.Parallel()

	st := memory.New()
	l := domain.Location{ID: domain.LocationID{1}, Name: "Existing Studio"}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Locations().Create(ctx, l)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := seed.LoadDemo(t.Context(), st); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("load into store with locations: want %v, got %v", domain.ErrConflict, err)
	}
}

func validAll[T interface{ Validate() error }](t *testing.T, items []T) {
	t.Helper()

	for _, v := range items {
		if err := v.Validate(); err != nil {
			t.Errorf("invalid %+v: %v", v, err)
		}
	}
}

func sameSlice[T comparable](t *testing.T, name string, a, b []T) {
	t.Helper()

	if len(a) != len(b) {
		t.Fatalf("%s: %d vs %d items", name, len(a), len(b))
	}

	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("%s: item %d differs:\n%+v\n%+v", name, i, a[i], b[i])
		}
	}
}

// sameSet compares got and want ignoring order.
func sameSet[T comparable](t *testing.T, name string, got, want []T) {
	t.Helper()

	if len(got) != len(want) {
		t.Errorf("%s: got %d items, want %d", name, len(got), len(want))

		return
	}

	count := make(map[T]int, len(want))
	for _, v := range want {
		count[v]++
	}

	for _, v := range got {
		if count[v] == 0 {
			t.Errorf("%s: unexpected %+v", name, v)

			continue
		}

		count[v]--
	}
}

func index[K comparable, T any](items []T, key func(T) K) map[K]T {
	m := make(map[K]T, len(items))
	for _, v := range items {
		m[key(v)] = v
	}

	return m
}

func exampleEmail(email string) bool {
	if email == "" {
		return true
	}

	for _, host := range []string{"@example.org", "@example.com", "@example.net"} {
		if strings.HasSuffix(email, host) {
			return true
		}
	}

	return false
}

func TestDemoLogins(t *testing.T) {
	t.Parallel()

	logins := seed.DemoLogins()
	d := seed.Demo()

	var roles []domain.Role
	for _, l := range logins {
		roles = append(roles, l.Role)
	}

	if want := []domain.Role{domain.RoleMember, domain.RoleInstructor, domain.RoleOffice, domain.RoleAdmin}; !slices.Equal(roles, want) {
		t.Fatalf("roles %v, want %v", roles, want)
	}

	hasher := password.New()

	for _, l := range logins {
		i := slices.IndexFunc(d.Accounts, func(a domain.Account) bool { return a.Email == l.Email })
		if i < 0 {
			t.Fatalf("%s is not a demo account", l.Email)
		}

		acc := d.Accounts[i]
		if acc.Role != l.Role || acc.ConfirmedAt.IsZero() {
			t.Errorf("%s: account %+v, want a confirmed %s", l.Email, acc, l.Role)
		}

		c := slices.IndexFunc(d.Credentials, func(c domain.Credential) bool { return c.AccountID == acc.ID })
		if c < 0 {
			t.Fatalf("%s has no password", l.Email)
		}

		if ok, err := hasher.Verify(d.Credentials[c].PasswordHash, l.Password); !ok || err != nil {
			t.Errorf("%s: the password does not verify: %v", l.Email, err)
		}
	}

	// The demo member is a parent: the family page has two children to show.
	member := d.Accounts[slices.IndexFunc(d.Accounts, func(a domain.Account) bool { return a.Email == logins[0].Email })]

	children := 0
	for _, g := range d.Guardians {
		if g.GuardianID == member.PersonID {
			children++
		}
	}

	if children != 2 {
		t.Errorf("the demo member %s has %d dependents, want 2", member.Email, children)
	}

	// Every account can sign in.
	if len(d.Credentials) != len(d.Accounts) {
		t.Errorf("%d credentials for %d accounts", len(d.Credentials), len(d.Accounts))
	}

	validAll(t, d.Credentials)
}

// TestDemoPasses checks the passes of every demo (ADR 0012): the catalog,
// the member login's family with passes (one paid for a child by her
// mother), a time pass, one given away and one voided; and that every
// redemption is an attended drop-in of the pass's owner in a lesson before
// the anchor that the pass covers, as the demo redeems on attendance, with
// the visits it used counted.
func TestDemoPasses(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation(seed.DemoSettings().Timezone)
	if err != nil {
		t.Fatal(err)
	}

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)

		if d.Settings.RedemptionMode != domain.RedeemOnAttendance || len(d.PassTypes) != 3 || len(d.Redemptions) == 0 {
			t.Fatalf("%v: mode %v, %d pass types, %d redemptions", anchor, d.Settings.RedemptionMode,
				len(d.PassTypes), len(d.Redemptions))
		}

		passes := index(d.Passes, func(p domain.Pass) domain.PassID { return p.ID })
		bookings := index(d.Bookings, func(b domain.Booking) domain.BookingID { return b.ID })
		lessons := index(d.Lessons, func(l domain.Lesson) domain.LessonID { return l.ID })
		used := map[domain.PassID]int{}

		for _, r := range d.Redemptions {
			p, b := passes[r.PassID], bookings[r.BookingID]
			l := lessons[b.LessonID]
			used[p.ID]++

			if b.PersonID != p.PersonID || b.Status != domain.BookingAttended || b.Source != domain.BookingSourceSelf ||
				domain.DateOf(l.StartsAt, loc).Compare(anchor) >= 0 || r.At.Before(l.StartsAt) {
				t.Errorf("%v: redemption %+v of booking %+v in lesson %+v", anchor, r, b, l)
			}

			before := p
			before.VisitsRemaining = p.VisitsTotal // as sold
			if !before.Covers(domain.DateOf(l.StartsAt, loc)) {
				t.Errorf("%v: pass %+v does not cover lesson %+v", anchor, p, l)
			}
		}

		var kinds, origins, voided, paidByOthers int

		for _, p := range d.Passes {
			if p.Kind == domain.PassVisits && p.VisitsRemaining != p.VisitsTotal-used[p.ID] {
				t.Errorf("%v: pass %+v used %d times", anchor, p, used[p.ID])
			}

			if p.Kind == domain.PassTime {
				kinds++
			}

			if p.Origin == domain.PassAssigned {
				origins++
			}

			if p.Status == domain.PassVoided {
				voided++
			}

			if !p.PayerID.IsZero() {
				paidByOthers++
			}
		}

		if kinds != 1 || origins != 1 || voided != 1 || paidByOthers != 1 {
			t.Errorf("%v: %d time passes, %d assigned, %d voided, %d paid by others; want one each", anchor, kinds,
				origins, voided, paidByOthers)
		}
	}
}
