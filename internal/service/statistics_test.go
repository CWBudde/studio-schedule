package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// The statistics tests run in Europe/Berlin (the demo settings). Sunday
// 2026-10-25 is the day the clocks go back from CEST (UTC+2) to CET
// (UTC+1); the test browser and machine zone is far away, which must not
// matter.

func statistics(t *testing.T, st store.Store, now string, q StatisticsQuery) Statistics {
	t.Helper()

	out, err := NewStatisticsService(st, clock.NewFixed(mustInstant(t, now))).Statistics(t.Context(), q)
	if err != nil {
		t.Fatalf("Statistics(%+v): %v", q, err)
	}

	return out
}

// statLesson stores a one-off lesson of the fixture from start (an
// instant) for an hour, with capacity places.
func (f *genFixture) statLesson(t *testing.T, start string, capacity int, status domain.LessonStatus) domain.Lesson {
	t.Helper()

	at := mustInstant(t, start)
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor,
		StartsAt: at, EndsAt: at.Add(time.Hour), Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic,
		Kind: domain.LessonWorkshop, Capacity: capacity,
	}
	if status == domain.LessonCanceled {
		l.Status, l.CanceledAt = domain.LessonCanceled, at.Add(-time.Hour)
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// statBooking stores a booking of a new person (a guest if guest) in l
// with the status; a canceled one is late if late.
func (f *genFixture) statBooking(t *testing.T, l domain.Lesson, status domain.BookingStatus, opts ...string) domain.Person {
	t.Helper()

	p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Pat", FamilyName: "Example"}
	b := domain.Booking{
		ID: ids.New[domain.BookingID](f.seq), LessonID: l.ID, PersonID: p.ID, Status: status,
		Source: domain.BookingSourceSelf, BookedAt: l.StartsAt.Add(-48 * time.Hour),
	}

	for _, o := range opts {
		switch o {
		case "late":
			b.Late = true
		case "trial":
			b.Trial = true
		case "guest":
			b.PersonID, b.GuestName = domain.PersonID{}, "Gina Guest"
		}
	}

	if status == domain.BookingCanceled {
		b.CanceledAt = l.StartsAt.Add(-time.Hour)
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		if !b.PersonID.IsZero() {
			if err := tx.Persons().Create(ctx, p); err != nil {
				return err
			}
		}

		return tx.Bookings().Create(ctx, b)
	})

	return p
}

// statReceipt stores a sale of gross (a cancellation of it if cancel)
// issued on the school date day.
func (f *genFixture) statReceipt(t *testing.T, seqNo int, day string, gross domain.Money, cancels domain.ReceiptID) domain.ReceiptID {
	t.Helper()

	d := mustDate(t, day)
	payer := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Paula", FamilyName: "Payer"}
	line := domain.NewReceiptLine(1, "Pass", 1, gross, 0, domain.TaxSmallBusiness)
	r := domain.Receipt{
		ID: ids.New[domain.ReceiptID](f.seq), Number: domain.ReceiptNumber("ST", d.Year, seqNo), Year: d.Year,
		Sequence: seqNo, Kind: domain.ReceiptSale, IssuedAt: d.At(domain.TimeOfDay{Hour: 12}, time.UTC), IssueDate: d,
		Locale: "en", Issuer: domain.ReceiptIssuer{Name: "Studio", Address: "Street 1", TaxID: "1/2/3"},
		TaxMode: domain.TaxSmallBusiness, PayerID: payer.ID, RecipientName: "Paula Payer",
		PaymentMethod: domain.PaymentCash, Currency: "EUR", Net: line.Net, VAT: line.VAT, Gross: line.Gross,
		Lines: []domain.ReceiptLine{line},
	}

	if !cancels.IsZero() {
		line = line.Negated()
		r.Kind, r.CancelsReceiptID, r.Lines = domain.ReceiptCancellation, cancels, []domain.ReceiptLine{line}
		r.Net, r.VAT, r.Gross = line.Net, line.VAT, line.Gross
	}

	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Persons().Create(ctx, payer), tx.Receipts().Create(ctx, r))
	})

	return r.ID
}

func periodDates(ps []StatisticsPeriod) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Start.String() + "/" + p.End.String()
	}

	return out
}

