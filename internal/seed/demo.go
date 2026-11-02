package seed

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/receiptfmt"
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
// anchor: two locations, their rooms, staff, course types, the level scale,
// one term around anchor with its weekly classes, members with their
// families and accounts, and a few one-off workshops in the weeks after
// anchor, one of them full with a waiting list (ADR 0010), and one with
// guests and a trial that its instructor added (ADR 0009, amendment). Of
// the weekly classes it has the lessons from the term start up to and including
// anchor (history), with the automatic bookings of the class members, as
// the lesson generator would have created them, their attendance and a few
// guests and trials, so the statistics have weeks to count; the generator
// creates the later ones, but never the past (ADR 0009). IDs come from a
// fresh ids.Sequence, so every call with the same anchor returns identical
// data.
//
// DemoAt panics if the demo time zone cannot be loaded (no zoneinfo
// database); LoadDemoAt reports that as an error instead.
func DemoAt(anchor domain.Date) Dataset {
	d, err := demoAt(anchor, programme())
	if err != nil {
		panic(err)
	}

	return d
}

// DemoSchoolAt is DemoAt for the demo school school, built from its own
// tables (School), with its settings and background, and its receipts
// issued with those settings (issuer, language, number and line text). An
// unknown school fails with domain.ErrValidation.
func DemoSchoolAt(anchor domain.Date, school School) (Dataset, error) {
	s, err := schoolOf(school)
	if err != nil {
		return Dataset{}, err
	}

	if !s.background.IsZero() {
		if err := s.background.Validate(); err != nil {
			return Dataset{}, fmt.Errorf("seed: background of %s: %w", school, err)
		}
	}

	return demoAt(anchor, s)
}

// demoAt builds the demo data for anchor from the school's tables s, with
// its settings, which the receipts are issued with, and its background.
func demoAt(anchor domain.Date, s schoolSpec) (Dataset, error) {
	// The workshops start at wall-clock times of the school.
	loc, err := time.LoadLocation(s.settings.Timezone)
	if err != nil {
		return Dataset{}, fmt.Errorf("seed: demo time zone %q: %w", s.settings.Timezone, err)
	}

	termStart := anchor.AddDays(-termLeadDays)

	b := builder{
		s:           s,
		loc:         loc,
		termStart:   termStart,
		seq:         ids.NewSequence(),
		d:           Dataset{Settings: s.settings, Background: s.background},
		persons:     map[string]domain.PersonID{},
		rooms:       map[string]domain.RoomID{},
		staff:       map[string]domain.StaffID{},
		courseTypes: map[string]domain.CourseTypeID{},
		levels:      map[string]domain.LevelID{},
	}

	for _, h := range s.households {
		b.household(h)
	}

	for _, l := range s.locations {
		b.location(l)
	}

	// A school with fewer places than the programme's skips the IDs they
	// would take, so that every later entity has the same ID in both. The
	// padding keeps the werkstatt school's IDs aligned with the programme's
	// until it has tables of its own (task 5.1).
	for range placeIDs(programme().locations) - placeIDs(s.locations) {
		b.seq.NewUUID()
	}

	for _, st := range s.staff {
		b.staffMember(st)
	}

	for _, c := range s.courseTypes {
		b.courseType(c)
	}

	for _, l := range s.levels {
		b.level(l)
	}

	termEnd := termStart.AddDays(termDays - 1)
	term := domain.Term{
		ID:       ids.New[domain.TermID](b.seq),
		Name:     s.termName(termStart, termEnd),
		StartsOn: termStart,
		EndsOn:   termEnd,
	}
	b.d.Terms = append(b.d.Terms, term)

	for _, c := range s.classes {
		b.class(term.ID, c)
	}

	for _, w := range s.workshops {
		b.workshop(anchor, w)
	}

	b.history(anchor)
	b.memberBookings()
	b.fullWorkshop(anchor, s.fullWorkshop)
	b.staffBookings(anchor, s.staffBookings)
	b.passes(anchor)
	b.attendance(anchor)
	b.visitors(anchor)
	b.announcements(anchor)
	b.duplicates()
	// Last, so every other entity keeps its ID.
	b.rental(anchor, s.rental)

	return b.d, nil
}

// ------------------------------------------------------------------ tables

