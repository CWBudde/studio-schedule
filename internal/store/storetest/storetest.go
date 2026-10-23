// Package storetest contains contract tests that every store.Store
// implementation must pass.
//
// Every case gets a fresh, empty store and runs in parallel with the others,
// so implementations must isolate the stores returned by newStore. Fixtures
// are fictional and use deterministic IDs from ids.Sequence.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

var errBoom = errors.New("storetest: boom")

// t0 is a fixed instant in UTC with microsecond precision, so values built
// from it survive a round trip unchanged.
var t0 = time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC)

// Run executes the store contract tests. newStore must return a fresh, empty
// store for every call.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	t.Run("Tx", func(t *testing.T) { testTx(t, newStore) })
	t.Run("Settings", func(t *testing.T) { testSettings(t, newStore) })
	t.Run("Persons", func(t *testing.T) { testPersons(t, newStore) })
	t.Run("Accounts", func(t *testing.T) { testAccounts(t, newStore) })
	t.Run("Catalog", func(t *testing.T) { testCatalogue(t, newStore) })
	t.Run("Timetable", func(t *testing.T) { testTimetable(t, newStore) })
	t.Run("Lessons", func(t *testing.T) { testLessons(t, newStore) })
	t.Run("Bookings", func(t *testing.T) { testBookings(t, newStore) })
}

// env is one test case's store plus an ID source for its fixtures.
type env struct {
	t   *testing.T
	st  store.Store
	seq *ids.Sequence
}

// cases runs every case as a parallel subtest with its own env.
func cases(t *testing.T, newStore func(*testing.T) store.Store, cs map[string]func(e *env)) {
	t.Helper()

	for name, fn := range cs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(&env{t: t, st: newStore(t), seq: ids.NewSequence()})
		})
	}
}

func newID[I domain.AnyID](e *env) I { return ids.New[I](e.seq) }

func (e *env) write(fn func(ctx context.Context, tx store.Tx) error) error {
	return e.st.InTx(e.t.Context(), store.ReadWrite, fn)
}

func (e *env) read(fn func(ctx context.Context, tx store.Tx) error) error {
	return e.st.InTx(e.t.Context(), store.ReadOnly, fn)
}

func (e *env) mustWrite(fn func(ctx context.Context, tx store.Tx) error) {
	e.t.Helper()

	if err := e.write(fn); err != nil {
		e.t.Fatalf("write: %v", err)
	}
}

// query runs fn in a read-only transaction and returns its result.
func query[T any](e *env, fn func(ctx context.Context, tx store.Tx) (T, error)) (T, error) {
	var v T

	err := e.read(func(ctx context.Context, tx store.Tx) error {
		var err error
		v, err = fn(ctx, tx)

		return err
	})

	return v, err
}

// mustQuery is query that fails the test on error.
func mustQuery[T any](e *env, fn func(ctx context.Context, tx store.Tx) (T, error)) T {
	e.t.Helper()

	v, err := query(e, fn)
	if err != nil {
		e.t.Fatalf("query: %v", err)
	}

	return v
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Fatalf("want error %v, got %v", target, err)
	}
}

func wantEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()

	if got != want {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func wantSlice[T comparable](t *testing.T, got, want []T) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d:\ngot  %+v\nwant %+v", len(got), len(want), got, want)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("item %d:\ngot  %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

// valid fails the test if a fixture is invalid: the contract only covers
// entities that services would write.
func valid(t *testing.T, v interface{ Validate() error }) {
	t.Helper()

	if err := v.Validate(); err != nil {
		t.Fatalf("invalid fixture %+v: %v", v, err)
	}
}

// ---------------------------------------------------------------- fixtures
//
// Each fixture builder creates its entity (and the records it references)
// in its own transaction and returns it.

func (e *env) person(given, family string) domain.Person {
	e.t.Helper()

	p := domain.Person{ID: newID[domain.PersonID](e), GivenName: given, FamilyName: family}
	valid(e.t, p)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Create(ctx, p) })

	return p
}

func (e *env) account(p domain.Person, email string) domain.Account {
	e.t.Helper()

	a := domain.Account{ID: newID[domain.AccountID](e), PersonID: p.ID, Email: email, Role: domain.RoleMember}
	valid(e.t, a)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Create(ctx, a) })

	return a
}

