package seed_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// programmeDigest is the SHA-256 of the programme school's dataset for
// FixedDemoAnchor, JSON-encoded. A deliberate change to the programme data
// updates it: the test prints the new digest when it fails.
const programmeDigest = "9d702f81c06732ac2bdf25322460fac9bdc91502d04a5a6f2f7336cf0b143155"

// TestProgrammeDatasetUnchanged pins every field of the programme school's
// dataset, so that building the demo schools from their own tables leaves
// the programme school's data exactly as it was.
func TestProgrammeDatasetUnchanged(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(seed.DemoAt(seed.FixedDemoAnchor))
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != programmeDigest {
		t.Fatalf("the programme dataset changed: digest %s, want %s", got, programmeDigest)
	}
}

// forEachSchool runs check as a parallel subtest, named after the school,
// on every demo school's dataset for FixedDemoAnchor.
func forEachSchool(t *testing.T, check func(t *testing.T, school seed.School, d seed.Dataset)) {
	t.Helper()

	for _, school := range seed.Schools() {
		t.Run(string(school), func(t *testing.T) {
			t.Parallel()

			d, err := seed.DemoSchoolAt(seed.FixedDemoAnchor, school)
			if err != nil {
				t.Fatalf("%s: %v", school, err)
			}

			check(t, school, d)
		})
	}
}

// schoolLocation returns the time zone of school's settings.
func schoolLocation(t *testing.T, school seed.School, d seed.Dataset) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(d.Settings.Timezone)
	if err != nil {
		t.Fatalf("%s: time zone %q: %v", school, d.Settings.Timezone, err)
	}

	return loc
}

// TestDemoSchoolProgrammeIsDemoAt: the programme school is DemoAt's data,
// for every anchor; the tests of DemoAt hold for it.
func TestDemoSchoolProgrammeIsDemoAt(t *testing.T) {
	t.Parallel()

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d, err := seed.DemoSchoolAt(anchor, seed.SchoolProgramme)
		if err != nil {
			t.Fatalf("%v: %v", anchor, err)
		}

		if !reflect.DeepEqual(d, seed.DemoAt(anchor)) {
			t.Errorf("%v: the programme school differs from DemoAt", anchor)
		}
	}
}

// receiptLineWords are the words of a pass's receipt line in each demo
// school's language: "<pass> for <person>, valid <from> – <to>".
var receiptLineWords = map[string][]string{
	"en": {" for ", ", valid "},
	"de": {" für ", ", gültig "},
}

// TestDemoReceiptsIssuedWithSchoolSettings: for every anchor, each demo
// school's receipts are issued with its own settings: its language (the
// line text too), issuer, footer, tax note and mode, currency and number
// prefix, numbered from 1 per year. The werkstatt school's are German, by
// its own issuer (TestWerkstattOwnIdentity, TestLoadDemoWerkstatt).
func TestDemoReceiptsIssuedWithSchoolSettings(t *testing.T) {
	t.Parallel()

	for _, school := range seed.Schools() {
		t.Run(string(school), func(t *testing.T) {
			t.Parallel()

			for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
				d, err := seed.DemoSchoolAt(anchor, school)
				if err != nil {
					t.Fatalf("%s %v: %v", school, anchor, err)
				}

				s := d.Settings

				words, ok := receiptLineWords[s.DefaultLocale]
				if !ok {
					t.Fatalf("%s: no receipt line words for the language %q", school, s.DefaultLocale)
				}

				if len(d.Receipts) == 0 {
					t.Errorf("%s %v: no receipts", school, anchor)
				}

				next := map[int]int{}

				for _, r := range d.Receipts {
					next[r.Year]++

					if r.Locale != s.DefaultLocale || r.Issuer != s.Receipts.Issuer || r.Footer != s.Receipts.Footer ||
						r.TaxNote != s.Receipts.TaxNote || r.TaxMode != s.Receipts.TaxMode || r.Currency != s.Currency ||
						r.Sequence != next[r.Year] || r.Number != domain.ReceiptNumber(s.ReceiptPrefix, r.Year, r.Sequence) {
						t.Errorf("%s %v: receipt %+v not issued with the school's settings", school, anchor, r)
					}

					if len(r.Lines) == 0 {
						t.Errorf("%s %v: receipt %s has no lines", school, anchor, r.Number)
					}

					for _, l := range r.Lines {
						for _, w := range words {
							if !strings.Contains(l.Description, w) {
								t.Errorf("%s %v: receipt %s line %q lacks %q (%s)", school, anchor, r.Number,
									l.Description, w, s.DefaultLocale)
							}
						}
					}
				}
			}
		})
	}
}