// TestStatisticsWeekEdges checks that lessons and receipts belong to the
// school date (Europe/Berlin) they start or are issued on, across the end
// of daylight saving time: Monday 00:15 CET is still Sunday in UTC, and
// Sunday 23:30 CET is still the week before.
func TestStatisticsWeekEdges(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	beforeRange := f.statLesson(t, "2026-10-18T21:45:00Z", 10, domain.LessonScheduled) // Sun 23:45 CEST
	firstMinute := f.statLesson(t, "2026-10-18T22:30:00Z", 10, domain.LessonScheduled) // Mon 00:30 CEST
	sundayNight := f.statLesson(t, "2026-10-25T22:30:00Z", 4, domain.LessonScheduled)  // Sun 23:30 CET
	nextMonday := f.statLesson(t, "2026-10-25T23:15:00Z", 5, domain.LessonScheduled)   // Mon 00:15 CET
	canceled := f.statLesson(t, "2026-10-20T16:00:00Z", 8, domain.LessonCanceled)

	f.statBooking(t, beforeRange, domain.BookingAttended)
	member := f.statBooking(t, firstMinute, domain.BookingAttended)
	f.statBooking(t, firstMinute, domain.BookingNoShow, "trial")
	f.statBooking(t, firstMinute, domain.BookingCanceled, "late")
	f.statBooking(t, firstMinute, domain.BookingCanceled) // in time: not counted
	f.statBooking(t, sundayNight, domain.BookingBooked, "guest")
	f.statBooking(t, nextMonday, domain.BookingAttended)
	f.statBooking(t, canceled, domain.BookingBooked)
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		// The member also books the next Monday: one active member.
		return errors.Join(
			tx.Bookings().Create(ctx, domain.Booking{
				ID: ids.New[domain.BookingID](f.seq), LessonID: nextMonday.ID, PersonID: member.ID,
				Status: domain.BookingNoShow, Source: domain.BookingSourceSelf, BookedAt: nextMonday.StartsAt.Add(-time.Hour),
			}),
			tx.Waitlist().Create(ctx, domain.WaitlistEntry{
				ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: sundayNight.ID, PersonID: member.ID,
				Status: domain.WaitlistWithdrawn, JoinedAt: sundayNight.StartsAt.Add(-2 * time.Hour),
			}),
		)
	})

	sale := f.statReceipt(t, 1, "2026-10-25", 9000, domain.ReceiptID{})
	f.statReceipt(t, 2, "2026-10-26", 9000, sale)
	f.statReceipt(t, 3, "2026-10-26", 4500, domain.ReceiptID{})
	f.statReceipt(t, 4, "2026-10-27", 4500, domain.ReceiptID{}) // after the range

	got := statistics(t, f.st, "2026-10-27T12:00:00Z", StatisticsQuery{
		Range: StatisticsCustom, From: mustDate(t, "2026-10-19"), To: mustDate(t, "2026-10-26"),
	})

	if got.Granularity != GranularityWeek || got.Timezone != "Europe/Berlin" || got.Currency != "EUR" ||
		got.Today != mustDate(t, "2026-10-27") {
		t.Errorf("statistics %+v", got)
	}

	wantStrings(t, periodDates(got.Periods), []string{"2026-10-19/2026-10-25", "2026-10-26/2026-10-26"})

	week1 := LessonCounts{
		Lessons: 2, Capacity: 14, Bookings: 3, Attended: 1, NoShows: 1, LateCancellations: 1, Trials: 1, Guests: 1,
		WaitlistEntries: 1,
	}
	week2 := LessonCounts{Lessons: 1, Capacity: 5, Bookings: 2, Attended: 1, NoShows: 1}

	if got.Periods[0].LessonCounts != week1 || got.Periods[1].LessonCounts != week2 {
		t.Errorf("periods %+v, want %+v and %+v", got.Periods, week1, week2)
	}

	if got.Periods[0].SalesCounts != (SalesCounts{PassesSold: 1, Gross: 9000}) ||
		got.Periods[1].SalesCounts != (SalesCounts{PassesSold: 0, Gross: -4500}) {
		t.Errorf("sales %+v / %+v", got.Periods[0].SalesCounts, got.Periods[1].SalesCounts)
	}

	wantTotals := StatisticsTotals{
		LessonCounts: LessonCounts{
			Lessons: 3, Capacity: 19, Bookings: 5, Attended: 2, NoShows: 2, LateCancellations: 1, Trials: 1, Guests: 1,
			WaitlistEntries: 1,
		},
		SalesCounts:     SalesCounts{PassesSold: 1, Gross: 4500},
		CanceledLessons: 1,
		ActiveMembers:   3, // the member, the trial's person and the next Monday's
	}
	if got.Totals != wantTotals {
		t.Errorf("totals %+v, want %+v", got.Totals, wantTotals)
	}

	if rate, ok := got.Totals.NoShowRate(); !ok || rate != 0.5 {
		t.Errorf("no-show rate %v, %v", rate, ok)
	}

	if u := got.Totals.Utilization(); u != 5.0/19 {
		t.Errorf("utilization %v", u)
	}

	// The one-off lessons by start, each its own row; one room, course type
	// and instructor.
	if len(got.Classes) != 3 || got.Classes[0].Lesson.ID != firstMinute.ID || got.Classes[2].Lesson.ID != nextMonday.ID ||
		len(got.Rooms) != 1 || len(got.CourseTypes) != 1 || len(got.Instructors) != 1 ||
		got.Rooms[0].LessonCounts != wantTotals.LessonCounts || got.Instructors[0].Staff.ID != f.instructor ||
		got.Rooms[0].Location.Name != "Studio Nowhere" {
		t.Errorf("groups %+v", got)
	}
}

