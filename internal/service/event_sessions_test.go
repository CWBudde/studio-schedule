package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The guards of an event's sessions (ADR 0019; ADR 0009, amendment): a
// session is not booked one by one, has no waiting list of its own, is
// never cloned, and the sessions of a draft event are staff's only.

// storeEvent stores an event of the fixture's course type with the status,
// ten places and the price (VAT 19 % if priced), created a week before
// bookingNow.
func (f *genFixture) storeEvent(t *testing.T, status domain.EventStatus, price domain.Money) domain.Event {
	t.Helper()

	created := mustInstant(t, bookingNow).Add(-7 * 24 * time.Hour)
	id := ids.New[domain.EventID](f.seq)
	e := domain.Event{
		ID: id, Slug: "event-" + id.String()[24:], Title: "Imaginary Intensive", CourseTypeID: f.courseType,
		Capacity: 10, Price: price, Status: status, CreatedAt: created, UpdatedAt: created,
	}

	if price > 0 {
		e.VATRate = 1900
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Events().Create(ctx, e) })

	return e
}

// storeSession stores a public one-hour workshop session of ev at start in
// the fixture's room, with the event's capacity.
func (f *genFixture) storeSession(t *testing.T, ev domain.Event, start string) domain.Lesson {
	t.Helper()

	at := mustInstant(t, start)
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), CourseTypeID: ev.CourseTypeID, RoomID: f.room, InstructorID: f.instructor,
		StartsAt: at, EndsAt: at.Add(time.Hour), Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic,
		Kind: domain.LessonWorkshop, Capacity: ev.Capacity, EventID: ev.ID,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// TestEventSessionNotBookable: booking a session, adding a participant to
// it and joining its waiting list are ErrEventSession; for a member, a
// session of a draft event does not exist.
func TestEventSessionNotBookable(t *testing.T) {
	t.Parallel()

	f := newStaffFixture(t)
	published := f.storeSession(t, f.storeEvent(t, domain.EventPublished, 0), waitlistLesson)
	draft := f.storeSession(t, f.storeEvent(t, domain.EventDraft, 0), waitlistLesson)
	anna, office := f.principal(t, f.anna), f.principal(t, f.olga)

	for _, c := range []struct {
		name   string
		lesson domain.Lesson
		p      Principal
		want   error
	}{
		{"published", published, anna, ErrEventSession},
		{"draft", draft, anna, domain.ErrNotFound},
		{"draft by the office", draft, office, ErrEventSession},
	} {
		_, err := f.bookingService(t, bookingNow).Book(t.Context(), c.p, c.lesson.ID, c.p.PersonID)
		if !errors.Is(err, c.want) {
			t.Errorf("Book %s: %v, want %v", c.name, err, c.want)
		}

		_, err = f.waitlistService(t, bookingNow).Join(t.Context(), c.p, c.lesson.ID, c.p.PersonID, false)
		if !errors.Is(err, c.want) {
			t.Errorf("Join %s: %v, want %v", c.name, err, c.want)
		}
	}

	for _, l := range []domain.Lesson{published, draft} {
		_, err := f.participants(t, bookingNow).Add(t.Context(), office, l.ID, NewParticipant{PersonID: f.mia.ID})
		if !errors.Is(err, ErrEventSession) {
			t.Errorf("Add: %v, want ErrEventSession", err)
		}
	}

	// Nothing was booked or put on a list.
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		active, err := tx.Bookings().CountActiveByLessons(ctx, []domain.LessonID{published.ID, draft.ID})
		if err != nil {
			return err
		}

		open, err := tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{published.ID, draft.ID})
		if len(active) > 0 || len(open) > 0 {
			t.Errorf("bookings %v, waiting %v", active, open)
		}

		return err
	})
}