// meets reports whether [aStart, aEnd) and [bStart, bEnd) overlap.
func meets(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}

// weeklyOn returns the times class c meets on day: ok is false unless c
// meets that day (its weekday, within its term, not retired).
func weeklyOn(c domain.Class, term domain.Term, day domain.Date, loc *time.Location) (start, end time.Time, ok bool) {
	if c.Retired || c.Weekday != day.Weekday() ||
		day.Compare(term.StartsOn) < 0 || day.Compare(term.EndsOn) > 0 {
		return time.Time{}, time.Time{}, false
	}

	start = day.At(c.StartTime, loc)

	return start, start.Add(c.Duration), true
}

// oneOffs returns d's lessons of no class (workshops, the rental) that are
// not canceled.
func oneOffs(d seed.Dataset) []domain.Lesson {
	var out []domain.Lesson

	for _, l := range d.Lessons {
		if l.ClassID.IsZero() && l.Status != domain.LessonCanceled {
			out = append(out, l)
		}
	}

	return out
}

// TestDemoWorkshopsFree: in every demo school, a workshop or the rental
// never meets a weekly class in its room. The dataset holds the class
// lessons only up to the anchor, the workshops lie after it: so each one-off
// lesson is checked against every class of its room on that date, as the
// class would meet then, and against every other lesson in the room (the
// other workshops, the rental and the past class lessons).
func TestDemoWorkshopsFree(t *testing.T) {
	t.Parallel()

	forEachSchool(t, func(t *testing.T, school seed.School, d seed.Dataset) {
		loc := schoolLocation(t, school, d)
		terms := index(d.Terms, func(t domain.Term) domain.TermID { return t.ID })
		rooms := index(d.Rooms, func(r domain.Room) domain.RoomID { return r.ID })

		lessons := oneOffs(d)
		if len(lessons) == 0 {
			t.Fatalf("%s: no workshops and no rental", school)
		}

		for _, o := range lessons {
			day := domain.DateOf(o.StartsAt, loc)
			room := rooms[o.RoomID].Name

			for _, c := range d.Classes {
				start, end, ok := weeklyOn(c, terms[c.TermID], day, loc)
				if ok && c.RoomID == o.RoomID && meets(o.StartsAt, o.EndsAt, start, end) {
					t.Errorf("%s: %s %v on %v (%v) meets class %v in %s", school, o.Kind, o.ID, day,
						o.StartsAt.In(loc).Format("15:04"), c.ID, room)
				}
			}

			for _, l := range d.Lessons {
				if l.ID != o.ID && l.RoomID == o.RoomID && l.Status != domain.LessonCanceled &&
					meets(o.StartsAt, o.EndsAt, l.StartsAt, l.EndsAt) {
					t.Errorf("%s: %s %v on %v meets lesson %v in %s", school, o.Kind, o.ID, day, l.ID, room)
				}
			}
		}
	})
}

// staffOf returns the staff an instructor and an optional accompanist name.
func staffOf(instructor, accompanist domain.StaffID) []domain.StaffID {
	var out []domain.StaffID

	for _, s := range []domain.StaffID{instructor, accompanist} {
		if !s.IsZero() {
			out = append(out, s)
		}
	}

	return out
}

// shared returns a staff member in both a and b, if any.
func shared(a, b []domain.StaffID) (domain.StaffID, bool) {
	for _, s := range a {
		if slices.Contains(b, s) {
			return s, true
		}
	}

	return domain.StaffID{}, false
}

