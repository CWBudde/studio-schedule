package seed

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
)

// FixedDemoAnchor is the visit date that Demo and LoadDemo build the demo
// for (the server and `studio-schedule seed`). It makes the demo term the
// school year 2026/27, 2026-09-01 to 2027-07-31, on every run. The web demo
// builds the data for the day of the visit instead (DemoAt).
//
// Its workshops fall before 2026-10-19, outside the weeks that the schedule
// tests of other packages fill with lessons of their own.
var FixedDemoAnchor = date(2026, time.September, 22)

// The demo term starts termLeadDays before the anchor and lasts termDays
// (inclusive), the length of a school year from September 1 to July 31.
// So every anchor lies in the term, with three weeks of timetable before
// it and about ten months (44 weeks) after it.
const (
	termLeadDays = 3 * 7
	termDays     = 334
)

// Demo returns the demo school for FixedDemoAnchor; see DemoAt.
func Demo() Dataset { return DemoAt(FixedDemoAnchor) }

// DemoAt returns the fictional demo school as seen on the visit date
// anchor: two locations, their rooms, staff, course types, one term around
// anchor with its weekly classes, members with their families and accounts,
// and a few one-off workshops in the weeks after anchor. Of the weekly
// classes it has the lessons of the week up to and including anchor
// (history); the lesson generator creates the later ones, but never the
// past. It has no bookings. IDs come from a fresh ids.Sequence, so every call with the same
// anchor returns identical data.
//
// DemoAt panics if the demo time zone cannot be loaded (no zoneinfo
// database); LoadDemoAt reports that as an error instead.
func DemoAt(anchor domain.Date) Dataset {
	d, err := demoAt(anchor)
	if err != nil {
		panic(err)
	}

	return d
}

func demoAt(anchor domain.Date) (Dataset, error) {
	settings := DemoSettings()

	// The workshops start at wall-clock times of the school.
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return Dataset{}, fmt.Errorf("seed: demo time zone %q: %w", settings.Timezone, err)
	}

	termStart := anchor.AddDays(-termLeadDays)

	b := builder{
		loc:         loc,
		termStart:   termStart,
		seq:         ids.NewSequence(),
		d:           Dataset{Settings: settings},
		persons:     map[string]domain.PersonID{},
		rooms:       map[string]domain.RoomID{},
		staff:       map[string]domain.StaffID{},
		courseTypes: map[string]domain.CourseTypeID{},
	}

	for _, h := range demoHouseholds {
		b.household(h)
	}

	for _, l := range demoLocations {
		b.location(l)
	}

	for _, s := range demoStaff {
		b.staffMember(s)
	}

	for _, c := range demoCourseTypes {
		b.courseType(c)
	}

	termEnd := termStart.AddDays(termDays - 1)
	term := domain.Term{
		ID:       ids.New[domain.TermID](b.seq),
		Name:     termName(termStart, termEnd),
		StartsOn: termStart,
		EndsOn:   termEnd,
	}
	b.d.Terms = append(b.d.Terms, term)

	for _, c := range demoClasses {
		b.class(term.ID, c)
	}

	for _, w := range demoWorkshops {
		b.workshop(anchor, w)
	}

	b.history(anchor)

	return b.d, nil
}

// termName is "School Year 2026/27", or "School Year 2027" for a term
// within one calendar year.
func termName(start, end domain.Date) string {
	if start.Year == end.Year {
		return fmt.Sprintf("School Year %d", start.Year)
	}

	return fmt.Sprintf("School Year %d/%02d", start.Year, end.Year%100)
}

// ------------------------------------------------------------------ tables
//
// All names, addresses, phone numbers and emails are invented. Emails use
// the reserved example domains only.

// adultSpec is an adult of a household. A zero role means no account.
type adultSpec struct {
	given string
	role  domain.Role
	phone string
}

// childSpec is a child of a household; the household's adults are its
// guardians.
type childSpec struct {
	given string
	born  domain.Date
}

// householdSpec is a family, a single adult or a staff member's person
// record. Adults with an account get the email given.family@host.
type householdSpec struct {
	family   string
	host     string
	adults   []adultSpec
	children []childSpec
}