// TestDraftSessionsHidden: the schedule, a lesson and the schedule feed
// show a draft event's sessions to staff only; published and canceled
// events' sessions to everyone who sees the lesson.
func TestDraftSessionsHidden(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	draft := f.storeSession(t, f.storeEvent(t, domain.EventDraft, 0), "2026-10-23T16:00:00Z")
	published := f.storeSession(t, f.storeEvent(t, domain.EventPublished, 0), "2026-10-23T18:00:00Z")
	canceled := f.storeSession(t, f.storeEvent(t, domain.EventCanceled, 0), "2026-10-24T16:00:00Z")
	plain := f.regular(t, "2026-10-24T18:00:00Z")
	svc := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, bookingNow)))
	q := ScheduleQuery{From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-28")}
	everyone := []domain.LessonID{published.ID, canceled.ID, plain.ID}

	for name, c := range map[string]struct {
		p    Principal
		want []domain.LessonID
	}{
		"guest":  {Principal{}, everyone},
		"member": {f.principal(t, f.anna), everyone},
		"office": {f.principal(t, f.olga), []domain.LessonID{draft.ID, published.ID, canceled.ID, plain.ID}},
	} {
		got, err := svc.Schedule(t.Context(), c.p, q)
		if err != nil {
			t.Fatal(err)
		}

		if ids := mapSlice(got.Lessons, func(sl ScheduledLesson) domain.LessonID { return sl.Lesson.ID }); !slices.Equal(ids, c.want) {
			t.Errorf("%s sees %v, want %v", name, ids, c.want)
		}

		_, err = svc.Lesson(t.Context(), c.p, draft.ID)
		if staff := name == "office"; (err == nil) != staff || !staff && !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s reads the draft's session: %v", name, err)
		}

		if _, err := svc.Lesson(t.Context(), c.p, published.ID); err != nil {
			t.Errorf("%s reads the published session: %v", name, err)
		}
	}

	feeds := f.feeds(t, bookingNow)

	for p, want := range map[domain.Person][]domain.LessonID{
		f.anna: everyone,
		f.olga: {draft.ID, published.ID, canceled.ID, plain.ID},
	} {
		feed := createFeed(t, feeds, f.principal(t, p), domain.FeedSchedule, domain.LocationID{})
		if got := lessonIDsIn(renderFeed(t, feeds, feed.Token)); !slices.Equal(got, want) {
			t.Errorf("%s's feed has %v, want %v", p.GivenName, got, want)
		}
	}
}

// TestDraftSessionStoreFaults: a failing read of the sessions' events is
// returned by every path that hides draft sessions or gives a session its
// event's slug.
func TestDraftSessionStoreFaults(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	session := f.storeSession(t, f.storeEvent(t, domain.EventPublished, 0), "2026-10-23T16:00:00Z")
	anna, office := f.principal(t, f.anna), f.principal(t, f.olga)
	feed := createFeed(t, f.feeds(t, bookingNow), anna, domain.FeedSchedule, domain.LocationID{})
	now := clock.NewFixed(mustInstant(t, bookingNow))
	fs := NewFaultStore(f.st)

	for name, op := range map[string]func() error{
		"Schedule": func() error {
			_, err := NewScheduleService(fs, now).Schedule(t.Context(), anna, ScheduleQuery{
				From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-28"),
			})

			return err
		},
		"Lesson": func() error {
			_, err := NewScheduleService(fs, now).Lesson(t.Context(), anna, session.ID)

			return err
		},
		"Schedule by the office": func() error {
			_, err := NewScheduleService(fs, now).Schedule(t.Context(), office, ScheduleQuery{
				From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-28"),
			})

			return err
		},
		"Lesson by the office": func() error {
			_, err := NewScheduleService(fs, now).Lesson(t.Context(), office, session.ID)

			return err
		},
		"Teach": func() error {
			_, err := NewTeachService(fs, now, f.seq, newTestRenderer(t)).Lessons(t.Context(), office, TeachQuery{})

			return err
		},
		"Render": func() error {
			_, err := NewCalendarFeedService(fs, now, f.seq, feedAppURL, true).Render(t.Context(), feed.Token)

			return err
		},
		"Book": func() error {
			_, err := NewBookingService(fs, now, f.seq, testMails(newTestRenderer(t))).
				Book(t.Context(), anna, session.ID, f.anna.ID)

			return err
		},
	} {
		fs.Fail("Events.ListByIDs", fs.Count("Events.ListByIDs")+1)

		if err := op(); !errors.Is(err, ErrInjected) {
			t.Errorf("%s: %v, want the injected fault", name, err)
		}
	}
}

