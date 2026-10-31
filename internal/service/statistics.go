package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// MaxStatisticsDays is the longest range of school dates the statistics
// cover: a year, a leap year included.
const MaxStatisticsDays = 366

// TopParticipantsLimit is the most persons Statistics.TopParticipants
// lists.
const TopParticipantsLimit = 20

// weeklyStatisticsDays is the longest range whose periods are weeks; a
// longer one is split into months.
const weeklyStatisticsDays = 26 * 7

// ErrNoCurrentTerm reports that no term includes today, so the statistics
// of the current term have no range. It wraps domain.ErrNotFound.
var ErrNoCurrentTerm = fmt.Errorf("%w: no term includes today", domain.ErrNotFound)

// StatisticsRange names the range of school dates the statistics cover.
type StatisticsRange string

// The ranges of the statistics. The presets end today; a custom range
// may lie anywhere.
const (
	// StatisticsLast4Weeks: the current week and the three before it.
	StatisticsLast4Weeks StatisticsRange = "last4Weeks"
	// StatisticsLast12Weeks: the current week and the eleven before it.
	StatisticsLast12Weeks StatisticsRange = "last12Weeks"
	// StatisticsTerm: the term that includes today, from its start.
	StatisticsTerm StatisticsRange = "term"
	// StatisticsCustom: the query's From and To.
	StatisticsCustom StatisticsRange = "custom"
)

// StatisticsRanges returns the ranges of the statistics.
func StatisticsRanges() []StatisticsRange {
	return []StatisticsRange{StatisticsLast4Weeks, StatisticsLast12Weeks, StatisticsTerm, StatisticsCustom}
}

// Granularity is the length of the periods the statistics split their
// range into.
type Granularity string

// The granularities of the statistics' periods.
const (
	GranularityWeek  Granularity = "week"
	GranularityMonth Granularity = "month"
)

// StatisticsQuery selects the range of the statistics. From and To (both
// inclusive) are only for StatisticsCustom.
type StatisticsQuery struct {
	Range    StatisticsRange
	From, To domain.Date
}

// LessonCounts are the aggregates of a set of lessons that were not
// canceled: how many, their places and their bookings.
type LessonCounts struct {
	Lessons int
	// Capacity is the sum of the lessons' places.
	Capacity int
	// Bookings counts the active (not canceled) bookings, guests and
	// trials included.
	Bookings int
	// Attended and NoShows count the bookings whose attendance was taken.
	Attended, NoShows int
	// LateCancellations counts the bookings canceled after the cutoff.
	LateCancellations int
	// Trials and Guests count the active trial and guest bookings.
	Trials, Guests int
	// WaitlistEntries counts the waiting-list entries, ended ones
	// included: everyone who asked for a place in a full lesson.
	WaitlistEntries int
}

// Utilization is Bookings / Capacity, 0 without capacity.
func (c LessonCounts) Utilization() float64 {
	if c.Capacity == 0 {
		return 0
	}

	return float64(c.Bookings) / float64(c.Capacity)
}

// NoShowRate is NoShows / (Attended + NoShows), false where no attendance
// was taken.
func (c LessonCounts) NoShowRate() (float64, bool) {
	taken := c.Attended + c.NoShows
	if taken == 0 {
		return 0, false
	}

	return float64(c.NoShows) / float64(taken), true
}

// lessonFigures are what the stores count of one lesson.
type lessonFigures struct {
	bookings store.BookingCounts
	late     int
	waitlist int
}

func (c *LessonCounts) add(l domain.Lesson, f lessonFigures) {
	c.Lessons++
	c.Capacity += l.Capacity
	c.Bookings += f.bookings.Active
	c.Attended += f.bookings.Attended
	c.NoShows += f.bookings.NoShow
	c.LateCancellations += f.late
	c.Trials += f.bookings.Trials
	c.Guests += f.bookings.Guests
	c.WaitlistEntries += f.waitlist
}

// SalesCounts are the pass sales of a range, from the receipts issued in
// it: cancellations (Storno) count against the day they were issued.
type SalesCounts struct {
	// PassesSold is the number of sales less the number of cancellations.
	PassesSold int
	// Gross is the sum of the receipts' totals, cancellations negative.
	Gross domain.Money
}

// StatisticsPeriod is one week or month of the statistics' range, cut to
// the range.
type StatisticsPeriod struct {
	// Start and End are the first and the last school date (inclusive).
	Start, End domain.Date
	LessonCounts
	SalesCounts
}