func wantStrings(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

// TestStatisticsRanges checks the presets, which end today, the term
// range, and the limits of a custom range.
func TestStatisticsRanges(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	// Sunday 2026-10-25, 00:30 in Berlin, still Saturday in UTC.
	const now = "2026-10-24T22:30:00Z"

	for _, tc := range []struct {
		q          StatisticsQuery
		from, to   string
		g          Granularity
		periods    int
		firstLast  string
		termRanged bool
	}{
		{q: StatisticsQuery{Range: StatisticsLast4Weeks}, from: "2026-09-28", to: "2026-10-25", g: GranularityWeek, periods: 4},
		{q: StatisticsQuery{Range: StatisticsLast12Weeks}, from: "2026-08-03", to: "2026-10-25", g: GranularityWeek, periods: 12},
		{
			q: StatisticsQuery{Range: StatisticsTerm}, from: "2026-09-01", to: "2026-10-25", g: GranularityWeek, periods: 8,
			firstLast: "2026-09-01/2026-09-06", termRanged: true,
		},
		{
			// 26 weeks: still weeks.
			q:    StatisticsQuery{Range: StatisticsCustom, From: mustDate(t, "2026-01-05"), To: mustDate(t, "2026-07-05")},
			from: "2026-01-05", to: "2026-07-05", g: GranularityWeek, periods: 26,
		},
		{
			// A day more: months, cut to the range.
			q:    StatisticsQuery{Range: StatisticsCustom, From: mustDate(t, "2026-01-05"), To: mustDate(t, "2026-07-06")},
			from: "2026-01-05", to: "2026-07-06", g: GranularityMonth, periods: 7, firstLast: "2026-01-05/2026-01-31",
		},
		{
			// 366 days, over the turn of the year.
			q:    StatisticsQuery{Range: StatisticsCustom, From: mustDate(t, "2026-03-15"), To: mustDate(t, "2027-03-15")},
			from: "2026-03-15", to: "2027-03-15", g: GranularityMonth, periods: 13, firstLast: "2026-03-15/2026-03-31",
		},
	} {
		got := statistics(t, f.st, now, tc.q)

		if got.From.String() != tc.from || got.To.String() != tc.to || got.Granularity != tc.g ||
			len(got.Periods) != tc.periods || got.Range != tc.q.Range || got.Today != mustDate(t, "2026-10-25") {
			t.Errorf("%+v: %v to %v by %s in %d periods", tc.q, got.From, got.To, got.Granularity, len(got.Periods))

			continue
		}

		if tc.firstLast != "" && periodDates(got.Periods)[0] != tc.firstLast {
			t.Errorf("%+v: first period %v", tc.q, periodDates(got.Periods)[0])
		}

		last := got.Periods[len(got.Periods)-1]
		if last.End != got.To || got.Periods[0].Start != got.From {
			t.Errorf("%+v: periods %v", tc.q, periodDates(got.Periods))
		}

		if (got.Term.ID == f.term.ID) != tc.termRanged {
			t.Errorf("%+v: term %+v", tc.q, got.Term)
		}
	}

	svc := NewStatisticsService(f.st, clock.NewFixed(mustInstant(t, now)))

	for _, q := range []StatisticsQuery{
		{Range: StatisticsCustom, From: mustDate(t, "2026-03-15"), To: mustDate(t, "2027-03-16")}, // 367 days
		{Range: StatisticsCustom, From: mustDate(t, "2026-03-15"), To: mustDate(t, "2026-03-14")},
		{Range: StatisticsCustom, From: mustDate(t, "2026-03-15")},
		{Range: "lastYear"},
	} {
		if _, err := svc.Statistics(t.Context(), q); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%+v: %v, want a validation error", q, err)
		}
	}

	// Between terms the term range has nothing to show.
	later := NewStatisticsService(f.st, clock.NewFixed(mustInstant(t, "2027-08-15T10:00:00Z")))
	if _, err := later.Statistics(t.Context(), StatisticsQuery{Range: StatisticsTerm}); !errors.Is(err, ErrNoCurrentTerm) ||
		!errors.Is(err, domain.ErrNotFound) {
		t.Errorf("term between terms: %v", err)
	}

	// Without settings: no school.
	if _, err := NewStatisticsService(memory.New(), clock.NewFixed(mustInstant(t, now))).Statistics(
		t.Context(), StatisticsQuery{Range: StatisticsLast4Weeks},
	); !errors.Is(err, ErrNoSchool) {
		t.Errorf("no school: %v", err)
	}
}

