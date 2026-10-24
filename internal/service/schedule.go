package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// MaxScheduleDays is the longest date range one schedule request may cover.
const MaxScheduleDays = 42

// ScheduleQuery selects the lessons of a schedule. From and To are school
// dates; To is exclusive. The other fields are optional filters.
type ScheduleQuery struct {
	From, To domain.Date
	Location domain.LocationID
	// Instructor matches the instructor or the accompanist of a lesson.
	Instructor domain.StaffID
	CourseType domain.CourseTypeID
	// Level matches the level of the lesson's class; one-off lessons have
	// none and never match.
	Level domain.Level
}

func (q ScheduleQuery) validate() error {
	switch {
	case !q.From.Valid():
		return fmt.Errorf("%w: invalid from date", domain.ErrValidation)
	case !q.To.Valid():
		return fmt.Errorf("%w: invalid to date", domain.ErrValidation)
	case q.To.Compare(q.From) <= 0:
		return fmt.Errorf("%w: to must be after from", domain.ErrValidation)
	case q.To.Compare(q.From.AddDays(MaxScheduleDays)) > 0:
		return fmt.Errorf("%w: the range must not exceed %d days", domain.ErrValidation, MaxScheduleDays)
	case q.Level != "" && !q.Level.Valid():
		return fmt.Errorf("%w: invalid level %q", domain.ErrValidation, q.Level)
	}

	return nil
}

func (q ScheduleQuery) matches(sl ScheduledLesson) bool {
	l := sl.Lesson

	switch {
	case !q.Location.IsZero() && sl.Room.LocationID != q.Location:
		return false
	case !q.Instructor.IsZero() && l.InstructorID != q.Instructor && l.AccompanistID != q.Instructor:
		return false
	case !q.CourseType.IsZero() && l.CourseTypeID != q.CourseType:
		return false
	case q.Level != "" && sl.Level != q.Level:
		return false
	}

	return true
}

// ScheduledLesson is a lesson with the records it references. Level is
// the level of its class (zero for one-off lessons); Accompanist is zero if
// the lesson has none.
type ScheduledLesson struct {
	Lesson                  domain.Lesson
	Level                   domain.Level
	CourseType              domain.CourseType
	Room                    domain.Room
	Location                domain.Location
	Instructor, Accompanist domain.Staff
	// Admin is what the office sees beyond the schedule; nil for
	// everyone else.
	Admin *LessonAdmin
	// FreePlaces is the capacity minus the active bookings and the places
	// that waiting-list invites hold, at least 0 (ADR 0010).
	FreePlaces int
	// BookingClosesAt is when booking closes: the booking cutoff before
	// the start (ADR 0009).
	BookingClosesAt time.Time
	// LateCancelFrom is when a cancellation becomes late: the cancellation
	// cutoff before the start; zero for a lesson the school canceled, which
	// is never canceled late.
	LateCancelFrom time.Time
	// BookedFor are the viewer and their dependents with an active booking
	// for the lesson, the viewer first; only in Schedule and Lesson.
	BookedFor []BookedPerson
	// Waiting are the viewer and their dependents on the lesson's waiting
	// list (open entries), the viewer first; only in Schedule and Lesson.
	Waiting []WaitingPerson
}

// WaitingPerson is someone of the viewer's family on a lesson's waiting
// list.
type WaitingPerson struct {
	Person domain.Person
	// Name is the person's given and family name, for display.
	Name string
	WaitlistPlace
	// Status is the entry's status as the viewer sees it: an invite that
	// has run out is expired, even before the waitlist-expiry job ends it.
	Status domain.WaitlistStatus
}

// BookedPerson is someone of the viewer's family with an active booking.
type BookedPerson struct {
	Person domain.Person
	// Name is the person's given and family name, for display.
	Name      string
	BookingID domain.BookingID
}

// Schedule is the published timetable of a date range as one viewer sees
// it.
type Schedule struct {
	// Timezone is the IANA time zone of the school.
	Timezone string
	// Now is the server time the release window was computed at.
	Now      time.Time
	From, To domain.Date
	Release  ReleaseWindow
	// VisibleUntil is the instant before which regular lessons are visible
	// to the viewer: ReleasedUntil for guests, later for members with early
	// access, zero for staff (no limit).
	VisibleUntil time.Time
	// Lessons are ordered by start and ID.
	Lessons []ScheduledLesson
}

