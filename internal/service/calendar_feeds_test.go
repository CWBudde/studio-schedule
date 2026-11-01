package service

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

var updateFeeds = flag.Bool("update", false, "rewrite the golden calendar feeds in testdata/feeds")

// The calendar feed tests run on the booking fixture (Europe/Berlin, see
// bookingNow): the schedule is released until Monday 2026-10-26, members
// see regular lessons until Thursday 2026-11-05, and lessons are generated
// (the horizon) until Thursday 2026-11-19. The clocks go back on Sunday
// 2026-10-25. Feeds reach back 28 days: to Wednesday 2026-09-23.

const feedAppURL = "https://school.example/"

// feedFixture adds to the booking fixture Ivy, an instructor whose person
// is linked to the staff record "Ada Example", and a second location.
type feedFixture struct {
	*bookingFixture

	ivy       domain.Person
	elsewhere domain.Location
	room2     domain.RoomID
}

func newFeedFixture(t *testing.T) *feedFixture {
	t.Helper()

	f := &feedFixture{bookingFixture: newBookingFixture(t)}
	f.ivy = f.person(t, "Ivy", "", "ivy@example.org", "")
	f.elsewhere = domain.Location{
		ID: ids.New[domain.LocationID](f.seq), Name: "Studio Elsewhere", Address: "1 Example Road\n12345 Sampletown",
	}
	f.room2 = ids.New[domain.RoomID](f.seq)

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().GetByPerson(ctx, f.ivy.ID)
		if err != nil {
			return err
		}

		acc.Role = domain.RoleInstructor

		st, err := tx.Staff().Get(ctx, f.instructor)
		if err != nil {
			return err
		}

		st.PersonID = f.ivy.ID

		return errors.Join(
			tx.Accounts().Update(ctx, acc),
			tx.Staff().Update(ctx, st),
			tx.Locations().Create(ctx, f.elsewhere),
			tx.Rooms().Create(ctx, domain.Room{ID: f.room2, LocationID: f.elsewhere.ID, Name: "Room Two", Capacity: 8, Position: 1}),
		)
	})

	return f
}

func (f *feedFixture) feeds(t *testing.T, now string) *CalendarFeedService {
	t.Helper()

	return NewCalendarFeedService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, feedAppURL, true)
}

// lessonAt stores a scheduled, public, regular one-off lesson of an hour
// from start, changed by edit.
func (f *feedFixture) lessonAt(t *testing.T, start string, edit func(*domain.Lesson)) domain.Lesson {
	t.Helper()

	at := mustInstant(t, start)
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor,
		StartsAt: at, EndsAt: at.Add(time.Hour), Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic,
		Kind: domain.LessonRegular, Capacity: 5,
	}
	if edit != nil {
		edit(&l)
	}

	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// account returns the account of person p.
func (f *feedFixture) account(t *testing.T, p domain.Person) domain.Account {
	t.Helper()

	var acc domain.Account

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		acc, err = tx.Accounts().GetByPerson(ctx, p.ID)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return acc
}

// editAccount changes the account of p.
func (f *feedFixture) editAccount(t *testing.T, p domain.Person, edit func(*domain.Account)) {
	t.Helper()

	acc := f.account(t, p)
	edit(&acc)
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, acc) })
}

// create makes a feed of p's account and fails the test on error.
func createFeed(
	t *testing.T, svc *CalendarFeedService, p Principal, scope domain.FeedScope, location domain.LocationID,
) NewCalendarFeed {
	t.Helper()

	feed, err := svc.Create(t.Context(), p, scope, location)
	if err != nil {
		t.Fatal(err)
	}

	return feed
}

func renderFeed(t *testing.T, svc *CalendarFeedService, token string) FeedDocument {
	t.Helper()

	doc, err := svc.Render(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}

	if body := string(doc.Body); strings.Contains(strings.ReplaceAll(body, "\r\n", ""), "\n") {
		t.Fatalf("not every line ends in CRLF:\n%q", body)
	}

	return doc
}

// golden compares the document with testdata/feeds/name.ics, whose lines
// end in LF (the document's in CRLF); -update rewrites the file.
func goldenFeed(t *testing.T, name string, doc FeedDocument) {
	t.Helper()

	got := strings.ReplaceAll(string(doc.Body), "\r\n", "\n")
	path := filepath.Join("testdata", "feeds", name+".ics")

	if *updateFeeds {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}

	if got != string(want) {
		t.Errorf("%s differs from %s (run with -update to accept):\n%s", name, path, got)
	}
}