// TestStatisticsDemo checks the statistics of the demo's last four weeks
// against its data set: the member bookings with their attendance, the
// drop-ins and the pass sales, the ballet lessons whose attendance nobody
// took, and the class, room, course type and instructor rows adding up.
func TestStatisticsDemo(t *testing.T) {
	t.Parallel()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	d := seed.Demo()
	loc, err := time.LoadLocation(d.Settings.Timezone)
	if err != nil {
		t.Fatal(err)
	}

	now := seed.FixedDemoAnchor.At(domain.TimeOfDay{Hour: 22}, loc).Format(time.RFC3339)
	got := statistics(t, st, now, StatisticsQuery{Range: StatisticsLast4Weeks})
	want := demoTotals(d, loc, got.From, got.To)

	if got.Totals != want {
		t.Errorf("totals\n got %+v\nwant %+v", got.Totals, want)
	}

	if want.Lessons == 0 || want.Attended == 0 || want.NoShows == 0 || want.LateCancellations == 0 ||
		want.Trials == 0 || want.Guests == 0 || want.PassesSold == 0 || want.ActiveMembers == 0 {
		t.Errorf("the demo lacks figures: %+v", want)
	}

	// The demo's history starts with its term: every week from there on has
	// lessons with attendance taken.
	for _, p := range got.Periods {
		if p.End.Compare(d.Terms[0].StartsOn) >= 0 && (p.Lessons == 0 || p.Attended == 0) {
			t.Errorf("period %v–%v without lessons or attendance: %+v", p.Start, p.End, p.LessonCounts)
		}
	}

	var periods StatisticsTotals
	for _, p := range got.Periods {
		periods.addCounts(p.LessonCounts)
		periods.SalesCounts.add(p.SalesCounts)
	}

	if periods.LessonCounts != want.LessonCounts || periods.SalesCounts != want.SalesCounts {
		t.Errorf("periods add up to %+v, want %+v", periods, want)
	}

	var classes, rooms, courseTypes, instructors LessonCounts
	for _, c := range got.Classes {
		classes.addCounts(c.LessonCounts)
	}

	for _, r := range got.Rooms {
		rooms.addCounts(r.LessonCounts)
	}

	for _, c := range got.CourseTypes {
		courseTypes.addCounts(c.LessonCounts)
	}

	for _, i := range got.Instructors {
		instructors.addCounts(i.LessonCounts)
	}

	for name, sum := range map[string]LessonCounts{
		"classes": classes, "rooms": rooms, "course types": courseTypes, "instructors": instructors,
	} {
		if sum != want.LessonCounts {
			t.Errorf("%s add up to %+v, want %+v", name, sum, want.LessonCounts)
		}
	}

	// Classes from Monday, by start time.
	for i := 1; i < len(got.Classes); i++ {
		a, b := got.Classes[i-1].Class, got.Classes[i].Class
		if !b.ID.IsZero() && (mondayIndex(a.Weekday) > mondayIndex(b.Weekday) ||
			a.Weekday == b.Weekday && a.StartTime.Compare(b.StartTime) > 0) {
			t.Errorf("class %v before %v", a, b)
		}
	}

	// Every class row carries its class's audience, a one-off row none.
	spans := 0

	for _, c := range got.Classes {
		a := c.Audience
		if c.Class.ID.IsZero() {
			if a != (Audience{}) {
				t.Errorf("one-off %v has the audience %+v", c.Lesson.ID, a)
			}

			continue
		}

		ends := []domain.LevelID{a.Low.ID, a.High.ID}
		if !slices.Contains(ends, c.Class.Levels.From) || !slices.Contains(ends, c.Class.Levels.To) ||
			a.AllLevels != c.Class.AllLevels || a.AgeFrom != c.Class.AgeFrom || a.AgeTo != c.Class.AgeTo {
			t.Errorf("class %v has the audience %+v", c.Class, a)
		}

		if a.Low.Name == "Beginner" && a.High.Name == "Intermediate" {
			spans++
		}
	}

	if spans != 1 {
		t.Errorf("%d rows for the Beginner–Intermediate class, want 1", spans)
	}
}

