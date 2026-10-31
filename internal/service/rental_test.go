package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Room rentals (ADR 0007, amendment 2026-10-31) run on the booking fixture
// at bookingNow. The rental is on Friday 2026-10-23, 16:00 to 18:00 in
// Berlin, in the fixture's room.
const rentalStart = "2026-10-23T14:00:00Z"

// rental stores a room rental of two hours from start.
func (f *bookingFixture) rental(t *testing.T, start string) domain.Lesson {
	t.Helper()

	at := mustInstant(t, start)
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), CourseTypeID: f.courseType, RoomID: f.room,
		StartsAt: at, EndsAt: at.Add(2 * time.Hour), Status: domain.LessonScheduled,
		Visibility: domain.VisibilityHidden, Kind: domain.LessonRental, Renter: "Copperleaf Theater Group",
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// rentalData is the rental of rentalStart as the office enters it.
func (f *bookingFixture) rentalData() LessonData {
	return LessonData{
		Date: domain.Date{Year: 2026, Month: time.October, Day: 23}, StartTime: domain.TimeOfDay{Hour: 16},
		Duration: 2 * time.Hour, RoomID: f.room, Visibility: domain.VisibilityHidden, Kind: domain.LessonRental,
		Renter: "  Copperleaf Theater Group ",
	}
}

func TestRentalVisibility(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	rental := f.rental(t, rentalStart)
	workshop := f.oneOff(t, "2026-10-23T10:00:00Z", 5, domain.VisibilityPublic, domain.LessonWorkshop)
	svc := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, bookingNow)))
	q := ScheduleQuery{From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-28")}
	instructor := Principal{AccountID: ids.New[domain.AccountID](f.seq), Role: domain.RoleInstructor}

	for name, p := range map[string]Principal{"guest": {}, "member": f.principal(t, f.anna)} {
		s, err := svc.Schedule(ctx, p, q)
		if err != nil {
			t.Fatal(err)
		}

		if len(s.Lessons) != 1 || s.Lessons[0].Lesson.ID != workshop.ID {
			t.Errorf("%s sees %+v, want only the workshop", name, s.Lessons)
		}

		if _, err := svc.Lesson(ctx, p, rental.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s gets the rental: %v", name, err)
		}
	}

	for name, p := range map[string]Principal{"office": f.principal(t, f.olga), "instructor": instructor} {
		s, err := svc.Schedule(ctx, p, q)
		if err != nil {
			t.Fatal(err)
		}

		if len(s.Lessons) != 2 || s.Lessons[1].Lesson.ID != rental.ID {
			t.Fatalf("%s sees %+v, want the workshop and the rental", name, s.Lessons)
		}

		got := s.Lessons[1]
		if !got.Instructor.ID.IsZero() || got.Lesson.Renter != rental.Renter || got.FreePlaces != 0 ||
			got.Room.ID != f.room {
			t.Errorf("%s: rental %+v", name, got)
		}

		if _, err := svc.Lesson(ctx, p, rental.ID); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// The instructor view has no participants of a rental to show.
	teach, err := NewTeachService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, nil).
		Lessons(ctx, f.principal(t, f.olga), TeachQuery{From: q.From, To: q.To})
	if err != nil {
		t.Fatal(err)
	}

	if len(teach.Lessons) != 1 || teach.Lessons[0].Lesson.ID != workshop.ID {
		t.Errorf("teach lessons %+v, want only the workshop", teach.Lessons)
	}

	// Nor do the statistics count it.
	stats, err := NewStatisticsService(f.st, clock.NewFixed(mustInstant(t, bookingNow))).
		Statistics(ctx, StatisticsQuery{Range: StatisticsCustom, From: q.From, To: mustDate(t, "2026-10-27")})
	if err != nil {
		t.Fatal(err)
	}

	if stats.Totals.Lessons != 1 || stats.Totals.Capacity != 5 || len(stats.Rooms) != 1 {
		t.Errorf("statistics totals %+v, rooms %+v; want only the workshop", stats.Totals, stats.Rooms)
	}
}

func TestRentalNotBookable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	rental := f.rental(t, rentalStart)
	olga := f.principal(t, f.olga)

	// A member does not see it; staff, who do, cannot book it.
	if _, err := f.bookingService(t, bookingNow).Book(ctx, f.principal(t, f.anna), rental.ID, f.anna.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("member books a rental: %v", err)
	}

	_, err := f.bookingService(t, bookingNow).Book(ctx, olga, rental.ID, f.olga.ID)
	wantClosed(t, err, ClosedRental)

	_, err = NewWaitlistService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(newTestRenderer(t))).
		Join(ctx, olga, rental.ID, f.olga.ID, false)
	wantClosed(t, err, ClosedRental)

	participants := NewParticipantService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, testMails(newTestRenderer(t)))
	_, err = participants.Add(ctx, olga, rental.ID, NewParticipant{GuestName: "Gina Guest"})
	wantClosed(t, err, ClosedRental)

	// Every booking passes the ledger, which refuses one too.
	err = f.st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return createBooking(ctx, tx, charges{}, mustInstant(t, bookingNow), rental, domain.Booking{
			ID: ids.New[domain.BookingID](f.seq), LessonID: rental.ID, PersonID: f.anna.ID,
			Status: domain.BookingBooked, Source: domain.BookingSourceSelf, BookedAt: mustInstant(t, bookingNow),
		})
	})
	wantClosed(t, err, ClosedRental)

	if offerable(bookingSettings(), mustInstant(t, bookingNow), rental) {
		t.Error("a rental takes waiting-list invites")
	}

	// An announcement cannot address its participants: it has none.
	_, err = NewAnnouncementService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, newTestRenderer(t), nil).
		Create(ctx, olga, AnnouncementInput{
			AnnouncementAudience: AnnouncementAudience{Audience: domain.AudienceLesson, LessonID: rental.ID},
			Title:                "Rehearsal", Body: "Bring your scripts.",
		})
	wantFieldError(t, err, "lessonId")
}