// lessonIDsIn returns the IDs of the lessons whose events doc has, in order.
func lessonIDsIn(doc FeedDocument) []domain.LessonID {
	var out []domain.LessonID

	for line := range strings.Lines(string(doc.Body)) {
		uid, ok := strings.CutPrefix(strings.TrimSpace(line), "UID:")
		if !ok {
			continue
		}

		id, err := domain.ParseID[domain.LessonID](uid[:36])
		if err != nil {
			panic(err)
		}

		out = append(out, id)
	}

	return out
}

func wantFeedLessons(t *testing.T, doc FeedDocument, want ...domain.Lesson) {
	t.Helper()

	got := lessonIDsIn(doc)

	ok := len(got) == len(want)
	for i := range want {
		ok = ok && got[i] == want[i].ID
	}

	if !ok {
		ids := make([]domain.LessonID, len(want))
		for i, l := range want {
			ids[i] = l.ID
		}

		t.Fatalf("lessons %v, want %v\n%s", got, ids, doc.Body)
	}
}

func TestCalendarFeedCreate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	anna := f.principal(t, f.anna)

	first := createFeed(t, svc, anna, domain.FeedBookings, domain.LocationID{})
	if len(first.Token) < 40 || first.Path != "/ical/"+first.Token+".ics" ||
		first.URL != "https://school.example/ical/"+first.Token+".ics" {
		t.Fatalf("new feed %+v", first)
	}

	if first.Feed.TokenHash != domain.HashToken(first.Token) || first.Feed.AccountID != anna.AccountID ||
		first.Feed.Scope != domain.FeedBookings || !first.Feed.CreatedAt.Equal(mustInstant(t, bookingNow)) {
		t.Errorf("feed %+v", first.Feed)
	}

	local := createFeed(t, svc, anna, domain.FeedSchedule, f.elsewhere.ID)
	if local.LocationName != "Studio Elsewhere" {
		t.Errorf("location name %q", local.LocationName)
	}

	school := createFeed(t, svc, anna, domain.FeedSchedule, domain.LocationID{})

	// A new link of a subject revokes the old one.
	second := createFeed(t, svc, anna, domain.FeedBookings, domain.LocationID{})
	if second.Token == first.Token {
		t.Fatal("the new link has the old token")
	}

	if _, err := svc.Render(ctx, first.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old link: %v, want not found", err)
	}

	renderFeed(t, svc, second.Token)

	list, err := svc.List(ctx, anna)
	if err != nil {
		t.Fatal(err)
	}

	var got []domain.CalendarFeedID
	for _, it := range list {
		got = append(got, it.Feed.ID)
	}

	if len(got) != 3 || got[0] != local.Feed.ID || got[1] != school.Feed.ID || got[2] != second.Feed.ID ||
		list[0].LocationName != "Studio Elsewhere" || list[1].LocationName != "" {
		t.Errorf("list %+v", list)
	}

	// Nobody else's.
	if list, err := svc.List(ctx, f.principal(t, f.tom)); err != nil || len(list) != 0 {
		t.Errorf("tom's feeds: %+v, %v", list, err)
	}

	// The app's hash route is not part of the feed's URL.
	hashed := NewCalendarFeedService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, "https://demo.example/app/#/", true)
	if feed := createFeed(t, hashed, f.principal(t, f.tom), domain.FeedBookings, domain.LocationID{}); feed.URL !=
		"https://demo.example/app/ical/"+feed.Token+".ics" {
		t.Errorf("URL with a hash route %q", feed.URL)
	}

	// No URL without the app's URL, or without a server calendars reach.
	for _, svc := range []*CalendarFeedService{
		NewCalendarFeedService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, "", true),
		NewCalendarFeedService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, feedAppURL, false),
	} {
		if feed := createFeed(t, svc, f.principal(t, f.olga), domain.FeedBookings, domain.LocationID{}); feed.URL != "" ||
			feed.Path != "/ical/"+feed.Token+".ics" {
			t.Errorf("URL %+v", feed)
		}
	}
}

func TestCalendarFeedCreateRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	anna, ivy, olga := f.principal(t, f.anna), f.principal(t, f.ivy), f.principal(t, f.olga)

	wantField := func(err error, field string) {
		t.Helper()

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("got %v, want a validation error on %s", err, field)
		}
	}

	_, err := svc.Create(ctx, anna, domain.FeedSchedule, ids.New[domain.LocationID](f.seq))
	wantField(err, "locationId")

	_, err = svc.Create(ctx, anna, domain.FeedBookings, f.elsewhere.ID)
	wantField(err, "locationId")

	_, err = svc.Create(ctx, anna, "everything", domain.LocationID{})
	wantField(err, "scope")

	// Teaching: staff with an active staff record of their own only.
	for name, p := range map[string]Principal{"member": anna, "office without a staff record": olga} {
		if _, err := svc.Create(ctx, p, domain.FeedTeaching, domain.LocationID{}); !errors.Is(err, ErrNoTeaching) {
			t.Errorf("%s: %v, want %v", name, err, ErrNoTeaching)
		}
	}

	createFeed(t, svc, ivy, domain.FeedTeaching, domain.LocationID{})

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		st, err := tx.Staff().Get(ctx, f.instructor)
		if err != nil {
			return err
		}

		st.Active = false

		return tx.Staff().Update(ctx, st)
	})

	if _, err := svc.Create(ctx, ivy, domain.FeedTeaching, domain.LocationID{}); !errors.Is(err, ErrNoTeaching) {
		t.Errorf("inactive staff record: %v", err)
	}

	// The link would outlast the impersonation.
	impersonated := anna
	impersonated.ImpersonatorID = f.account(t, f.olga).ID

	if _, err := svc.Create(ctx, impersonated, domain.FeedBookings, domain.LocationID{}); !errors.Is(err, ErrImpersonating) {
		t.Errorf("impersonating: %v", err)
	}
}

func TestCalendarFeedRevoke(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	anna, tom := f.principal(t, f.anna), f.principal(t, f.tom)

	feed := createFeed(t, svc, anna, domain.FeedBookings, domain.LocationID{})
	kept := createFeed(t, svc, anna, domain.FeedSchedule, domain.LocationID{})

	// Only the owner's.
	if err := svc.Revoke(ctx, tom, feed.Feed.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("someone else's: %v", err)
	}

	if err := svc.Revoke(ctx, anna, feed.Feed.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.Revoke(ctx, anna, feed.Feed.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("revoked twice: %v", err)
	}

	if err := svc.Revoke(ctx, anna, ids.New[domain.CalendarFeedID](f.seq)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}

	if _, err := svc.Render(ctx, feed.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("revoked feed: %v", err)
	}

	list, err := svc.List(ctx, anna)
	if err != nil || len(list) != 1 || list[0].Feed.ID != kept.Feed.ID {
		t.Errorf("list %+v, %v", list, err)
	}

	for _, token := range []string{"", "unknown", strings.Repeat("x", 200)} {
		if _, err := svc.Render(ctx, token); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("token %q: %v", token, err)
		}
	}
}

// TestCalendarFeedBookings renders a family's bookings: one event per
// lesson naming who is booked (Anna's mails are German, so is her feed),
// lessons the school canceled with their status, bookings the family
// canceled left out, from four weeks back and as far ahead as they go.
func TestCalendarFeedBookings(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	anna := f.principal(t, f.anna)
	feed := createFeed(t, svc, anna, domain.FeedBookings, domain.LocationID{})

	tooOld := f.lessonAt(t, "2026-09-22T16:00:00Z", nil) // Tuesday before the window
	past := f.lessonAt(t, "2026-09-30T16:00:00Z", nil)
	before := f.lessonAt(t, "2026-10-22T16:00:00Z", func(l *domain.Lesson) { // Thursday 18:00 summer time
		l.AccompanistID = f.pianist
	})
	after := f.lessonAt(t, "2026-10-29T17:00:00Z", func(l *domain.Lesson) { // Thursday 18:00 winter time
		l.RoomID = f.room2
		l.Kind = domain.LessonWorkshop
	})
	canceled := f.lessonAt(t, "2026-10-30T17:00:00Z", func(l *domain.Lesson) {
		l.Status, l.CancelNote = domain.LessonCanceled, "Instructor ill; sorry, no replacement"
		l.CanceledAt, l.CanceledBy = mustInstant(t, "2026-10-20T08:00:00Z"), f.account(t, f.olga).ID
	})
	hidden := f.lessonAt(t, "2026-12-10T17:00:00Z", func(l *domain.Lesson) { // beyond the horizon, hidden
		l.Visibility, l.Kind = domain.VisibilityHidden, domain.LessonPrivate
	})
	memberCanceled := f.lessonAt(t, "2026-10-31T09:00:00Z", nil)
	strangers := f.lessonAt(t, "2026-11-02T17:00:00Z", nil)

	f.put(t, tooOld, f.anna, domain.BookingAttended, false, "2026-09-01T10:00:00Z")
	f.put(t, past, f.mia, domain.BookingAttended, false, "2026-09-01T10:00:00Z")
	f.put(t, before, f.mia, domain.BookingBooked, false, "2026-10-01T10:00:00Z")
	f.put(t, before, f.anna, domain.BookingBooked, false, "2026-10-02T10:00:00Z")
	f.put(t, after, f.tom, domain.BookingBooked, false, "2026-10-01T10:00:00Z")
	f.put(t, canceled, f.anna, domain.BookingBooked, false, "2026-10-01T10:00:00Z")
	f.put(t, hidden, f.mia, domain.BookingBooked, false, "2026-10-01T10:00:00Z")
	f.put(t, memberCanceled, f.anna, domain.BookingCanceled, false, "2026-10-01T10:00:00Z")
	f.put(t, strangers, f.sam, domain.BookingBooked, false, "2026-10-01T10:00:00Z")

	doc := renderFeed(t, svc, feed.Token)
	wantFeedLessons(t, doc, past, before, after, canceled, hidden)
	goldenFeed(t, "bookings", doc)

	if doc.Filename != "studio-example-bookings.ics" {
		t.Errorf("file name %q", doc.Filename)
	}
}