// addCounts adds o to c, for the tests' sums.
func (c *LessonCounts) addCounts(o LessonCounts) {
	c.Lessons += o.Lessons
	c.Capacity += o.Capacity
	c.Bookings += o.Bookings
	c.Attended += o.Attended
	c.NoShows += o.NoShows
	c.LateCancellations += o.LateCancellations
	c.Trials += o.Trials
	c.Guests += o.Guests
	c.WaitlistEntries += o.WaitlistEntries
}

// demoTotals computes the totals of the school dates from to to of the demo
// data set d straight from its records.
func demoTotals(d seed.Dataset, loc *time.Location, from, to domain.Date) StatisticsTotals {
	var out StatisticsTotals

	lessons := map[domain.LessonID]bool{}

	for _, l := range d.Lessons {
		day := domain.DateOf(l.StartsAt, loc)
		if day.Compare(from) < 0 || day.Compare(to) > 0 {
			continue
		}

		if l.Status == domain.LessonCanceled {
			out.CanceledLessons++

			continue
		}

		lessons[l.ID] = true
		out.Lessons++
		out.Capacity += l.Capacity
	}

	members := map[domain.PersonID]bool{}

	for _, b := range d.Bookings {
		if !lessons[b.LessonID] {
			continue
		}

		switch b.Status {
		case domain.BookingCanceled:
			if b.Late {
				out.LateCancellations++
			}

			continue
		case domain.BookingAttended:
			out.Attended++
		case domain.BookingNoShow:
			out.NoShows++
		case domain.BookingBooked:
		}

		out.Bookings++

		if b.Trial {
			out.Trials++
		}

		if b.PersonID.IsZero() {
			out.Guests++
		} else {
			members[b.PersonID] = true
		}
	}

	out.ActiveMembers = len(members)

	for _, e := range d.WaitlistEntries {
		if lessons[e.LessonID] {
			out.WaitlistEntries++
		}
	}

	for _, r := range d.Receipts {
		if r.IssueDate.Compare(from) < 0 || r.IssueDate.Compare(to) > 0 {
			continue
		}

		out.Gross += r.Gross
		if r.Kind == domain.ReceiptCancellation {
			out.PassesSold--
		} else {
			out.PassesSold++
		}
	}

	return out
}

