package seed_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
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

	got, err := service.NewSettingsService(st, clock.System{}, ids.NewSequence()).Public(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if got.Settings != seed.DemoSettings() {
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
	sameSlice(t, "levels", a.Levels, b.Levels)
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

// TestDemoWorkshops checks the one-off workshops of no event: valid,
// without a class, free of clashes with the weekly classes, and placed so
// that the demo shows one beyond the release window (published early). One
// is for members and one hidden (staff only); the others are public.
func TestDemoWorkshops(t *testing.T) {
	t.Parallel()

	settings := seed.DemoSettings()

	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		t.Fatal(err)
	}

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)
		// The events' sessions are workshops too: TestDemoEvents.
		d.Lessons = slices.DeleteFunc(d.Lessons, func(l domain.Lesson) bool {
			return !l.ClassID.IsZero() || l.Kind == domain.LessonRental || !l.EventID.IsZero()
		})

		if n := len(d.Lessons); n < 5 || n > 6 {
			t.Fatalf("%v: %d one-off lessons, want 5..6 workshops", anchor, n)
		}

		validAll(t, anchor.String(), d.Lessons)

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

// TestDemoRental checks the room rental: one valid, hidden rental after the
// anchor, in the term, that clashes with no weekly class and no workshop in
// its room.
func TestDemoRental(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation(seed.DemoSettings().Timezone)
	if err != nil {
		t.Fatal(err)
	}

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)

		var rentals []domain.Lesson

		for _, l := range d.Lessons {
			if l.Kind == domain.LessonRental {
				rentals = append(rentals, l)
			}
		}

		if len(rentals) != 1 {
			t.Fatalf("%v: %d rentals, want 1", anchor, len(rentals))
		}

		r := rentals[0]
		validAll(t, anchor.String(), rentals)

		// The demo shows a rental without a course type, which only a rental
		// may lack.
		if !r.CourseTypeID.IsZero() {
			t.Errorf("%v: rental has course type %v, want none", anchor, r.CourseTypeID)
		}

		day := domain.DateOf(r.StartsAt, loc)
		if day.Compare(anchor) <= 0 || day.Compare(d.Terms[0].EndsOn) > 0 {
			t.Errorf("%v: rental on %v, want after the anchor in the term", anchor, day)
		}

		for _, l := range d.Lessons {
			if l.ID != r.ID && l.RoomID == r.RoomID && l.StartsAt.Before(r.EndsAt) && r.StartsAt.Before(l.EndsAt) {
				t.Errorf("%v: rental clashes with lesson %+v", anchor, l)
			}
		}

		for _, c := range d.Classes {
			start := day.At(c.StartTime, loc)
			if c.Weekday == day.Weekday() && c.RoomID == r.RoomID &&
				r.StartsAt.Before(start.Add(c.Duration)) && start.Before(r.EndsAt) {
				t.Errorf("%v: rental clashes with class %v", anchor, c.ID)
			}
		}
	}
}

// TestDemoHistory checks the lessons of the weekly classes: exactly one per
// class slot from the term start up to and including the anchor, as the
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
		termStart := d.Terms[0].StartsOn

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
			case l.SlotDate.Compare(termStart) < 0 || l.SlotDate.Compare(anchor) > 0:
				t.Errorf("%v: lesson on %v outside the term start to the anchor", anchor, l.SlotDate)
			case l.SlotDate.Weekday() != c.Weekday || !l.StartsAt.Equal(start) || !l.EndsAt.Equal(start.Add(c.Duration)) ||
				l.RoomID != c.RoomID || l.InstructorID != c.InstructorID || l.Capacity != c.Capacity:
				t.Errorf("%v: lesson %+v does not follow class %+v", anchor, l, c)
			}

			got[slot{c.ID, l.SlotDate}] = true
		}

		// The term starts three weeks before the anchor: 22 days hold every
		// weekday three times, the anchor's four times.
		want := 0
		for _, c := range d.Classes {
			want += 3
			if c.Weekday == anchor.Weekday() {
				want++
			}
		}

		if len(got) != want {
			t.Errorf("%v: %d class lessons, want %d", anchor, len(got), want)
		}
	}
}

