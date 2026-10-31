package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// faultFeeds returns the feed service at bookingNow on a FaultStore over
// the fixture's store.
func (f *feedFixture) faultFeeds(t *testing.T) (*FaultStore, *CalendarFeedService) {
	t.Helper()

	fs := NewFaultStore(f.st)

	return fs, NewCalendarFeedService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, feedAppURL, true)
}

// withFeedLessons adds lessons to every kind of feed of the fixture: a
// lesson of a Thursday class that Ivy teaches and Mia is booked for, and
// one in the second location's room.
func (f *feedFixture) withFeedLessons(t *testing.T) {
	t.Helper()

	class := f.addClass(t, time.Thursday, domain.TimeOfDay{Hour: 18})
	l := f.lessonAt(t, "2026-10-22T16:00:00Z", func(l *domain.Lesson) {
		l.ClassID, l.SlotDate = class.ID, mustDate(t, "2026-10-22")
	})
	f.bookFor(t, l, f.mia)
	f.lessonAt(t, "2026-10-23T16:00:00Z", func(l *domain.Lesson) { l.RoomID = f.room2 })
}

// TestCalendarFeedStoreFaults: a store failure while a feed is created
// keeps the old link and adds none; reading the feeds or rendering one,
// whatever its scope, returns the failure instead of a calendar with
// lessons missing, and records no use.
func TestCalendarFeedStoreFaults(t *testing.T) {
	t.Parallel()

	// A feedCase prepares the fixture through its own store and returns the
	// operation on the faulty service.
	type feedCase func(t *testing.T, f *feedFixture) func(svc *CalendarFeedService) error

	sweep := func(c feedCase) FaultSweep {
		return FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newFeedFixture(t)
			f.withFeedLessons(t)
			op := c(t, f)
			fs, svc := f.faultFeeds(t)

			return fs, func() error { return op(svc) }
		}}
	}

	// render creates a feed of the person and renders it.
	render := func(person func(f *feedFixture) domain.Person, scope domain.FeedScope, location bool) feedCase {
		return func(t *testing.T, f *feedFixture) func(svc *CalendarFeedService) error {
			var loc domain.LocationID
			if location {
				loc = f.elsewhere.ID
			}

			feed := createFeed(t, f.feeds(t, bookingNow), f.principal(t, person(f)), scope, loc)

			return func(svc *CalendarFeedService) error {
				doc, err := svc.Render(t.Context(), feed.Token)
				if err == nil && len(lessonIDsIn(doc)) == 0 {
					t.Fatalf("an empty feed:\n%s", doc.Body)
				}

				return err
			}
		}
	}

	// create replaces the person's feed of the scope with a new one.
	create := func(person func(f *feedFixture) domain.Person, scope domain.FeedScope, location bool) feedCase {
		return func(t *testing.T, f *feedFixture) func(svc *CalendarFeedService) error {
			var loc domain.LocationID
			if location {
				loc = f.elsewhere.ID
			}

			p := f.principal(t, person(f))
			createFeed(t, f.feeds(t, bookingNow), p, scope, loc)

			return func(svc *CalendarFeedService) error {
				_, err := svc.Create(t.Context(), p, scope, loc)

				return err
			}
		}
	}

	anna := func(f *feedFixture) domain.Person { return f.anna }
	ivy := func(f *feedFixture) domain.Person { return f.ivy }

	for name, c := range map[string]feedCase{
		"Create schedule": create(anna, domain.FeedSchedule, true),
		"Create teaching": create(ivy, domain.FeedTeaching, false),
		"List": func(t *testing.T, f *feedFixture) func(svc *CalendarFeedService) error {
			p := f.principal(t, f.anna)
			createFeed(t, f.feeds(t, bookingNow), p, domain.FeedSchedule, f.elsewhere.ID)
			createFeed(t, f.feeds(t, bookingNow), p, domain.FeedBookings, domain.LocationID{})

			return func(svc *CalendarFeedService) error {
				infos, err := svc.List(t.Context(), p)
				if err == nil && (len(infos) != 2 || infos[0].LocationName != "Studio Elsewhere") {
					t.Fatalf("feeds %+v", infos)
				}

				return err
			}
		},
		"Render bookings": render(anna, domain.FeedBookings, false),
		"Render teaching": render(ivy, domain.FeedTeaching, false),
		"Render schedule": render(anna, domain.FeedSchedule, true),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			SweepFaults(t, sweep(c))
		})
	}
}

// TestCalendarFeedUsedMeanwhile: a feed deleted (with its location) while
// it was rendered still gets its document; there is only no use left to
// record.
func TestCalendarFeedUsedMeanwhile(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	feed := createFeed(t, f.feeds(t, bookingNow), f.principal(t, f.anna), domain.FeedSchedule, domain.LocationID{})
	fs, svc := f.faultFeeds(t)
	fs.FailWith("CalendarFeeds.TouchLastUsed", 1, domain.ErrNotFound)

	if doc, err := svc.Render(t.Context(), feed.Token); err != nil || len(doc.Body) == 0 {
		t.Fatalf("Render = %v, want the document", err)
	}
}

// TestCalendarFeedTeachingNothing: an instructor without lessons gets an
// empty calendar, not an error.
func TestCalendarFeedTeachingNothing(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	feed := createFeed(t, svc, f.principal(t, f.ivy), domain.FeedTeaching, domain.LocationID{})

	doc := renderFeed(t, svc, feed.Token)
	if body := string(doc.Body); !strings.Contains(body, "BEGIN:VCALENDAR") || strings.Contains(body, "BEGIN:VEVENT") {
		t.Errorf("want an empty calendar:\n%s", body)
	}
}

// TestCalendarFeedUnknownScope: a stored feed of a scope the service does
// not know (only broken data: PostgreSQL's CHECK refuses one, the memory
// store does not) renders no document. It fails as a validation error.
func TestCalendarFeedUnknownScope(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	token := "broken-feed-token"
	owner := f.principal(t, f.olga).AccountID
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return tx.CalendarFeeds().Create(ctx, domain.CalendarFeed{
			ID: ids.New[domain.CalendarFeedID](f.seq), AccountID: owner, Scope: "lessons",
			TokenHash: domain.HashToken(token), CreatedAt: mustInstant(t, bookingNow),
		})
	})

	doc, err := f.feeds(t, bookingNow).Render(t.Context(), token)
	if !errors.Is(err, domain.ErrValidation) || len(doc.Body) != 0 {
		t.Fatalf("Render = %d bytes, %v; want no document and a validation error", len(doc.Body), err)
	}
}

// TestFeedLabelsFallBack: a feed for a language without labels is in
// English; a regional or underscored tag finds its language.
func TestFeedLabelsFallBack(t *testing.T) {
	t.Parallel()

	for locale, want := range map[string]string{"fr-CH": "en", "": "en", "de_AT": "de", "DE": "de"} {
		if got := feedLabelsFor(locale); got != feedLabelSets[want] {
			t.Errorf("%q: labels %q, want those of %q", locale, got.bookings, want)
		}
	}
}