// ScheduleService publishes the timetable. What a caller sees depends on
// their role (ADR 0003): guests see public, released lessons; members also
// lessons for members and, with early access, a few more days; staff see
// everything.
type ScheduleService struct {
	store store.Store
	clock clock.Clock
}

// NewScheduleService returns a ScheduleService backed by st.
func NewScheduleService(st store.Store, clk clock.Clock) *ScheduleService {
	return &ScheduleService{store: st, clock: clk}
}

// Schedule returns the lessons p may see that start in the school dates
// [q.From, q.To) and match the filters of q; a zero p is a guest. An
// invalid query fails with an error wrapping domain.ErrValidation.
func (s *ScheduleService) Schedule(ctx context.Context, p Principal, q ScheduleQuery) (Schedule, error) {
	if err := q.validate(); err != nil {
		return Schedule{}, err
	}

	out := Schedule{Now: s.clock.Now(), From: q.From, To: q.To}

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		out.Timezone = settings.Timezone
		out.Release = ComputeReleaseWindow(settings, loc, out.Now)
		out.Lessons = nil // the callback may run more than once

		v := viewerOf(p, settings, loc, out.Now)
		out.VisibleUntil = v.visibleUntil(out.Release)

		lessons, err := tx.Lessons().ListRange(ctx, q.From.At(domain.TimeOfDay{}, loc), q.To.At(domain.TimeOfDay{}, loc))
		if err != nil {
			return fmt.Errorf("lessons: %w", err)
		}

		refs, err := loadRangeRefs(ctx, tx, q.From, q.To)
		if err != nil {
			return err
		}

		for _, l := range lessons {
			if !v.sees(l, out.Release) {
				continue
			}

			if err := refs.loadClass(ctx, tx, l.ClassID); err != nil {
				return err
			}

			sl, err := refs.resolve(l)
			if err != nil {
				return err
			}

			if q.matches(sl) {
				out.Lessons = append(out.Lessons, sl)
			}
		}

		return addAvailability(ctx, tx, p, settings, out.Now, out.Lessons)
	})
	if err != nil {
		return Schedule{}, fmt.Errorf("schedule: %w", err)
	}

	return out, nil
}

// Lesson returns one lesson p may see; a zero p is a guest. A lesson that
// does not exist or is not visible fails alike with domain.ErrNotFound, so
// the response does not reveal hidden or unreleased lessons. Office and
// admin accounts also get the lesson's Admin details.
func (s *ScheduleService) Lesson(ctx context.Context, p Principal, id domain.LessonID) (ScheduledLesson, error) {
	now := s.clock.Now()

	var out ScheduledLesson

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		l, err := tx.Lessons().Get(ctx, id)
		if err != nil {
			return err
		}

		if !viewerOf(p, settings, loc, now).sees(l, ComputeReleaseWindow(settings, loc, now)) {
			return domain.ErrNotFound
		}

		// One count serves the free places and the office's details.
		active, err := tx.Bookings().CountActiveByLessons(ctx, []domain.LessonID{l.ID})
		if err != nil {
			return fmt.Errorf("active bookings: %w", err)
		}

		open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{l.ID})
		if err != nil {
			return fmt.Errorf("waiting lists: %w", err)
		}

		if out, err = s.resolveLesson(ctx, tx, p, settings, l, active[l.ID], heldPlaces(open, now)[l.ID]); err != nil {
			return err
		}

		lessons := []ScheduledLesson{out}
		if err := addFamily(ctx, tx, p, now, lessons, open); err != nil {
			return err
		}

		out = lessons[0]

		return nil
	})
	if err != nil {
		return ScheduledLesson{}, fmt.Errorf("lesson %v: %w", id, err)
	}

	return out, nil
}