// TestDemoInstructorsNeverDoubleBooked: in every demo school, no staff
// member, as instructor or accompanist, has two weekly classes at once, nor
// a workshop at the time of one of their classes on that date or of another
// lesson of theirs.
func TestDemoInstructorsNeverDoubleBooked(t *testing.T) {
	t.Parallel()

	forEachSchool(t, func(t *testing.T, school seed.School, d seed.Dataset) {
		loc := schoolLocation(t, school, d)
		terms := index(d.Terms, func(t domain.Term) domain.TermID { return t.ID })
		staff := index(d.Staff, func(s domain.Staff) domain.StaffID { return s.ID })

		for i, a := range d.Classes {
			for _, b := range d.Classes[i+1:] {
				if a.Retired || b.Retired || !classesOverlap(a, b) {
					continue
				}

				if s, ok := shared(staffOf(a.InstructorID, a.AccompanistID), staffOf(b.InstructorID, b.AccompanistID)); ok {
					t.Errorf("%s: classes %v and %v overlap for %s", school, a.ID, b.ID, staff[s].DisplayName)
				}
			}
		}

		for _, o := range oneOffs(d) {
			people := staffOf(o.InstructorID, o.AccompanistID)
			if len(people) == 0 {
				continue // the rental
			}

			day := domain.DateOf(o.StartsAt, loc)

			for _, c := range d.Classes {
				start, end, ok := weeklyOn(c, terms[c.TermID], day, loc)
				if !ok || !meets(o.StartsAt, o.EndsAt, start, end) {
					continue
				}

				if s, ok := shared(people, staffOf(c.InstructorID, c.AccompanistID)); ok {
					t.Errorf("%s: %s %v on %v meets class %v for %s", school, o.Kind, o.ID, day, c.ID, staff[s].DisplayName)
				}
			}

			for _, l := range d.Lessons {
				if l.ID == o.ID || l.Status == domain.LessonCanceled || !meets(o.StartsAt, o.EndsAt, l.StartsAt, l.EndsAt) {
					continue
				}

				if s, ok := shared(people, staffOf(l.InstructorID, l.AccompanistID)); ok {
					t.Errorf("%s: %s %v on %v meets lesson %v for %s", school, o.Kind, o.ID, day, l.ID, staff[s].DisplayName)
				}
			}
		}
	})
}

// TestSchoolLogins: every demo school lists exactly one login per role that
// has an account, from member to admin: the first account of the role in
// that school's dataset, with the person's names, and each signs in through
// the AuthService on a store loaded with that school. DemoLogins are the
// programme school's; an unknown school has none.
func TestSchoolLogins(t *testing.T) {
	t.Parallel()

	forEachSchool(t, func(t *testing.T, school seed.School, d seed.Dataset) {
		logins, err := seed.SchoolLogins(school)
		if err != nil {
			t.Fatalf("%s: %v", school, err)
		}

		var roles, withAccount []domain.Role
		for _, l := range logins {
			roles = append(roles, l.Role)
		}

		for _, r := range domain.Roles() {
			if slices.ContainsFunc(d.Accounts, func(a domain.Account) bool { return a.Role == r }) {
				withAccount = append(withAccount, r)
			}
		}

		want := []domain.Role{domain.RoleMember, domain.RoleInstructor, domain.RoleOffice, domain.RoleAdmin}
		if !slices.Equal(roles, want) || !slices.Equal(withAccount, want) {
			t.Fatalf("%s: login roles %v, roles with an account %v; want %v", school, roles, withAccount, want)
		}

		st := memory.New()
		if err := seed.LoadDemoSchoolAt(t.Context(), st, seed.FixedDemoAnchor, school); err != nil {
			t.Fatalf("%s: %v", school, err)
		}

		hasher, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
		if err != nil {
			t.Fatal(err)
		}

		clk := clock.NewFixed(seed.FixedDemoAnchor.At(domain.TimeOfDay{Hour: 12}, schoolLocation(t, school, d)))
		auth := service.NewAuthService(st, clk, ids.V7{}, hasher, nil, "https://school.example.org/")

		for _, l := range logins {
			i := slices.IndexFunc(d.Accounts, func(a domain.Account) bool { return a.Role == l.Role })
			acc := d.Accounts[i]

			p := slices.IndexFunc(d.Persons, func(p domain.Person) bool { return p.ID == acc.PersonID })
			if p < 0 {
				t.Fatalf("%s: login %s: the account's person is not in the dataset", school, l.Email)
			}

			person := d.Persons[p]
			if l.Email != acc.Email || l.Password != seed.DemoPassword ||
				l.GivenName != person.GivenName || l.FamilyName != person.FamilyName {
				t.Errorf("%s: login %+v, want the first %s account %s of %s %s", school, l, l.Role, acc.Email,
					person.GivenName, person.FamilyName)
			}

			signed, err := auth.Login(t.Context(), l.Email, l.Password)
			if err != nil {
				t.Errorf("%s: %s cannot sign in: %v", school, l.Email, err)

				continue
			}

			if pr := signed.Principal; pr.AccountID != acc.ID || pr.PersonID != acc.PersonID || pr.Role != l.Role {
				t.Errorf("%s: %s signed in as %+v, want account %v as %s", school, l.Email, pr, acc.ID, l.Role)
			}
		}

		if school == seed.SchoolProgramme && !reflect.DeepEqual(seed.DemoLogins(), logins) {
			t.Errorf("%s: DemoLogins %+v, want SchoolLogins %+v", school, seed.DemoLogins(), logins)
		}
	})

	for _, school := range []seed.School{"", "x"} {
		if logins, err := seed.SchoolLogins(school); !errors.Is(err, domain.ErrValidation) || logins != nil {
			t.Errorf("SchoolLogins(%q) = %+v, %v; want %v", school, logins, err, domain.ErrValidation)
		}
	}
}

