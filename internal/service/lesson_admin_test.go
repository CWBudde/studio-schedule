package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The lesson administration tests run on the class fixture (Europe/Berlin,
// term 2026-09-01 to 2027-07-31) at 12:00 on Wednesday 2026-10-21; the
// Tuesday 18:00 class of classData has lessons from 2026-10-27 on.
const lessonNow = "2026-10-21T10:00:00Z"

func (f *genFixture) lessonService(t *testing.T, now string) *LessonService {
	t.Helper()

	r, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	return NewLessonService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, r)
}

// lessonData is a one-off slot on Friday 2026-10-30, 17:00 to 18:30, in
// the fixture's room.
func (f *genFixture) lessonData() LessonData {
	return LessonData{
		Date: domain.Date{Year: 2026, Month: time.October, Day: 30}, StartTime: domain.TimeOfDay{Hour: 17},
		Duration: 90 * time.Minute, RoomID: f.room, InstructorID: f.instructor, Capacity: 8,
		Visibility: domain.VisibilityPublic, Kind: domain.LessonWorkshop,
	}
}

// dataOf returns the editable fields of l as they are.
func dataOf(t *testing.T, l domain.Lesson) LessonData {
	t.Helper()

	loc := berlin(t)
	start := l.StartsAt.In(loc)

	return LessonData{
		Date: domain.DateOf(start, loc), StartTime: domain.TimeOfDay{Hour: start.Hour(), Minute: start.Minute()},
		Duration: l.EndsAt.Sub(l.StartsAt), RoomID: l.RoomID, InstructorID: l.InstructorID,
		AccompanistID: l.AccompanistID, Capacity: l.Capacity, Visibility: l.Visibility, Kind: l.Kind,
	}
}

// mails returns the outbox, oldest first.
func (f *genFixture) mails(t *testing.T) []domain.OutboxMail {
	t.Helper()

	var out []domain.OutboxMail

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.Outbox().List(ctx, 1000)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	slices.Reverse(out)

	return out
}

func (f *genFixture) lesson(t *testing.T, id domain.LessonID) domain.Lesson {
	t.Helper()

	var l domain.Lesson

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		l, err = tx.Lessons().Get(ctx, id)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return l
}

// person stores a person with an optional own address and, if accountEmail
// is set, an account with the locale.
func (f *genFixture) person(t *testing.T, given, email, accountEmail, locale string) domain.Person {
	t.Helper()

	p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: given, FamilyName: "Example", Email: email}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		if err := tx.Persons().Create(ctx, p); err != nil {
			return err
		}

		if accountEmail == "" {
			return nil
		}

		return tx.Accounts().Create(ctx, domain.Account{
			ID: ids.New[domain.AccountID](f.seq), PersonID: p.ID, Email: accountEmail, Role: domain.RoleMember,
			Locale: locale, ConfirmedAt: mustInstant(t, "2026-01-01T00:00:00Z"),
		})
	})

	return p
}

// bookFor books the persons (or a guest, for a zero person) into l.
func (f *genFixture) bookFor(t *testing.T, l domain.Lesson, persons ...domain.Person) {
	t.Helper()

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		for _, p := range persons {
			b := domain.Booking{
				ID: ids.New[domain.BookingID](f.seq), LessonID: l.ID, PersonID: p.ID,
				Status: domain.BookingBooked, Source: domain.BookingSourceSelf, BookedAt: mustInstant(t, "2026-10-20T10:00:00Z"),
			}
			if p.ID.IsZero() {
				b.GuestName, b.GuestEmail = "Gina Guest", "gina@example.org"
			}

			if err := tx.Bookings().Create(ctx, b); err != nil {
				return err
			}
		}

		return nil
	})
}

func wantFieldError(t *testing.T, err error, field string) {
	t.Helper()

	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("want a validation error on %s, got %v", field, err)
	}
}