var demoHouseholds = []householdSpec{
	// Families with children.
	{
		"Amberly", "example.org",
		[]adultSpec{{"Clara", domain.RoleMember, "+49 000 0000101"}, {"Tom", domain.RoleMember, ""}},
		[]childSpec{{"Ella", date(2016, time.April, 12)}, {"Finn", date(2019, time.September, 3)}},
	},
	{
		"Birchmoor", "example.com",
		[]adultSpec{{"Hanna", domain.RoleMember, "+49 000 0000102"}},
		[]childSpec{{"Lotta", date(2014, time.February, 20)}, {"Mia", date(2020, time.June, 15)}},
	},
	{
		"Cloudsley", "example.org",
		[]adultSpec{{"Jan", domain.RoleMember, ""}},
		[]childSpec{{"Noah", date(2013, time.November, 8)}},
	},
	{
		"Dunfield", "example.com",
		[]adultSpec{{"Sara", domain.RoleMember, "+49 000 0000104"}, {"Ali", "", ""}},
		[]childSpec{{"Leni", date(2017, time.January, 25)}},
	},
	{
		"Elderwick", "example.org",
		[]adultSpec{{"Petra", domain.RoleMember, ""}},
		[]childSpec{{"Ida", date(2012, time.July, 30)}, {"Emil", date(2015, time.March, 14)}, {"Rosa", date(2018, time.October, 1)}},
	},
	{
		"Fernwhistle", "example.com",
		[]adultSpec{{"Marta", domain.RoleMember, ""}, {"Lukas", domain.RoleMember, "+49 000 0000106"}},
		[]childSpec{{"Jona", date(2011, time.May, 19)}},
	},
	{
		"Gildenmoor", "example.org",
		[]adultSpec{{"Yara", domain.RoleMember, ""}},
		[]childSpec{{"Kira", date(2016, time.December, 2)}, {"Ben", date(2014, time.August, 22)}},
	},
	{
		"Hollowmere", "example.com",
		[]adultSpec{{"David", domain.RoleMember, ""}},
		[]childSpec{{"Sophie", date(2010, time.March, 11)}},
	},
	{
		"Ivorycombe", "example.org",
		[]adultSpec{{"Nora", domain.RoleMember, "+49 000 0000109"}},
		[]childSpec{{"Jakob", date(2015, time.July, 7)}},
	},

	// Adults without children.
	{"Inkwell", "example.com", []adultSpec{{"Irene", domain.RoleMember, ""}}, nil},
	{"Lanternby", "example.org", []adultSpec{{"Kai", domain.RoleMember, ""}}, nil},
	{"Mistlewood", "example.com", []adultSpec{{"Lina", domain.RoleMember, "+49 000 0000112"}}, nil},
	{"Nettlebury", "example.org", []adultSpec{{"Oskar", domain.RoleMember, ""}}, nil},
	{"Pinecrest", "example.com", []adultSpec{{"Rhea", domain.RoleMember, ""}}, nil},
	{"Quarrystone", "example.org", []adultSpec{{"Tilo", domain.RoleMember, ""}}, nil},
	{"Rushwick", "example.com", []adultSpec{{"Vera", domain.RoleMember, ""}}, nil},
	{"Sparrowhill", "example.org", []adultSpec{{"Wanda", domain.RoleMember, ""}}, nil},
	{"Tallowfield", "", []adultSpec{{"Ute", "", "+49 000 0000118"}}, nil}, // walk-in guest without an account

	// Staff and office.
	{"Starling", "example.net", []adultSpec{{"Alina", domain.RoleInstructor, ""}}, nil},
	{"Quillon", "", []adultSpec{{"Sofia", "", ""}}, nil},
	{"Keysworth", "", []adultSpec{{"Felix", "", ""}}, nil},
	{"Ledgerby", "example.net", []adultSpec{{"Olga", domain.RoleOffice, ""}}, nil},
	{"Keystone", "example.net", []adultSpec{{"Arno", domain.RoleAdmin, ""}}, nil},
}

type locationSpec struct {
	name    string
	address string
	rooms   []roomSpec
}

type roomSpec struct {
	name     string
	capacity int
}