func TestRentalAdministration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.lessonService(t, lessonNow)

	rental, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.rentalData(), CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	got := f.lesson(t, rental.Lesson.ID)
	if got.Kind != domain.LessonRental || got.Renter != "Copperleaf Theater Group" || got.Capacity != 0 ||
		!got.InstructorID.IsZero() || !got.StartsAt.Equal(mustInstant(t, rentalStart)) {
		t.Fatalf("created %+v", got)
	}

	t.Run("invalid", func(t *testing.T) {
		for field, mutate := range map[string]func(*LessonData){
			"instructorId": func(d *LessonData) { d.InstructorID = f.instructor },
			"capacity":     func(d *LessonData) { d.Capacity = 4 },
			"visibility":   func(d *LessonData) { d.Visibility = domain.VisibilityPublic },
			"renter":       func(d *LessonData) { d.Renter = " " },
		} {
			d := f.rentalData()
			d.StartTime, d.ConfirmRoomOverlap = domain.TimeOfDay{Hour: 9}, true
			mutate(&d)

			_, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType})
			wantFieldError(t, err, field)
		}
	})

	t.Run("a lesson in the rented room", func(t *testing.T) {
		// A workshop that overlaps the rental's last hour.
		d := f.lessonData()
		d.Date, d.StartTime = domain.Date{Year: 2026, Month: time.October, Day: 23}, domain.TimeOfDay{Hour: 17}

		_, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType})

		var taken *RoomTakenError
		if !errors.As(err, &taken) || taken.Lesson.ID != rental.Lesson.ID {
			t.Fatalf("want RoomTakenError naming the rental, got %v", err)
		}
	})

	t.Run("a rental in a taken room", func(t *testing.T) {
		workshop := f.oneOff(t, "2026-10-24T08:00:00Z", 5, domain.VisibilityPublic, domain.LessonWorkshop)
		d := f.rentalData()
		d.Date, d.StartTime = domain.Date{Year: 2026, Month: time.October, Day: 24}, domain.TimeOfDay{Hour: 9}

		_, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType})

		var taken *RoomTakenError
		if !errors.As(err, &taken) || taken.Lesson.ID != workshop.ID {
			t.Fatalf("want RoomTakenError naming the workshop, got %v", err)
		}
	})

	t.Run("a booked lesson stays one", func(t *testing.T) {
		booked := f.oneOff(t, "2026-10-26T08:00:00Z", 5, domain.VisibilityPublic, domain.LessonWorkshop)
		f.bookFor(t, booked, f.mia)

		d := dataOf(t, booked)
		d.Kind, d.InstructorID, d.Capacity = domain.LessonRental, domain.StaffID{}, 0
		d.Visibility, d.Renter = domain.VisibilityHidden, "Copperleaf Theater Group"

		_, err := svc.UpdateLesson(ctx, booked.ID, d)
		wantFieldError(t, err, "kind")

		// Without bookings it may become a rental, and back.
		free := f.oneOff(t, "2026-10-27T08:00:00Z", 5, domain.VisibilityPublic, domain.LessonWorkshop)
		d = dataOf(t, free)
		d.Kind, d.InstructorID, d.Capacity = domain.LessonRental, domain.StaffID{}, 0
		d.Visibility, d.Renter = domain.VisibilityHidden, "Copperleaf Theater Group"

		if _, err := svc.UpdateLesson(ctx, free.ID, d); err != nil {
			t.Fatal(err)
		}

		if _, err := svc.UpdateLesson(ctx, free.ID, dataOf(t, free)); err != nil {
			t.Fatal(err)
		}

		if got := f.lesson(t, free.ID); got.Kind != domain.LessonWorkshop || got.Renter != "" {
			t.Errorf("back from a rental: %+v", got)
		}
	})
}

func TestRentalFeedEvent(t *testing.T) {
	t.Parallel()

	// Only staff see a rental, so only their schedule feed has it: named by
	// its renter, without an instructor.
	start := mustInstant(t, rentalStart)
	e := feedEvent{ScheduledLesson: ScheduledLesson{
		Lesson: domain.Lesson{
			ID: ids.New[domain.LessonID](ids.NewSequence()), StartsAt: start, EndsAt: start.Add(time.Hour),
			Status: domain.LessonScheduled, Visibility: domain.VisibilityHidden, Kind: domain.LessonRental,
			Renter: "Copperleaf Theater Group",
		},
		CourseType: domain.CourseType{Name: "Imaginary Ballet"},
		Room:       domain.Room{Name: "Room One"},
		Location:   domain.Location{Name: "Studio Nowhere"},
	}}

	ev := (&CalendarFeedService{}).event(e, domain.FeedSchedule, "-schedule@example.org", feedLabelsFor("en"), time.UTC)

	if ev.Summary != "Imaginary Ballet – Copperleaf Theater Group" {
		t.Errorf("summary %q", ev.Summary)
	}

	if want := "Room rental\nRented by: Copperleaf Theater Group\nRoom: Studio Nowhere, Room One"; ev.Description != want {
		t.Errorf("description %q, want %q", ev.Description, want)
	}
}