func TestCreateLesson(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	l, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	got := f.lesson(t, l.Lesson.ID)
	if !got.ClassID.IsZero() || got.Detached || got.Status != domain.LessonScheduled ||
		got.Kind != domain.LessonWorkshop || got.Capacity != 8 ||
		!got.StartsAt.Equal(mustInstant(t, "2026-10-30T16:00:00Z")) ||
		!got.EndsAt.Equal(mustInstant(t, "2026-10-30T17:30:00Z")) {
		t.Fatalf("created %+v", got)
	}

	if l.Admin == nil || l.Admin.ActiveBookings != 0 || l.CourseType.ID != f.courseType {
		t.Errorf("result %+v", l)
	}

	t.Run("room taken", func(t *testing.T) {
		d := f.lessonData()
		d.StartTime = domain.TimeOfDay{Hour: 18} // overlaps the last half hour

		_, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType})

		var taken *RoomTakenError
		if !errors.As(err, &taken) || taken.Lesson.ID != l.Lesson.ID || taken.CourseType.ID != f.courseType ||
			!errors.Is(err, domain.ErrConflict) {
			t.Fatalf("want RoomTakenError naming %s, got %v", l.Lesson.ID, err)
		}

		// Saved anyway on confirmation.
		d.ConfirmRoomOverlap = true
		if _, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType}); err != nil {
			t.Fatal(err)
		}

		// Right after the lesson the room is free.
		d.StartTime, d.ConfirmRoomOverlap = domain.TimeOfDay{Hour: 20}, false
		if _, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("past after confirmation", func(t *testing.T) {
		d := NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType}
		d.Date, d.StartTime = domain.Date{Year: 2026, Month: time.October, Day: 21}, domain.TimeOfDay{Hour: 11}

		_, err := svc.CreateLesson(ctx, d)
		if !errors.Is(err, ErrLessonPast) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("want ErrLessonPast, got %v", err)
		}

		d.ConfirmPast = true

		l, err := svc.CreateLesson(ctx, d)
		if err != nil {
			t.Fatal(err)
		}

		if !l.Lesson.StartsAt.Equal(mustInstant(t, "2026-10-21T09:00:00Z")) {
			t.Errorf("starts at %v", l.Lesson.StartsAt)
		}
	})

	t.Run("canceled lessons do not take the room", func(t *testing.T) {
		d := f.lessonData()
		d.Date = domain.Date{Year: 2026, Month: time.November, Day: 6}

		first, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := svc.CancelLesson(ctx, Principal{}, first.Lesson.ID, ""); err != nil {
			t.Fatal(err)
		}

		if _, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType}); err != nil {
			t.Fatalf("room of a canceled lesson: %v", err)
		}
	})

	for field, mutate := range map[string]func(*NewLesson){
		"courseTypeId":    func(d *NewLesson) { d.CourseTypeID = ids.New[domain.CourseTypeID](f.seq) },
		"roomId":          func(d *NewLesson) { d.RoomID = ids.New[domain.RoomID](f.seq) },
		"instructorId":    func(d *NewLesson) { d.InstructorID = ids.New[domain.StaffID](f.seq) },
		"accompanistId":   func(d *NewLesson) { d.AccompanistID = d.InstructorID },
		"capacity":        func(d *NewLesson) { d.Capacity = 0 },
		"visibility":      func(d *NewLesson) { d.Visibility = "secret" },
		"kind":            func(d *NewLesson) { d.Kind = "party" },
		"date":            func(d *NewLesson) { d.Date = domain.Date{} },
		"startTime":       func(d *NewLesson) { d.StartTime = domain.TimeOfDay{Hour: 25} },
		"durationMinutes": func(d *NewLesson) { d.StartTime = domain.TimeOfDay{Hour: 23} },
	} {
		t.Run("invalid "+field, func(t *testing.T) {
			d := NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType}
			d.Date = domain.Date{Year: 2027, Month: time.January, Day: 8} // a free room
			mutate(&d)

			_, err := svc.CreateLesson(ctx, d)
			wantFieldError(t, err, field)
		})
	}
}

