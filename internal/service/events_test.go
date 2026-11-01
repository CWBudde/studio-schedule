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
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The event tests run on the pass fixture (Europe/Berlin, receipts
// complete, a fake PDF renderer) at bookingNow, Wednesday 2026-10-21, 12:00
// in Berlin. Olga is the office; Anna (an account, German) is the guardian
// of Mia (none) and Tom (an account of his own).

// eventFixture adds the event administration to the pass fixture.
type eventFixture struct {
	*passFixture
}

func newEventFixture(t *testing.T) *eventFixture {
	t.Helper()

	return &eventFixture{passFixture: newPassFixture(t, domain.RedeemOnBooking)}
}

// eventsAt returns the event service at now.
func (f *eventFixture) eventsAt(t *testing.T, now string) *EventService {
	t.Helper()

	return NewEventService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, testMails(newTestRenderer(t)),
		f.receiptService(t, now))
}

func (f *eventFixture) events(t *testing.T) *EventService {
	t.Helper()

	return f.eventsAt(t, bookingNow)
}

// eventData is a priced intensive of the fixture's course type with two
// places.
func (f *eventFixture) eventData() EventData {
	return EventData{
		Title: "Imaginary Intensive", CourseTypeID: f.courseType, Capacity: 2, Price: 4500, VATRate: 1900,
	}
}

// sessionAt is a session of 75 minutes in the fixture's room on the school
// date day at the time.
func (f *eventFixture) sessionAt(t *testing.T, day string, hour int) SessionData {
	t.Helper()

	return SessionData{
		Date: mustDate(t, day), StartTime: domain.TimeOfDay{Hour: hour}, Duration: 75 * time.Minute, RoomID: f.room,
		InstructorID: f.instructor,
	}
}

// published creates a published event of d with sessions on Friday
// 2026-10-23 and Saturday 2026-10-24 at 18:00 (16:00 UTC).
func (f *eventFixture) published(t *testing.T, d EventData) (domain.Event, []domain.Lesson) {
	t.Helper()

	return f.publishedAt(t, d, 18)
}

// publishedAt is published with the sessions at the hour.
func (f *eventFixture) publishedAt(t *testing.T, d EventData, hour int) (domain.Event, []domain.Lesson) {
	t.Helper()

	svc := f.events(t)

	e, err := svc.Create(t.Context(), f.office, d)
	if err != nil {
		t.Fatal(err)
	}

	var sessions []domain.Lesson

	for _, day := range []string{"2026-10-23", "2026-10-24"} {
		sl, err := svc.AddSession(t.Context(), e.Event.ID, f.sessionAt(t, day, hour))
		if err != nil {
			t.Fatal(err)
		}

		sessions = append(sessions, sl.Lesson)
	}

	if e, err = svc.Publish(t.Context(), e.Event.ID); err != nil {
		t.Fatal(err)
	}

	return e.Event, sessions
}

func (f *eventFixture) register(t *testing.T, e domain.Event, in NewRegistration) domain.EventRegistration {
	t.Helper()

	v, err := f.events(t).AddRegistration(t.Context(), f.office, e.ID, in)
	if err != nil {
		t.Fatal(err)
	}

	return v.Registration
}

func (f *eventFixture) registration(t *testing.T, id domain.EventRegistrationID) domain.EventRegistration {
	t.Helper()

	var r domain.EventRegistration

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		r, err = tx.EventRegistrations().Get(ctx, id)

		return err
	})

	return r
}

func (f *eventFixture) event(t *testing.T, id domain.EventID) domain.Event {
	t.Helper()

	var e domain.Event

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		e, err = tx.Events().Get(ctx, id)

		return err
	})

	return e
}

// audits returns the audit log, oldest first.
func (f *eventFixture) audits(t *testing.T) []domain.AuditEvent {
	t.Helper()

	var out []domain.AuditEvent

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.Audit().List(ctx, 1000)

		return err
	})
	slices.Reverse(out)

	return out
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("abcdefghi ", 10) // ten groups of 9, 99 characters with hyphens

	for title, want := range map[string]string{
		"Ballet Intensive 2026":        "ballet-intensive-2026",
		"  Übungs-Wochenende: Größe! ": "uebungs-wochenende-groesse",
		"Café crème à la Française":    "cafe-creme-a-la-francaise",
		"Ötztal & Äpfel – Straße":      "oetztal-aepfel-strasse",
		"!!! ---":                      "",
		"Ballet---and   Jazz":          "ballet-and-jazz",
		long:                           strings.TrimSuffix(strings.Repeat("abcdefghi-", 8), "-"),
		strings.Repeat("x", 100):       strings.Repeat("x", domain.MaxEventSlugLength),
		"Ἀθῆναι 東京":                    "",
	} {
		got := slugify(title)
		if got != want {
			t.Errorf("slugify(%q) = %q, want %q", title, got, want)
		}

		if got != "" && !domain.ValidEventSlug(got) {
			t.Errorf("slugify(%q) = %q, not a valid slug", title, got)
		}
	}
}

