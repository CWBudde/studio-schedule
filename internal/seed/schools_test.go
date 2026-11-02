package seed_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