// TestCalendarFeedTeaching renders the lessons an instructor teaches or
// accompanies, canceled and hidden ones included, up to the horizon.
func TestCalendarFeedTeaching(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	ivy := f.principal(t, f.ivy)
	feed := createFeed(t, svc, ivy, domain.FeedTeaching, domain.LocationID{})

	teaches := f.lessonAt(t, "2026-10-22T16:00:00Z", func(l *domain.Lesson) { l.AccompanistID = f.pianist })
	accompanies := f.lessonAt(t, "2026-10-26T17:00:00Z", func(l *domain.Lesson) {
		l.InstructorID, l.AccompanistID = f.pianist, f.instructor
	})
	hidden := f.lessonAt(t, "2026-10-27T17:00:00Z", func(l *domain.Lesson) {
		l.Visibility, l.Status = domain.VisibilityHidden, domain.LessonCanceled
	})
	lastDay := f.lessonAt(t, "2026-11-18T17:00:00Z", nil)
	f.lessonAt(t, "2026-11-19T17:00:00Z", nil)                                                   // beyond the horizon
	f.lessonAt(t, "2026-10-23T16:00:00Z", func(l *domain.Lesson) { l.InstructorID = f.pianist }) // not hers

	doc := renderFeed(t, svc, feed.Token)
	wantFeedLessons(t, doc, teaches, accompanies, hidden, lastDay)
	goldenFeed(t, "teaching", doc)

	// An account that is no staff any more teaches nothing in its feed.
	f.editAccount(t, f.ivy, func(a *domain.Account) { a.Role = domain.RoleMember })
	wantFeedLessons(t, renderFeed(t, svc, feed.Token))
}