// StatisticsTotals are the aggregates of the whole range.
type StatisticsTotals struct {
	LessonCounts
	SalesCounts
	// CanceledLessons counts the lessons the school canceled; they are
	// left out of every other figure.
	CanceledLessons int
	// ActiveMembers counts the persons with an active booking.
	ActiveMembers int
}

// ClassStatistics are the aggregates of the lessons of a class, or of a
// one-off lesson (a zero Class).
type ClassStatistics struct {
	Class domain.Class
	// CourseType is the course type of the latest lesson in the range,
	// which may differ from the class's: every lesson counts under its own
	// course type in CourseTypes.
	CourseType domain.CourseType
	// Lesson is the one-off lesson; zero for a class.
	Lesson domain.Lesson
	LessonCounts
}

// RoomStatistics are the aggregates of the lessons in a room.
type RoomStatistics struct {
	Room     domain.Room
	Location domain.Location
	LessonCounts
}

// CourseTypeStatistics are the aggregates of the lessons of a course type.
type CourseTypeStatistics struct {
	CourseType domain.CourseType
	LessonCounts
}

// InstructorStatistics are the aggregates of the lessons a staff member
// taught as the instructor.
type InstructorStatistics struct {
	Staff domain.Staff
	LessonCounts
}

// ParticipantStatistics are the bookings of a person in the lessons of the
// range that were not canceled.
type ParticipantStatistics struct {
	Person domain.Person
	// Bookings counts the person's active (not canceled) bookings, trials
	// included.
	Bookings int
	// Attended counts those marked as attended.
	Attended int
	// LateCancellations counts the bookings the person canceled after the
	// cutoff.
	LateCancellations int
}

// Statistics are the aggregates of the school's lessons, bookings and pass
// sales in a range of school dates. They count the persons and name only
// the top participants.
type Statistics struct {
	Timezone string
	Currency string
	// Today is the school date the presets end on.
	Today domain.Date
	Range StatisticsRange
	// From and To are the first and the last school date (inclusive).
	From, To domain.Date
	// Term is the term of StatisticsTerm; zero otherwise.
	Term        domain.Term
	Granularity Granularity
	Totals      StatisticsTotals
	Periods     []StatisticsPeriod
	// Classes are ordered by weekday (Monday first), start time, course
	// type name and ID, the one-off lessons after them by start.
	Classes []ClassStatistics
	// Rooms are ordered by location name, room name and ID.
	Rooms []RoomStatistics
	// CourseTypes are ordered by name and ID.
	CourseTypes []CourseTypeStatistics
	// Instructors are ordered by display name and ID.
	Instructors []InstructorStatistics
	// TopParticipants are the TopParticipantsLimit persons with the most
	// active bookings (at least one), ordered by them (descending), family
	// name, given name and ID. Merged (ADR 0015) and anonymized (ADR 0016)
	// persons are left out.
	TopParticipants []ParticipantStatistics
}

// StatisticsService computes the statistics of the school for the office
// (ADR 0005: office and admin). The figures are computed live from the
// lessons, bookings, waiting lists and receipts of the range.
type StatisticsService struct {
	store store.Store
	clock clock.Clock
}

// NewStatisticsService returns a StatisticsService.
func NewStatisticsService(st store.Store, clk clock.Clock) *StatisticsService {
	return &StatisticsService{store: st, clock: clk}
}

// Statistics returns the statistics of the range q selects, in the school
// time zone: a lesson belongs to the school date it starts on, a receipt
// to its issue date. A custom range longer than MaxStatisticsDays, or one
// that ends before it starts, fails with domain.ErrValidation; the term
// range without a term that includes today with ErrNoCurrentTerm.
//
// The cost does not grow with the data outside the range: one read each
// for the lessons, their booking counts, their late cancellations, their
// waiting-list counts, the active members, the bookings by person and
// their persons, and the receipts, besides the catalog.
func (s *StatisticsService) Statistics(ctx context.Context, q StatisticsQuery) (Statistics, error) {
	var out Statistics

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		out = Statistics{
			Timezone: settings.Timezone, Currency: settings.Currency, Range: q.Range,
			Today: domain.DateOf(s.clock.Now(), loc),
		}

		if err := out.resolveRange(ctx, tx, q); err != nil {
			return err
		}

		out.Granularity = GranularityWeek
		if out.From.DaysUntil(out.To) >= weeklyStatisticsDays {
			out.Granularity = GranularityMonth
		}

		out.Periods = periods(out.From, out.To, out.Granularity)

		return out.compute(ctx, tx, loc)
	})
	if err != nil {
		return Statistics{}, fmt.Errorf("statistics: %w", err)
	}

	return out, nil
}