var demoLocations = []locationSpec{
	{"Studio North", "Example Street 12, 00000 Example Town", []roomSpec{{"Studio A", 20}, {"Studio B", 14}, {"Studio C", 10}}},
	{"Studio South", "Sample Road 3, 00000 Example Town", []roomSpec{{"Hall 1", 24}, {"Hall 2", 12}}},
}

// staffSpec is a staff member; person is the "Given Family" key of their
// person record, or empty for staff without one.
type staffSpec struct {
	name   string
	person string
	kind   domain.StaffKind
	active bool
}

var demoStaff = []staffSpec{
	{"Alina Starling", "Alina Starling", domain.StaffInstructor, true},
	{"Sofia Quillon", "Sofia Quillon", domain.StaffInstructor, true},
	{"Marco Wrenfield", "", domain.StaffInstructor, true},
	{"Jonas Featherby", "", domain.StaffInstructor, true},
	{"Lea Moonfield", "", domain.StaffInstructor, true},
	{"Nina Tapworth", "", domain.StaffInstructor, true},
	{"Quentin Oldstage", "", domain.StaffInstructor, false}, // former instructor
	{"Felix Keysworth", "Felix Keysworth", domain.StaffAccompanist, true},
	{"Greta Ivorylane", "", domain.StaffAccompanist, true},
}

var demoCourseTypes = []domain.CourseType{
	{Name: "Creative Dance", Description: "Playful movement and music for children aged 4 to 6."},
	{Name: "Pre-Ballet", Description: "First ballet steps and posture for children aged 6 to 8."},
	{Name: "Classical Ballet", Description: "Barre, center work and allegro with live piano."},
	{Name: "Pointe Work", Description: "Pointe technique for advanced ballet students."},
	{Name: "Contemporary", Description: "Floor work, release technique and improvisation."},
	{Name: "Jazz Dance", Description: "Isolations, turns and jumps to jazz and pop music."},
	{Name: "Hip-Hop", Description: "Grooves, footwork and choreography."},
	{Name: "Tap", Description: "Rhythm and sound with tap shoes."},
	{Name: "Barre Fitness", Description: "Strength and stretching at the barre, no experience needed."},
}

// classSpec is a weekly class. Names refer to the tables above; members are
// "Given Family" keys of persons.
type classSpec struct {
	day         time.Weekday
	start       domain.TimeOfDay
	minutes     int
	room        string
	courseType  string
	instructor  string
	accompanist string
	level       domain.Level
	capacity    int
	members     []string
}