// werkstattCourseTypes are the 23 course types of the werkstatt school's
// weekly plan (the spec's §3, in its order), then the three it offers only
// as Sunday workshops.
var werkstattCourseTypes = []string{
	"Tanzzwerge", "Tänzerische Früherziehung", "Kinderballett Vorstufe", "Kinderballett", "Ballett Jugend",
	"Ballett Jugend & Spitze", "Spitze", "Ballett Erwachsene", "Ballett 45+", "Tanz für Einsteiger",
	"Tanzen 50+", "Contemporary", "Contemporary Kids", "Hip-Hop & Contemporary Kids",
	"Hip-Hop & Contemporary Teens", "Modern Jazz", "Jazz Dance", "Stepptanz", "Irish Dance",
	"Stretch & Strength", "Qigong", "Taiji", "Morning Workout",
	"Floor Barre", "Pas de deux", "Yoga für Tänzerinnen und Tänzer",
}

// werkstattAt returns the werkstatt school's dataset for anchor.
func werkstattAt(t *testing.T, anchor domain.Date) seed.Dataset {
	t.Helper()

	d, err := seed.DemoSchoolAt(anchor, seed.SchoolWerkstatt)
	if err != nil {
		t.Fatalf("werkstatt %v: %v", anchor, err)
	}

	return d
}