// resolveRange sets From, To and Term from q.
func (st *Statistics) resolveRange(ctx context.Context, tx store.Tx, q StatisticsQuery) error {
	monday := weekOf(st.Today)

	switch q.Range {
	case StatisticsLast4Weeks:
		st.From, st.To = monday.AddDays(-3*7), st.Today
	case StatisticsLast12Weeks:
		st.From, st.To = monday.AddDays(-11*7), st.Today
	case StatisticsTerm:
		term, err := currentTerm(ctx, tx, st.Today)
		if err != nil {
			return err
		}

		st.Term, st.From, st.To = term, term.StartsOn, st.Today
		if term.EndsOn.Compare(st.To) < 0 {
			st.To = term.EndsOn
		}

		// A term of more than a year: its latest year.
		if st.From.DaysUntil(st.To) >= MaxStatisticsDays {
			st.From = st.To.AddDays(1 - MaxStatisticsDays)
		}
	case StatisticsCustom:
		switch {
		case !q.From.Valid() || !q.To.Valid():
			return fmt.Errorf("%w: a custom range needs valid from and to dates", domain.ErrValidation)
		case q.To.Compare(q.From) < 0:
			return fmt.Errorf("%w: to must not be before from", domain.ErrValidation)
		case q.From.DaysUntil(q.To) >= MaxStatisticsDays:
			return fmt.Errorf("%w: the range must not exceed %d days", domain.ErrValidation, MaxStatisticsDays)
		}

		st.From, st.To = q.From, q.To
	default:
		return fmt.Errorf("%w: unknown range %q", domain.ErrValidation, q.Range)
	}

	return nil
}

// currentTerm returns the term that includes today, the one that started
// last if terms overlap.
func currentTerm(ctx context.Context, tx store.Tx, today domain.Date) (domain.Term, error) {
	terms, err := tx.Terms().List(ctx)
	if err != nil {
		return domain.Term{}, fmt.Errorf("terms: %w", err)
	}

	// Ordered by start date: the last match started last.
	var found domain.Term

	for _, t := range terms {
		if t.StartsOn.Compare(today) <= 0 && t.EndsOn.Compare(today) >= 0 {
			found = t
		}
	}

	if found.ID.IsZero() {
		return domain.Term{}, ErrNoCurrentTerm
	}

	return found, nil
}

// periods splits the school dates from to to (inclusive) into weeks
// (Monday to Sunday) or calendar months, the first and the last cut to the
// range.
func periods(from, to domain.Date, g Granularity) []StatisticsPeriod {
	var out []StatisticsPeriod

	for start := from; start.Compare(to) <= 0; {
		var next domain.Date

		if g == GranularityMonth {
			next = domain.Date{Year: start.Year, Month: start.Month + 1, Day: 1}
			if start.Month == time.December {
				next = domain.Date{Year: start.Year + 1, Month: time.January, Day: 1}
			}
		} else {
			next = weekOf(start).AddDays(7)
		}

		end := next.AddDays(-1)
		if end.Compare(to) > 0 {
			end = to
		}

		out = append(out, StatisticsPeriod{Start: start, End: end})
		start = next
	}

	return out
}

// periodOf returns the index of the period that includes d, or -1.
func periodOf(ps []StatisticsPeriod, d domain.Date) int {
	i, found := slices.BinarySearchFunc(ps, d, func(p StatisticsPeriod, d domain.Date) int {
		switch {
		case p.End.Compare(d) < 0:
			return -1
		case p.Start.Compare(d) > 0:
			return 1
		default:
			return 0
		}
	})
	if !found {
		return -1
	}

	return i
}