// resolveLesson combines l, which has active bookings that are not
// canceled and held places of waiting-list invites, with the records it
// references, its free places and cutoffs and, for office and admin
// accounts, the office's details.
func (s *ScheduleService) resolveLesson(
	ctx context.Context, tx store.Tx, p Principal, settings domain.Settings, l domain.Lesson, active, held int,
) (ScheduledLesson, error) {
	if p.Role.AtLeast(domain.RoleOffice) {
		return adminLesson(ctx, tx, settings, l, active, held)
	}

	refs, err := loadRefsOf(ctx, tx, l)
	if err != nil {
		return ScheduledLesson{}, err
	}

	sl, err := refs.resolve(l)
	if err != nil {
		return ScheduledLesson{}, err
	}

	sl.setAvailability(settings, active, held)

	return sl, nil
}

// addAvailability sets the free places and cutoffs of the lessons at now
// and, for a signed-in p, whom of p's family each has booked and has on its
// waiting list (ADR 0009, ADR 0010). It counts the active bookings of all
// lessons with one query and reads their open waiting-list entries with
// another.
func addAvailability(
	ctx context.Context, tx store.Tx, p Principal, settings domain.Settings, now time.Time, lessons []ScheduledLesson,
) error {
	if len(lessons) == 0 {
		return nil
	}

	ids := lessonIDsOf(lessons)

	active, err := tx.Bookings().CountActiveByLessons(ctx, ids)
	if err != nil {
		return fmt.Errorf("active bookings: %w", err)
	}

	open, err := tx.Waitlist().ListOpenByLessons(ctx, ids)
	if err != nil {
		return fmt.Errorf("waiting lists: %w", err)
	}

	held := heldPlaces(open, now)
	for i := range lessons {
		id := lessons[i].Lesson.ID
		lessons[i].setAvailability(settings, active[id], held[id])
	}

	return addFamily(ctx, tx, p, now, lessons, open)
}

// addFamily sets, for a signed-in p, whom of p's family each lesson has
// booked (three queries for all lessons together) and has on its waiting
// list, from open, the open entries of the lessons, with their places.
func addFamily(
	ctx context.Context, tx store.Tx, p Principal, now time.Time, lessons []ScheduledLesson,
	open []domain.WaitlistEntry,
) error {
	if p.PersonID.IsZero() || len(lessons) == 0 {
		return nil
	}

	lessonIDs := lessonIDsOf(lessons)

	me, err := tx.Persons().Get(ctx, p.PersonID)
	if err != nil {
		return fmt.Errorf("person %v: %w", p.PersonID, err)
	}

	dependents, err := tx.Persons().ListDependents(ctx, p.PersonID)
	if err != nil {
		return fmt.Errorf("dependents: %w", err)
	}

	family := append([]domain.Person{me}, dependents...)
	persons := make([]domain.PersonID, len(family))
	rank := make(map[domain.PersonID]int, len(family))

	for i, f := range family {
		persons[i], rank[f.ID] = f.ID, i
	}

	booked, err := tx.Bookings().ListActiveByLessonsAndPersons(ctx, lessonIDs, persons)
	if err != nil {
		return fmt.Errorf("family bookings: %w", err)
	}

	slices.SortStableFunc(booked, func(a, b domain.Booking) int { return cmp.Compare(rank[a.PersonID], rank[b.PersonID]) })

	byLesson := map[domain.LessonID][]BookedPerson{}
	for _, b := range booked {
		person := family[rank[b.PersonID]]
		byLesson[b.LessonID] = append(byLesson[b.LessonID], BookedPerson{
			Person: person, Name: fullName(person), BookingID: b.ID,
		})
	}

	byLessonOpen := map[domain.LessonID][]domain.WaitlistEntry{}
	for _, e := range open {
		byLessonOpen[e.LessonID] = append(byLessonOpen[e.LessonID], e)
	}

	waiting := map[domain.LessonID][]WaitingPerson{}

	for _, e := range open {
		r, ok := rank[e.PersonID]
		if !ok {
			continue
		}

		person := family[r]
		status := e.Status

		if status == domain.WaitlistInvited && !holdsPlace(e, now) {
			status = domain.WaitlistExpired
		}

		waiting[e.LessonID] = append(waiting[e.LessonID], WaitingPerson{
			Person: person, Name: fullName(person),
			WaitlistPlace: WaitlistPlace{Entry: e, Position: position(byLessonOpen[e.LessonID], e.ID)}, Status: status,
		})
	}

	for i := range lessons {
		id := lessons[i].Lesson.ID
		lessons[i].BookedFor = byLesson[id]
		lessons[i].Waiting = waiting[id]
		slices.SortStableFunc(lessons[i].Waiting, func(a, b WaitingPerson) int {
			return cmp.Compare(rank[a.Person.ID], rank[b.Person.ID])
		})
	}

	return nil
}