// TestCalendarFeedSchedule renders the schedule as its owner may see it at
// the time of the fetch (ADR 0003): a member sees lessons for members and,
// with early access, regular lessons up to 2026-11-05, workshops up to the
// horizon, no hidden ones; staff see them all. A location feed has the
// location's lessons only.
func TestCalendarFeedSchedule(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	tom := f.principal(t, f.tom) // a member with English mails
	school := createFeed(t, svc, tom, domain.FeedSchedule, domain.LocationID{})
	local := createFeed(t, svc, tom, domain.FeedSchedule, f.elsewhere.ID)
	staff := createFeed(t, svc, f.principal(t, f.olga), domain.FeedSchedule, domain.LocationID{})

	released := f.lessonAt(t, "2026-10-22T16:00:00Z", nil)
	members := f.lessonAt(t, "2026-10-23T16:00:00Z", func(l *domain.Lesson) {
		l.Visibility, l.RoomID = domain.VisibilityMembers, f.room2
	})
	hidden := f.lessonAt(t, "2026-10-24T08:00:00Z", func(l *domain.Lesson) { l.Visibility = domain.VisibilityHidden })
	canceled := f.lessonAt(t, "2026-10-28T17:00:00Z", func(l *domain.Lesson) { l.Status = domain.LessonCanceled })
	early := f.lessonAt(t, "2026-11-04T17:00:00Z", func(l *domain.Lesson) { l.RoomID = f.room2 })
	notYet := f.lessonAt(t, "2026-11-05T17:00:00Z", nil)
	workshop := f.lessonAt(t, "2026-11-14T09:00:00Z", func(l *domain.Lesson) { l.Kind = domain.LessonWorkshop })
	f.lessonAt(t, "2026-11-21T09:00:00Z", func(l *domain.Lesson) { l.Kind = domain.LessonWorkshop }) // beyond the horizon

	doc := renderFeed(t, svc, school.Token)
	wantFeedLessons(t, doc, released, members, canceled, early, workshop)
	goldenFeed(t, "schedule", doc)

	localDoc := renderFeed(t, svc, local.Token)
	wantFeedLessons(t, localDoc, members, early)

	// A lesson in both feeds is two events: their UIDs differ.
	uid := "UID:" + members.ID.String() + "-schedule"
	unfold := func(d FeedDocument) string { return strings.ReplaceAll(string(d.Body), "\r\n ", "") }
	if !strings.Contains(unfold(doc), uid+"@school.example") ||
		!strings.Contains(unfold(localDoc), uid+"-"+f.elsewhere.ID.String()+"@school.example") {
		t.Errorf("UIDs of %v:\n%s\n%s", members.ID, doc.Body, localDoc.Body)
	}
	goldenFeed(t, "schedule-location", localDoc)

	if localDoc.Filename != "studio-example-schedule-studio-elsewhere.ics" {
		t.Errorf("file name %q", localDoc.Filename)
	}

	wantFeedLessons(t, renderFeed(t, svc, staff.Token), released, members, hidden, canceled, early, notYet, workshop)

	// The owner's rights at the time of the fetch: more early access shows
	// more lessons.
	f.editAccount(t, f.tom, func(a *domain.Account) { a.VisibleDays = 20 })
	wantFeedLessons(t, renderFeed(t, svc, school.Token), released, members, canceled, early, notYet, workshop)
}

// TestCalendarFeedWindow covers the bounds: from midnight 28 days before
// today, in the school's time zone.
func TestCalendarFeedWindow(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	feed := createFeed(t, svc, f.principal(t, f.olga), domain.FeedSchedule, domain.LocationID{})

	f.lessonAt(t, "2026-09-22T21:59:59Z", nil) // a second before midnight in Berlin
	first := f.lessonAt(t, "2026-09-22T22:00:00Z", nil)

	wantFeedLessons(t, renderFeed(t, svc, feed.Token), first)
}

// TestCalendarFeedOwner refuses the feeds of an account that may not sign
// in any more.
func TestCalendarFeedOwner(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	feed := createFeed(t, svc, f.principal(t, f.anna), domain.FeedBookings, domain.LocationID{})

	f.editAccount(t, f.anna, func(a *domain.Account) { a.DisabledAt = mustInstant(t, "2026-10-21T09:00:00Z") })

	if _, err := svc.Render(ctx, feed.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("disabled owner: %v", err)
	}

	f.editAccount(t, f.anna, func(a *domain.Account) { a.DisabledAt = time.Time{} })
	renderFeed(t, svc, feed.Token)
}

// TestCalendarFeedUse records the first fetch of each school day, and the
// ETag follows the content, not the generation time.
func TestCalendarFeedUse(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	feed := createFeed(t, f.feeds(t, bookingNow), f.principal(t, f.olga), domain.FeedSchedule, domain.LocationID{})
	l := f.lessonAt(t, "2026-10-22T16:00:00Z", nil)

	lastUsed := func() time.Time {
		t.Helper()

		var got domain.CalendarFeed

		err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			var err error
			got, err = tx.CalendarFeeds().Get(ctx, feed.Feed.ID)

			return err
		})
		if err != nil {
			t.Fatal(err)
		}

		return got.LastUsedAt
	}

	if !lastUsed().IsZero() {
		t.Fatal("a new feed was used")
	}

	for _, fetch := range []struct{ at, want string }{
		{"2026-10-21T10:00:00Z", "2026-10-21T10:00:00Z"}, // the first
		{"2026-10-21T21:59:59Z", "2026-10-21T10:00:00Z"}, // 23:59:59 in Berlin: the same school day
		{"2026-10-21T22:00:00Z", "2026-10-21T22:00:00Z"}, // midnight in Berlin: the next one
		{"2026-10-22T21:00:00Z", "2026-10-21T22:00:00Z"},
	} {
		renderFeed(t, f.feeds(t, fetch.at), feed.Token)

		if want := mustInstant(t, fetch.want); !lastUsed().Equal(want) {
			t.Errorf("fetched at %s: last used %v, want %v", fetch.at, lastUsed(), want)
		}
	}

	first := renderFeed(t, f.feeds(t, "2026-10-22T10:00:00Z"), feed.Token)
	again := renderFeed(t, f.feeds(t, "2026-10-22T11:00:00Z"), feed.Token)

	if again.ETag != first.ETag || string(again.Body) == string(first.Body) {
		t.Errorf("the ETag must ignore the stamp only: %q, %q", first.ETag, again.ETag)
	}

	l.Status = domain.LessonCanceled
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, l) })

	if changed := renderFeed(t, f.feeds(t, "2026-10-22T11:00:00Z"), feed.Token); changed.ETag == first.ETag {
		t.Error("the ETag did not change with the content")
	}
}