// TestWerkstattCatalogue: the werkstatt school has its own house: four
// rooms in the house's order with their capacities, its five levels with
// codes, its course types, and the 70 classes of its weekly plan from
// Monday to Saturday, each in a room large enough, with 1 to 4 members.
// The workshop-only course types have no class.
func TestWerkstattCatalogue(t *testing.T) {
	t.Parallel()

	d := werkstattAt(t, seed.FixedDemoAnchor)

	var rooms []string
	for _, r := range d.Rooms {
		rooms = append(rooms, fmt.Sprintf("%d %s %d", r.Position, r.Name, r.Capacity))
	}

	if want := []string{"1 Oben 24", "2 Unten 18", "3 Online 40", "4 Trainingsraum 8"}; !slices.Equal(rooms, want) {
		t.Errorf("rooms = %q, want %q", rooms, want)
	}

	want := []string{"1 Einsteiger E", "2 Anfänger A", "3 Mittelstufe M", "4 Fortgeschritten F", "5 Profi P"}
	if got := levelLines(d.Levels); !slices.Equal(got, want) {
		t.Errorf("levels = %q, want %q", got, want)
	}

	var courseTypes []string
	for _, ct := range d.CourseTypes {
		courseTypes = append(courseTypes, ct.Name)

		if ct.Description == "" || strings.Count(ct.Description, ".") != 1 || !strings.HasSuffix(ct.Description, ".") {
			t.Errorf("course type %s: description %q, want one sentence", ct.Name, ct.Description)
		}
	}

	if !slices.Equal(courseTypes, werkstattCourseTypes) {
		t.Errorf("course types = %q\nwant %q", courseTypes, werkstattCourseTypes)
	}

	if len(d.Classes) != 70 {
		t.Errorf("%d classes, want 70", len(d.Classes))
	}

	roomsByID := index(d.Rooms, func(r domain.Room) domain.RoomID { return r.ID })
	names := index(d.CourseTypes, func(ct domain.CourseType) domain.CourseTypeID { return ct.ID })
	perDay := map[time.Weekday]int{}
	members := map[domain.ClassID]int{}
	used := map[string]bool{}

	for _, m := range d.ClassMembers {
		members[m.ClassID]++
	}

	for _, c := range d.Classes {
		perDay[c.Weekday]++
		used[names[c.CourseTypeID].Name] = true

		if r := roomsByID[c.RoomID]; c.Capacity > r.Capacity {
			t.Errorf("class %v: %d places in %s with %d", c.ID, c.Capacity, r.Name, r.Capacity)
		}

		if n := members[c.ID]; n < 1 || n > 4 {
			t.Errorf("class %v (%s): %d members, want 1 to 4", c.ID, names[c.CourseTypeID].Name, n)
		}
	}

	wantDays := map[time.Weekday]int{
		time.Monday: 13, time.Tuesday: 14, time.Wednesday: 15, time.Thursday: 10, time.Friday: 12, time.Saturday: 6,
	}
	if !reflect.DeepEqual(perDay, wantDays) {
		t.Errorf("classes per weekday = %v, want %v (none on Sunday)", perDay, wantDays)
	}

	for i, name := range werkstattCourseTypes {
		if weekly := i < 23; used[name] != weekly {
			t.Errorf("course type %s in the weekly plan: %v, want %v", name, used[name], weekly)
		}
	}

	// Every room has members, so the history, the statistics and the
	// participants pages have data in each.
	withMembers := map[domain.RoomID]bool{}
	for _, c := range d.Classes {
		if members[c.ID] > 0 {
			withMembers[c.RoomID] = true
		}
	}

	if len(withMembers) != len(d.Rooms) {
		t.Errorf("classes with members in %d of %d rooms", len(withMembers), len(d.Rooms))
	}
}

// audienceText writes a class's audience in the spec's notation: level
// codes ("A/M", "alle"), "St. n" and ages in years ("ab 3 J.", "4–6 J.").
func audienceText(c domain.Class, levels map[domain.LevelID]domain.Level) string {
	var parts []string

	switch {
	case c.AllLevels:
		parts = append(parts, "alle")
	case !c.Levels.IsZero():
		from, to := levels[c.Levels.From], levels[c.Levels.To]
		if from.Position > to.Position {
			from, to = to, from
		}

		code := from.Code
		if to.ID != from.ID {
			code += "/" + to.Code
		}

		parts = append(parts, code)
	}

	if c.Stage > 0 {
		parts = append(parts, fmt.Sprintf("St. %d", c.Stage))
	}

	switch {
	case c.AgeFrom > 0 && c.AgeTo > 0:
		parts = append(parts, fmt.Sprintf("%d–%d J.", c.AgeFrom/12, c.AgeTo/12))
	case c.AgeFrom > 0:
		parts = append(parts, fmt.Sprintf("ab %d J.", c.AgeFrom/12))
	}

	return strings.Join(parts, ", ")
}