// TestStatisticsClassFollowsItsLessons checks that a class's lessons count
// under their own course type, not the class's: the class row names the
// course type of its latest lesson in the range, and the course-type rows
// split its lessons.
func TestStatisticsClassFollowsItsLessons(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	c := f.addClass(t, time.Tuesday, domain.TimeOfDay{Hour: 17})
	jazz := domain.CourseType{ID: ids.New[domain.CourseTypeID](f.seq), Name: "Imaginary Jazz"}

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}

	lesson := func(slot string, courseType domain.CourseTypeID) domain.Lesson {
		d := mustDate(t, slot)
		start := d.At(c.StartTime, loc)

		return domain.Lesson{
			ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: d, CourseTypeID: courseType,
			RoomID: c.RoomID, InstructorID: c.InstructorID, AccompanistID: c.AccompanistID,
			StartsAt: start, EndsAt: start.Add(c.Duration), Status: domain.LessonScheduled,
			Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: c.Capacity,
		}
	}
	// The office changed the course type of the later lesson.
	before, after := lesson("2026-10-20", f.courseType), lesson("2026-10-27", jazz.ID)
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.CourseTypes().Create(ctx, jazz), tx.Lessons().Create(ctx, before), tx.Lessons().Create(ctx, after),
		)
	})

	got := statistics(t, f.st, "2026-10-29T12:00:00Z", StatisticsQuery{
		Range: StatisticsCustom, From: mustDate(t, "2026-10-19"), To: mustDate(t, "2026-11-01"),
	})

	if len(got.Classes) != 1 || got.Classes[0].Class.ID != c.ID || got.Classes[0].Lessons != 2 ||
		got.Classes[0].CourseType.ID != jazz.ID {
		t.Errorf("classes %+v, want the class with 2 lessons as %s", got.Classes, jazz.Name)
	}

	if len(got.CourseTypes) != 2 || got.CourseTypes[0].CourseType.ID != f.courseType ||
		got.CourseTypes[0].Lessons != 1 || got.CourseTypes[1].CourseType.ID != jazz.ID || got.CourseTypes[1].Lessons != 1 {
		t.Errorf("course types %+v, want one lesson each", got.CourseTypes)
	}
}