// schoolSpec is everything a demo school is built from: its settings and
// background, and the tables of its people, places, timetable and history.
// The names in the tables are keys that the other tables refer to; a
// person's key is "Given Family".
type schoolSpec struct {
	// settings are the school's settings; its receipts are issued with them.
	settings domain.Settings
	// background is the school's background image, zero for none.
	background domain.Background
	// households are the families, single adults and staff members' person
	// records, with their accounts.
	households []householdSpec
	// locations are the school's places and their rooms.
	locations []locationSpec
	// staff are the instructors and accompanists.
	staff []staffSpec
	// levels is the level scale, from least to most advanced.
	levels []levelSpec
	// courseTypes are the course types, by name.
	courseTypes []domain.CourseType
	// classes are the weekly classes of the term.
	classes []classSpec
	// workshops are the one-off workshops after the anchor.
	workshops []workshopSpec
	// fullWorkshop is the workshop booked up, with a waiting list.
	fullWorkshop fullWorkshopSpec
	// staffBookings are the participants an instructor added to her first
	// workshop.
	staffBookings staffBookingsSpec
	// rental is the room rented out to an outside renter.
	rental rentalSpec
	// passTypes is the pass catalog.
	passTypes []domain.PassType
	// passes are the passes sold and given away.
	passes []passSpec
	// announcements are the office's announcements.
	announcements []announcementSpec
	// duplicates are the persons that the duplicate detection finds in pairs.
	duplicates []domain.Person
	// office is the person key of the office worker whose account sells the
	// passes, writes the announcements and records attendance for
	// instructors without a login.
	office string
	// attendanceSkipped is the course type whose lessons have no attendance
	// taken yet.
	attendanceSkipped string
	// guests are the walk-in guests the instructors added to past lessons,
	// in turn.
	guests []string
	// trialists are the person keys of those who tried a class they are no
	// member of, in turn.
	trialists []string
	// trialNotes are the notes of the trials, in turn; empty for none.
	trialNotes []string
	// termName names the term, e.g. "School Year 2026/27".
	termName func(start, end domain.Date) string
}

// adultSpec is an adult of a household. A zero role means no account.
type adultSpec struct {
	given string
	role  domain.Role
	phone string
}

// childSpec is a child of a household; the household's adults are its
// guardians. A teenager with a role has an account of their own, which
// makes them a managed dependent (ADR 0008).
type childSpec struct {
	given string
	born  domain.Date
	role  domain.Role
}

// householdSpec is a family, a single adult or a staff member's person
// record. Adults with an account get the email given.family@host.
type householdSpec struct {
	family   string
	host     string
	adults   []adultSpec
	children []childSpec
}

type locationSpec struct {
	name    string
	address string
	rooms   []roomSpec
}

// roomSpec is a room; holds names the programme rooms whose classes it
// holds in another school's places, none for a programme room itself.
type roomSpec struct {
	name     string
	capacity int
	holds    []string
}

// placeIDs is the number of IDs the places of locations take.
func placeIDs(locations []locationSpec) int {
	n := len(locations)
	for _, l := range locations {
		n += len(l.rooms)
	}

	return n
}

// staffSpec is a staff member; person is the "Given Family" key of their
// person record, or empty for staff without one.
type staffSpec struct {
	name   string
	person string
	kind   domain.StaffKind
	active bool
}

// levelSpec is a step on the school's level scale; the table lists them
// from least to most advanced, so a level's position is its place in it.
type levelSpec struct{ name, code string }

// audienceSpec is who a class is for: from and to name levels of the
// school's level scale (both or neither), all is for every level; ages are in months.
type audienceSpec struct {
	from, to       string
	all            bool
	stage          int
	ageFrom, ageTo domain.AgeMonths
}

// level is a class for one level of the scale.
func level(name string) audienceSpec { return audienceSpec{from: name, to: name} }

// allLevels is a class for every level.
var allLevels = audienceSpec{all: true}

// ages is a class for children of an age, in months, instead of a level.
func ages(from, to domain.AgeMonths) audienceSpec { return audienceSpec{ageFrom: from, ageTo: to} }