// setAvailability sets the free places of sl, which has active bookings
// that are not canceled and places held by waiting-list invites, when
// booking it closes and when canceling it becomes late (ADR 0009, ADR
// 0010). It is the only place that sets them.
func (sl *ScheduledLesson) setAvailability(settings domain.Settings, active, held int) {
	sl.FreePlaces = max(0, sl.Lesson.Capacity-active-held)
	sl.BookingClosesAt = bookingClosesAt(settings, sl.Lesson)
	sl.LateCancelFrom = lateCancelFrom(settings, sl.Lesson)
}

// lessonIDsOf returns the IDs of the lessons.
func lessonIDsOf(lessons []ScheduledLesson) []domain.LessonID {
	out := make([]domain.LessonID, len(lessons))
	for i, sl := range lessons {
		out[i] = sl.Lesson.ID
	}

	return out
}

// lessonRefs holds the records lessons reference, by ID.
type lessonRefs struct {
	locations   map[domain.LocationID]domain.Location
	rooms       map[domain.RoomID]domain.Room
	staff       map[domain.StaffID]domain.Staff
	courseTypes map[domain.CourseTypeID]domain.CourseType
	classes     map[domain.ClassID]domain.Class
}

func newLessonRefs() lessonRefs {
	return lessonRefs{
		locations:   map[domain.LocationID]domain.Location{},
		rooms:       map[domain.RoomID]domain.Room{},
		staff:       map[domain.StaffID]domain.Staff{},
		courseTypes: map[domain.CourseTypeID]domain.CourseType{},
		classes:     map[domain.ClassID]domain.Class{},
	}
}

// loadRangeRefs loads the catalog and the classes of the terms that overlap
// the school dates [from, to), so the cost does not grow with past terms.
func loadRangeRefs(ctx context.Context, tx store.Tx, from, to domain.Date) (lessonRefs, error) {
	refs := newLessonRefs()

	locations, err := tx.Locations().List(ctx)
	if err != nil {
		return refs, fmt.Errorf("locations: %w", err)
	}

	for _, l := range locations {
		refs.locations[l.ID] = l
	}

	rooms, err := tx.Rooms().List(ctx, domain.LocationID{})
	if err != nil {
		return refs, fmt.Errorf("rooms: %w", err)
	}

	for _, r := range rooms {
		refs.rooms[r.ID] = r
	}

	staff, err := tx.Staff().List(ctx)
	if err != nil {
		return refs, fmt.Errorf("staff: %w", err)
	}

	for _, s := range staff {
		refs.staff[s.ID] = s
	}

	courseTypes, err := tx.CourseTypes().List(ctx)
	if err != nil {
		return refs, fmt.Errorf("course types: %w", err)
	}

	for _, c := range courseTypes {
		refs.courseTypes[c.ID] = c
	}

	terms, err := tx.Terms().List(ctx)
	if err != nil {
		return refs, fmt.Errorf("terms: %w", err)
	}

	for _, t := range terms {
		if t.EndsOn.Compare(from) < 0 || t.StartsOn.Compare(to) >= 0 {
			continue
		}

		classes, err := tx.Classes().ListByTerm(ctx, t.ID)
		if err != nil {
			return refs, fmt.Errorf("classes of term %v: %w", t.ID, err)
		}

		for _, c := range classes {
			refs.classes[c.ID] = c
		}
	}

	return refs, nil
}