var demoClasses = []classSpec{
	// Monday
	{
		time.Monday, at(15, 30), 45, "Studio C", "Creative Dance", "Sofia Quillon", "", domain.LevelOpen, 10,
		[]string{"Mia Birchmoor", "Finn Amberly"},
	},
	{
		time.Monday, at(16, 30), 60, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", domain.LevelBeginner, 16,
		[]string{"Rosa Elderwick", "Leni Dunfield", "Ella Amberly"},
	},
	{
		time.Monday, at(17, 45), 75, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", domain.LevelIntermediate, 16,
		[]string{"Lotta Birchmoor", "Ben Gildenmoor", "Emil Elderwick"},
	},
	{
		time.Monday, at(18, 0), 60, "Hall 1", "Hip-Hop", "Jonas Featherby", "", domain.LevelBeginner, 20,
		[]string{"Noah Cloudsley", "Jakob Ivorycombe"},
	},
	{
		time.Monday, at(19, 15), 90, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", domain.LevelAdvanced, 16,
		[]string{"Rhea Pinecrest", "Wanda Sparrowhill"},
	},

	// Tuesday
	{
		time.Tuesday, at(16, 0), 45, "Studio B", "Pre-Ballet", "Sofia Quillon", "", domain.LevelBeginner, 12,
		[]string{"Mia Birchmoor", "Rosa Elderwick"},
	},
	{
		time.Tuesday, at(17, 0), 60, "Hall 1", "Jazz Dance", "Nina Tapworth", "", domain.LevelBeginner, 20,
		[]string{"Kira Gildenmoor", "Ella Amberly", "Emil Elderwick"},
	},
	{
		time.Tuesday, at(18, 30), 75, "Studio A", "Contemporary", "Marco Wrenfield", "", domain.LevelIntermediate, 18,
		[]string{"Kai Lanternby", "Lina Mistlewood", "Marta Fernwhistle"},
	},
	{
		time.Tuesday, at(19, 0), 60, "Hall 2", "Barre Fitness", "Nina Tapworth", "", domain.LevelOpen, 12,
		[]string{"Clara Amberly", "Irene Inkwell", "Vera Rushwick", "Hanna Birchmoor"},
	},

	// Wednesday
	{
		time.Wednesday, at(15, 30), 45, "Studio C", "Creative Dance", "Sofia Quillon", "", domain.LevelOpen, 10,
		[]string{"Mia Birchmoor"},
	},
	{
		time.Wednesday, at(16, 30), 75, "Studio A", "Classical Ballet", "Lea Moonfield", "Greta Ivorylane", domain.LevelIntermediate, 16,
		[]string{"Ida Elderwick", "Kira Gildenmoor", "Lotta Birchmoor"},
	},
	{
		time.Wednesday, at(18, 0), 45, "Studio B", "Pointe Work", "Lea Moonfield", "Greta Ivorylane", domain.LevelAdvanced, 12,
		[]string{"Ida Elderwick", "Jona Fernwhistle", "Sophie Hollowmere"},
	},
	{
		time.Wednesday, at(18, 30), 60, "Hall 1", "Hip-Hop", "Jonas Featherby", "", domain.LevelIntermediate, 20,
		[]string{"Noah Cloudsley", "Ben Gildenmoor"},
	},
	{
		time.Wednesday, at(19, 0), 90, "Studio A", "Classical Ballet", "Lea Moonfield", "Greta Ivorylane", domain.LevelProfessional, 16,
		[]string{"Wanda Sparrowhill"},
	},

	// Thursday
	{
		time.Thursday, at(16, 0), 60, "Hall 2", "Tap", "Nina Tapworth", "", domain.LevelBeginner, 12,
		[]string{"Leni Dunfield", "Jakob Ivorycombe"},
	},
	{
		time.Thursday, at(17, 0), 60, "Studio A", "Contemporary", "Marco Wrenfield", "", domain.LevelBeginner, 18,
		[]string{"Lotta Birchmoor", "Ida Elderwick"},
	},
	{
		time.Thursday, at(18, 30), 90, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", domain.LevelBeginner, 16,
		[]string{"Oskar Nettlebury", "Tilo Quarrystone", "Sara Dunfield", "Petra Elderwick"},
	},
	{
		time.Thursday, at(19, 30), 60, "Hall 1", "Jazz Dance", "Marco Wrenfield", "", domain.LevelIntermediate, 20,
		[]string{"Lukas Fernwhistle", "Kai Lanternby"},
	},

	// Friday
	{
		time.Friday, at(15, 30), 45, "Studio B", "Pre-Ballet", "Sofia Quillon", "", domain.LevelBeginner, 12,
		[]string{"Finn Amberly"},
	},
	{
		time.Friday, at(16, 30), 60, "Studio A", "Classical Ballet", "Lea Moonfield", "Greta Ivorylane", domain.LevelBeginner, 16,
		[]string{"Leni Dunfield", "Kira Gildenmoor"},
	},
	{
		time.Friday, at(17, 45), 60, "Hall 1", "Hip-Hop", "Jonas Featherby", "", domain.LevelOpen, 24,
		[]string{"Jan Cloudsley", "Tilo Quarrystone"},
	},
	{
		time.Friday, at(18, 0), 75, "Studio B", "Pointe Work", "Lea Moonfield", "", domain.LevelAdvanced, 12,
		[]string{"Sophie Hollowmere"},
	},

	// Saturday
	{time.Saturday, at(10, 0), 45, "Studio C", "Creative Dance", "Sofia Quillon", "", domain.LevelOpen, 10, nil},
	{
		time.Saturday, at(10, 0), 90, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", domain.LevelAdvanced, 16,
		[]string{"Jona Fernwhistle", "Sophie Hollowmere", "Ida Elderwick"},
	},
	{
		time.Saturday, at(11, 0), 60, "Hall 2", "Tap", "Nina Tapworth", "", domain.LevelOpen, 12,
		[]string{"Irene Inkwell", "Nora Ivorycombe", "David Hollowmere"},
	},
	{
		time.Saturday, at(12, 0), 75, "Studio A", "Contemporary", "Marco Wrenfield", "", domain.LevelOpen, 18,
		[]string{"Lina Mistlewood", "Yara Gildenmoor"},
	},
}