func (e *env) location(name string) domain.Location {
	e.t.Helper()

	l := domain.Location{ID: newID[domain.LocationID](e), Name: name}
	valid(e.t, l)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Locations().Create(ctx, l) })

	return l
}

func (e *env) room(l domain.Location, name string) domain.Room {
	e.t.Helper()

	r := domain.Room{ID: newID[domain.RoomID](e), LocationID: l.ID, Name: name, Capacity: 12}
	valid(e.t, r)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Create(ctx, r) })

	return r
}

func (e *env) staffMember(name string) domain.Staff {
	e.t.Helper()

	s := domain.Staff{ID: newID[domain.StaffID](e), DisplayName: name, Kind: domain.StaffInstructor, Active: true}
	valid(e.t, s)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Staff().Create(ctx, s) })

	return s
}

func (e *env) courseType(name string) domain.CourseType {
	e.t.Helper()

	c := domain.CourseType{ID: newID[domain.CourseTypeID](e), Name: name}
	valid(e.t, c)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.CourseTypes().Create(ctx, c) })

	return c
}

func (e *env) term(name string, start domain.Date) domain.Term {
	e.t.Helper()

	t := domain.Term{
		ID: newID[domain.TermID](e), Name: name, StartsOn: start,
		EndsOn: domain.Date{Year: start.Year, Month: time.December, Day: 31},
	}
	valid(e.t, t)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Terms().Create(ctx, t) })

	return t
}

// setting is the set of catalog records a class or lesson refers to.
type setting struct {
	room       domain.Room
	instructor domain.Staff
	courseType domain.CourseType
	term       domain.Term
}

func (e *env) setting() setting {
	e.t.Helper()

	return setting{
		room:       e.room(e.location("North Studio"), "Room A"),
		instructor: e.staffMember("Instructor One"),
		courseType: e.courseType("Ballet Basics"),
		term:       e.term("Spring", domain.Date{Year: 2026, Month: time.March, Day: 1}),
	}
}

// newClass returns a weekly class without creating it.
func (e *env) newClass(s setting, day time.Weekday, start domain.TimeOfDay) domain.Class {
	e.t.Helper()

	c := domain.Class{
		ID: newID[domain.ClassID](e), TermID: s.term.ID, CourseTypeID: s.courseType.ID, RoomID: s.room.ID,
		InstructorID: s.instructor.ID, Level: domain.LevelBeginner, Weekday: day, StartTime: start,
		Duration: 60 * time.Minute, Capacity: 10,
	}
	valid(e.t, c)

	return c
}

func (e *env) class(s setting, day time.Weekday, start domain.TimeOfDay) domain.Class {
	e.t.Helper()

	c := e.newClass(s, day, start)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Classes().Create(ctx, c) })

	return c
}

// newLesson returns a one-off lesson without creating it.
func (e *env) newLesson(s setting, start time.Time, capacity int) domain.Lesson {
	e.t.Helper()

	l := domain.Lesson{
		ID: newID[domain.LessonID](e), CourseTypeID: s.courseType.ID, RoomID: s.room.ID,
		InstructorID: s.instructor.ID, StartsAt: start, EndsAt: start.Add(time.Hour),
		Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular,
		Capacity: capacity,
	}
	valid(e.t, l)

	return l
}

func (e *env) lesson(s setting, start time.Time, capacity int) domain.Lesson {
	e.t.Helper()

	l := e.newLesson(s, start, capacity)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// newBooking returns an active booking of p without creating it.
func (e *env) newBooking(l domain.Lesson, p domain.Person, at time.Time) domain.Booking {
	e.t.Helper()

	b := domain.Booking{
		ID: newID[domain.BookingID](e), LessonID: l.ID, PersonID: p.ID,
		Status: domain.BookingBooked, BookedAt: at,
	}
	valid(e.t, b)

	return b
}

func (e *env) booking(l domain.Lesson, p domain.Person, at time.Time) domain.Booking {
	e.t.Helper()

	b := e.newBooking(l, p, at)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

	return b
}

func errorsIs(err, target error) bool { return errors.Is(err, target) }