// loadClass adds the class with the given ID unless it is zero (a one-off
// lesson) or already loaded, e.g. for a lesson outside its term's dates.
func (r lessonRefs) loadClass(ctx context.Context, tx store.Tx, id domain.ClassID) error {
	if id.IsZero() {
		return nil
	}

	if _, ok := r.classes[id]; ok {
		return nil
	}

	c, err := tx.Classes().Get(ctx, id)
	if err != nil {
		return fmt.Errorf("class %v: %w", id, refErr(err))
	}

	r.classes[id] = c

	return nil
}

// loadRefsOf loads the records that l references.
func loadRefsOf(ctx context.Context, tx store.Tx, l domain.Lesson) (lessonRefs, error) {
	refs := newLessonRefs()

	room, err := tx.Rooms().Get(ctx, l.RoomID)
	if err != nil {
		return refs, fmt.Errorf("room %v: %w", l.RoomID, refErr(err))
	}

	refs.rooms[room.ID] = room

	location, err := tx.Locations().Get(ctx, room.LocationID)
	if err != nil {
		return refs, fmt.Errorf("location %v: %w", room.LocationID, refErr(err))
	}

	refs.locations[location.ID] = location

	courseType, err := tx.CourseTypes().Get(ctx, l.CourseTypeID)
	if err != nil {
		return refs, fmt.Errorf("course type %v: %w", l.CourseTypeID, refErr(err))
	}

	refs.courseTypes[courseType.ID] = courseType

	for _, id := range []domain.StaffID{l.InstructorID, l.AccompanistID} {
		if id.IsZero() {
			continue
		}

		s, err := tx.Staff().Get(ctx, id)
		if err != nil {
			return refs, fmt.Errorf("staff %v: %w", id, refErr(err))
		}

		refs.staff[s.ID] = s
	}

	if !l.ClassID.IsZero() {
		c, err := tx.Classes().Get(ctx, l.ClassID)
		if err != nil {
			return refs, fmt.Errorf("class %v: %w", l.ClassID, refErr(err))
		}

		refs.classes[c.ID] = c
	}

	return refs, nil
}

// errBrokenReference reports a lesson that references a missing record.
// The stores enforce references, so this means broken data; it must not
// turn into a 404 for the lesson.
var errBrokenReference = errors.New("lesson references a missing record")

// refErr replaces domain.ErrNotFound for a referenced record with
// errBrokenReference; other errors pass unchanged.
func refErr(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return errBrokenReference
	}

	return err
}

// resolve combines l with the records it references.
func (r lessonRefs) resolve(l domain.Lesson) (ScheduledLesson, error) {
	sl := ScheduledLesson{Lesson: l}

	var ok bool

	if sl.Room, ok = r.rooms[l.RoomID]; !ok {
		return sl, fmt.Errorf("lesson %v: room %v: %w", l.ID, l.RoomID, errBrokenReference)
	}

	if sl.Location, ok = r.locations[sl.Room.LocationID]; !ok {
		return sl, fmt.Errorf("lesson %v: location %v: %w", l.ID, sl.Room.LocationID, errBrokenReference)
	}

	if sl.CourseType, ok = r.courseTypes[l.CourseTypeID]; !ok {
		return sl, fmt.Errorf("lesson %v: course type %v: %w", l.ID, l.CourseTypeID, errBrokenReference)
	}

	if sl.Instructor, ok = r.staff[l.InstructorID]; !ok {
		return sl, fmt.Errorf("lesson %v: instructor %v: %w", l.ID, l.InstructorID, errBrokenReference)
	}

	if !l.AccompanistID.IsZero() {
		if sl.Accompanist, ok = r.staff[l.AccompanistID]; !ok {
			return sl, fmt.Errorf("lesson %v: accompanist %v: %w", l.ID, l.AccompanistID, errBrokenReference)
		}
	}

	if !l.ClassID.IsZero() {
		c, ok := r.classes[l.ClassID]
		if !ok {
			return sl, fmt.Errorf("lesson %v: class %v: %w", l.ID, l.ClassID, errBrokenReference)
		}

		sl.Level = c.Level
	}

	return sl, nil
}