// TestUpdateLesson edits a generated lesson: it is detached, keeps its slot,
// and neither a class edit nor generation touch it again.
func TestUpdateLesson(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	timetable := f.timetable(t, lessonNow)
	svc := f.lessonService(t, lessonNow)

	c, err := timetable.CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	// Move the lesson of 2026-11-03 to Thursday 2026-11-05 at 19:00 and
	// raise its capacity; the one of 2026-11-10 only gets a new capacity.
	moved := f.lessonAt(t, c.ID, "2026-11-03")
	d := dataOf(t, moved)
	d.Date, d.StartTime, d.Capacity = mustDate(t, "2026-11-05"), domain.TimeOfDay{Hour: 19}, 12

	res, err := svc.UpdateLesson(ctx, moved.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	if res.Admin == nil || !res.Admin.Detached {
		t.Errorf("result %+v, want detached", res.Admin)
	}

	got := f.lesson(t, moved.ID)
	if !got.Detached || got.SlotDate != moved.SlotDate || got.Capacity != 12 ||
		!got.StartsAt.Equal(mustInstant(t, "2026-11-05T18:00:00Z")) {
		t.Fatalf("updated %+v", got)
	}

	// Only the capacity: the fields still are what the class generates
	// but for the capacity, which goes back to the class's value.
	same := f.lessonAt(t, c.ID, "2026-11-10")
	d = dataOf(t, same)
	if _, err := svc.UpdateLesson(ctx, same.ID, d); err != nil {
		t.Fatal(err)
	}

	if l := f.lesson(t, same.ID); !l.Detached {
		t.Fatal("an unchanged save does not detach the lesson")
	}

	// The class moves to 17:00: the followers move, the detached lessons
	// stay, even the one that still equals what the class generated.
	cd := f.classData()
	cd.StartTime = domain.TimeOfDay{Hour: 17}

	if _, err := timetable.UpdateClass(ctx, c.ID, cd); err != nil {
		t.Fatal(err)
	}

	if l := f.lesson(t, moved.ID); !l.StartsAt.Equal(got.StartsAt) {
		t.Errorf("moved lesson follows the class: %v", l.StartsAt)
	}

	if l := f.lesson(t, same.ID); !l.StartsAt.Equal(same.StartsAt) {
		t.Errorf("detached lesson follows the class: %v", l.StartsAt)
	}

	if l := f.lessonAt(t, c.ID, "2026-10-27"); !l.StartsAt.Equal(mustInstant(t, "2026-10-27T16:00:00Z")) {
		t.Errorf("follower starts at %v, want 16:00 UTC", l.StartsAt)
	}

	// Generation creates nothing in the weeks of the edited lessons.
	before := len(f.classLessons(t, c.ID))

	if _, err := generatorAt(f.st, mustInstant(t, lessonNow)).GenerateAhead(ctx); err != nil {
		t.Fatal(err)
	}

	if after := len(f.classLessons(t, c.ID)); after != before {
		t.Errorf("generation changed the lessons from %d to %d", before, after)
	}

	for _, slot := range []string{"2026-11-03", "2026-11-10"} {
		n := 0

		for _, l := range f.classLessons(t, c.ID) {
			if weekOf(l.SlotDate) == weekOf(mustDate(t, slot)) {
				n++
			}
		}

		if n != 1 {
			t.Errorf("week of %s has %d lessons, want 1", slot, n)
		}
	}

	t.Run("capacity below bookings", func(t *testing.T) {
		l := f.lessonAt(t, c.ID, "2026-10-27")
		f.book(t, l, 3)

		d := dataOf(t, l)
		d.Capacity = 2

		_, err := svc.UpdateLesson(ctx, l.ID, d)
		wantFieldError(t, err, "capacity")

		d.Capacity = 3
		if _, err := svc.UpdateLesson(ctx, l.ID, d); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("started", func(t *testing.T) {
		l := f.lessonAt(t, c.ID, "2026-10-27")
		late := f.lessonService(t, "2026-10-27T16:00:00Z")

		_, err := late.UpdateLesson(ctx, l.ID, dataOf(t, l))
		if !errors.Is(err, ErrLessonStarted) || !errors.Is(err, domain.ErrValidation) ||
			errors.Is(err, domain.ErrConflict) {
			t.Fatalf("want ErrLessonStarted as a validation error, got %v", err)
		}
	})

	t.Run("moved into the past after confirmation", func(t *testing.T) {
		l := f.lessonAt(t, c.ID, "2026-11-10")
		d := dataOf(t, l)
		d.Date, d.StartTime = mustDate(t, "2026-10-21"), domain.TimeOfDay{Hour: 9}

		if _, err := svc.UpdateLesson(ctx, l.ID, d); !errors.Is(err, ErrLessonPast) {
			t.Fatalf("want ErrLessonPast, got %v", err)
		}

		d.ConfirmPast = true
		if _, err := svc.UpdateLesson(ctx, l.ID, d); err != nil {
			t.Fatal(err)
		}

		if got := f.lesson(t, l.ID); !got.StartsAt.Equal(mustInstant(t, "2026-10-21T07:00:00Z")) {
			t.Errorf("starts at %v", got.StartsAt)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		_, err := svc.UpdateLesson(ctx, ids.New[domain.LessonID](f.seq), f.lessonData())
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("room taken only on a new time or room", func(t *testing.T) {
		l := f.lessonAt(t, c.ID, "2026-11-17")
		// Another lesson in the room at the same time, saved anyway.
		d := dataOf(t, l)
		d.ConfirmRoomOverlap = true

		if _, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType}); err != nil {
			t.Fatal(err)
		}

		// Hiding the lesson keeps its time and room: no check.
		d = dataOf(t, l)
		d.Visibility = domain.VisibilityHidden

		if _, err := svc.UpdateLesson(ctx, l.ID, d); err != nil {
			t.Fatalf("hide: %v", err)
		}

		d.Duration = 2 * time.Hour

		var taken *RoomTakenError
		if _, err := svc.UpdateLesson(ctx, l.ID, d); !errors.As(err, &taken) {
			t.Fatalf("want RoomTakenError, got %v", err)
		}
	})
}

// TestLessonMails cancels, restores and moves a booked lesson and checks
// who gets which mail.
func TestLessonMails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	created, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	l := created.Lesson

	// Anna has a German account and books for herself and her son Max,
	// who has no address; Ben has only his own address; Carla's account is
	// disabled, so nobody is mailed for her; Dora is booked but canceled;
	// Emil's guardians are Anna, Finn, Gus (unconfirmed account) and Hal
	// (disabled account), so only Anna and Finn are mailed for him; Hanna's
	// account is unconfirmed, so her own address counts; and a guest.
	anna := f.person(t, "Anna", "anna.private@example.org", "anna@example.org", "de")
	maxi := f.person(t, "Max", "", "", "")
	ben := f.person(t, "Ben", "ben@example.org", "", "")
	carla := f.person(t, "Carla", "carla@example.org", "carla.account@example.org", "")
	dora := f.person(t, "Dora", "dora@example.org", "", "")
	emil := f.person(t, "Emil", "", "", "")
	finn := f.person(t, "Finn", "", "finn@example.org", "en")
	gus := f.person(t, "Gus", "gus.private@example.org", "gus@example.org", "")
	hal := f.person(t, "Hal", "hal.private@example.org", "hal@example.org", "")
	hanna := f.person(t, "Hanna", "hanna@example.org", "hanna.account@example.org", "")

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		account := func(p domain.Person, change func(*domain.Account)) error {
			acc, err := tx.Accounts().GetByPerson(ctx, p.ID)
			if err != nil {
				return err
			}

			change(&acc)

			return tx.Accounts().Update(ctx, acc)
		}
		disable := func(a *domain.Account) { a.DisabledAt = mustInstant(t, "2026-10-01T00:00:00Z") }
		unconfirm := func(a *domain.Account) { a.ConfirmedAt = time.Time{} }

		return errors.Join(
			account(carla, disable), account(hal, disable), account(gus, unconfirm), account(hanna, unconfirm),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: anna.ID, DependentID: maxi.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: anna.ID, DependentID: emil.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: finn.ID, DependentID: emil.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: gus.ID, DependentID: emil.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: hal.ID, DependentID: emil.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: finn.ID, DependentID: carla.ID}),
		)
	})
	f.bookFor(t, l, anna, maxi, ben, carla, dora, emil, hanna, domain.Person{})
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		bookings, err := tx.Bookings().ListByLesson(ctx, l.ID)
		if err != nil {
			return err
		}

		for _, b := range bookings {
			if b.PersonID == dora.ID {
				b.Status, b.CanceledAt = domain.BookingCanceled, mustInstant(t, "2026-10-20T11:00:00Z")

				return tx.Bookings().Update(ctx, b)
			}
		}

		return errors.New("no booking of Dora")
	})

	type sent struct{ to, template, subjectPart string }

	check := func(t *testing.T, mails []domain.OutboxMail, want []sent) {
		t.Helper()

		if len(mails) != len(want) {
			t.Fatalf("%d mails, want %d: %+v", len(mails), len(want), mails)
		}

		for i, w := range want {
			m := mails[i]
			if m.To != w.to || m.Template != w.template || !strings.Contains(m.Subject, w.subjectPart) {
				t.Errorf("mail %d to %s (%s) %q, want to %s (%s) containing %q",
					i, m.To, m.Template, m.Subject, w.to, w.template, w.subjectPart)
			}
		}
	}

	everyone := func(template, de, en string) []sent {
		return []sent{
			{"anna@example.org", template, de},
			{"ben@example.org", template, en},
			{"finn@example.org", template, en},
			{"hanna@example.org", template, en},
			{"gina@example.org", template, en},
		}
	}

	office := Principal{AccountID: f.accountOf(t, f.person(t, "Olga", "", "olga@example.org", "").ID)}
	// An admin signed in as the office account cancels: the admin is
	// recorded.
	adminID := f.accountOf(t, f.person(t, "Ada", "", "ada@example.org", "").ID)
	impersonated := office
	impersonated.ImpersonatorID = adminID

	res, err := svc.CancelLesson(ctx, impersonated, l.ID, "  The instructor is ill.  ")
	if err != nil {
		t.Fatal(err)
	}

	if res.Lesson.Status != domain.LessonCanceled || res.Admin.CancelNote != "The instructor is ill." ||
		!res.Admin.CanceledAt.Equal(mustInstant(t, lessonNow)) ||
		res.Admin.CanceledBy != (CanceledBy{AccountID: adminID, Name: "Ada Example"}) ||
		res.Admin.ActiveBookings != 7 {
		t.Fatalf("canceled %+v %+v", res.Lesson, res.Admin)
	}

	mails := f.mails(t)
	check(t, mails, everyone(mail.TemplateLessonCanceled, "fällt aus", "is canceled"))

	// Anna's one mail names herself, Max and Emil, and the note.
	for _, w := range []string{"Anna Example, Max Example, Emil Example", "The instructor is ill.", "Hallo Anna"} {
		if !strings.Contains(mails[0].Text, w) {
			t.Errorf("Anna's mail lacks %q:\n%s", w, mails[0].Text)
		}
	}

	if !strings.Contains(mails[2].Text, "Booked for: Emil Example\n") {
		t.Errorf("Finn's mail is about Emil only:\n%s", mails[2].Text)
	}

	if !strings.Contains(mails[4].Text, "Hello Gina Guest") {
		t.Errorf("guest mail:\n%s", mails[4].Text)
	}

	// Canceling again changes nothing, not even the note, and mails
	// nobody.
	if res, err := svc.CancelLesson(ctx, office, l.ID, "Again"); err != nil ||
		res.Admin.CancelNote != "The instructor is ill." || res.Admin.CanceledBy.AccountID != adminID {
		t.Fatalf("canceled twice: %+v, %v", res.Admin, err)
	}

	if n := len(f.mails(t)); n != 5 {
		t.Fatalf("%d mails after canceling twice, want 5", n)
	}

	res, err = svc.RestoreLesson(ctx, l.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	if got := f.lesson(t, l.ID); got.Status != domain.LessonScheduled || got.CancelNote != "" ||
		!got.CanceledAt.IsZero() || !got.CanceledBy.IsZero() {
		t.Fatalf("restored %+v", got)
	}

	if res.Admin.CanceledBy != (CanceledBy{}) {
		t.Errorf("restored admin %+v", res.Admin)
	}

	check(t, f.mails(t)[5:], everyone(mail.TemplateLessonRestored, "findet doch statt", "takes place after all"))

	// Staff, capacity, duration and visibility edits mail nobody.
	d := dataOf(t, l)
	d.InstructorID, d.AccompanistID, d.Capacity = f.pianist, f.instructor, 20
	d.Duration, d.Visibility = time.Hour, domain.VisibilityHidden

	if _, err := svc.UpdateLesson(ctx, l.ID, d); err != nil {
		t.Fatal(err)
	}

	if n := len(f.mails(t)); n != 10 {
		t.Fatalf("%d mails after a staff edit, want 10", n)
	}

	// A new start mails the change, naming the old and the new time.
	d.StartTime = domain.TimeOfDay{Hour: 18}
	if _, err := svc.UpdateLesson(ctx, l.ID, d); err != nil {
		t.Fatal(err)
	}

	changed := f.mails(t)[10:]
	check(t, changed, everyone(mail.TemplateLessonChanged, "wurde geändert", "has changed"))

	if !strings.Contains(changed[1].Text, "Friday, 30 October 2026, 18:00–19:00") ||
		!strings.Contains(changed[1].Text, "Friday, 30 October 2026, 17:00–18:00") {
		t.Errorf("change mail:\n%s", changed[1].Text)
	}

	// A canceled lesson that moves mails nobody; its restore names the new
	// time.
	if res, err := svc.CancelLesson(ctx, office, l.ID, ""); err != nil || res.Admin.CanceledBy.Name != "Olga Example" {
		t.Fatalf("cancel: %+v, %v", res.Admin, err)
	}

	d.StartTime = domain.TimeOfDay{Hour: 19}
	if _, err := svc.UpdateLesson(ctx, l.ID, d); err != nil {
		t.Fatal(err)
	}

	if n := len(f.mails(t)); n != 20 {
		t.Fatalf("%d mails, want 20 (no change mail for a canceled lesson)", n)
	}

	if _, err := svc.RestoreLesson(ctx, l.ID, false); err != nil {
		t.Fatal(err)
	}

	restored := f.mails(t)[20:]
	check(t, restored, everyone(mail.TemplateLessonRestored, "findet doch statt", "takes place after all"))

	if !strings.Contains(restored[1].Text, "Friday, 30 October 2026, 19:00–20:00") {
		t.Errorf("restore mail lacks the new time:\n%s", restored[1].Text)
	}

	t.Run("started", func(t *testing.T) {
		late := f.lessonService(t, "2026-10-30T18:00:00Z")

		// Restoring a scheduled lesson changes nothing, started or not.
		if _, err := late.RestoreLesson(ctx, l.ID, false); err != nil {
			t.Fatalf("restore a scheduled lesson: %v", err)
		}

		if _, err := late.CancelLesson(ctx, office, l.ID, ""); !errors.Is(err, ErrLessonStarted) ||
			!errors.Is(err, domain.ErrValidation) {
			t.Fatalf("cancel: want ErrLessonStarted, got %v", err)
		}

		if _, err := svc.CancelLesson(ctx, office, l.ID, ""); err != nil {
			t.Fatal(err)
		}

		if _, err := late.CancelLesson(ctx, office, l.ID, "Late"); err != nil {
			t.Fatalf("cancel a canceled lesson: %v", err)
		}

		if _, err := late.RestoreLesson(ctx, l.ID, false); !errors.Is(err, ErrLessonStarted) {
			t.Fatalf("restore: want ErrLessonStarted, got %v", err)
		}
	})

	t.Run("no mails for the past", func(t *testing.T) {
		other, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
		if err != nil {
			t.Fatal(err)
		}

		f.bookFor(t, other.Lesson, ben)
		before := len(f.mails(t))

		d := dataOf(t, other.Lesson)
		d.Date, d.StartTime, d.ConfirmPast = mustDate(t, "2026-10-21"), domain.TimeOfDay{Hour: 9}, true

		if _, err := svc.UpdateLesson(ctx, other.Lesson.ID, d); err != nil {
			t.Fatal(err)
		}

		if n := len(f.mails(t)); n != before {
			t.Fatalf("%d mails after a move into the past, want %d", n, before)
		}
	})

	t.Run("note too long", func(t *testing.T) {
		other, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
		if err != nil {
			t.Fatal(err)
		}

		_, err = svc.CancelLesson(ctx, office, other.Lesson.ID, strings.Repeat("x", domain.MaxCancelNoteLength+1))
		wantFieldError(t, err, "note")
	})
}