func TestCreateEvent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)

	got, err := svc.Create(ctx, f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	e := got.Event
	if e.Status != domain.EventDraft || e.Slug != "imaginary-intensive" || e.CreatedBy != f.office.AccountID ||
		!e.CreatedAt.Equal(mustInstant(t, bookingNow)) || len(got.Sessions) != 0 || f.event(t, e.ID) != e {
		t.Errorf("created %+v", got)
	}

	// The same title again gets a number; a given slug stays as given.
	second, err := svc.Create(ctx, f.office, f.eventData())
	if err != nil || second.Event.Slug != "imaginary-intensive-2" {
		t.Errorf("second: %+v, %v", second.Event, err)
	}

	d := f.eventData()
	d.Slug = " winter-days "

	if third, err := svc.Create(ctx, f.office, d); err != nil || third.Event.Slug != "winter-days" {
		t.Errorf("given slug: %+v, %v", third.Event, err)
	}

	d.Slug = "imaginary-intensive"
	if _, err := svc.Create(ctx, f.office, d); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("taken slug: %v, want ErrDuplicate", err)
	}

	for field, edit := range map[string]func(*EventData){
		"slug":         func(d *EventData) { d.Title = "!!!" },
		"title":        func(d *EventData) { d.Title, d.Slug = " ", "untitled" },
		"courseTypeId": func(d *EventData) { d.CourseTypeID = ids.New[domain.CourseTypeID](f.seq) },
		"capacity":     func(d *EventData) { d.Capacity = 0 },
	} {
		d := f.eventData()
		edit(&d)

		_, err := svc.Create(ctx, f.office, d)
		wantFieldError(t, err, field)
	}
}

// TestCreateEventSlugNumbers: a long title's numbered slugs stay within the
// limit, and after maxSlugTries taken ones the office chooses one.
func TestCreateEventSlugNumbers(t *testing.T) {
	t.Parallel()

	f := newEventFixture(t)
	svc := f.events(t)
	d := f.eventData()
	d.Title = strings.Repeat("abcdefghi ", 9) // 89 characters as a slug

	for _, want := range []string{
		strings.TrimSuffix(strings.Repeat("abcdefghi-", 8), "-"),
		strings.Repeat("abcdefghi-", 7) + "2",
		strings.Repeat("abcdefghi-", 7) + "3",
	} {
		got, err := svc.Create(t.Context(), f.office, d)
		if err != nil || got.Event.Slug != want {
			t.Fatalf("slug %q, %v; want %q", got.Event.Slug, err, want)
		}
	}

	d.Title = "Taken"
	for range maxSlugTries {
		if _, err := svc.Create(t.Context(), f.office, d); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.Create(t.Context(), f.office, d); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("all slugs taken: %v, want ErrDuplicate", err)
	}
}

