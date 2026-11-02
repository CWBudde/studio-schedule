package servicetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// FreeRooms runs the scenarios of CatalogService.FreeRooms on stores from
// newStore: which lessons take a room for a time on a school date, and which
// rooms the location filter and the order of Rooms give back.
func FreeRooms(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	for name, fn := range map[string]func(t *testing.T, r freeRoomsWorld){
		"Overlap":         freeRoomsOverlap,
		"Excluded":        freeRoomsExcluded,
		"Rental":          freeRoomsRental,
		"Hidden":          freeRoomsHidden,
		"Locations":       freeRoomsLocations,
		"UnknownLocation": freeRoomsUnknownLocation,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, newFreeRoomsWorld(t, newWorld(t, newStore(t))))
		})
	}
}

// freeDate is the scenarios' school date, Wednesday 4 November 2026: two
// weeks after the world's own lessons, and after the change to winter time,
// so the school's offset differs from the one at now.
var freeDate = domain.Date{Year: 2026, Month: time.November, Day: 4}

// freeRoomsWorld is the world with a second room in its location, placed
// after the first but named before it, and a second location, "Annex",
// whose name sorts before the world's, with one room.
type freeRoomsWorld struct {
	*world

	loc     *time.Location
	studio  domain.LocationID
	annex   domain.LocationID
	roomOne domain.RoomID // the world's room
	roomTwo domain.RoomID
	hall    domain.RoomID // the annex's room
}

func newFreeRoomsWorld(t *testing.T, w *world) freeRoomsWorld {
	t.Helper()

	loc, err := time.LoadLocation(w.settings.Timezone)
	if err != nil {
		t.Fatal(err)
	}

	one := read(w, func(ctx context.Context, tx store.Tx) (domain.Room, error) { return tx.Rooms().Get(ctx, w.room) })
	annex := domain.Location{ID: ids.New[domain.LocationID](w.seq), Name: "Annex"}
	two := domain.Room{
		ID: ids.New[domain.RoomID](w.seq), LocationID: one.LocationID, Name: "Ballet Hall", Capacity: 8, Position: 2,
	}
	hall := domain.Room{ID: ids.New[domain.RoomID](w.seq), LocationID: annex.ID, Name: "Annex Hall", Capacity: 20, Position: 1}

	w.write(func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Locations().Create(ctx, annex), tx.Rooms().Create(ctx, two), tx.Rooms().Create(ctx, hall))
	})

	return freeRoomsWorld{
		world: w, loc: loc, studio: one.LocationID, annex: annex.ID, roomOne: one.ID, roomTwo: two.ID, hall: hall.ID,
	}
}

// lesson stores a scheduled public lesson in room on date from start for
// d, changed by edit if set.
func (r freeRoomsWorld) lesson(
	room domain.RoomID, date domain.Date, start domain.TimeOfDay, d time.Duration, edit func(l *domain.Lesson),
) domain.Lesson {
	r.t.Helper()

	at := date.At(start, r.loc)
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](r.seq), CourseTypeID: r.courseType, RoomID: room, InstructorID: r.instructor,
		StartsAt: at, EndsAt: at.Add(d), Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic,
		Kind: domain.LessonRegular, Capacity: 5,
	}
	if edit != nil {
		edit(&l)
	}

	if err := l.Validate(); err != nil {
		r.t.Fatal(err)
	}

	r.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// free returns the IDs of the rooms FreeRooms finds free in location from
// start on freeDate for d.
func (r freeRoomsWorld) free(location domain.LocationID, start domain.TimeOfDay, d time.Duration) []domain.RoomID {
	r.t.Helper()

	rooms, err := service.NewCatalogService(r.st, r.seq).FreeRooms(r.t.Context(), service.FreeRoomsQuery{
		Date: freeDate, Start: start, Duration: d, Location: location,
	})
	if err != nil {
		r.t.Fatalf("free rooms at %v for %s: %v", start, d, err)
	}

	got := make([]domain.RoomID, 0, len(rooms))
	for _, room := range rooms {
		got = append(got, room.ID)
	}

	return got
}

// wantFree checks the free rooms of the world's location from start for d.
func (r freeRoomsWorld) wantFree(start domain.TimeOfDay, d time.Duration, want ...domain.RoomID) {
	r.t.Helper()

	if got := r.free(r.studio, start, d); !slices.Equal(got, want) {
		r.t.Errorf("free at %02d:%02d for %s: %v, want %v", start.Hour, start.Minute, d, got, want)
	}
}