// workshopSpec is a one-off public workshop; days counts from the anchor.
type workshopSpec struct {
	days        int
	start       domain.TimeOfDay
	minutes     int
	room        string
	courseType  string
	instructor  string
	accompanist string
	capacity    int
	// visibility defaults to public.
	visibility domain.LessonVisibility
}

// demoWorkshops start at 13:30 and end at 15:30, when no weekly class runs,
// so they never clash with one on whatever weekday they fall. The demo
// releases one week ahead (DemoSettings), so the first workshop is usually
// released already and the others lie beyond every release window: the
// schedule shows them because workshops are published early. Two more show
// the other visibilities: one only signed-in members see, and one only
// staff see.
var demoWorkshops = []workshopSpec{
	{3, at(13, 30), 120, "Studio A", "Contemporary", "Marco Wrenfield", "", 18, ""},
	{17, at(13, 30), 120, "Hall 1", "Jazz Dance", "Nina Tapworth", "", 20, ""},
	{24, at(13, 30), 120, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", 16, ""},
	{5, at(13, 30), 120, "Studio B", "Contemporary", "Marco Wrenfield", "", 12, domain.VisibilityMembers},
	{6, at(13, 30), 120, "Hall 1", "Hip-Hop", "Nina Tapworth", "", 15, domain.VisibilityHidden},
}

// ----------------------------------------------------------------- builder

// builder assigns IDs in table order and resolves the names used in the
// tables. An unknown name is a bug in the tables and panics; the tests call
// Demo, so it cannot reach a running server.
type builder struct {
	loc         *time.Location // school time zone
	termStart   domain.Date
	seq         *ids.Sequence
	d           Dataset
	persons     map[string]domain.PersonID
	rooms       map[string]domain.RoomID
	staff       map[string]domain.StaffID
	courseTypes map[string]domain.CourseTypeID
}

func (b *builder) household(h householdSpec) {
	var guardians []domain.PersonID

	for _, a := range h.adults {
		p := domain.Person{GivenName: a.given, FamilyName: h.family, Phone: a.phone}
		if a.role != "" {
			p.Email = strings.ToLower(a.given + "." + h.family + "@" + h.host)
		}

		p = b.person(p)
		guardians = append(guardians, p.ID)

		if a.role != "" {
			// Demo accounts are confirmed when the term starts and sign in
			// with DemoPassword.
			confirmed := b.termStart.At(domain.TimeOfDay{Hour: 9}, b.loc)
			acc := domain.Account{
				ID:          ids.New[domain.AccountID](b.seq),
				PersonID:    p.ID,
				Email:       p.Email,
				Role:        a.role,
				ConfirmedAt: confirmed,
			}
			b.d.Accounts = append(b.d.Accounts, acc)
			b.d.Credentials = append(b.d.Credentials, domain.Credential{
				AccountID: acc.ID, PasswordHash: demoPasswordHash, ChangedAt: confirmed,
			})
		}
	}

	for _, c := range h.children {
		child := b.person(domain.Person{GivenName: c.given, FamilyName: h.family, BirthDate: c.born})

		for _, g := range guardians {
			b.d.Guardians = append(b.d.Guardians, domain.Guardian{GuardianID: g, DependentID: child.ID})
		}
	}
}

func (b *builder) person(p domain.Person) domain.Person {
	p.ID = ids.New[domain.PersonID](b.seq)
	b.d.Persons = append(b.d.Persons, p)
	b.persons[p.GivenName+" "+p.FamilyName] = p.ID

	return p
}

func (b *builder) location(spec locationSpec) {
	l := domain.Location{ID: ids.New[domain.LocationID](b.seq), Name: spec.name, Address: spec.address}
	b.d.Locations = append(b.d.Locations, l)

	for _, r := range spec.rooms {
		room := domain.Room{ID: ids.New[domain.RoomID](b.seq), LocationID: l.ID, Name: r.name, Capacity: r.capacity}
		b.d.Rooms = append(b.d.Rooms, room)
		b.rooms[r.name] = room.ID
	}
}

func (b *builder) staffMember(spec staffSpec) {
	s := domain.Staff{
		ID:          ids.New[domain.StaffID](b.seq),
		DisplayName: spec.name,
		Kind:        spec.kind,
		Active:      spec.active,
	}
	if spec.person != "" {
		s.PersonID = lookup(b.persons, spec.person)
	}

	b.d.Staff = append(b.d.Staff, s)
	b.staff[spec.name] = s.ID
}

func (b *builder) courseType(c domain.CourseType) {
	c.ID = ids.New[domain.CourseTypeID](b.seq)
	b.d.CourseTypes = append(b.d.CourseTypes, c)
	b.courseTypes[c.Name] = c.ID
}

func (b *builder) class(term domain.TermID, spec classSpec) {
	c := domain.Class{
		ID:           ids.New[domain.ClassID](b.seq),
		TermID:       term,
		CourseTypeID: lookup(b.courseTypes, spec.courseType),
		RoomID:       lookup(b.rooms, spec.room),
		InstructorID: lookup(b.staff, spec.instructor),
		Level:        spec.level,
		Weekday:      spec.day,
		StartTime:    spec.start,
		Duration:     time.Duration(spec.minutes) * time.Minute,
		Capacity:     spec.capacity,
	}
	if spec.accompanist != "" {
		c.AccompanistID = lookup(b.staff, spec.accompanist)
	}

	b.d.Classes = append(b.d.Classes, c)

	for _, name := range spec.members {
		b.d.ClassMembers = append(b.d.ClassMembers, domain.ClassMember{
			ClassID:  c.ID,
			PersonID: lookup(b.persons, name),
			Since:    b.termStart,
		})
	}
}

// history adds the lessons of the weekly classes from a week before anchor
// up to and including anchor, as the lesson generator would have created
// them, so the demo has a recent past. The term starts three weeks before
// anchor, so the whole week lies in it.
func (b *builder) history(anchor domain.Date) {
	for d := anchor.AddDays(-7); d.Compare(anchor) <= 0; d = d.AddDays(1) {
		for _, c := range b.d.Classes {
			if c.Weekday != d.Weekday() {
				continue
			}

			start := d.At(c.StartTime, b.loc)
			b.d.Lessons = append(b.d.Lessons, domain.Lesson{
				ID:            ids.New[domain.LessonID](b.seq),
				ClassID:       c.ID,
				SlotDate:      d,
				CourseTypeID:  c.CourseTypeID,
				RoomID:        c.RoomID,
				InstructorID:  c.InstructorID,
				AccompanistID: c.AccompanistID,
				StartsAt:      start,
				EndsAt:        start.Add(c.Duration),
				Status:        domain.LessonScheduled,
				Visibility:    domain.VisibilityPublic,
				Kind:          domain.LessonRegular,
				Capacity:      c.Capacity,
			})
		}
	}
}

func (b *builder) workshop(anchor domain.Date, spec workshopSpec) {
	start := anchor.AddDays(spec.days).At(spec.start, b.loc)

	l := domain.Lesson{
		ID:           ids.New[domain.LessonID](b.seq),
		CourseTypeID: lookup(b.courseTypes, spec.courseType),
		RoomID:       lookup(b.rooms, spec.room),
		InstructorID: lookup(b.staff, spec.instructor),
		StartsAt:     start,
		EndsAt:       start.Add(time.Duration(spec.minutes) * time.Minute),
		Status:       domain.LessonScheduled,
		Visibility:   cmp.Or(spec.visibility, domain.VisibilityPublic),
		Kind:         domain.LessonWorkshop,
		Capacity:     spec.capacity,
	}
	if spec.accompanist != "" {
		l.AccompanistID = lookup(b.staff, spec.accompanist)
	}

	b.d.Lessons = append(b.d.Lessons, l)
}

func lookup[I domain.AnyID](m map[string]I, name string) I {
	id, ok := m[name]
	if !ok {
		panic("seed: unknown name " + name)
	}

	return id
}

func date(year int, month time.Month, day int) domain.Date {
	return domain.Date{Year: year, Month: month, Day: day}
}

func at(hour, minute int) domain.TimeOfDay { return domain.TimeOfDay{Hour: hour, Minute: minute} }