// TestRestoreLessonRoom restores a canceled lesson whose room another
// lesson took in the meantime.
func TestRestoreLessonRoom(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	first, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CancelLesson(ctx, Principal{}, first.Lesson.ID, ""); err != nil {
		t.Fatal(err)
	}

	second, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	var taken *RoomTakenError
	if _, err := svc.RestoreLesson(ctx, first.Lesson.ID, false); !errors.As(err, &taken) ||
		taken.Lesson.ID != second.Lesson.ID {
		t.Fatalf("want RoomTakenError naming %s, got %v", second.Lesson.ID, err)
	}

	if got := f.lesson(t, first.Lesson.ID); got.Status != domain.LessonCanceled {
		t.Fatalf("restored despite the room: %+v", got)
	}

	if res, err := svc.RestoreLesson(ctx, first.Lesson.ID, true); err != nil || res.Lesson.Status != domain.LessonScheduled {
		t.Fatalf("restore anyway: %+v, %v", res.Lesson, err)
	}
}

// accountOf returns the account of the person.
func (f *genFixture) accountOf(t *testing.T, person domain.PersonID) domain.AccountID {
	t.Helper()

	var acc domain.Account

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		acc, err = tx.Accounts().GetByPerson(ctx, person)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return acc.ID
}

func TestDeleteLesson(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	c, err := f.timetable(t, lessonNow).CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteLesson(ctx, f.lessonAt(t, c.ID, "2026-10-27").ID); !errors.Is(err, ErrGeneratedLesson) {
		t.Fatalf("generated: want ErrGeneratedLesson, got %v", err)
	}

	newLesson := func(day int) domain.Lesson {
		d := f.lessonData()
		d.Date.Day = day

		l, err := svc.CreateLesson(ctx, NewLesson{LessonData: d, CourseTypeID: f.courseType})
		if err != nil {
			t.Fatal(err)
		}

		return l.Lesson
	}

	booked := newLesson(29)
	f.book(t, booked, 1)

	if err := svc.DeleteLesson(ctx, booked.ID); !errors.Is(err, domain.ErrInUse) {
		t.Fatalf("booked: want ErrInUse, got %v", err)
	}

	// A one-off that has started stays, like it does for every other write.
	started := newLesson(30)
	if err := f.lessonService(t, "2026-10-30T18:00:00Z").DeleteLesson(ctx, started.ID); !errors.Is(err, ErrLessonStarted) {
		t.Fatalf("started: want ErrLessonStarted, got %v", err)
	}

	free := newLesson(28)
	if err := svc.DeleteLesson(ctx, free.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteLesson(ctx, free.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted twice: want ErrNotFound, got %v", err)
	}
}

// TestLessonAdminDetails shows the office's details in the lesson sheet,
// and nobody else.
func TestLessonAdminDetails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)
	schedule := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, lessonNow)))

	l, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	f.book(t, l.Lesson, 2)

	for role, wantAdmin := range map[domain.Role]bool{
		domain.RoleMember: false, domain.RoleInstructor: false, domain.RoleOffice: true, domain.RoleAdmin: true,
	} {
		p := Principal{AccountID: ids.New[domain.AccountID](f.seq), Role: role}

		got, err := schedule.Lesson(ctx, p, l.Lesson.ID)
		if err != nil {
			t.Fatal(err)
		}

		if (got.Admin != nil) != wantAdmin {
			t.Errorf("%s: admin %+v", role, got.Admin)
		}

		if wantAdmin && got.Admin.ActiveBookings != 2 {
			t.Errorf("%s: %d active bookings, want 2", role, got.Admin.ActiveBookings)
		}
	}
}