// TestWerkstattAudiences spot-checks rows of the spec's weekly plan: the
// room, the time, the course type, the audience and the places.
func TestWerkstattAudiences(t *testing.T) {
	t.Parallel()

	d := werkstattAt(t, seed.FixedDemoAnchor)
	levels := index(d.Levels, func(l domain.Level) domain.LevelID { return l.ID })
	rooms := index(d.Rooms, func(r domain.Room) domain.RoomID { return r.ID })
	courseTypes := index(d.CourseTypes, func(ct domain.CourseType) domain.CourseTypeID { return ct.ID })

	row := func(c domain.Class) string {
		end := c.StartTime.SinceMidnight() + c.Duration

		return fmt.Sprintf("%s %s %s–%02d:%02d %s | %s | %d", c.Weekday, rooms[c.RoomID].Name, c.StartTime,
			int(end.Hours()), int(end.Minutes())%60, courseTypes[c.CourseTypeID].Name, audienceText(c, levels), c.Capacity)
	}

	var got []string
	for _, c := range d.Classes {
		got = append(got, row(c))
	}

	for _, want := range []string{
		"Monday Oben 18:00–19:10 Contemporary | A/M | 18",
		"Monday Oben 15:45–16:35 Tänzerische Früherziehung | St. 2, 4–6 J. | 14",
		"Monday Oben 14:55–15:45 Tanzzwerge | ab 3 J. | 12",
		"Tuesday Trainingsraum 17:00–17:30 Spitze | E, ab 12 J. | 8",
		"Tuesday Online 08:45–09:15 Morning Workout | alle | 40",
		"Wednesday Oben 18:55–19:55 Hip-Hop & Contemporary Teens | alle, 12–17 J. | 16",
		"Thursday Unten 17:30–18:40 Taiji | M/F | 16",
		"Friday Online 18:45–19:45 Taiji | alle | 40",
		"Saturday Oben 13:30–14:45 Contemporary | M/F | 18",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("no class %q in the weekly plan:\n%s", want, strings.Join(got, "\n"))
		}
	}
}

// TestWerkstattChildrenOfAge: the members of a class that states an age
// range are of that age on FixedDemoAnchor in whole years ("4–6 J." takes
// children of 4, 5 and 6, "ab 12 J." from 12), and every class for children
// (one that states an age or a stage) has children only.
func TestWerkstattChildrenOfAge(t *testing.T) {
	t.Parallel()

	d := werkstattAt(t, seed.FixedDemoAnchor)
	persons := index(d.Persons, func(p domain.Person) domain.PersonID { return p.ID })
	classes := index(d.Classes, func(c domain.Class) domain.ClassID { return c.ID })
	anchor := seed.FixedDemoAnchor

	for _, m := range d.ClassMembers {
		c, p := classes[m.ClassID], persons[m.PersonID]
		if c.AgeFrom == 0 && c.Stage == 0 {
			continue
		}

		if p.BirthDate.IsZero() {
			t.Errorf("class %v for children has the adult %s %s", c.ID, p.GivenName, p.FamilyName)

			continue
		}

		months := (anchor.Year-p.BirthDate.Year)*12 + int(anchor.Month-p.BirthDate.Month)
		if anchor.Day < p.BirthDate.Day {
			months--
		}

		if months < int(c.AgeFrom) || (c.AgeTo > 0 && months >= int(c.AgeTo)+12) {
			t.Errorf("class %v (%d–%d months): %s %s is %d months old", c.ID, c.AgeFrom, c.AgeTo,
				p.GivenName, p.FamilyName, months)
		}
	}
}

