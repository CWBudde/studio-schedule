package service

import (
	"context"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func TestEventAnnouncementAudiences(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	f.register(t, e, NewRegistration{GuestName: "Example Guest", GuestEmail: "guest@example.org"})
	ctx := t.Context()
	svc := NewAnnouncementService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, newTestRenderer(t), nil)
	for _, aud := range []AnnouncementAudience{{Audience: domain.AudienceEvent, EventID: e.ID}, {Audience: domain.AudienceLesson, LessonID: sessions[0].ID}} {
		entry, err := svc.Create(ctx, f.office, AnnouncementInput{AnnouncementAudience: aud, Title: string(aud.Audience), Body: "Room changed.", Email: true})
		if err != nil {
			t.Fatal(err)
		}
		if aud.Audience == domain.AudienceEvent && entry.Event.Title != e.Title {
			t.Errorf("event title %q", entry.Event.Title)
		}
		if n, err := svc.CountRecipients(ctx, aud); err != nil || n != 2 {
			t.Errorf("recipient count %d, %v", n, err)
		}
	}
	for _, tc := range []struct {
		person domain.Person
		want   int
	}{{f.anna, 2}, {f.tom, 0}} {
		got, err := svc.ForAccount(ctx, f.principal(t, tc.person))
		if err != nil || len(got) != tc.want {
			t.Errorf("%s sees %v, %v", tc.person.GivenName, titles(got), err)
		}
	}
	if n, err := svc.SendDue(ctx); err != nil || n != 4 {
		t.Errorf("mails %d, %v", n, err)
	}
	var addresses []string
	for _, m := range f.mailsOf(t, mail.TemplateAnnouncement) {
		addresses = append(addresses, m.To)
	}
	slices.Sort(addresses)
	if !slices.Equal(addresses, []string{"anna@example.org", "anna@example.org", "guest@example.org", "guest@example.org"}) {
		t.Errorf("mailed to %v", addresses)
	}
}

func TestEventRegistrationFeed(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	f.register(t, e, NewRegistration{PersonID: f.anna.ID})
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		l := sessions[1]
		l.Status, l.CanceledAt = domain.LessonCanceled, mustInstant(t, bookingNow)
		return tx.Lessons().Update(ctx, l)
	})
	svc := NewCalendarFeedService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, feedAppURL, true)
	feed := createFeed(t, svc, f.principal(t, f.anna), domain.FeedBookings, domain.LocationID{})
	doc := renderFeed(t, svc, feed.Token)
	wantFeedLessons(t, doc, sessions...)
	goldenFeed(t, "registered-sessions", doc)
}

func TestEventSessionParticipantsAndAvailability(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	f.register(t, e, NewRegistration{GuestName: "Example Guest", GuestEmail: "guest@example.org"})
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return tx.Persons().Update(ctx, f.mia.Anonymize(mustInstant(t, bookingNow)))
	})
	clk := clock.NewFixed(mustInstant(t, bookingNow))
	svc := NewParticipantService(f.st, clk, f.seq, testMails(newTestRenderer(t)))
	result, err := svc.Participants(t.Context(), f.office, sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Registrants) != 2 || len(result.Bookings) != 0 || result.Active != 2 || result.FreePlaces() != 0 {
		t.Fatalf("participants %+v", result)
	}
	if !result.Registrants[0].Anonymized || result.Registrants[0].Name != "Deleted person" || result.Registrants[1].Name != "Example Guest" {
		t.Errorf("registrants %+v", result.Registrants)
	}
	schedule := NewScheduleService(f.st, clk)
	sl, err := schedule.Lesson(t.Context(), Principal{}, sessions[0].ID)
	if err != nil || sl.FreePlaces != 0 {
		t.Errorf("session detail free %d, %v", sl.FreePlaces, err)
	}
	week, err := schedule.Schedule(t.Context(), Principal{}, ScheduleQuery{From: mustDate(t, "2026-10-23"), To: mustDate(t, "2026-10-25")})
	if err != nil || len(week.Lessons) != 2 || week.Lessons[1].FreePlaces != 0 {
		t.Errorf("schedule %+v, %v", week.Lessons, err)
	}
}

func TestEventInviteHoldsSessionPlaces(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newEventFixture(t)
	d := f.eventData()
	d.Capacity = 1
	e, sessions := f.published(t, d)
	occupant := f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	parent := f.principal(t, f.anna)
	svc := f.events(t)
	if _, err := svc.JoinWaitlist(ctx, parent, e.ID, f.tom.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelOwn(ctx, parent, occupant.ID); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFixed(mustInstant(t, bookingNow))
	pt, err := NewParticipantService(f.st, clk, f.seq, testMails(newTestRenderer(t))).Participants(ctx, f.office, sessions[0].ID)
	if err != nil || len(pt.Registrants) != 0 || pt.Active != 0 || pt.Held != 1 || pt.FreePlaces() != 0 {
		t.Fatalf("invited participants %+v, %v", pt, err)
	}
	sl, err := NewScheduleService(f.st, clk).Lesson(ctx, Principal{}, sessions[0].ID)
	if err != nil || sl.FreePlaces != 0 || sl.Booked != 0 {
		t.Errorf("held session %+v, %v", sl, err)
	}
	feeds := NewCalendarFeedService(f.st, clk, f.seq, feedAppURL, true)
	feed := createFeed(t, feeds, parent, domain.FeedBookings, domain.LocationID{})
	wantFeedLessons(t, renderFeed(t, feeds, feed.Token))
}

func TestEventStatistics(t *testing.T) {
	t.Parallel()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	f.paid(t, f.register(t, e, NewRegistration{PersonID: f.mia.ID}))
	svc := NewStatisticsService(f.st, clock.NewFixed(mustInstant(t, bookingNow)))
	st, err := svc.Statistics(t.Context(), StatisticsQuery{Range: StatisticsCustom, From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-24")})
	if err != nil {
		t.Fatal(err)
	}
	if st.Totals.EventFees != e.Price || st.Totals.PassesSold != 0 || st.Totals.Gross != 0 || st.Totals.Lessons != 0 {
		t.Errorf("totals %+v", st.Totals)
	}
	if len(st.Events) != 1 || st.Events[0] != (store.EventStats{EventID: e.ID, Title: e.Title, Registered: 1, Revenue: e.Price}) {
		t.Errorf("events %+v", st.Events)
	}
}