// TestDemoValid: every record of every demo school is valid, its settings
// and background included; the programme school has DemoSettings and no
// background, the werkstatt school a background.
func TestDemoValid(t *testing.T) {
	t.Parallel()

	forEachSchool(t, func(t *testing.T, school seed.School, d seed.Dataset) {
		if err := d.Settings.Validate(); err != nil {
			t.Errorf("%s: settings %+v: %v", school, d.Settings, err)
		}

		if !d.Background.IsZero() {
			if err := d.Background.Validate(); err != nil {
				t.Errorf("%s: background: %v", school, err)
			}
		}

		what := string(school)
		validAll(t, what, d.Locations)
		validAll(t, what, d.Rooms)
		validAll(t, what, d.Staff)
		validAll(t, what, d.CourseTypes)
		validAll(t, what, d.Levels)
		validAll(t, what, d.Terms)
		validAll(t, what, d.Classes)
		validAll(t, what, d.Persons)
		validAll(t, what, d.Guardians)
		validAll(t, what, d.Accounts)
		validAll(t, what, d.Credentials)
		validAll(t, what, d.ClassMembers)
		validAll(t, what, d.Lessons)
		validAll(t, what, d.Bookings)
		validAll(t, what, d.BookingEvents)
		validAll(t, what, d.WaitlistEntries)
		validAll(t, what, d.PassTypes)
		validAll(t, what, d.Passes)
		validAll(t, what, d.Redemptions)
		validAll(t, what, d.Receipts)
		validAll(t, what, d.Announcements)

		switch school {
		case seed.SchoolProgramme:
			if d.Settings != seed.DemoSettings() || !d.Background.IsZero() {
				t.Errorf("%s: settings %+v, background %d bytes; want DemoSettings, none",
					school, d.Settings, len(d.Background.Data))
			}
		case seed.SchoolWerkstatt:
			if d.Background.IsZero() {
				t.Errorf("%s: no background", school)
			}
		}
	})
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
	// The attendance taken since: TestDemoAttendance.
	d.BookingEvents = slices.DeleteFunc(d.BookingEvents, func(e domain.BookingEvent) bool {
		return !slices.ContainsFunc(d.Bookings, func(b domain.Booking) bool { return b.ID == e.BookingID }) ||
			e.Status != domain.BookingBooked
	})

	booked := map[domain.LessonID][]domain.PersonID{}

	for _, b := range d.Bookings {
		if b.Source != domain.BookingSourceMember {
			t.Errorf("booking %+v, want a member booking", b)
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

// TestDemoAttendance checks the attendance of the demo's past member
// bookings: taken for the lessons before the anchor but Classical
// Ballet's, with no-shows and late cancellations among it, each recorded
// by one event after the booking; the later lessons' and the ballet
// lessons' bookings stay booked.
func TestDemoAttendance(t *testing.T) {
	t.Parallel()

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)
		validAll(t, anchor.String(), d.Bookings)
		validAll(t, anchor.String(), d.BookingEvents)

		ballet := d.CourseTypes[slices.IndexFunc(d.CourseTypes, func(c domain.CourseType) bool {
			return c.Name == "Classical Ballet"
		})].ID
		lessons := map[domain.LessonID]domain.Lesson{}

		for _, l := range d.Lessons {
			lessons[l.ID] = l
		}

		events := map[domain.BookingID][]domain.BookingEvent{}
		for _, e := range d.BookingEvents {
			events[e.BookingID] = append(events[e.BookingID], e)
		}

		statuses := map[domain.BookingStatus]int{}

		for _, b := range d.Bookings {
			l := lessons[b.LessonID]
			if b.Source != domain.BookingSourceMember {
				continue
			}

			taken := l.SlotDate.Compare(anchor) < 0 && l.CourseTypeID != ballet
			es := events[b.ID]

			switch {
			case !taken && (b.Status != domain.BookingBooked || len(es) != 1):
				t.Errorf("%v: booking %+v of lesson on %v, want it booked", anchor, b, l.SlotDate)
			case taken && (b.Status == domain.BookingBooked || len(es) != 2 || es[1].Status != b.Status):
				t.Errorf("%v: booking %+v with events %+v, want its attendance", anchor, b, es)
			case b.Status == domain.BookingCanceled && (!b.Late || !b.CanceledAt.Before(l.StartsAt) || !es[1].Late):
				t.Errorf("%v: canceled booking %+v, want a late cancellation", anchor, b)
			}

			statuses[b.Status]++
		}

		for _, s := range domain.BookingStatuses() {
			if statuses[s] == 0 {
				t.Errorf("%v: no member booking %s, got %v", anchor, s, statuses)
			}
		}
	}
}

// TestDemoVisitors checks the guests and trials of the weekly classes'
// past lessons: only before the anchor and never in Classical Ballet's,
// added by the lesson's instructor (or the office) before the start, a
// trial only for a person who is no member of the class and nobody of the
// member login's family, each with its attendance at the end; some guests
// did not show.
func TestDemoVisitors(t *testing.T) {
	t.Parallel()

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)
		validAll(t, anchor.String(), d.Bookings)
		validAll(t, anchor.String(), d.BookingEvents)

		lessons := index(d.Lessons, func(l domain.Lesson) domain.LessonID { return l.ID })
		staff := index(d.Staff, func(s domain.Staff) domain.StaffID { return s.ID })
		accounts := index(d.Accounts, func(a domain.Account) domain.AccountID { return a.ID })
		persons := index(d.Persons, func(p domain.Person) domain.PersonID { return p.ID })
		ballet := d.CourseTypes[slices.IndexFunc(d.CourseTypes, func(c domain.CourseType) bool {
			return c.Name == "Classical Ballet"
		})].ID
		events := map[domain.BookingID][]domain.BookingEvent{}

		for _, e := range d.BookingEvents {
			events[e.BookingID] = append(events[e.BookingID], e)
		}

		var guests, noShows, trials int

		for _, b := range d.Bookings {
			l := lessons[b.LessonID]
			if b.Source != domain.BookingSourceStaff || l.ClassID.IsZero() {
				continue
			}

			es := events[b.ID]
			actor := accounts[es[0].ActorAccountID]

			switch {
			case l.SlotDate.Compare(anchor) >= 0 || l.CourseTypeID == ballet:
				t.Errorf("%v: visitor %+v in lesson %+v", anchor, b, l)
			case !b.BookedAt.Before(l.StartsAt) || len(es) != 2 || es[0].Status != domain.BookingBooked ||
				es[1].Status != b.Status || !es[1].At.Equal(l.EndsAt) || es[1].ActorAccountID != actor.ID:
				t.Errorf("%v: visitor %+v with events %+v", anchor, b, es)
			case actor.Role != domain.RoleOffice && actor.PersonID != staff[l.InstructorID].PersonID:
				t.Errorf("%v: visitor %+v added by %+v, want the instructor or the office", anchor, b, actor)
			case b.Status != domain.BookingAttended && (b.Trial || b.Status != domain.BookingNoShow):
				t.Errorf("%v: visitor %+v, want attended or a guest's no-show", anchor, b)
			case b.Trial && (b.PersonID.IsZero() || persons[b.PersonID].FamilyName == "Amberly" ||
				slices.ContainsFunc(d.ClassMembers, func(m domain.ClassMember) bool {
					return m.ClassID == l.ClassID && m.PersonID == b.PersonID
				})):
				t.Errorf("%v: trial %+v, want a person who is no member of the class", anchor, b)
			case !b.Trial && (!b.PersonID.IsZero() || b.GuestName == ""):
				t.Errorf("%v: visitor %+v, want a guest", anchor, b)
			}

			switch {
			case b.Trial:
				trials++
			case b.Status == domain.BookingNoShow:
				noShows++
			default:
				guests++
			}
		}

		if guests == 0 || noShows == 0 || trials == 0 {
			t.Errorf("%v: %d guests, %d guests' no-shows, %d trials; want some of each", anchor, guests, noShows, trials)
		}
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
		validAll(t, anchor.String(), d.WaitlistEntries)

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
// staff records, Alina being the demo's instructor login. The visitors of
// the past weekly lessons: TestDemoVisitors.
func TestDemoStaffBookings(t *testing.T) {
	t.Parallel()

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := seed.DemoAt(anchor)
		staff := index(d.Staff, func(s domain.Staff) domain.StaffID { return s.ID })
		accounts := index(d.Accounts, func(a domain.Account) domain.PersonID { return a.PersonID })
		lessons := index(d.Lessons, func(l domain.Lesson) domain.LessonID { return l.ID })

		var guests, guestsWithEmail, trials int

		for _, b := range d.Bookings {
			l := lessons[b.LessonID]
			if b.Source != domain.BookingSourceStaff || !l.ClassID.IsZero() {
				continue
			}

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

// TestDemoReferences checks, for every demo school, the rules the stores
// enforce across rows, plus the plausibility of the timetable.
func TestDemoReferences(t *testing.T) {
	t.Parallel()

	forEachSchool(t, func(t *testing.T, school seed.School, d seed.Dataset) {
		persons := index(d.Persons, func(p domain.Person) domain.PersonID { return p.ID })
		locations := index(d.Locations, func(l domain.Location) domain.LocationID { return l.ID })
		rooms := index(d.Rooms, func(r domain.Room) domain.RoomID { return r.ID })
		staff := index(d.Staff, func(s domain.Staff) domain.StaffID { return s.ID })
		courseTypes := index(d.CourseTypes, func(c domain.CourseType) domain.CourseTypeID { return c.ID })
		terms := index(d.Terms, func(t domain.Term) domain.TermID { return t.ID })
		classes := index(d.Classes, func(c domain.Class) domain.ClassID { return c.ID })

		for _, r := range d.Rooms {
			if _, ok := locations[r.LocationID]; !ok {
				t.Errorf("%s: room %q: unknown location", school, r.Name)
			}
		}

		for _, s := range d.Staff {
			if _, ok := persons[s.PersonID]; !s.PersonID.IsZero() && !ok {
				t.Errorf("%s: staff %q: unknown person", school, s.DisplayName)
			}
		}

		emails := map[string]bool{}
		accountHolders := map[domain.PersonID]bool{}

		for _, a := range d.Accounts {
			if _, ok := persons[a.PersonID]; !ok {
				t.Errorf("%s: account %q: unknown person", school, a.Email)
			}

			email := strings.ToLower(a.Email)
			if emails[email] {
				t.Errorf("%s: account %q: duplicate email", school, a.Email)
			}

			if accountHolders[a.PersonID] {
				t.Errorf("%s: account %q: person already has an account", school, a.Email)
			}

			emails[email] = true
			accountHolders[a.PersonID] = true
		}

		for _, p := range d.Persons {
			if !exampleEmail(p.Email) {
				t.Errorf("%s: person %s %s: email %q is not on an example domain", school, p.GivenName, p.FamilyName, p.Email)
			}
		}

		for _, a := range d.Accounts {
			if !exampleEmail(a.Email) {
				t.Errorf("%s: account email %q is not on an example domain", school, a.Email)
			}
		}

		links := map[domain.Guardian]bool{}

		for _, g := range d.Guardians {
			if _, ok := persons[g.GuardianID]; !ok {
				t.Errorf("%s: guardian link %+v: unknown guardian", school, g)
			}

			if _, ok := persons[g.DependentID]; !ok {
				t.Errorf("%s: guardian link %+v: unknown dependent", school, g)
			}

			if links[g] {
				t.Errorf("%s: guardian link %+v: duplicate", school, g)
			}

			links[g] = true
		}

		for _, c := range d.Classes {
			if _, ok := terms[c.TermID]; !ok {
				t.Errorf("%s: class %v: unknown term", school, c.ID)
			}

			if _, ok := courseTypes[c.CourseTypeID]; !ok {
				t.Errorf("%s: class %v: unknown course type", school, c.ID)
			}

			if room, ok := rooms[c.RoomID]; !ok {
				t.Errorf("%s: class %v: unknown room", school, c.ID)
			} else if c.Capacity > room.Capacity {
				t.Errorf("%s: class %v: capacity %d exceeds room capacity %d", school, c.ID, c.Capacity, room.Capacity)
			}

			if s, ok := staff[c.InstructorID]; !ok || s.Kind != domain.StaffInstructor || !s.Active {
				t.Errorf("%s: class %v: instructor %+v is not an active instructor", school, c.ID, s)
			}

			if !c.AccompanistID.IsZero() {
				if s, ok := staff[c.AccompanistID]; !ok || s.Kind != domain.StaffAccompanist || !s.Active {
					t.Errorf("%s: class %v: accompanist %+v is not an active accompanist", school, c.ID, s)
				}
			}

			if c.Weekday == time.Sunday {
				t.Errorf("%s: class %v: the demo school is closed on Sundays", school, c.ID)
			}

			// The werkstatt school's plan has half-hour classes (Spitze,
			// Morning Workout).
			shortest := map[seed.School]time.Duration{seed.SchoolProgramme: 45 * time.Minute, seed.SchoolWerkstatt: 30 * time.Minute}[school]
			if c.Duration < shortest || c.Duration > 90*time.Minute {
				t.Errorf("%s: class %v: duration %v outside %v..90 minutes", school, c.ID, c.Duration, shortest)
			}
		}

		members := map[domain.ClassMember]bool{}
		perClass := map[domain.ClassID]int{}

		for _, m := range d.ClassMembers {
			if _, ok := classes[m.ClassID]; !ok {
				t.Errorf("%s: class member %+v: unknown class", school, m)
			}

			if _, ok := persons[m.PersonID]; !ok {
				t.Errorf("%s: class member %+v: unknown person", school, m)
			}

			key := domain.ClassMember{ClassID: m.ClassID, PersonID: m.PersonID}
			if members[key] {
				t.Errorf("%s: class member %+v: duplicate", school, m)
			}

			members[key] = true
			perClass[m.ClassID]++
		}

		for id, n := range perClass {
			if n > classes[id].Capacity {
				t.Errorf("%s: class %v: %d members exceed capacity %d", school, id, n, classes[id].Capacity)
			}
		}

		accounts := index(d.Accounts, func(a domain.Account) domain.AccountID { return a.ID })
		events := index(d.Events, func(e domain.Event) domain.EventID { return e.ID })
		receipts := index(d.Receipts, func(r domain.Receipt) domain.ReceiptID { return r.ID })

		for _, e := range d.Events {
			if _, ok := courseTypes[e.CourseTypeID]; !ok {
				t.Errorf("%s: event %q: unknown course type", school, e.Slug)
			}

			if _, ok := accounts[e.CreatedBy]; !ok {
				t.Errorf("%s: event %q: unknown creator", school, e.Slug)
			}
		}

		for _, l := range d.Lessons {
			if _, ok := events[l.EventID]; !l.EventID.IsZero() && !ok {
				t.Errorf("%s: lesson %v: unknown event", school, l.ID)
			}
		}

		for _, r := range d.EventRegistrations {
			if _, ok := events[r.EventID]; !ok {
				t.Errorf("%s: event registration %v: unknown event", school, r.ID)
			}

			if _, ok := persons[r.PersonID]; !r.PersonID.IsZero() && !ok {
				t.Errorf("%s: event registration %v: unknown person", school, r.ID)
			}

			if _, ok := accounts[r.RegisteredBy]; !ok {
				t.Errorf("%s: event registration %v: unknown account", school, r.ID)
			}

			for _, id := range []domain.ReceiptID{r.ReceiptID, r.CancellationReceiptID} {
				if _, ok := receipts[id]; !id.IsZero() && !ok {
					t.Errorf("%s: event registration %v: unknown receipt %v", school, r.ID, id)
				}
			}

			if !exampleEmail(r.GuestEmail) {
				t.Errorf("%s: event registration %v: guest email %q is not on an example domain", school, r.ID,
					r.GuestEmail)
			}
		}
	})
}

// TestDemoTimetableConflicts checks that in no demo school two classes meet
// in one room at once, and no member is in two classes at once. The staff:
// TestDemoInstructorsNeverDoubleBooked.
func TestDemoTimetableConflicts(t *testing.T) {
	t.Parallel()

	forEachSchool(t, func(t *testing.T, school seed.School, d seed.Dataset) {
		members := map[domain.ClassID]map[domain.PersonID]bool{}
		for _, m := range d.ClassMembers {
			if members[m.ClassID] == nil {
				members[m.ClassID] = map[domain.PersonID]bool{}
			}

			members[m.ClassID][m.PersonID] = true
		}

		for i, a := range d.Classes {
			for _, b := range d.Classes[i+1:] {
				if !classesOverlap(a, b) {
					continue
				}

				if a.RoomID == b.RoomID {
					t.Errorf("%s: classes %v and %v overlap in the same room", school, a.ID, b.ID)
				}

				for p := range members[a.ID] {
					if members[b.ID][p] {
						t.Errorf("%s: classes %v and %v overlap for member %v", school, a.ID, b.ID, p)
					}
				}
			}
		}
	})
}

// classesOverlap reports whether two weekly classes of one term meet at the
// same time.
func classesOverlap(a, b domain.Class) bool {
	if a.TermID != b.TermID || a.Weekday != b.Weekday {
		return false
	}

	aStart, bStart := a.StartTime.SinceMidnight(), b.StartTime.SinceMidnight()

	return aStart < bStart+b.Duration && bStart < aStart+a.Duration
}

// TestDemoChildren checks that in every demo school children (dependents)
// are minors with a guardian, only share classes with children, and that
// children with an account are members with their own email (managed
// dependents); in the programme school exactly one teenager has one.
func TestDemoChildren(t *testing.T) {
	t.Parallel()

	forEachSchool(t, func(t *testing.T, school seed.School, d seed.Dataset) {
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
				t.Errorf("%s: child %s is also a guardian", school, p.GivenName)
			}

			adult := domain.Date{Year: p.BirthDate.Year + 18, Month: p.BirthDate.Month, Day: p.BirthDate.Day}
			if p.BirthDate.IsZero() || adult.Compare(termStart) <= 0 {
				t.Errorf("%s: child %s %s: birth date %v is not a minor's", school, p.GivenName, p.FamilyName, p.BirthDate)
			}

			// Only a teenager has an email and an account of their own (a
			// managed dependent, ADR 0008).
			teen := domain.Date{Year: p.BirthDate.Year + 14, Month: p.BirthDate.Month, Day: p.BirthDate.Day}
			if p.Email != "" && teen.Compare(termStart) > 0 {
				t.Errorf("%s: child %s %s under 14 has an email", school, p.GivenName, p.FamilyName)
			}
		}

		if len(children) == 0 {
			t.Fatalf("%s: no children in the demo data", school)
		}

		managed := 0

		for _, a := range d.Accounts {
			if !children[a.PersonID] {
				continue
			}

			managed++

			p := d.Persons[slices.IndexFunc(d.Persons, func(p domain.Person) bool { return p.ID == a.PersonID })]
			if a.Role != domain.RoleMember || p.Email != a.Email {
				t.Errorf("%s: child account %+v, want a member account with the child's email", school, a)
			}
		}

		if school == seed.SchoolProgramme && managed != 1 {
			t.Errorf("%s: %d children with an account, want one managed dependent", school, managed)
		}

		// A class is either a children's or an adults' class.
		kind := map[domain.ClassID]bool{}
		seen := map[domain.ClassID]bool{}

		for _, m := range d.ClassMembers {
			child := children[m.PersonID]
			if seen[m.ClassID] && kind[m.ClassID] != child {
				t.Errorf("%s: class %v mixes children and adults", school, m.ClassID)
			}

			seen[m.ClassID] = true
			kind[m.ClassID] = child
		}
	})
}

// TestLoadDemoRoundTrip loads every demo school into a memory store and
// reads every record back, the background included.
func TestLoadDemoRoundTrip(t *testing.T) {
	t.Parallel()

	forEachSchool(t, func(t *testing.T, school seed.School, want seed.Dataset) {
		st := memory.New()
		if err := seed.LoadDemoSchoolAt(t.Context(), st, seed.FixedDemoAnchor, school); err != nil {
			t.Fatalf("%s: %v", school, err)
		}

		// name prefixes a record kind with the school for the failures.
		name := func(kind string) string { return string(school) + ": " + kind }

		err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			settings, err := tx.Settings().Get(ctx)
			if err != nil {
				return err
			}

			if settings != want.Settings {
				t.Errorf("%s: settings: got %+v, want %+v", school, settings, want.Settings)
			}

			info, err := tx.Settings().BackgroundInfo(ctx)
			if err != nil {
				return err
			}

			if want.Background.IsZero() {
				if !info.IsZero() {
					t.Errorf("%s: background info %+v, want none", school, info)
				}
			} else {
				bg, err := tx.Settings().Background(ctx)
				if err != nil {
					return err
				}

				if bg.ContentType != want.Background.ContentType || !slices.Equal(bg.Data, want.Background.Data) {
					t.Errorf("%s: background %s of %d bytes, want %s of %d bytes", school,
						bg.ContentType, len(bg.Data), want.Background.ContentType, len(want.Background.Data))
				}
			}

			persons, err := tx.Persons().List(ctx)
			if err != nil {
				return err
			}

			sameSet(t, name("persons"), persons, want.Persons)

			accounts, err := tx.Accounts().List(ctx)
			if err != nil {
				return err
			}

			sameSet(t, name("accounts"), accounts, want.Accounts)

			for _, c := range want.Credentials {
				got, err := tx.Credentials().Get(ctx, c.AccountID)
				if err != nil {
					return err
				}

				if got != c {
					t.Errorf("%s: credential: got %+v, want %+v", school, got, c)
				}
			}

			for _, a := range want.Accounts {
				got, err := tx.Accounts().GetByEmail(ctx, strings.ToUpper(a.Email))
				if err != nil {
					return err
				}

				if got != a {
					t.Errorf("%s: GetByEmail(%q): got %+v, want %+v", school, a.Email, got, a)
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

			sameSet(t, name("guardians"), guardians, want.Guardians)

			locations, err := tx.Locations().List(ctx)
			if err != nil {
				return err
			}

			sameSet(t, name("locations"), locations, want.Locations)

			rooms, err := tx.Rooms().List(ctx, domain.LocationID{})
			if err != nil {
				return err
			}

			sameSet(t, name("rooms"), rooms, want.Rooms)

			staff, err := tx.Staff().List(ctx)
			if err != nil {
				return err
			}

			sameSet(t, name("staff"), staff, want.Staff)

			courseTypes, err := tx.CourseTypes().List(ctx)
			if err != nil {
				return err
			}

			sameSet(t, name("course types"), courseTypes, want.CourseTypes)

			levels, err := tx.Levels().List(ctx)
			if err != nil {
				return err
			}

			// Listed by position, the order of the scale.
			sameSlice(t, name("levels"), levels, want.Levels)

			terms, err := tx.Terms().List(ctx)
			if err != nil {
				return err
			}

			sameSet(t, name("terms"), terms, want.Terms)

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

			sameSet(t, name("classes"), classes, want.Classes)
			sameSet(t, name("class members"), members, want.ClassMembers)

			from, to := allTime()

			lessons, err := tx.Lessons().ListRange(ctx, from, to)
			if err != nil {
				return err
			}

			if len(want.Lessons) == 0 {
				t.Errorf("%s: the demo has no lessons", school)
			}

			sameSet(t, name("lessons"), lessons, want.Lessons)

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

			sameSet(t, name("bookings"), bookings, want.Bookings)
			sameSet(t, name("booking events"), events, want.BookingEvents)

			var entries []domain.WaitlistEntry

			for _, l := range lessons {
				es, err := tx.Waitlist().ListByLesson(ctx, l.ID)
				if err != nil {
					return err
				}

				entries = append(entries, es...)
			}

			sameSet(t, name("waitlist entries"), entries, want.WaitlistEntries)

			passTypes, err := tx.PassTypes().List(ctx)
			if err != nil {
				return err
			}

			sameSet(t, name("pass types"), passTypes, want.PassTypes)

			personIDs := make([]domain.PersonID, len(persons))
			for i, p := range persons {
				personIDs[i] = p.ID
			}

			passes, err := tx.Passes().ListByPersons(ctx, personIDs)
			if err != nil {
				return err
			}

			sameSet(t, name("passes"), passes, want.Passes)

			passIDs := make([]domain.PassID, len(passes))
			for i, p := range passes {
				passIDs[i] = p.ID
			}

			redemptions, err := tx.Redemptions().ListByPasses(ctx, passIDs, 0)
			if err != nil {
				return err
			}

			sameSet(t, name("redemptions"), redemptions, want.Redemptions)

			receipts, total, err := tx.Receipts().List(ctx, store.ReceiptFilter{Limit: 100})
			if err != nil {
				return err
			}

			if total != len(want.Receipts) {
				t.Errorf("%s: %d receipts stored, want %d", school, total, len(want.Receipts))
			}

			for _, r := range receipts {
				if i := slices.IndexFunc(want.Receipts, func(w domain.Receipt) bool { return w.ID == r.ID }); i < 0 ||
					!reflect.DeepEqual(r, want.Receipts[i]) {
					t.Errorf("%s: stored receipt %+v is not the seed's", school, r)
				}
			}

			announcements, err := tx.Announcements().List(ctx)
			if err != nil {
				return err
			}

			sameSet(t, name("announcements"), announcements, want.Announcements)

			return checkStoredEvents(ctx, t, name, tx, want)
		})
		if err != nil {
			t.Fatalf("%s: %v", school, err)
		}
	})
}

// checkStoredEvents compares the events in tx, with their sessions and
// registrations, to want's. The stores keep instants in UTC, as the seed
// makes them (domain.Date.At).
func checkStoredEvents(ctx context.Context, t *testing.T, name func(string) string, tx store.Tx, want seed.Dataset) error {
	t.Helper()

	events, err := tx.Events().List(ctx)
	if err != nil {
		return err
	}

	if len(want.Events) == 0 {
		t.Errorf("%s", name("the demo has no events"))
	}

	sameSet(t, name("events"), events, want.Events)

	for _, e := range want.Events {
		sessions, err := tx.Lessons().ListByEvent(ctx, e.ID)
		if err != nil {
			return err
		}

		var wantSessions []domain.Lesson

		for _, l := range want.Lessons {
			if l.EventID == e.ID {
				wantSessions = append(wantSessions, l)
			}
		}

		sameSet(t, name("sessions of "+e.Slug), sessions, wantSessions)

		regs, err := tx.EventRegistrations().ListByEvent(ctx, e.ID)
		if err != nil {
			return err
		}

		var wantRegs []domain.EventRegistration

		for _, r := range want.EventRegistrations {
			if r.EventID == e.ID {
				wantRegs = append(wantRegs, r)
			}
		}

		sameSet(t, name("registrations for "+e.Slug), regs, wantRegs)
	}

	return nil
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

// TestLoadDemoRefusesStoreWithLevels: a school that has only set up its
// level scale is in use, too.
func TestLoadDemoRefusesStoreWithLevels(t *testing.T) {
	t.Parallel()

	st := memory.New()
	l := domain.Level{ID: domain.LevelID{1}, Name: "Existing Level", Position: 1}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Levels().Create(ctx, l)
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := seed.LoadDemo(t.Context(), st); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("load into store with levels: want %v, got %v", domain.ErrConflict, err)
	}
}

// validAll checks that every item is valid; what names the dataset (the
// school or the anchor) in the failure.
func validAll[T interface{ Validate() error }](t *testing.T, what string, items []T) {
	t.Helper()

	for _, v := range items {
		if err := v.Validate(); err != nil {
			t.Errorf("%s: invalid %T %+v: %v", what, v, v, err)
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

// exampleEmail reports whether email is empty or on a domain reserved for
// examples (RFC 2606): example.org, .com or .net, or a host under the
// .example top-level domain.
func exampleEmail(email string) bool {
	if email == "" {
		return true
	}

	for _, host := range []string{"@example.org", "@example.com", "@example.net"} {
		if strings.HasSuffix(email, host) {
			return true
		}
	}

	at := strings.LastIndex(email, "@")
	host := email[at+1:]

	return at > 0 && strings.HasSuffix(host, ".example") && len(host) > len(".example")
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

	validAll(t, string(seed.SchoolProgramme), d.Credentials)
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

		checkReceipts(t, anchor, d, loc)
	}
}

// checkReceipts: every sold pass has its receipt for the price paid, by
// the payer, the voided one a cancellation, numbered gaplessly per year in
// the order of issue in the school's time zone.
func checkReceipts(t *testing.T, anchor domain.Date, d seed.Dataset, loc *time.Location) {
	t.Helper()

	byID := index(d.Receipts, func(r domain.Receipt) domain.ReceiptID { return r.ID })
	next := map[int]int{}

	for i, r := range d.Receipts {
		next[r.Year]++

		if r.Sequence != next[r.Year] || r.Year != domain.DateOf(r.IssuedAt, loc).Year ||
			r.Number != domain.ReceiptNumber("SD", r.Year, r.Sequence) ||
			(i > 0 && r.IssuedAt.Before(d.Receipts[i-1].IssuedAt)) {
			t.Errorf("%v: receipt %s (%d of %d) out of order", anchor, r.Number, r.Sequence, r.Year)
		}
	}

	cancellations := 0

	for _, r := range d.Receipts {
		if r.Kind != domain.ReceiptCancellation {
			continue
		}

		cancellations++

		if orig, ok := byID[r.CancelsReceiptID]; !ok || r.Gross != -orig.Gross || !orig.IssuedAt.Before(r.IssuedAt) {
			t.Errorf("%v: cancellation %+v of %+v", anchor, r, orig)
		}
	}

	for _, p := range d.Passes {
		r, ok := byID[p.ReceiptID]

		switch {
		case p.Origin == domain.PassAssigned:
			if !p.ReceiptID.IsZero() {
				t.Errorf("%v: assigned pass %+v has a receipt", anchor, p)
			}
		case !ok || r.Gross != p.PricePaid || r.PayerID != cmp.Or(p.PayerID, p.PersonID) || r.Kind != domain.ReceiptSale ||
			!r.IssuedAt.Equal(p.SoldAt):
			t.Errorf("%v: pass %+v, receipt %+v", anchor, p, r)
		}
	}

	if cancellations != 1 {
		t.Errorf("%v: %d cancellations, want 1", anchor, cancellations)
	}
}

func TestSchoolValid(t *testing.T) {
	t.Parallel()

	for _, s := range []seed.School{seed.SchoolProgramme, seed.SchoolWerkstatt} {
		if !s.Valid() {
			t.Errorf("%q is not valid", s)
		}
	}

	// The zero School is not a default: the caller names the school.
	for _, s := range []seed.School{"", "x", "Programme", "werkstatt "} {
		if s.Valid() {
			t.Errorf("%q is valid", s)
		}
	}

	if got := seed.Schools(); !slices.Equal(got, []seed.School{seed.SchoolProgramme, seed.SchoolWerkstatt}) {
		t.Errorf("Schools() = %v, want programme first, then werkstatt", got)
	}
}

// werkstattIssuer is the werkstatt school's fictional receipt issuer.
var werkstattIssuer = domain.ReceiptIssuer{
	Name: "Tanzhaus am Lindenhof", Address: "Lindenhofweg 12\n10999 Beispielstadt",
	TaxID: "99/999/99999", Email: "buero@tanzhaus-lindenhof.example", Phone: "+49 000 0000000",
}

// TestWerkstattOwnIdentity: nothing of the programme school's identity
// (its name, imprint, issuer, mail domain, receipt prefix) is left in the
// werkstatt school's settings or receipts.
func TestWerkstattOwnIdentity(t *testing.T) {
	t.Parallel()

	d, err := seed.DemoSchoolAt(seed.FixedDemoAnchor, seed.SchoolWerkstatt)
	if err != nil {
		t.Fatal(err)
	}

	for what, text := range map[string]string{
		"settings": fmt.Sprintf("%+v", d.Settings),
		"receipts": fmt.Sprintf("%+v", d.Receipts),
	} {
		lower := strings.ToLower(text)
		for _, programme := range []string{"studio demo", "studio-demo", "pirouette", "exampletown", "example bank"} {
			if strings.Contains(lower, programme) {
				t.Errorf("the werkstatt %s contain %q: %s", what, programme, text)
			}
		}
	}

	s := d.Settings
	if s.Receipts.Issuer != werkstattIssuer || s.ReceiptPrefix != "TL" || s.DefaultLocale != "de" ||
		!strings.Contains(s.LegalNotice, "Tanzhaus am Lindenhof") || !strings.Contains(s.LegalNotice, "erfunden") ||
		!strings.Contains(s.Receipts.Footer, "Beispielbank") {
		t.Errorf("werkstatt settings %+v: want its own issuer, prefix TL, German, its own imprint and footer", s)
	}

	if err := s.Receipts.Complete(); err != nil {
		t.Errorf("werkstatt receipt settings incomplete: %v", err)
	}

	fees := map[domain.ReceiptID]bool{}
	for _, reg := range d.EventRegistrations {
		if !reg.ReceiptID.IsZero() {
			fees[reg.ReceiptID] = true
		}
	}

	for _, r := range d.Receipts {
		if r.Locale != "de" || r.Issuer != werkstattIssuer || !strings.HasPrefix(r.Number, "TL-") {
			t.Errorf("receipt %+v: want German, issued by %s, numbered TL-…", r, werkstattIssuer.Name)
		}

		// A pass's line says how long it is valid, an event fee's names the
		// participant: "<event> für <person>, <first> – <last>".
		word := "gültig"
		if fees[r.ID] {
			word = " für "
		}

		if r.Kind == domain.ReceiptSale && !strings.Contains(r.Lines[0].Description, word) {
			t.Errorf("receipt %s line %q is not German", r.Number, r.Lines[0].Description)
		}
	}

	if len(fees) == 0 {
		t.Error("no werkstatt receipt is for an event fee")
	}
}

// TestLoadDemoWerkstatt loads the werkstatt school: its own name, theme,
// accent, language, imprint, receipt issuer and background, its own
// records, and receipts in German by its own issuer.
func TestLoadDemoWerkstatt(t *testing.T) {
	t.Parallel()

	image, err := os.ReadFile("assets/werkstatt-background.jpg")
	if err != nil {
		t.Fatal(err)
	}

	want := seed.DemoSettings()
	want.SchoolName = "Tanzhaus am Lindenhof"
	want.Theme = domain.ThemeWerkstatt
	want.AccentColor = "#9c215b"
	want.DefaultLocale = "de"
	want.ReceiptPrefix = "TL"
	want.LegalNotice = "Tanzhaus am Lindenhof · Lindenhofweg 12 · 10999 Beispielstadt\n" +
		"Eine erfundene Schule: Alle Namen, Adressen und Nummern in dieser Demo sind ausgedacht."
	want.Receipts.Issuer = werkstattIssuer
	want.Receipts.Footer = "Bankverbindung: Beispielbank · IBAN XX00 0000 0000 0000 0000 00"

	st := memory.New()
	if err := seed.LoadDemoSchoolAt(t.Context(), st, seed.FixedDemoAnchor, seed.SchoolWerkstatt); err != nil {
		t.Fatal(err)
	}

	d, err := seed.DemoSchoolAt(seed.FixedDemoAnchor, seed.SchoolWerkstatt)
	if err != nil {
		t.Fatal(err)
	}

	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		got, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		if got != want {
			t.Errorf("settings = %+v\nwant %+v", got, want)
		}

		info, err := tx.Settings().BackgroundInfo(ctx)
		if err != nil {
			return err
		}

		if info.IsZero() || info.ContentType != domain.BackgroundJPEG {
			t.Errorf("background info = %+v, want a JPEG", info)
		}

		bg, err := tx.Settings().Background(ctx)
		if err != nil {
			return err
		}

		if bg.ContentType != domain.BackgroundJPEG || !slices.Equal(bg.Data, image) {
			t.Errorf("background: %s, %d bytes; want the embedded JPEG of %d bytes",
				bg.ContentType, len(bg.Data), len(image))
		}

		if info.Digest != bg.Digest() {
			t.Errorf("background digest %q, want %q", info.Digest, bg.Digest())
		}

		persons, err := tx.Persons().List(ctx)
		if err != nil {
			return err
		}

		sameSet(t, "persons", persons, d.Persons)

		classes, err := tx.Classes().ListByTerm(ctx, d.Terms[0].ID)
		if err != nil {
			return err
		}

		sameSet(t, "classes", classes, d.Classes)

		var rooms []domain.Room

		for _, l := range d.Locations {
			at, err := tx.Rooms().List(ctx, l.ID)
			if err != nil {
				return err
			}

			rooms = append(rooms, at...)
		}

		sameSet(t, "rooms", rooms, d.Rooms)

		from, to := allTime()

		lessons, err := tx.Lessons().ListRange(ctx, from, to)
		if err != nil {
			return err
		}

		sameSet(t, "lessons", lessons, d.Lessons)

		receipts, total, err := tx.Receipts().List(ctx, store.ReceiptFilter{Limit: 100})
		if err != nil {
			return err
		}

		if total != len(d.Receipts) || total == 0 {
			t.Errorf("%d receipts, want %d", total, len(d.Receipts))
		}

		for _, r := range receipts {
			if r.Locale != "de" || r.Issuer != werkstattIssuer || !strings.HasPrefix(r.Number, "TL-") ||
				r.Footer != want.Receipts.Footer {
				t.Errorf("receipt %+v: want German, issued by %s, numbered TL-…", r, werkstattIssuer.Name)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestLoadDemoProgrammeHasNoBackground: the programme school keeps the app's
// own look, the zero theme, without a background.
func TestLoadDemoProgrammeHasNoBackground(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemoSchoolAt(t.Context(), st, seed.FixedDemoAnchor, seed.SchoolProgramme); err != nil {
		t.Fatal(err)
	}

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		got, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		if got != seed.DemoSettings() || got.Theme != "" {
			t.Errorf("settings = %+v, want DemoSettings with the zero theme", got)
		}

		info, err := tx.Settings().BackgroundInfo(ctx)
		if err != nil {
			return err
		}

		if !info.IsZero() {
			t.Errorf("background info = %+v, want none", info)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestLoadDemoUnknownSchool: an unknown school is an error, and nothing is
// written.
func TestLoadDemoUnknownSchool(t *testing.T) {
	t.Parallel()

	for _, school := range []seed.School{"", "x"} {
		st := memory.New()

		err := seed.LoadDemoSchoolAt(t.Context(), st, seed.FixedDemoAnchor, school)
		if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "programme, werkstatt") {
			t.Errorf("school %q: error = %v, want a validation error naming the schools", school, err)
		}

		if _, err := seed.DemoSchoolAt(seed.FixedDemoAnchor, school); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("DemoSchoolAt(%q): error = %v, want %v", school, err, domain.ErrValidation)
		}

		err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			if _, err := tx.Settings().Get(ctx); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("school %q: settings error = %v, want %v", school, err, domain.ErrNotFound)
			}

			persons, err := tx.Persons().List(ctx)
			if len(persons) > 0 {
				t.Errorf("school %q: %d persons written", school, len(persons))
			}

			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestParseSchool: the web demo's worker names the school in DEMO_SCHOOL;
// an empty or unknown name falls back to the programme school, and ok tells
// the caller it did.
func TestParseSchool(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		want seed.School
		ok   bool
	}{
		{"programme", seed.SchoolProgramme, true},
		{"werkstatt", seed.SchoolWerkstatt, true},
		{"", seed.SchoolProgramme, false},
		{"x", seed.SchoolProgramme, false},
		{"Werkstatt", seed.SchoolProgramme, false},
		{" werkstatt", seed.SchoolProgramme, false},
	} {
		if got, ok := seed.ParseSchool(c.name); got != c.want || ok != c.ok {
			t.Errorf("ParseSchool(%q) = %q, %v; want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

// TestWerkstattPlaces: the werkstatt school has one house of its own,
// the Tanzhaus am Lindenhof, with its four rooms; for every anchor, no two
// of its lessons, the workshops and the rental included, meet in one room.
func TestWerkstattPlaces(t *testing.T) {
	t.Parallel()

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		werkstatt, err := seed.DemoSchoolAt(anchor, seed.SchoolWerkstatt)
		if err != nil {
			t.Fatal(err)
		}

		if len(werkstatt.Locations) != 1 || werkstatt.Locations[0].Name != "Tanzhaus am Lindenhof" {
			t.Fatalf("werkstatt locations = %+v, want only the Tanzhaus am Lindenhof", werkstatt.Locations)
		}

		rooms := index(werkstatt.Rooms, func(r domain.Room) domain.RoomID { return r.ID })

		var names []string
		for _, r := range werkstatt.Rooms {
			names = append(names, r.Name)
		}

		if want := []string{"Oben", "Unten", "Online", "Trainingsraum"}; !slices.Equal(names, want) {
			t.Errorf("werkstatt rooms = %q, want %q", names, want)
		}

		byRoom := map[domain.RoomID][]domain.Lesson{}
		for _, l := range werkstatt.Lessons {
			for _, other := range byRoom[l.RoomID] {
				if l.StartsAt.Before(other.EndsAt) && other.StartsAt.Before(l.EndsAt) {
					t.Errorf("%v: in %s, lesson %v (%v) meets lesson %v (%v)", anchor,
						rooms[l.RoomID].Name, l.ID, l.StartsAt, other.ID, other.StartsAt)
				}
			}

			byRoom[l.RoomID] = append(byRoom[l.RoomID], l)
		}
	}
}

// TestDemoRoomPositions: every school's rooms are numbered from 1 within
// their location in the order of its table; Werkstatt's halls come first,
// then the online room, then its training room.
func TestDemoRoomPositions(t *testing.T) {
	t.Parallel()

	for _, school := range seed.Schools() {
		d, err := seed.DemoSchoolAt(seed.FixedDemoAnchor, school)
		if err != nil {
			t.Fatal(err)
		}

		next := map[domain.LocationID]int{}
		for _, r := range d.Rooms {
			next[r.LocationID]++
			if r.Position != next[r.LocationID] {
				t.Errorf("%s: room %s at position %d, want %d", school, r.Name, r.Position, next[r.LocationID])
			}
		}

		if len(next) != len(d.Locations) {
			t.Errorf("%s: rooms at %d of %d locations", school, len(next), len(d.Locations))
		}
	}

	werkstatt, err := seed.DemoSchoolAt(seed.FixedDemoAnchor, seed.SchoolWerkstatt)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, r := range werkstatt.Rooms {
		got = append(got, fmt.Sprintf("%d %s", r.Position, r.Name))
	}

	if want := []string{"1 Oben", "2 Unten", "3 Online", "4 Trainingsraum"}; !slices.Equal(got, want) {
		t.Errorf("werkstatt rooms = %q, want %q", got, want)
	}
}

// TestDemoLevels: every demo school has its own levels, from least to most
// advanced, at positions from 1, with their codes: the programme school four
// in English, the werkstatt school five in German. Loading puts them into the
// store in that order.
func TestDemoLevels(t *testing.T) {
	t.Parallel()

	levels := map[seed.School][]string{
		seed.SchoolProgramme: {"1 Beginner B", "2 Intermediate I", "3 Advanced A", "4 Professional P"},
		seed.SchoolWerkstatt: {"1 Einsteiger E", "2 Anfänger A", "3 Mittelstufe M", "4 Fortgeschritten F", "5 Profi P"},
	}

	for _, school := range seed.Schools() {
		want := levels[school]

		d, err := seed.DemoSchoolAt(seed.FixedDemoAnchor, school)
		if err != nil {
			t.Fatal(err)
		}

		if got := levelLines(d.Levels); !slices.Equal(got, want) {
			t.Errorf("%s: levels = %q, want %q", school, got, want)
		}

		st := memory.New()
		if err := seed.LoadDemoSchoolAt(t.Context(), st, seed.FixedDemoAnchor, school); err != nil {
			t.Fatal(err)
		}

		var stored []domain.Level

		err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			stored, err = tx.Levels().List(ctx)

			return err
		})
		if err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(stored, d.Levels) {
			t.Errorf("%s: stored levels %+v, want %+v", school, stored, d.Levels)
		}
	}
}

func levelLines(levels []domain.Level) []string {
	lines := make([]string, 0, len(levels))
	for _, l := range levels {
		lines = append(lines, fmt.Sprintf("%d %s %s", l.Position, l.Name, l.Code))
	}

	return lines
}

// TestDemoClassAudience: every class's levels are levels of its school; in
// the programme school the children's classes state ages instead of a
// level, and one Classical Ballet class spans Beginner to Intermediate, so
// the demo shows a span (the werkstatt school's audiences:
// TestWerkstattAudiences).
func TestDemoClassAudience(t *testing.T) {
	t.Parallel()

	for _, school := range seed.Schools() {
		d, err := seed.DemoSchoolAt(seed.FixedDemoAnchor, school)
		if err != nil {
			t.Fatal(err)
		}

		levels := map[domain.LevelID]string{}
		for _, l := range d.Levels {
			levels[l.ID] = l.Name
		}

		courseTypes := map[domain.CourseTypeID]string{}
		for _, ct := range d.CourseTypes {
			courseTypes[ct.ID] = ct.Name
		}

		spans := 0

		for _, c := range d.Classes {
			if !c.Levels.IsZero() {
				if _, ok := levels[c.Levels.From]; !ok {
					t.Errorf("%s: class %v starts at unknown level %v", school, c.ID, c.Levels.From)
				}

				if _, ok := levels[c.Levels.To]; !ok {
					t.Errorf("%s: class %v ends at unknown level %v", school, c.ID, c.Levels.To)
				}
			}

			switch name := courseTypes[c.CourseTypeID]; name {
			case "Creative Dance", "Pre-Ballet":
				want := map[string][2]domain.AgeMonths{"Creative Dance": {48, 72}, "Pre-Ballet": {72, 96}}[name]
				if !c.Levels.IsZero() || c.AllLevels || [2]domain.AgeMonths{c.AgeFrom, c.AgeTo} != want {
					t.Errorf("%s: %s class %+v, want ages %v and no level", school, name, c, want)
				}
			case "Classical Ballet":
				if levels[c.Levels.From] == "Beginner" && levels[c.Levels.To] == "Intermediate" {
					spans++
				}
			}
		}

		if school == seed.SchoolProgramme && spans != 1 {
			t.Errorf("%s: %d Beginner–Intermediate Classical Ballet classes, want 1", school, spans)
		}
	}
}