// TestLessonMailOneAddress mails an address once when a booked child's own
// address is the account address of the parent through whom a sibling is
// mailed: the account's entry wins, with its language and name.
func TestLessonMailOneAddress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	l, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	paula := f.person(t, "Paula", "", "paula@example.org", "de")
	mia := f.person(t, "Mia", "PAULA@example.org", "", "")
	leo := f.person(t, "Leo", "", "", "")
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: paula.ID, DependentID: mia.ID}),
			tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: paula.ID, DependentID: leo.ID}),
		)
	})
	f.bookFor(t, l.Lesson, mia, leo)

	if _, err := svc.CancelLesson(ctx, Principal{}, l.Lesson.ID, ""); err != nil {
		t.Fatal(err)
	}

	mails := f.mails(t)
	if len(mails) != 1 {
		t.Fatalf("%d mails, want 1: %+v", len(mails), mails)
	}

	m := mails[0]
	if m.To != "paula@example.org" || !strings.Contains(m.Subject, "fällt aus") {
		t.Errorf("mail to %s %q, want to paula@example.org in German", m.To, m.Subject)
	}

	for _, w := range []string{"Hallo Paula", "Mia Example, Leo Example"} {
		if !strings.Contains(m.Text, w) {
			t.Errorf("mail lacks %q:\n%s", w, m.Text)
		}
	}
}

// TestLessonMailBadAddress refuses a cancellation whose mail cannot be
// queued as an internal error, not as the office's validation error.
func TestLessonMailBadAddress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	l, err := svc.CreateLesson(ctx, NewLesson{LessonData: f.lessonData(), CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	f.bookFor(t, l.Lesson, f.person(t, "Bert", "not an address", "", ""))

	_, err = svc.CancelLesson(ctx, Principal{}, l.Lesson.ID, "")
	if err == nil || errors.Is(err, domain.ErrValidation) {
		t.Fatalf("want an internal error, got %v", err)
	}

	if got := f.lesson(t, l.Lesson.ID); got.Status != domain.LessonScheduled {
		t.Fatalf("canceled without its mail: %+v", got)
	}
}