// TestCloneSkipsSessions: a clone neither copies an event's session nor
// lists it as skipped.
func TestCloneSkipsSessions(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	session := f.storeSession(t, f.storeEvent(t, domain.EventPublished, 0), "2026-10-23T16:00:00Z")
	plain := f.regular(t, "2026-10-23T08:00:00Z")

	got, err := f.lessonService(t, bookingNow).Clone(t.Context(), CloneRequest{
		Unit: CloneDay, Source: mustDate(t, "2026-10-23"), Target: mustDate(t, "2026-10-30"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Created) != 1 || got.Created[0].Source.Lesson.ID != plain.ID || len(got.Skipped) != 0 {
		t.Errorf("clone %+v, want only %v copied (not %v)", got, plain.ID, session.ID)
	}
}

// TestSessionEventSlug: the schedule, a lesson and the instructor view give
// a session its event's slug and a plain lesson none, for a member and for
// staff, with one read of the events per response that has a session and
// none for a response without one.
func TestSessionEventSlug(t *testing.T) {
	t.Parallel()

	f := newStaffFixture(t)
	ev := f.storeEvent(t, domain.EventPublished, 0)
	session := f.storeSession(t, ev, "2026-10-23T16:00:00Z")
	plain := f.regular(t, "2026-10-24T18:00:00Z")
	want := map[domain.LessonID]string{session.ID: ev.Slug, plain.ID: ""}
	fs := NewFaultStore(f.st)
	now := clock.NewFixed(mustInstant(t, bookingNow))
	schedule := NewScheduleService(fs, now)
	teach := NewTeachService(fs, now, f.seq, newTestRenderer(t))
	week := ScheduleQuery{From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-28")}
	saturday := ScheduleQuery{From: mustDate(t, "2026-10-24"), To: mustDate(t, "2026-10-25")}

	// reads runs op and returns how often it read events.
	reads := func(op func() error) int {
		t.Helper()

		before := fs.Count("Events.ListByIDs")
		if err := op(); err != nil {
			t.Fatal(err)
		}

		return fs.Count("Events.ListByIDs") - before
	}

	// slugs checks the slugs of the lessons of want among got, which must
	// have them all.
	slugs := func(name string, got []ScheduledLesson) {
		t.Helper()

		found := 0

		for _, sl := range got {
			if slug, ok := want[sl.Lesson.ID]; ok {
				found++

				if sl.EventSlug != slug {
					t.Errorf("%s: lesson %v has the slug %q, want %q", name, sl.Lesson.ID, sl.EventSlug, slug)
				}
			}
		}

		if found != len(want) {
			t.Errorf("%s: %d of the lessons %v, want all", name, found, want)
		}
	}

	ivy, olga := f.principal(t, f.ivy), f.principal(t, f.olga)

	for name, p := range map[string]Principal{"member": f.principal(t, f.anna), "instructor": ivy, "office": olga} {
		var got Schedule

		if n := reads(func() (err error) { got, err = schedule.Schedule(t.Context(), p, week); return err }); n != 1 {
			t.Errorf("%s: the schedule read events %d times, want once", name, n)
		}

		slugs(name+"'s schedule", got.Lessons)

		if n := reads(func() (err error) { got, err = schedule.Schedule(t.Context(), p, saturday); return err }); n != 0 {
			t.Errorf("%s: a schedule without a session read events %d times", name, n)
		}

		for id, slug := range want {
			var sl ScheduledLesson

			n := reads(func() (err error) { sl, err = schedule.Lesson(t.Context(), p, id); return err })
			if sl.EventSlug != slug || n != min(len(slug), 1) {
				t.Errorf("%s: lesson %v has the slug %q after %d reads, want %q", name, id, sl.EventSlug, n, slug)
			}
		}
	}

	for name, p := range map[string]Principal{"instructor": ivy, "office": olga} {
		var got TeachLessons

		if n := reads(func() (err error) { got, err = teach.Lessons(t.Context(), p, TeachQuery{}); return err }); n != 1 {
			t.Errorf("%s: the instructor view read events %d times, want once", name, n)
		}

		slugs(name+"'s instructor view", mapSlice(got.Lessons, func(l TeachLesson) ScheduledLesson { return l.ScheduledLesson }))

		q := TeachQuery{From: saturday.From, To: saturday.To}
		if n := reads(func() (err error) { got, err = teach.Lessons(t.Context(), p, q); return err }); n != 0 || len(got.Lessons) == 0 {
			t.Errorf("%s: an instructor view of %d lessons without a session read events %d times", name, len(got.Lessons), n)
		}
	}
}