// compute fills the figures of the range.
func (st *Statistics) compute(ctx context.Context, tx store.Tx, loc *time.Location) error {
	start := st.From.At(domain.TimeOfDay{}, loc)
	end := st.To.AddDays(1).At(domain.TimeOfDay{}, loc)

	all, err := tx.Lessons().ListRange(ctx, start, end)
	if err != nil {
		return fmt.Errorf("lessons: %w", err)
	}

	lessons := make([]domain.Lesson, 0, len(all))

	for _, l := range all {
		if l.Status == domain.LessonCanceled {
			st.Totals.CanceledLessons++

			continue
		}

		lessons = append(lessons, l)
	}

	ids := lessonIDs(lessons)

	counts, err := tx.Bookings().CountsByLessons(ctx, ids)
	if err != nil {
		return fmt.Errorf("booking counts: %w", err)
	}

	late, err := tx.Bookings().CountLateCanceledByLessons(ctx, ids)
	if err != nil {
		return fmt.Errorf("late cancellations: %w", err)
	}

	waitlist, err := tx.Waitlist().CountByLessons(ctx, ids)
	if err != nil {
		return fmt.Errorf("waiting-list counts: %w", err)
	}

	st.Totals.ActiveMembers, err = tx.Bookings().CountActivePersons(ctx, ids)
	if err != nil {
		return fmt.Errorf("active members: %w", err)
	}

	refs, err := loadRangeRefs(ctx, tx, st.From, st.To.AddDays(1))
	if err != nil {
		return err
	}

	groups := newStatisticsGroups()

	for _, l := range lessons {
		if err := refs.loadClass(ctx, tx, l.ClassID); err != nil {
			return err
		}

		sl, err := refs.resolve(l)
		if err != nil {
			return err
		}

		f := lessonFigures{bookings: counts[l.ID], late: late[l.ID], waitlist: waitlist[l.ID]}
		st.Totals.LessonCounts.add(l, f)

		if i := periodOf(st.Periods, domain.DateOf(l.StartsAt, loc)); i >= 0 {
			st.Periods[i].LessonCounts.add(l, f)
		}

		groups.add(sl, refs.classes[l.ClassID], f)
	}

	groups.fill(st)

	if err := st.addTopParticipants(ctx, tx, ids); err != nil {
		return err
	}

	return st.addSales(ctx, tx)
}

// addTopParticipants sets TopParticipants from the bookings by person in
// the lessons: one read for the counts, one for the persons who booked.
func (st *Statistics) addTopParticipants(ctx context.Context, tx store.Tx, lessons []domain.LessonID) error {
	counts, err := tx.Bookings().CountsByPersons(ctx, lessons)
	if err != nil {
		return fmt.Errorf("bookings by person: %w", err)
	}

	booked := make([]domain.PersonID, 0, len(counts))

	for id, c := range counts {
		if c.Active > 0 {
			booked = append(booked, id)
		}
	}

	persons, err := tx.Persons().ListByIDs(ctx, booked)
	if err != nil {
		return fmt.Errorf("participants: %w", err)
	}

	rows := make([]ParticipantStatistics, 0, len(persons))

	for _, p := range persons {
		if p.Merged() || p.Anonymized() {
			continue
		}

		c := counts[p.ID]
		rows = append(rows, ParticipantStatistics{
			Person: p, Bookings: c.Active, Attended: c.Attended, LateCancellations: c.LateCanceled,
		})
	}

	slices.SortFunc(rows, func(a, b ParticipantStatistics) int {
		return cmp.Or(
			cmp.Compare(b.Bookings, a.Bookings),
			cmp.Compare(a.Person.FamilyName, b.Person.FamilyName), cmp.Compare(a.Person.GivenName, b.Person.GivenName),
			compareIDs(a.Person.ID, b.Person.ID),
		)
	})

	st.TopParticipants = rows[:min(len(rows), TopParticipantsLimit)]

	return nil
}

// addSales adds the pass sales of the receipts issued in the range, in
// the school's currency.
func (st *Statistics) addSales(ctx context.Context, tx store.Tx) error {
	days, err := tx.Receipts().TotalsByDay(ctx, st.From, st.To)
	if err != nil {
		return fmt.Errorf("receipt totals: %w", err)
	}

	for _, d := range days {
		// The currency is fixed once a receipt exists (ADR 0012).
		if d.Currency != st.Currency {
			continue
		}

		sales := SalesCounts{PassesSold: d.Sales - d.Cancellations, Gross: d.Gross}
		st.Totals.SalesCounts.add(sales)

		if i := periodOf(st.Periods, d.Date); i >= 0 {
			st.Periods[i].SalesCounts.add(sales)
		}
	}

	return nil
}

func (c *SalesCounts) add(o SalesCounts) {
	c.PassesSold += o.PassesSold
	c.Gross += o.Gross
}