func TestUpdateEvent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	f.register(t, e, NewRegistration{PersonID: f.tom.ID})

	// Saturday's session has started when Saturday evening edits it.
	svc := f.eventsAt(t, "2026-10-24T16:30:00Z")

	d := f.eventData()
	d.Title, d.Description, d.Capacity = " Imaginary Intensive, revised ", " Two evenings. ", 5

	got, err := svc.Update(ctx, e.ID, d)
	if err != nil {
		t.Fatal(err)
	}

	if g := got.Event; g.Title != "Imaginary Intensive, revised" || g.Description != "Two evenings." ||
		g.Capacity != 5 || g.Slug != e.Slug || g.Status != domain.EventPublished || g.CreatedBy != e.CreatedBy ||
		!g.UpdatedAt.Equal(mustInstant(t, "2026-10-24T16:30:00Z")) || got.Counts.Registered != 2 {
		t.Errorf("updated %+v", got)
	}

	// Friday's session is over and keeps its capacity; Saturday's has
	// started too. Neither is changed.
	for _, l := range sessions {
		if got := f.lesson(t, l.ID); got.Capacity != 2 {
			t.Errorf("a started session got capacity %d", got.Capacity)
		}
	}

	// Before the sessions, they follow the event.
	d.Capacity, d.Slug = 3, "renamed"

	if got, err = f.events(t).Update(ctx, e.ID, d); err != nil || got.Event.Slug != "renamed" {
		t.Fatalf("update %+v, %v", got.Event, err)
	}

	for _, l := range sessions {
		if got := f.lesson(t, l.ID); got.Capacity != 3 {
			t.Errorf("session capacity %d, want 3", got.Capacity)
		}
	}

	d.Capacity = 1
	_, err = f.events(t).Update(ctx, e.ID, d)
	wantFieldError(t, err, "capacity")

	// Registered beyond its capacity, the event stays editable.
	d.Capacity = 2
	if _, err := f.events(t).Update(ctx, e.ID, d); err != nil {
		t.Fatal(err)
	}

	f.register(t, e, NewRegistration{PersonID: f.anna.ID, ConfirmOverCapacity: true})

	d.Title = "Imaginary Intensive, full"
	if _, err := f.events(t).Update(ctx, e.ID, d); err != nil {
		t.Errorf("edit an event beyond its capacity: %v", err)
	}

	d.Capacity = 3

	d.CourseTypeID = f.otherCourseType(t)
	_, err = f.events(t).Update(ctx, e.ID, d)
	wantFieldError(t, err, "courseTypeId")

	if _, err := f.events(t).Update(ctx, ids.New[domain.EventID](f.seq), d); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown event: %v", err)
	}

	// Without sessions the course type changes.
	d.Slug = ""

	draft, err := f.events(t).Create(ctx, f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	if got, err := f.events(t).Update(ctx, draft.Event.ID, d); err != nil || got.Event.CourseTypeID != d.CourseTypeID {
		t.Errorf("course type of a draft: %+v, %v", got.Event, err)
	}
}

// otherCourseType stores another course type.
func (f *eventFixture) otherCourseType(t *testing.T) domain.CourseTypeID {
	t.Helper()

	ct := domain.CourseType{ID: ids.New[domain.CourseTypeID](f.seq), Name: "Imaginary Jazz"}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.CourseTypes().Create(ctx, ct) })

	return ct.ID
}

func TestDeleteEvent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)

	registered, _ := f.published(t, f.eventData())
	r := f.register(t, registered, NewRegistration{PersonID: f.mia.ID})

	if _, err := f.events(t).CancelRegistration(ctx, f.office, r.ID, false); err != nil {
		t.Fatal(err)
	}

	// A canceled registration still keeps the event.
	if err := f.events(t).Delete(ctx, registered.ID); !errors.Is(err, domain.ErrInUse) {
		t.Errorf("with a registration: %v, want ErrInUse", err)
	}

	unregistered, sessions := f.publishedAt(t, f.eventData(), 10)
	if err := f.eventsAt(t, "2026-10-23T08:00:00Z").Delete(ctx, unregistered.ID); !errors.Is(err, domain.ErrInUse) {
		t.Errorf("a session started: %v, want ErrInUse", err)
	}

	if err := f.events(t).Delete(ctx, unregistered.ID); err != nil {
		t.Fatal(err)
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Events().Get(ctx, unregistered.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("event stays: %v", err)
		}

		left, err := tx.Lessons().ListByIDs(ctx, []domain.LessonID{sessions[0].ID, sessions[1].ID})
		if len(left) > 0 {
			t.Errorf("sessions stay: %v", left)
		}

		return err
	})

	if err := f.events(t).Delete(ctx, unregistered.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
}