// TestWerkstattOwnPeople: the werkstatt school's people are its own: no
// person, staff member, guest or email of it is in the programme school,
// every email is on an example host, and the addresses are ASCII
// (vorname.nachname, ä as ae, ß as ss) while the names keep their
// umlauts.
func TestWerkstattOwnPeople(t *testing.T) {
	t.Parallel()

	programme := seed.DemoAt(seed.FixedDemoAnchor)
	werkstatt := werkstattAt(t, seed.FixedDemoAnchor)

	names := func(d seed.Dataset) map[string]bool {
		m := map[string]bool{}
		for _, p := range d.Persons {
			m[p.GivenName+" "+p.FamilyName] = true
			if p.Email != "" {
				m[strings.ToLower(p.Email)] = true
			}
		}

		for _, s := range d.Staff {
			m[s.DisplayName] = true
		}

		for _, b := range d.Bookings {
			if b.GuestName != "" {
				m[b.GuestName] = true
			}

			if b.GuestEmail != "" {
				m[strings.ToLower(b.GuestEmail)] = true
			}
		}

		for _, a := range d.Accounts {
			m[strings.ToLower(a.Email)] = true
		}

		return m
	}

	theirs := names(programme)
	for name := range names(werkstatt) {
		if theirs[name] {
			t.Errorf("%q is in both schools", name)
		}
	}

	fold := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")
	umlauts := 0

	for _, p := range werkstatt.Persons {
		if strings.ContainsAny(p.GivenName+p.FamilyName, "äöüß") {
			umlauts++
		}

		if p.Email == "" {
			continue
		}

		host := p.Email[strings.LastIndex(p.Email, "@")+1:]
		if !strings.HasSuffix(host, ".example") || len(host) == len(".example") {
			t.Errorf("%s %s: email %q is not on a .example host", p.GivenName, p.FamilyName, p.Email)
		}

		local := strings.ToLower(fold.Replace(strings.ToLower(p.GivenName + "." + p.FamilyName)))
		if !strings.EqualFold(p.Email[:strings.LastIndex(p.Email, "@")], local) {
			t.Errorf("%s %s: email %q, want %s@…", p.GivenName, p.FamilyName, p.Email, local)
		}

		for _, r := range p.Email {
			if r > 127 {
				t.Errorf("%s %s: email %q is not ASCII", p.GivenName, p.FamilyName, p.Email)

				break
			}
		}
	}

	if umlauts == 0 {
		t.Error("no werkstatt name has an umlaut or ß")
	}

	if !strings.HasPrefix(werkstatt.Terms[0].Name, "Schuljahr ") {
		t.Errorf("term %q, want a German name", werkstatt.Terms[0].Name)
	}
}

// TestWerkstattWorkshopsOnSunday: for every anchor, the werkstatt
// school's workshops lie on the Sunday on or after their day, the one day
// without classes, and its rental on a Wednesday evening in Unten, after
// the anchor.
func TestWerkstattWorkshopsOnSunday(t *testing.T) {
	t.Parallel()

	for _, anchor := range append([]domain.Date{seed.FixedDemoAnchor}, demoAnchors...) {
		d := werkstattAt(t, anchor)
		loc := schoolLocation(t, seed.SchoolWerkstatt, d)
		rooms := index(d.Rooms, func(r domain.Room) domain.RoomID { return r.ID })

		workshops, rentals := 0, 0

		for _, l := range oneOffs(d) {
			day := domain.DateOf(l.StartsAt, loc)
			if day.Compare(anchor) <= 0 {
				t.Errorf("%v: %s on %v, not after the anchor", anchor, l.Kind, day)
			}

			switch l.Kind {
			case domain.LessonWorkshop:
				workshops++

				if day.Weekday() != time.Sunday {
					t.Errorf("%v: workshop %v on %v, a %s", anchor, l.ID, day, day.Weekday())
				}
			case domain.LessonRental:
				rentals++

				if day.Weekday() != time.Wednesday || rooms[l.RoomID].Name != "Unten" ||
					l.StartsAt.In(loc).Hour() < 18 || l.Renter != "Theatergruppe Kupferblatt" {
					t.Errorf("%v: rental %+v, want the Theatergruppe Kupferblatt in Unten on a Wednesday evening",
						anchor, l)
				}
			}
		}

		if workshops != 6 || rentals != 1 {
			t.Errorf("%v: %d workshops and %d rentals, want 6 and 1", anchor, workshops, rentals)
		}
	}
}

// TestDemoTermNameOfOneYear: a term within one calendar year (an anchor
// in February) is named after that year, in each school's language.
func TestDemoTermNameOfOneYear(t *testing.T) {
	t.Parallel()

	anchor := domain.Date{Year: 2027, Month: time.February, Day: 10}
	want := map[seed.School]string{seed.SchoolProgramme: "School Year 2027", seed.SchoolWerkstatt: "Schuljahr 2027"}

	for _, school := range seed.Schools() {
		d, err := seed.DemoSchoolAt(anchor, school)
		if err != nil {
			t.Fatal(err)
		}

		if got := d.Terms[0].Name; got != want[school] {
			t.Errorf("%s: term %q, want %q", school, got, want[school])
		}
	}

	if got := werkstattAt(t, seed.FixedDemoAnchor).Terms[0].Name; got != "Schuljahr 2026/27" {
		t.Errorf("werkstatt term %q, want Schuljahr 2026/27", got)
	}
}