// TestStatisticsTopParticipants checks the ranking of the persons by their
// active bookings in the range: the cut after TopParticipantsLimit, ties by
// name and ID, attendance and late cancellations beside, and who is left
// out (guests, merged and anonymized persons, canceled bookings, canceled
// lessons and lessons outside the range).
func TestStatisticsTopParticipants(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	before := f.statLesson(t, "2026-10-18T21:45:00Z", 99, domain.LessonScheduled) // Sun 23:45 CEST, the week before
	in1 := f.statLesson(t, "2026-10-19T16:00:00Z", 99, domain.LessonScheduled)
	in2 := f.statLesson(t, "2026-10-21T16:00:00Z", 99, domain.LessonScheduled)
	in3 := f.statLesson(t, "2026-10-25T22:30:00Z", 99, domain.LessonScheduled)   // Sun 23:30 CET, the last minutes
	after := f.statLesson(t, "2026-10-25T23:15:00Z", 99, domain.LessonScheduled) // Mon 00:15 CET, the week after
	canceledLesson := f.statLesson(t, "2026-10-22T16:00:00Z", 99, domain.LessonCanceled)

	var (
		persons  []domain.Person
		bookings []domain.Booking
	)

	person := func(given, family string) domain.Person {
		p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: given, FamilyName: family}
		persons = append(persons, p)

		return p
	}

	book := func(l domain.Lesson, p domain.PersonID, status domain.BookingStatus, opts ...string) {
		b := domain.Booking{
			ID: ids.New[domain.BookingID](f.seq), LessonID: l.ID, PersonID: p, Status: status,
			Source: domain.BookingSourceSelf, BookedAt: l.StartsAt.Add(-48 * time.Hour),
		}
		if status == domain.BookingCanceled {
			b.CanceledAt = l.StartsAt.Add(-time.Hour)
		}

		for _, o := range opts {
			switch o {
			case "late":
				b.Late = true
			case "trial":
				b.Trial = true
			}
		}

		if p.IsZero() {
			b.GuestName = "Gina Guest"
		}

		bookings = append(bookings, b)
	}

	ava := person("Ava", "Alpha")
	book(in1, ava.ID, domain.BookingAttended)
	book(in2, ava.ID, domain.BookingAttended)
	book(in3, ava.ID, domain.BookingBooked)

	bea := person("Bea", "Beta")
	book(in1, bea.ID, domain.BookingNoShow)
	book(in2, bea.ID, domain.BookingBooked, "trial")
	book(in3, bea.ID, domain.BookingCanceled, "late")
	book(before, bea.ID, domain.BookingAttended)
	book(after, bea.ID, domain.BookingBooked)
	book(canceledLesson, bea.ID, domain.BookingBooked)

	gus := person("Gus", "Gamma")
	book(in1, gus.ID, domain.BookingAttended)
	book(in3, gus.ID, domain.BookingBooked)
	book(in2, gus.ID, domain.BookingCanceled) // in time: not counted

	// Twins with the same name: their IDs order them.
	twin1, twin2 := person("Kim", "Twin"), person("Kim", "Twin")
	book(in2, twin2.ID, domain.BookingBooked)
	book(in2, twin1.ID, domain.BookingBooked)

	// Fillers with one booking each, more than the list holds.
	fillers := make([]domain.Person, 20)
	for i := range fillers {
		fillers[i] = person(fmt.Sprintf("Fil %02d", 19-i), "Zulu")
		book(in1, fillers[i].ID, domain.BookingBooked)
	}

	// No active booking: not listed; guests neither.
	book(in1, person("Lou", "Late").ID, domain.BookingCanceled, "late")
	book(in1, person("Ian", "Intime").ID, domain.BookingCanceled)
	book(in1, domain.PersonID{}, domain.BookingBooked)
	book(in2, domain.PersonID{}, domain.BookingBooked)

	// The most bookings, but merged into Ava or anonymized: left out.
	merged := person("Mo", "Merged")
	persons[len(persons)-1].MergedInto = ava.ID
	anonymized := person(domain.AnonymizedGivenName, domain.AnonymizedFamilyName)
	persons[len(persons)-1].AnonymizedAt = mustInstant(t, "2026-10-26T09:00:00Z")

	for _, p := range []domain.Person{merged, anonymized} {
		for _, l := range []domain.Lesson{in1, in2, in3} {
			book(l, p.ID, domain.BookingAttended)
		}
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		for _, p := range persons {
			if err := tx.Persons().Create(ctx, p); err != nil {
				return err
			}
		}

		for _, b := range bookings {
			if err := tx.Bookings().Create(ctx, b); err != nil {
				return err
			}
		}

		return nil
	})

	got := statistics(t, f.st, "2026-10-27T12:00:00Z", StatisticsQuery{
		Range: StatisticsCustom, From: mustDate(t, "2026-10-19"), To: mustDate(t, "2026-10-25"),
	})

	row := func(p domain.Person, booked, attended, late int) string {
		return fmt.Sprintf("%s %s %s %d/%d/%d", p.ID, p.GivenName, p.FamilyName, booked, attended, late)
	}

	if compareIDs(twin2.ID, twin1.ID) < 0 {
		twin1, twin2 = twin2, twin1
	}

	want := []string{row(ava, 3, 2, 0), row(bea, 2, 0, 1), row(gus, 2, 1, 0), row(twin1, 1, 0, 0), row(twin2, 1, 0, 0)}
	// By given name: the fillers were created from Fil 19 down.
	for i := range TopParticipantsLimit - len(want) {
		want = append(want, row(fillers[19-i], 1, 0, 0))
	}

	gotRows := make([]string, len(got.TopParticipants))
	for i, p := range got.TopParticipants {
		gotRows[i] = row(p.Person, p.Bookings, p.Attended, p.LateCancellations)
	}

	wantStrings(t, gotRows, want)

	// A range without bookings lists nobody.
	empty := statistics(t, f.st, "2026-10-27T12:00:00Z", StatisticsQuery{
		Range: StatisticsCustom, From: mustDate(t, "2026-11-02"), To: mustDate(t, "2026-11-08"),
	})
	if len(empty.TopParticipants) != 0 {
		t.Errorf("top participants of an empty range: %+v", empty.TopParticipants)
	}
}