// statisticsGroups collect the lessons by class, room, course type and
// instructor.
type statisticsGroups struct {
	classes     map[domain.ClassID]*ClassStatistics
	oneOffs     []ClassStatistics
	rooms       map[domain.RoomID]*RoomStatistics
	courseTypes map[domain.CourseTypeID]*CourseTypeStatistics
	instructors map[domain.StaffID]*InstructorStatistics
}

func newStatisticsGroups() statisticsGroups {
	return statisticsGroups{
		classes:     map[domain.ClassID]*ClassStatistics{},
		rooms:       map[domain.RoomID]*RoomStatistics{},
		courseTypes: map[domain.CourseTypeID]*CourseTypeStatistics{},
		instructors: map[domain.StaffID]*InstructorStatistics{},
	}
}

// add counts the resolved lesson sl, of class (zero for a one-off), in
// its groups. Every group takes the lesson's own references, not its
// class's: a lesson that the office moved to another room or gave to
// another instructor counts there, and the course type of a class's row
// is the one of its latest lesson in the range, so a class row and the
// course-type row it adds to agree (lessons arrive by start).
func (g *statisticsGroups) add(sl ScheduledLesson, class domain.Class, f lessonFigures) {
	l := sl.Lesson

	if l.ClassID.IsZero() {
		one := ClassStatistics{CourseType: sl.CourseType, Lesson: l}
		one.add(l, f)
		g.oneOffs = append(g.oneOffs, one)
	} else {
		c, ok := g.classes[l.ClassID]
		if !ok {
			c = &ClassStatistics{Class: class}
			g.classes[l.ClassID] = c
		}

		c.CourseType = sl.CourseType
		c.add(l, f)
	}

	r, ok := g.rooms[sl.Room.ID]
	if !ok {
		r = &RoomStatistics{Room: sl.Room, Location: sl.Location}
		g.rooms[sl.Room.ID] = r
	}

	r.add(l, f)

	ct, ok := g.courseTypes[sl.CourseType.ID]
	if !ok {
		ct = &CourseTypeStatistics{CourseType: sl.CourseType}
		g.courseTypes[sl.CourseType.ID] = ct
	}

	ct.add(l, f)

	in, ok := g.instructors[sl.Instructor.ID]
	if !ok {
		in = &InstructorStatistics{Staff: sl.Instructor}
		g.instructors[sl.Instructor.ID] = in
	}

	in.add(l, f)
}

// fill sets the groups of st in their documented order.
func (g *statisticsGroups) fill(st *Statistics) {
	st.Classes = make([]ClassStatistics, 0, len(g.classes)+len(g.oneOffs))
	for _, c := range g.classes {
		st.Classes = append(st.Classes, *c)
	}

	slices.SortFunc(st.Classes, func(a, b ClassStatistics) int {
		return cmp.Or(
			cmp.Compare(mondayIndex(a.Class.Weekday), mondayIndex(b.Class.Weekday)),
			a.Class.StartTime.Compare(b.Class.StartTime),
			cmp.Compare(a.CourseType.Name, b.CourseType.Name),
			compareIDs(a.Class.ID, b.Class.ID),
		)
	})
	// The lessons arrive by start, so the one-offs are in order.
	st.Classes = append(st.Classes, g.oneOffs...)

	st.Rooms = sortedValues(g.rooms, func(a, b RoomStatistics) int {
		return cmp.Or(
			cmp.Compare(a.Location.Name, b.Location.Name), cmp.Compare(a.Room.Name, b.Room.Name),
			compareIDs(a.Room.ID, b.Room.ID),
		)
	})
	st.CourseTypes = sortedValues(g.courseTypes, func(a, b CourseTypeStatistics) int {
		return cmp.Or(cmp.Compare(a.CourseType.Name, b.CourseType.Name), compareIDs(a.CourseType.ID, b.CourseType.ID))
	})
	st.Instructors = sortedValues(g.instructors, func(a, b InstructorStatistics) int {
		return cmp.Or(cmp.Compare(a.Staff.DisplayName, b.Staff.DisplayName), compareIDs(a.Staff.ID, b.Staff.ID))
	})
}

func sortedValues[K comparable, V any](m map[K]*V, compare func(a, b V) int) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}

	slices.SortFunc(out, compare)

	return out
}

func compareIDs[I domain.AnyID](a, b I) int { return cmp.Compare(a.String(), b.String()) }