// classSpec is a weekly class. Names refer to the school's other tables;
// members are "Given Family" keys of persons.
type classSpec struct {
	day         time.Weekday
	start       domain.TimeOfDay
	minutes     int
	room        string
	courseType  string
	instructor  string
	accompanist string
	audience    audienceSpec
	capacity    int
	members     []string
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

// rentalSpec is a room rental (ADR 0007, amendment): a room blocked for an
// outside renter, without a course type; days counts from the anchor.
type rentalSpec struct {
	days    int
	start   domain.TimeOfDay
	minutes int
	room    string
	renter  string
}

func (b *builder) rental(anchor domain.Date, spec rentalSpec) {
	start := anchor.AddDays(spec.days).At(spec.start, b.loc)

	b.d.Lessons = append(b.d.Lessons, domain.Lesson{
		ID:         ids.New[domain.LessonID](b.seq),
		RoomID:     lookup(b.rooms, spec.room),
		StartsAt:   start,
		EndsAt:     start.Add(time.Duration(spec.minutes) * time.Minute),
		Status:     domain.LessonScheduled,
		Visibility: domain.VisibilityHidden,
		Kind:       domain.LessonRental,
		Renter:     spec.renter,
	})
}

// fullWorkshopSpec is a small public workshop that members have booked up,
// with persons on its waiting list in this order.
type fullWorkshopSpec struct {
	workshop workshopSpec
	booked   []string
	waiting  []waitingSpec
}

type waitingSpec struct {
	person     string
	stayOnList bool
}

// staffBookingsSpec is what an instructor added to her first workshop: a
// booking per participant, by her account, the evening before the anchor.
type staffBookingsSpec struct {
	instructor   string
	participants []participantSpec
}

// participantSpec is a guest (name, optional email) or a person, optionally
// on a trial with a note.
type participantSpec struct {
	guest, email string
	person       string
	trial        bool
	note         string
}

// ----------------------------------------------------------------- builder

// builder assigns IDs in table order and resolves the names used in the
// tables. An unknown name is a bug in the tables and panics; the tests call
// Demo, so it cannot reach a running server.
type builder struct {
	s           schoolSpec     // the school's tables
	loc         *time.Location // school time zone
	termStart   domain.Date
	seq         *ids.Sequence
	d           Dataset
	persons     map[string]domain.PersonID
	rooms       map[string]domain.RoomID
	staff       map[string]domain.StaffID
	courseTypes map[string]domain.CourseTypeID
	levels      map[string]domain.LevelID
}

func (b *builder) household(h householdSpec) {
	var guardians []domain.PersonID

	for _, a := range h.adults {
		p := b.person(domain.Person{GivenName: a.given, FamilyName: h.family, Phone: a.phone}, h.host, a.role)
		guardians = append(guardians, p.ID)
	}

	for _, c := range h.children {
		child := b.person(domain.Person{GivenName: c.given, FamilyName: h.family, BirthDate: c.born}, h.host, c.role)

		for _, g := range guardians {
			b.d.Guardians = append(b.d.Guardians, domain.Guardian{GuardianID: g, DependentID: child.ID})
		}
	}
}

// person adds p with an ID. With a role, p gets the email
// given.family@host and an account with that role, confirmed when the term
// starts, that signs in with DemoPassword.
func (b *builder) person(p domain.Person, host string, role domain.Role) domain.Person {
	if role != "" {
		p.Email = strings.ToLower(p.GivenName + "." + p.FamilyName + "@" + host)
	}

	p.ID = ids.New[domain.PersonID](b.seq)
	b.d.Persons = append(b.d.Persons, p)
	b.persons[p.GivenName+" "+p.FamilyName] = p.ID

	if role == "" {
		return p
	}

	confirmed := b.termStart.At(domain.TimeOfDay{Hour: 9}, b.loc)
	acc := domain.Account{
		ID:          ids.New[domain.AccountID](b.seq),
		PersonID:    p.ID,
		Email:       p.Email,
		Role:        role,
		ConfirmedAt: confirmed,
	}
	b.d.Accounts = append(b.d.Accounts, acc)
	b.d.Credentials = append(b.d.Credentials, domain.Credential{
		AccountID: acc.ID, PasswordHash: demoPasswordHash, ChangedAt: confirmed,
	})

	return p
}

func (b *builder) location(spec locationSpec) {
	l := domain.Location{ID: ids.New[domain.LocationID](b.seq), Name: spec.name, Address: spec.address}
	b.d.Locations = append(b.d.Locations, l)

	for i, r := range spec.rooms {
		room := domain.Room{
			ID: ids.New[domain.RoomID](b.seq), LocationID: l.ID, Name: r.name, Capacity: r.capacity, Position: i + 1,
		}
		b.d.Rooms = append(b.d.Rooms, room)

		if len(r.holds) == 0 {
			b.rooms[r.name] = room.ID
		}

		for _, key := range r.holds {
			b.rooms[key] = room.ID
		}
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

// level adds the next step of the level scale, after the ones before it.
func (b *builder) level(spec levelSpec) {
	l := domain.Level{
		ID: ids.New[domain.LevelID](b.seq), Name: spec.name, Code: spec.code, Position: len(b.d.Levels) + 1,
	}
	b.d.Levels = append(b.d.Levels, l)
	b.levels[l.Name] = l.ID
}

// levelRange resolves the level names of a class's audience.
func (b *builder) levelRange(a audienceSpec) domain.LevelRange {
	if a.from == "" && a.to == "" {
		return domain.LevelRange{}
	}

	return domain.LevelRange{From: lookup(b.levels, a.from), To: lookup(b.levels, a.to)}
}

func (b *builder) class(term domain.TermID, spec classSpec) {
	c := domain.Class{
		ID:           ids.New[domain.ClassID](b.seq),
		TermID:       term,
		CourseTypeID: lookup(b.courseTypes, spec.courseType),
		RoomID:       lookup(b.rooms, spec.room),
		InstructorID: lookup(b.staff, spec.instructor),
		Levels:       b.levelRange(spec.audience),
		AllLevels:    spec.audience.all,
		Stage:        spec.audience.stage,
		AgeFrom:      spec.audience.ageFrom,
		AgeTo:        spec.audience.ageTo,
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

// history adds the lessons of the weekly classes from the term start up to
// and including anchor, as the lesson generator would have created them,
// so the demo has a past: three weeks and a day.
func (b *builder) history(anchor domain.Date) {
	for d := b.termStart; d.Compare(anchor) <= 0; d = d.AddDays(1) {
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

// memberBookings books the class members into the lessons of their
// classes from their Since date on, as generation does: automatic member
// bookings without an actor, made when the term starts (ADR 0009).
func (b *builder) memberBookings() {
	bookedAt := b.termStart.At(domain.TimeOfDay{Hour: 9}, b.loc)

	for _, l := range b.d.Lessons {
		if l.ClassID.IsZero() {
			continue
		}

		for _, m := range b.d.ClassMembers {
			if m.ClassID != l.ClassID || !m.ActiveOn(l.SlotDate) {
				continue
			}

			booking := domain.Booking{
				ID: ids.New[domain.BookingID](b.seq), LessonID: l.ID, PersonID: m.PersonID,
				Status: domain.BookingBooked, Source: domain.BookingSourceMember, BookedAt: bookedAt,
			}
			b.d.Bookings = append(b.d.Bookings, booking)
			b.d.BookingEvents = append(b.d.BookingEvents, domain.BookingEvent{
				ID: ids.New[domain.BookingEventID](b.seq), BookingID: booking.ID, At: bookedAt,
				Status: domain.BookingBooked,
			})
		}
	}
}

// attendance takes the attendance of the automatic member bookings of the
// weekly classes before anchor, as their instructors (or the office, for
// an instructor without a login) would have, except the attendanceSkipped
// course type's, so the instructor and office views show lessons to take
// attendance of: most attended, every seventh did not show, and every
// eleventh canceled late, which the office recorded. So the statistics
// have attendance, no-shows and late cancellations to count.
func (b *builder) attendance(anchor domain.Date) {
	office := b.accountOf(lookup(b.persons, b.s.office))
	skipped := lookup(b.courseTypes, b.s.attendanceSkipped)
	lessons := map[domain.LessonID]domain.Lesson{}

	for _, l := range b.d.Lessons {
		if !l.ClassID.IsZero() && l.SlotDate.Compare(anchor) < 0 && l.CourseTypeID != skipped {
			lessons[l.ID] = l
		}
	}

	n := 0

	for i := range b.d.Bookings {
		booking := &b.d.Bookings[i]

		l, ok := lessons[booking.LessonID]
		if !ok || booking.Source != domain.BookingSourceMember || booking.Status != domain.BookingBooked {
			continue
		}

		event := domain.BookingEvent{ID: ids.New[domain.BookingEventID](b.seq), BookingID: booking.ID}

		switch {
		case n%11 == 5:
			booking.Status, booking.CanceledAt, booking.Late = domain.BookingCanceled, l.StartsAt.Add(-2*time.Hour), true
			event.At, event.Status, event.Late, event.ActorAccountID = booking.CanceledAt, domain.BookingCanceled, true, office
		case n%7 == 3:
			booking.Status = domain.BookingNoShow
			event.At, event.Status, event.ActorAccountID = l.EndsAt, domain.BookingNoShow, b.staffAccount(l.InstructorID, office)
		default:
			booking.Status = domain.BookingAttended
			event.At, event.Status, event.ActorAccountID = l.EndsAt, domain.BookingAttended, b.staffAccount(l.InstructorID, office)
		}

		b.d.BookingEvents = append(b.d.BookingEvents, event)
		n++
	}
}

// staffAccount returns the account of a staff member's person, or
// fallback for a staff member without a login.
func (b *builder) staffAccount(id domain.StaffID, fallback domain.AccountID) domain.AccountID {
	i := slices.IndexFunc(b.d.Staff, func(s domain.Staff) bool { return s.ID == id })
	if i < 0 || b.d.Staff[i].PersonID.IsZero() {
		return fallback
	}

	for _, a := range b.d.Accounts {
		if a.PersonID == b.d.Staff[i].PersonID {
			return a.ID
		}
	}

	return fallback
}

// visitors adds the school's guests and trialists to the weekly classes'
// lessons before anchor, but the attendanceSkipped course type's: in every
// fourth a guest, in every fifth a person on a trial, whom the instructor
// (or the office, for an instructor without a login) added just before the
// start (source staff, ADR 0009, amendment) and marked attended at its
// end, every third guest as a no-show. Trials are free; the guests paid at
// the desk.
func (b *builder) visitors(anchor domain.Date) {
	office := b.accountOf(lookup(b.persons, b.s.office))
	skipped := lookup(b.courseTypes, b.s.attendanceSkipped)
	guests, trials := 0, 0
	i := 0

	for _, l := range b.d.Lessons {
		if l.ClassID.IsZero() || l.SlotDate.Compare(anchor) >= 0 || l.CourseTypeID == skipped {
			continue
		}

		actor := b.staffAccount(l.InstructorID, office)

		if i%4 == 1 {
			status := domain.BookingAttended
			if guests%3 == 2 {
				status = domain.BookingNoShow
			}

			b.visit(l, domain.Booking{GuestName: b.s.guests[guests%len(b.s.guests)], Status: status}, actor)
			guests++
		}

		if i%5 == 3 {
			for k := range b.s.trialists {
				person := lookup(b.persons, b.s.trialists[(trials+k)%len(b.s.trialists)])
				if b.memberOf(l.ClassID, person) {
					continue
				}

				b.visit(l, domain.Booking{
					PersonID: person, Status: domain.BookingAttended, Trial: true,
					TrialNote: b.s.trialNotes[trials%len(b.s.trialNotes)],
				}, actor)
				trials++

				break
			}
		}

		i++
	}
}

// visit adds booking v to the past lesson l, booked by actor ten minutes
// before the start, with its attendance at the end.
func (b *builder) visit(l domain.Lesson, v domain.Booking, actor domain.AccountID) {
	v.ID, v.LessonID, v.Source, v.BookedAt = ids.New[domain.BookingID](b.seq), l.ID, domain.BookingSourceStaff,
		l.StartsAt.Add(-10*time.Minute)
	b.d.Bookings = append(b.d.Bookings, v)
	b.d.BookingEvents = append(b.d.BookingEvents,
		domain.BookingEvent{
			ID: ids.New[domain.BookingEventID](b.seq), BookingID: v.ID, At: v.BookedAt,
			Status: domain.BookingBooked, ActorAccountID: actor,
		},
		domain.BookingEvent{
			ID: ids.New[domain.BookingEventID](b.seq), BookingID: v.ID, At: l.EndsAt,
			Status: v.Status, ActorAccountID: actor,
		},
	)
}

// memberOf reports whether person is a member of the class.
func (b *builder) memberOf(class domain.ClassID, person domain.PersonID) bool {
	return slices.ContainsFunc(b.d.ClassMembers, func(m domain.ClassMember) bool {
		return m.ClassID == class && m.PersonID == person
	})
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

// fullWorkshop adds spec's workshop, the members' own bookings of all its
// places (each with its event, by the member's account) and its waiting
// list.
func (b *builder) fullWorkshop(anchor domain.Date, spec fullWorkshopSpec) {
	if len(spec.booked) != spec.workshop.capacity {
		panic("seed: the full workshop must be booked up")
	}

	b.workshop(anchor, spec.workshop)
	l := b.d.Lessons[len(b.d.Lessons)-1]

	bookedAt := anchor.AddDays(-2).At(at(19, 0), b.loc)
	for i, name := range spec.booked {
		person := lookup(b.persons, name)
		at := bookedAt.Add(time.Duration(i) * time.Minute)
		booking := domain.Booking{
			ID: ids.New[domain.BookingID](b.seq), LessonID: l.ID, PersonID: person,
			Status: domain.BookingBooked, Source: domain.BookingSourceSelf, BookedAt: at,
		}
		b.d.Bookings = append(b.d.Bookings, booking)
		b.d.BookingEvents = append(b.d.BookingEvents, domain.BookingEvent{
			ID: ids.New[domain.BookingEventID](b.seq), BookingID: booking.ID, At: at,
			Status: domain.BookingBooked, ActorAccountID: b.accountOf(person),
		})
	}

	joinedAt := anchor.AddDays(-1).At(at(18, 0), b.loc)
	for i, w := range spec.waiting {
		person := lookup(b.persons, w.person)
		b.d.WaitlistEntries = append(b.d.WaitlistEntries, domain.WaitlistEntry{
			ID: ids.New[domain.WaitlistEntryID](b.seq), LessonID: l.ID, PersonID: person,
			Status: domain.WaitlistWaiting, JoinedAt: joinedAt.Add(time.Duration(i) * 30 * time.Minute),
			StayOnList: w.stayOnList, JoinedBy: b.accountOf(person),
		})
	}
}

// staffBookings adds the bookings of spec to the first workshop of its
// instructor, each with its event by the instructor's account (source
// staff, ADR 0009, amendment).
func (b *builder) staffBookings(anchor domain.Date, spec staffBookingsSpec) {
	staff := lookup(b.staff, spec.instructor)

	var person domain.PersonID

	for _, s := range b.d.Staff {
		if s.ID == staff {
			person = s.PersonID
		}
	}

	actor := b.accountOf(person)

	i := slices.IndexFunc(b.d.Lessons, func(l domain.Lesson) bool {
		return l.Kind == domain.LessonWorkshop && l.InstructorID == staff
	})
	if i < 0 {
		panic("seed: no workshop of " + spec.instructor)
	}

	l := b.d.Lessons[i]
	bookedAt := anchor.AddDays(-1).At(at(17, 0), b.loc)

	for i, p := range spec.participants {
		booking := domain.Booking{
			ID: ids.New[domain.BookingID](b.seq), LessonID: l.ID, GuestName: p.guest, GuestEmail: p.email,
			Status: domain.BookingBooked, Source: domain.BookingSourceStaff, Trial: p.trial, TrialNote: p.note,
			BookedAt: bookedAt.Add(time.Duration(i) * time.Minute),
		}
		if p.person != "" {
			booking.PersonID = lookup(b.persons, p.person)
		}

		b.d.Bookings = append(b.d.Bookings, booking)
		b.d.BookingEvents = append(b.d.BookingEvents, domain.BookingEvent{
			ID: ids.New[domain.BookingEventID](b.seq), BookingID: booking.ID, At: booking.BookedAt,
			Status: domain.BookingBooked, ActorAccountID: actor,
		})
	}
}

// passSpec is a pass of a person: sold (with an optional payer) or given
// away (assigned, with a note) on the day soldDays from the anchor, valid
// from that day, and perhaps voided a day later. dropIns are the classes
// (by weekday and start) whose lesson in the week before the anchor the
// person dropped into, attended and paid for with the pass.
type passSpec struct {
	person   string
	passType string
	soldDays int
	payer    string
	assigned bool
	note     string
	voided   bool
	dropIns  []classSlot
	// method is how a sale was paid, printed on its receipt.
	method domain.PaymentMethod
}

// classSlot names a weekly class by its weekday and start.
type classSlot struct {
	day   time.Weekday
	start domain.TimeOfDay
}

// passes adds the school's pass catalog, created when the term starts, and
// its passes, sold by the office, with their drop-in bookings
// (each with its booking and attendance events) and redemptions.
func (b *builder) passes(anchor domain.Date) {
	created := b.termStart.At(domain.TimeOfDay{Hour: 9}, b.loc)
	types := map[string]domain.PassType{}

	for _, pt := range b.s.passTypes {
		pt.ID, pt.CreatedAt = ids.New[domain.PassTypeID](b.seq), created
		b.d.PassTypes = append(b.d.PassTypes, pt)
		types[pt.Name] = pt
	}

	office := b.accountOf(lookup(b.persons, b.s.office))

	var issued []issue

	for _, spec := range b.s.passes {
		pt, ok := types[spec.passType]
		if !ok {
			panic("seed: unknown pass type " + spec.passType)
		}

		day := anchor.AddDays(spec.soldDays)
		p := domain.Pass{
			ID: ids.New[domain.PassID](b.seq), PassTypeID: pt.ID, PersonID: lookup(b.persons, spec.person), Kind: pt.Kind,
			ValidFrom: day, ValidUntil: pt.ValidUntil(day), Status: domain.PassActive, Origin: domain.PassSold,
			SoldAt: day.At(at(10, 0), b.loc), SoldBy: office, PricePaid: pt.Price, Note: spec.note,
		}

		if pt.Kind == domain.PassVisits {
			p.VisitsTotal, p.VisitsRemaining = pt.Visits, pt.Visits
		}

		if spec.payer != "" {
			p.PayerID = lookup(b.persons, spec.payer)
		}

		if spec.assigned {
			p.Origin, p.PricePaid = domain.PassAssigned, 0
		}

		if spec.voided {
			// Its sale is canceled the next day, which voids it.
			p.Status, p.VoidedAt = domain.PassVoided, p.SoldAt.Add(24*time.Hour)
		}

		for _, slot := range spec.dropIns {
			b.dropIn(anchor, &p, slot, office)
		}

		b.d.Passes = append(b.d.Passes, p)

		if !spec.assigned {
			issued = append(issued, issue{at: p.SoldAt, pass: len(b.d.Passes) - 1, typ: pt, method: spec.method})
			if spec.voided {
				issued = append(issued, issue{
					at: p.VoidedAt, pass: len(b.d.Passes) - 1, cancel: true, reason: strings.TrimSuffix(spec.note, "."),
				})
			}
		}
	}

	b.receipts(issued, office)
}

// issue is a receipt the seed issues: the sale of a pass, or its
// cancellation.
type issue struct {
	at     time.Time
	pass   int // index in b.d.Passes
	typ    domain.PassType
	method domain.PaymentMethod
	cancel bool
	reason string
}

// receipts issues the receipts of the sales, and the cancellations, in the
// order they happened, numbered gaplessly per year like ReceiptService:
// mailed to nobody, and without PDFs, which are rendered when first read
// (rendering needs the typst module, which the web demo loads only then).
func (b *builder) receipts(issued []issue, office domain.AccountID) {
	slices.SortStableFunc(issued, func(a, b issue) int { return a.at.Compare(b.at) })

	settings := b.d.Settings
	byPass := map[int]domain.Receipt{}

	for _, is := range issued {
		p := &b.d.Passes[is.pass]
		day := domain.DateOf(is.at, b.loc)
		seq := 1

		for _, r := range b.d.Receipts {
			if r.Year == day.Year {
				seq++
			}
		}

		payer := cmp.Or(p.PayerID, p.PersonID)
		r := domain.Receipt{
			ID: ids.New[domain.ReceiptID](b.seq), Number: domain.ReceiptNumber(settings.ReceiptPrefix, day.Year, seq),
			Year: day.Year, Sequence: seq, Kind: domain.ReceiptSale, IssuedAt: is.at, IssueDate: day,
			Locale: settings.DefaultLocale, Issuer: settings.Receipts.Issuer, TaxMode: settings.Receipts.TaxMode,
			TaxNote: settings.Receipts.TaxNote, Footer: settings.Receipts.Footer, PayerID: payer,
			RecipientName: b.name(payer), PaymentMethod: is.method, Currency: settings.Currency, CreatedBy: office,
		}

		if is.cancel {
			orig := byPass[is.pass]
			r.Kind, r.CancelsReceiptID, r.Reason, r.PaymentMethod = domain.ReceiptCancellation, orig.ID, is.reason, orig.PaymentMethod
			r.Lines = []domain.ReceiptLine{orig.Lines[0].Negated()}
		} else {
			desc := receiptfmt.PassLine(r.Locale, is.typ.Name, b.name(p.PersonID), p.ValidFrom, p.ValidUntil)
			r.Lines = []domain.ReceiptLine{domain.NewReceiptLine(1, desc, 1, p.PricePaid, is.typ.VATRate, r.TaxMode)}
			p.ReceiptID = r.ID
			byPass[is.pass] = r
		}

		r.Net, r.VAT, r.Gross = r.Lines[0].Net, r.Lines[0].VAT, r.Lines[0].Gross
		b.d.Receipts = append(b.d.Receipts, r)
	}
}

// name returns the full name of a person of the data set.
func (b *builder) name(id domain.PersonID) string {
	for _, p := range b.d.Persons {
		if p.ID == id {
			return p.GivenName + " " + p.FamilyName
		}
	}

	panic("seed: unknown person " + id.String())
}

// dropIn books the owner of pass p into the lesson of the class at slot in
// the week before the anchor, the evening before it (by the account of the
// owner, or the payer's for a child), marks it attended by the office at
// its end and lets p pay for it then.
func (b *builder) dropIn(anchor domain.Date, p *domain.Pass, slot classSlot, office domain.AccountID) {
	i := slices.IndexFunc(b.d.Lessons, func(l domain.Lesson) bool {
		if l.ClassID.IsZero() || l.SlotDate.Compare(anchor.AddDays(-7)) < 0 || l.SlotDate.Compare(anchor) >= 0 ||
			l.SlotDate.Weekday() != slot.day {
			return false
		}

		c := b.d.Classes[slices.IndexFunc(b.d.Classes, func(c domain.Class) bool { return c.ID == l.ClassID })]

		return c.StartTime == slot.start
	})
	if i < 0 {
		panic(fmt.Sprintf("seed: no lesson of the class on %v at %v in the week before the anchor", slot.day, slot.start))
	}

	l := b.d.Lessons[i]
	booker := p.PersonID

	if !p.PayerID.IsZero() {
		booker = p.PayerID
	}

	booking := domain.Booking{
		ID: ids.New[domain.BookingID](b.seq), LessonID: l.ID, PersonID: p.PersonID, Status: domain.BookingAttended,
		Source: domain.BookingSourceSelf, BookedAt: l.StartsAt.Add(-20 * time.Hour),
	}
	b.d.Bookings = append(b.d.Bookings, booking)
	b.d.BookingEvents = append(b.d.BookingEvents,
		domain.BookingEvent{
			ID: ids.New[domain.BookingEventID](b.seq), BookingID: booking.ID, At: booking.BookedAt,
			Status: domain.BookingBooked, ActorAccountID: b.accountOf(booker),
		},
		domain.BookingEvent{
			ID: ids.New[domain.BookingEventID](b.seq), BookingID: booking.ID, At: l.EndsAt,
			Status: domain.BookingAttended, ActorAccountID: office,
		},
	)
	b.d.Redemptions = append(b.d.Redemptions, domain.PassRedemption{PassID: p.ID, BookingID: booking.ID, At: l.EndsAt})

	if p.Kind == domain.PassVisits {
		p.VisitsRemaining--
	}
}

// accountOf returns the account of a person with a login.
func (b *builder) accountOf(person domain.PersonID) domain.AccountID {
	for _, a := range b.d.Accounts {
		if a.PersonID == person {
			return a.ID
		}
	}

	panic("seed: no account for person " + person.String())
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