func TestPublishEvent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)

	draft, err := svc.Create(ctx, f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	id := draft.Event.ID
	if _, err := svc.Publish(ctx, id); !errors.Is(err, ErrEventNoSessions) {
		t.Errorf("without sessions: %v", err)
	}

	sl, err := svc.AddSession(ctx, id, f.sessionAt(t, "2026-10-23", 18))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.lessonService(t, bookingNow).CancelLesson(ctx, f.office, sl.Lesson.ID, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Publish(ctx, id); !errors.Is(err, ErrEventNoSessions) {
		t.Errorf("with a canceled session only: %v", err)
	}

	if _, err := svc.AddSession(ctx, id, f.sessionAt(t, "2026-10-24", 18)); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Publish(ctx, id)
	if err != nil || got.Event.Status != domain.EventPublished || len(got.Sessions) != 2 {
		t.Fatalf("published %+v, %v", got, err)
	}

	if _, err := svc.Publish(ctx, id); !errors.Is(err, ErrEventNotDraft) {
		t.Errorf("published twice: %v", err)
	}

	r := f.register(t, got.Event, NewRegistration{PersonID: f.mia.ID})
	if _, err := svc.Unpublish(ctx, id); !errors.Is(err, domain.ErrInUse) {
		t.Errorf("unpublish with a registration: %v", err)
	}

	if _, err := svc.CancelRegistration(ctx, f.office, r.ID, false); err != nil {
		t.Fatal(err)
	}

	if got, err = svc.Unpublish(ctx, id); err != nil || got.Event.Status != domain.EventDraft {
		t.Fatalf("unpublished %+v, %v", got.Event, err)
	}

	if _, err := svc.Unpublish(ctx, id); !errors.Is(err, ErrEventNotPublished) {
		t.Errorf("unpublished twice: %v", err)
	}

	if _, err := svc.Publish(ctx, ids.New[domain.EventID](f.seq)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown event: %v", err)
	}
}

func TestListAndGetEvents(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)

	empty, err := svc.List(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("no events: %v, %v", empty, err)
	}

	draft, err := svc.Create(ctx, f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	e, sessions := f.published(t, f.eventData())
	f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	f.register(t, e, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// With sessions first; the draft has none.
	if len(list) != 2 || list[0].Event.ID != e.ID || list[1].Event.ID != draft.Event.ID ||
		len(list[0].Sessions) != 2 || list[0].Sessions[0].Lesson.ID != sessions[0].ID ||
		list[0].Sessions[0].Room.Name == "" || list[0].Counts.Registered != 2 || len(list[1].Sessions) != 0 {
		t.Errorf("list %+v", list)
	}

	img := domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nimage")}
	if err := svc.PutImage(ctx, e.ID, img); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(ctx, e.ID)
	if err != nil || got.Image.Digest != img.Digest() || got.Counts.Registered != 2 || len(got.Sessions) != 2 {
		t.Errorf("get %+v, %v", got, err)
	}

	if _, err := svc.Get(ctx, ids.New[domain.EventID](f.seq)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown event: %v", err)
	}
}

func TestEventImage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)
	e, _ := f.published(t, f.eventData())
	img := domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nimage")}

	if got, err := svc.Image(ctx, e.ID); err != nil || !got.IsZero() {
		t.Errorf("no image: %+v, %v", got, err)
	}

	wantFieldError(t, svc.PutImage(ctx, e.ID, domain.Background{ContentType: "image/gif", Data: []byte("GIF")}),
		"contentType")
	wantFieldError(t, svc.PutImage(ctx, e.ID, domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("x")}),
		"data")

	if err := svc.PutImage(ctx, e.ID, img); err != nil {
		t.Fatal(err)
	}

	if got, err := svc.Image(ctx, e.ID); err != nil || got.Digest() != img.Digest() {
		t.Errorf("image %+v, %v", got, err)
	}

	if err := svc.DeleteImage(ctx, e.ID); err != nil {
		t.Fatal(err)
	}

	if got, err := svc.Image(ctx, e.ID); err != nil || !got.IsZero() {
		t.Errorf("deleted image: %+v, %v", got, err)
	}

	unknown := ids.New[domain.EventID](f.seq)
	if _, err := svc.Image(ctx, unknown); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("image of an unknown event: %v", err)
	}

	if err := svc.PutImage(ctx, unknown, img); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("put image of an unknown event: %v", err)
	}
}

func TestAddSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)

	draft, err := svc.Create(ctx, f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	id := draft.Event.ID
	d := f.sessionAt(t, "2026-10-23", 18)
	d.AccompanistID = f.pianist

	got, err := svc.AddSession(ctx, id, d)
	if err != nil {
		t.Fatal(err)
	}

	l := got.Lesson
	if l.EventID != id || l.Kind != domain.LessonWorkshop || l.Visibility != domain.VisibilityPublic ||
		l.Capacity != 2 || l.CourseTypeID != f.courseType || !l.ClassID.IsZero() ||
		!l.StartsAt.Equal(mustInstant(t, "2026-10-23T16:00:00Z")) || l.EndsAt.Sub(l.StartsAt) != 75*time.Minute ||
		got.Accompanist.ID != f.pianist || got.Admin == nil {
		t.Errorf("session %+v", got)
	}

	// The same room at the same time.
	var taken *RoomTakenError
	if _, err := svc.AddSession(ctx, id, f.sessionAt(t, "2026-10-23", 18)); !errors.As(err, &taken) ||
		taken.Lesson.ID != l.ID {
		t.Errorf("room taken: %v", err)
	}

	overlap := f.sessionAt(t, "2026-10-23", 18)
	overlap.ConfirmRoomOverlap = true

	if _, err := svc.AddSession(ctx, id, overlap); err != nil {
		t.Errorf("confirmed overlap: %v", err)
	}

	past := f.sessionAt(t, "2026-10-20", 18)
	if _, err := svc.AddSession(ctx, id, past); !errors.Is(err, ErrLessonPast) {
		t.Errorf("past: %v", err)
	}

	past.ConfirmPast = true
	if _, err := svc.AddSession(ctx, id, past); err != nil {
		t.Errorf("confirmed past: %v", err)
	}

	bad := f.sessionAt(t, "2026-10-25", 18)
	bad.RoomID = ids.New[domain.RoomID](f.seq)
	_, err = svc.AddSession(ctx, id, bad)
	wantFieldError(t, err, "roomId")

	bad = f.sessionAt(t, "2026-10-25", 18)
	bad.Duration = 0
	_, err = svc.AddSession(ctx, id, bad)
	wantFieldError(t, err, "durationMinutes")

	if _, err := svc.AddSession(ctx, ids.New[domain.EventID](f.seq), f.sessionAt(t, "2026-10-26", 18)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown event: %v", err)
	}

	if _, err := svc.Publish(ctx, id); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Cancel(ctx, f.office, id, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddSession(ctx, id, f.sessionAt(t, "2026-10-26", 18)); !errors.Is(err, ErrEventClosed) {
		t.Errorf("canceled event: %v", err)
	}
}

// TestSessionLessonOps: the lesson administration keeps a session's
// capacity its event's, deletes one only while its event has no open
// registration, and mails a session's cancellation to the event's
// registered persons and guests.
func TestSessionLessonOps(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	lessons := f.lessonService(t, bookingNow)

	d := dataOf(t, sessions[0])
	d.Capacity = 3
	_, err := lessons.UpdateLesson(ctx, sessions[0].ID, d)
	wantFieldError(t, err, "capacity")

	d.Capacity, d.StartTime = 2, domain.TimeOfDay{Hour: 19}
	if _, err := lessons.UpdateLesson(ctx, sessions[0].ID, d); err != nil {
		t.Errorf("moved session: %v", err)
	}

	r := f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	f.register(t, e, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})
	f.register(t, e, NewRegistration{PersonID: f.tom.ID, ConfirmOverCapacity: true})

	if err := lessons.DeleteLesson(ctx, sessions[1].ID); !errors.Is(err, domain.ErrInUse) {
		t.Errorf("delete with registrations: %v, want ErrInUse", err)
	}

	before := len(f.mails(t))

	if _, err := lessons.CancelLesson(ctx, f.office, sessions[1].ID, "The studio is closed."); err != nil {
		t.Fatal(err)
	}

	var to []string

	for _, m := range f.mails(t)[before:] {
		if m.Template != "lesson-canceled" {
			t.Errorf("mail %s", m.Template)
		}

		to = append(to, m.To)
	}

	// Mia's guardian Anna, the guest, and Tom's own account.
	if !slices.Equal(to, []string{"anna@example.org", "gina@example.org", "tom@example.org"}) {
		t.Errorf("mailed %v", to)
	}

	if _, err := f.events(t).CancelRegistration(ctx, f.office, r.ID, false); err != nil {
		t.Fatal(err)
	}

	// Canceled registrations are mailed no longer.
	before = len(f.mails(t))
	if _, err := lessons.RestoreLesson(ctx, sessions[1].ID, false); err != nil {
		t.Fatal(err)
	}

	if mails := f.mails(t)[before:]; len(mails) != 2 || mails[0].To != "gina@example.org" {
		t.Errorf("restore mailed %+v", mails)
	}

	// A session of an event without registrations is deleted.
	draft, err := f.events(t).Create(ctx, f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	sl, err := f.events(t).AddSession(ctx, draft.Event.ID, f.sessionAt(t, "2026-10-26", 18))
	if err != nil {
		t.Fatal(err)
	}

	if err := lessons.DeleteLesson(ctx, sl.Lesson.ID); err != nil {
		t.Errorf("delete a session without registrations: %v", err)
	}
}

// TestEventSessionBrokenRoom: a session whose room is missing is broken
// data in the office's event views, not a missing event.
func TestEventSessionBrokenRoom(t *testing.T) {
	t.Parallel()

	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	dropRows(t, f.st, "rooms", f.room.String())

	_, err := f.events(t).Get(t.Context(), e.ID)
	wantBroken(t, "event", err)
}