// TestCalendarFeedCreateConcurrently creates the same subject's link many
// times at once: each replaces the one before, and one stays active.
func TestCalendarFeedCreateConcurrently(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	svc := f.feeds(t, bookingNow)
	anna := f.principal(t, f.anna)

	const n = 8

	var wg sync.WaitGroup

	errs := make(chan error, n)

	for range n {
		wg.Go(func() {
			_, err := svc.Create(t.Context(), anna, domain.FeedSchedule, f.elsewhere.ID)
			errs <- err
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}

	if list, err := svc.List(t.Context(), anna); err != nil || len(list) != 1 {
		t.Errorf("list %+v, %v", list, err)
	}
}

// TestCalendarFeedSpring renders lessons across the change to summer time
// of 2027 in Berlin.
func TestCalendarFeedSpring(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)
	svc := f.feeds(t, "2027-03-20T10:00:00Z")
	feed := createFeed(t, svc, f.principal(t, f.ivy), domain.FeedTeaching, domain.LocationID{})

	winter := f.lessonAt(t, "2027-03-25T17:00:00Z", nil) // Thursday 18:00 winter time
	summer := f.lessonAt(t, "2027-04-01T16:00:00Z", nil) // Thursday 18:00 summer time

	doc := renderFeed(t, svc, feed.Token)
	wantFeedLessons(t, doc, winter, summer)
	goldenFeed(t, "teaching-spring", doc)
}

// TestCalendarFeedZone renders a school in New York across its change to
// winter time on 2026-11-01.
func TestCalendarFeedZone(t *testing.T) {
	t.Parallel()

	settings := bookingSettings()
	settings.Timezone = "America/New_York"

	f := &feedFixture{bookingFixture: &bookingFixture{genFixture: newGenFixture(t, settings)}}
	f.olga = f.person(t, "Olga", "", "olga@example.net", "")
	f.editAccount(t, f.olga, func(a *domain.Account) { a.Role = domain.RoleOffice })

	svc := f.feeds(t, bookingNow)
	feed := createFeed(t, svc, f.principal(t, f.olga), domain.FeedSchedule, domain.LocationID{})

	summer := f.lessonAt(t, "2026-10-29T22:00:00Z", nil) // Thursday 18:00 EDT
	winter := f.lessonAt(t, "2026-11-05T23:00:00Z", nil) // Thursday 18:00 EST

	doc := renderFeed(t, svc, feed.Token)
	wantFeedLessons(t, doc, summer, winter)
	goldenFeed(t, "schedule-new-york", doc)
}

// TestTeaches decides who may have a teaching feed (and getMe's teaches):
// staff with an active staff record linked to their person.
func TestTeaches(t *testing.T) {
	t.Parallel()

	f := newFeedFixture(t)

	check := func(name string, p domain.Person, role domain.Role, want bool) {
		t.Helper()

		var got bool

		err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			var err error
			got, err = teaches(ctx, tx, role, p.ID)

			return err
		})
		if err != nil || got != want {
			t.Errorf("%s: %v, %v; want %v", name, got, err, want)
		}
	}

	check("instructor with a staff record", f.ivy, domain.RoleInstructor, true)
	check("member with a staff record", f.ivy, domain.RoleMember, false)
	check("office without a staff record", f.olga, domain.RoleOffice, false)
	check("member", f.anna, domain.RoleMember, false)

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		st, err := tx.Staff().Get(ctx, f.instructor)
		if err != nil {
			return err
		}

		st.Active = false

		return tx.Staff().Update(ctx, st)
	})
	check("instructor with an inactive staff record", f.ivy, domain.RoleInstructor, false)
}