// freeRoomsOverlap: a lesson from 18:00 to 19:00 (school time) takes its
// room for any time that overlaps it, and only its room; a time that ends
// when it starts or starts when it ends leaves the room free.
func freeRoomsOverlap(t *testing.T, r freeRoomsWorld) {
	r.lesson(r.roomOne, freeDate, domain.TimeOfDay{Hour: 18}, time.Hour, nil)

	r.wantFree(domain.TimeOfDay{Hour: 18, Minute: 30}, time.Hour, r.roomTwo)
	r.wantFree(domain.TimeOfDay{Hour: 17, Minute: 30}, time.Hour, r.roomTwo)
	r.wantFree(domain.TimeOfDay{Hour: 18, Minute: 15}, 15*time.Minute, r.roomTwo)
	r.wantFree(domain.TimeOfDay{Hour: 17}, 3*time.Hour, r.roomTwo)
	r.wantFree(domain.TimeOfDay{Hour: 18, Minute: 59}, time.Minute, r.roomTwo)

	// Back to back on either side. The wall clock is the school's: read in
	// UTC, 17:00 for an hour would overlap the lesson, as the school's zone
	// is ahead of UTC on the date.
	if _, offset := freeDate.At(domain.TimeOfDay{Hour: 18}, r.loc).In(r.loc).Zone(); offset <= 0 || offset > int(time.Hour/time.Second) {
		t.Fatalf("the school's zone %s is %ds from UTC on %v; the check below needs it ahead by at most an hour",
			r.loc, offset, freeDate)
	}

	r.wantFree(domain.TimeOfDay{Hour: 19}, time.Hour, r.roomOne, r.roomTwo)
	r.wantFree(domain.TimeOfDay{Hour: 17}, time.Hour, r.roomOne, r.roomTwo)
}

// freeRoomsExcluded: a canceled lesson and a lesson on another date leave
// the room free.
func freeRoomsExcluded(_ *testing.T, r freeRoomsWorld) {
	r.lesson(r.roomOne, freeDate, domain.TimeOfDay{Hour: 18}, time.Hour, func(l *domain.Lesson) {
		l.Status, l.CanceledAt, l.CancelNote = domain.LessonCanceled, now, "The instructor is ill."
	})
	r.lesson(r.roomTwo, freeDate.AddDays(1), domain.TimeOfDay{Hour: 18}, time.Hour, nil)
	r.lesson(r.roomTwo, freeDate.AddDays(-1), domain.TimeOfDay{Hour: 18}, time.Hour, nil)

	r.wantFree(domain.TimeOfDay{Hour: 18}, time.Hour, r.roomOne, r.roomTwo)
}

// freeRoomsRental: a room rental, without instructor, capacity or course
// type, takes its room like a lesson.
func freeRoomsRental(_ *testing.T, r freeRoomsWorld) {
	r.lesson(r.roomTwo, freeDate, domain.TimeOfDay{Hour: 10}, 2*time.Hour, func(l *domain.Lesson) {
		l.Kind, l.InstructorID, l.Capacity, l.CourseTypeID = domain.LessonRental, domain.StaffID{}, 0, domain.CourseTypeID{}
		l.Visibility, l.Renter = domain.VisibilityHidden, "Copperleaf Theater Group"
	})

	r.wantFree(domain.TimeOfDay{Hour: 11}, time.Hour, r.roomOne)
	r.wantFree(domain.TimeOfDay{Hour: 12}, time.Hour, r.roomOne, r.roomTwo)
}

// freeRoomsHidden: a hidden lesson, which the schedule does not show,
// takes its room too.
func freeRoomsHidden(_ *testing.T, r freeRoomsWorld) {
	r.lesson(r.roomOne, freeDate, domain.TimeOfDay{Hour: 9}, 90*time.Minute, func(l *domain.Lesson) {
		l.Visibility = domain.VisibilityHidden
	})

	r.wantFree(domain.TimeOfDay{Hour: 10}, time.Hour, r.roomTwo)
}

// freeRoomsLocations: without a location FreeRooms looks at every room, by
// location name, then position before name; a location gives its rooms
// only, and one without rooms none.
func freeRoomsLocations(t *testing.T, r freeRoomsWorld) {
	start := domain.TimeOfDay{Hour: 14}

	if got, want := r.free(domain.LocationID{}, start, time.Hour), []domain.RoomID{r.hall, r.roomOne, r.roomTwo}; !slices.Equal(got, want) {
		t.Errorf("every location: %v, want %v", got, want)
	}

	if got, want := r.free(r.annex, start, time.Hour), []domain.RoomID{r.hall}; !slices.Equal(got, want) {
		t.Errorf("the annex: %v, want %v", got, want)
	}

	// A lesson in the annex leaves every location's other rooms free.
	r.lesson(r.hall, freeDate, start, time.Hour, nil)

	if got, want := r.free(domain.LocationID{}, start, time.Hour), []domain.RoomID{r.roomOne, r.roomTwo}; !slices.Equal(got, want) {
		t.Errorf("every location with the annex taken: %v, want %v", got, want)
	}

	if got := r.free(r.annex, start, time.Hour); len(got) != 0 {
		t.Errorf("the taken annex: %v, want none", got)
	}

	empty := domain.Location{ID: ids.New[domain.LocationID](r.seq), Name: "Empty Loft"}
	r.write(func(ctx context.Context, tx store.Tx) error { return tx.Locations().Create(ctx, empty) })

	if got := r.free(empty.ID, start, time.Hour); len(got) != 0 {
		t.Errorf("a location without rooms: %v, want none", got)
	}
}

// freeRoomsUnknownLocation: an unknown location is not found, not a
// location without free rooms.
func freeRoomsUnknownLocation(t *testing.T, r freeRoomsWorld) {
	_, err := service.NewCatalogService(r.st, r.seq).FreeRooms(t.Context(), service.FreeRoomsQuery{
		Date: freeDate, Start: domain.TimeOfDay{Hour: 18}, Duration: time.Hour,
		Location: ids.New[domain.LocationID](r.seq),
	})
	if !errors.Is(err, domain.ErrNotFound) || errors.Is(err, service.ErrNoSchool) {
		t.Fatalf("want ErrNotFound for the location, got %v", err)
	}
}
