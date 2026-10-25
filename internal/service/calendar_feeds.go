package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ical"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Calendar feeds (ADR 0011): an account's iCalendar subscription URLs, one
// per subject (its family's bookings, its teaching, the schedule or one
// location of it), each secured by a random token that is shown once.

const (
	// FeedDaysBack is how many days before today a feed reaches back.
	FeedDaysBack = 28
	// FeedPathPrefix starts a feed's path, followed by its token.
	FeedPathPrefix = "/ical/"
	// FeedPathSuffix ends a feed's path, after its token.
	FeedPathSuffix = ".ics"
	// maxFeedToken is longer than any token newToken makes; a longer one is
	// unknown without a lookup.
	maxFeedToken = 128
)

// ErrNoTeaching refuses a teaching feed to an account that is no staff or
// has no active staff record linked to its person.
var ErrNoTeaching = fmt.Errorf("%w: the account teaches no lessons", domain.ErrForbidden)

// FeedPath returns the path of the feed with the given token.
func FeedPath(token string) string { return FeedPathPrefix + token + FeedPathSuffix }

// CalendarFeedInfo is a feed with what its owner sees of it.
type CalendarFeedInfo struct {
	Feed domain.CalendarFeed
	// LocationName is the name of the feed's location; empty without one.
	LocationName string
}

// NewCalendarFeed is a feed just created, with its secret, which is shown
// only now.
type NewCalendarFeed struct {
	CalendarFeedInfo

	Token string
	// Path is the feed's path below the app's origin (FeedPath).
	Path string
	// URL is the feed's absolute URL; empty without the app's URL or a
	// server that calendars can reach (the web demo).
	URL string
}

// FeedDocument is the iCalendar document of a feed.
type FeedDocument struct {
	Body []byte
	// ETag changes exactly when the content does (not with the generation
	// time alone).
	ETag string
	// Filename names the document when it is downloaded, e.g.
	// "studio-example-schedule.ics".
	Filename string
}

// CalendarFeedService manages the calendar feeds of accounts and renders
// them (ADR 0011). A feed shows what its owner may see at the time it is
// fetched: the owner's current role, family and staff records count.
type CalendarFeedService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
	// appURL is the app's URL for links (Deps.AppURL); may be empty.
	appURL string
	// reachable tells that calendars can reach the app's server at appURL,
	// so new feeds come with their URL.
	reachable bool
}

// NewCalendarFeedService returns a CalendarFeedService whose feeds link to
// the app at appURL (ending in "/" or "#/"; empty for no links). reachable
// tells that calendars can reach the app's server there: the web demo has
// none, so its feeds have no URL, only their path.
func NewCalendarFeedService(
	st store.Store, clk clock.Clock, src ids.Source, appURL string, reachable bool,
) *CalendarFeedService {
	return &CalendarFeedService{store: st, clock: clk, ids: src, appURL: appURL, reachable: reachable}
}

// List returns the active feeds of p's account, oldest first. Their tokens
// are not stored, so they cannot be shown again.
func (s *CalendarFeedService) List(ctx context.Context, p Principal) ([]CalendarFeedInfo, error) {
	var out []CalendarFeedInfo

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		feeds, err := tx.CalendarFeeds().ListByAccount(ctx, p.AccountID)
		if err != nil {
			return err
		}

		out, err = feedInfos(ctx, tx, feeds)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list calendar feeds: %w", err)
	}

	return out, nil
}

// feedInfos adds the location names to the feeds.
func feedInfos(ctx context.Context, tx store.Tx, feeds []domain.CalendarFeed) ([]CalendarFeedInfo, error) {
	out := make([]CalendarFeedInfo, 0, len(feeds))

	var locations map[domain.LocationID]string

	for _, f := range feeds {
		info := CalendarFeedInfo{Feed: f}

		if !f.LocationID.IsZero() {
			if locations == nil {
				list, err := tx.Locations().List(ctx)
				if err != nil {
					return nil, fmt.Errorf("locations: %w", err)
				}

				locations = map[domain.LocationID]string{}
				for _, l := range list {
					locations[l.ID] = l.Name
				}
			}

			info.LocationName = locations[f.LocationID]
		}

		out = append(out, info)
	}

	return out, nil
}

// Create makes a new feed of p's account for the scope and, for a schedule
// feed, optionally a location, and revokes the account's feed of that
// subject: there is one link per subject, and a new one replaces the old.
// The token is returned only here. A teaching feed needs an account that is
// staff with an active staff record linked to its person (ErrNoTeaching); a
// location must exist (errUnknownLocation). An admin signed in as the
// account may not create one (ErrImpersonating): the link would outlast the
// impersonation.
func (s *CalendarFeedService) Create(
	ctx context.Context, p Principal, scope domain.FeedScope, location domain.LocationID,
) (NewCalendarFeed, error) {
	if !p.ImpersonatorID.IsZero() {
		return NewCalendarFeed{}, ErrImpersonating
	}

	token, err := newToken()
	if err != nil {
		return NewCalendarFeed{}, err
	}

	f := domain.CalendarFeed{
		ID: ids.New[domain.CalendarFeedID](s.ids), AccountID: p.AccountID, Scope: scope, LocationID: location,
		TokenHash: domain.HashToken(token), CreatedAt: s.clock.Now(),
	}
	if err := f.Validate(); err != nil {
		return NewCalendarFeed{}, err
	}

	out := NewCalendarFeed{Token: token, Path: FeedPath(token), URL: s.feedURL(token)}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		out.LocationName = ""

		// One create of the account at a time, so that the new feed always
		// replaces the old one instead of colliding with a concurrent new one.
		if err := tx.Persons().Lock(ctx, p.PersonID); err != nil {
			return err
		}

		switch {
		case scope == domain.FeedTeaching:
			if err := mayTeach(ctx, tx, p); err != nil {
				return err
			}
		case !location.IsZero():
			l, err := tx.Locations().Get(ctx, location)
			if errors.Is(err, domain.ErrNotFound) {
				return errUnknownLocation
			} else if err != nil {
				return err
			}

			out.LocationName = l.Name
		}

		if _, err := tx.CalendarFeeds().RevokeActive(ctx, p.AccountID, scope, location, f.CreatedAt); err != nil {
			return err
		}

		if err := tx.CalendarFeeds().Create(ctx, f); err != nil {
			return err
		}

		out.Feed, err = tx.CalendarFeeds().Get(ctx, f.ID) // as stored

		return err
	})
	if err != nil {
		return NewCalendarFeed{}, fmt.Errorf("create calendar feed: %w", err)
	}

	return out, nil
}

// mayTeach checks that p teaches (ErrNoTeaching otherwise).
func mayTeach(ctx context.Context, tx store.Tx, p Principal) error {
	ok, err := teaches(ctx, tx, p.Role, p.PersonID)
	if err != nil {
		return err
	}

	if !ok {
		return ErrNoTeaching
	}

	return nil
}

// teaches reports whether an account of role whose person is person
// teaches lessons: it is staff with an active staff record linked to the
// person. Only such an account has a teaching calendar feed.
func teaches(ctx context.Context, tx store.Tx, role domain.Role, person domain.PersonID) (bool, error) {
	if !role.Staff() {
		return false, nil
	}

	own, err := ownStaff(ctx, tx, person)
	if err != nil {
		return false, fmt.Errorf("staff records: %w", err)
	}

	return len(own) > 0, nil
}

// feedURL returns the absolute URL of the feed with the token: the path
// below the origin (and path) of the app's URL, without its hash route.
func (s *CalendarFeedService) feedURL(token string) string {
	if s.appURL == "" || !s.reachable {
		return ""
	}

	base, _, _ := strings.Cut(s.appURL, "#")

	return strings.TrimSuffix(base, "/") + FeedPath(token)
}

// Revoke revokes the active feed id of p's account; any other feed, of
// another account or revoked already, is domain.ErrNotFound.
func (s *CalendarFeedService) Revoke(ctx context.Context, p Principal, id domain.CalendarFeedID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		f, err := tx.CalendarFeeds().Get(ctx, id)
		if err != nil {
			return err
		}

		if f.AccountID != p.AccountID || !f.Active() {
			return domain.ErrNotFound
		}

		return tx.CalendarFeeds().Revoke(ctx, id, s.clock.Now())
	})
	if err != nil {
		return fmt.Errorf("revoke calendar feed %v: %w", id, err)
	}

	return nil
}

// Render returns the document of the feed with the token, as its owner may
// see it now (ADR 0011). An unknown or revoked token, and the feed of an
// account that may not sign in (disabled), is domain.ErrNotFound. The
// first fetch of a school day records when the feed was last used.
func (s *CalendarFeedService) Render(ctx context.Context, token string) (FeedDocument, error) {
	if token == "" || len(token) > maxFeedToken {
		return FeedDocument{}, domain.ErrNotFound
	}

	now := s.clock.Now()

	var (
		feed  domain.CalendarFeed
		doc   FeedDocument
		touch bool
	)

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		if feed, err = tx.CalendarFeeds().GetByTokenHash(ctx, domain.HashToken(token)); err != nil {
			return err
		}

		if !feed.Active() {
			return domain.ErrNotFound
		}

		owner, err := tx.Accounts().Get(ctx, feed.AccountID)
		if err != nil {
			return err
		}

		if !mayActAs(owner) {
			return domain.ErrNotFound
		}

		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		today := domain.DateOf(now, loc)
		touch = feed.LastUsedAt.IsZero() || domain.DateOf(feed.LastUsedAt, loc) != today

		doc, err = s.render(ctx, tx, feed, owner, settings, loc, now)

		return err
	})
	if err != nil {
		return FeedDocument{}, fmt.Errorf("calendar feed: %w", err)
	}

	if !touch {
		return doc, nil
	}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.CalendarFeeds().TouchLastUsed(ctx, feed.ID, now)
	})
	if err != nil && !errors.Is(err, domain.ErrNotFound) { // deleted with its location meanwhile
		return FeedDocument{}, fmt.Errorf("calendar feed used: %w", err)
	}

	return doc, nil
}

// feedEvent is a lesson of a feed with what its event says beyond the
// lesson: the persons booked (bookings) and the owner's part (teaching).
type feedEvent struct {
	ScheduledLesson

	names []string
	role  TeachRole
}

// render builds the document of feed for owner at now, in the school of
// settings and its zone loc.
func (s *CalendarFeedService) render(
	ctx context.Context, tx store.Tx, feed domain.CalendarFeed, owner domain.Account, settings domain.Settings,
	loc *time.Location, now time.Time,
) (FeedDocument, error) {
	p := Principal{
		AccountID: owner.ID, PersonID: owner.PersonID, Email: owner.Email, Role: owner.Role,
		VisibleDays: owner.VisibleDays,
	}

	today := domain.DateOf(now, loc)
	from := today.AddDays(-FeedDaysBack)

	_, until, err := horizon(ctx, tx, settings, today)
	if err != nil {
		return FeedDocument{}, err
	}

	var (
		events   []feedEvent
		location domain.Location
	)

	switch feed.Scope {
	case domain.FeedBookings:
		events, err = bookingEvents(ctx, tx, settings, p, from.At(domain.TimeOfDay{}, loc))
	case domain.FeedTeaching:
		events, err = teachingEvents(ctx, tx, p, from, until, loc)
	case domain.FeedSchedule:
		if !feed.LocationID.IsZero() {
			if location, err = tx.Locations().Get(ctx, feed.LocationID); err != nil {
				return FeedDocument{}, err
			}
		}

		events, err = scheduleEvents(ctx, tx, settings, p, feed.LocationID, from, until, loc, now)
	default:
		err = fmt.Errorf("%w: calendar feed scope %q", domain.ErrValidation, feed.Scope)
	}

	if err != nil {
		return FeedDocument{}, err
	}

	labels := feedLabelsFor(cmp.Or(owner.Locale, settings.DefaultLocale))
	cal := ical.Calendar{
		Name: settings.SchoolName + " – " + labels.subject(feed.Scope, location.Name), Zone: loc, Stamp: now,
		Events: make([]ical.Event, 0, len(events)),
	}

	uid := s.uidSuffix(feed)
	for _, e := range events {
		cal.Events = append(cal.Events, s.event(e, feed.Scope, uid, labels, loc))
	}

	subject := string(feed.Scope)
	if location.Name != "" {
		subject += "-" + location.Name
	}

	encoded := ical.Encode(cal)

	return FeedDocument{
		Body: encoded.Body, ETag: encoded.Fingerprint, Filename: slug(settings.SchoolName+"-"+subject) + ".ics",
	}, nil
}

// bookingEvents returns the lessons from from on that p's family has active
// bookings for, whatever their visibility (as in My lessons), each once with
// the names of the family members booked, p first.
func bookingEvents(
	ctx context.Context, tx store.Tx, settings domain.Settings, p Principal, from time.Time,
) ([]feedEvent, error) {
	fam, err := familyOf(ctx, tx, p)
	if err != nil || len(fam.persons) == 0 {
		return nil, err
	}

	bookings, err := tx.Bookings().ListByPersons(ctx, store.BookingQuery{
		Persons: fam.ids(), Active: true, StartsFrom: from,
	})
	if err != nil {
		return nil, fmt.Errorf("bookings: %w", err)
	}

	lessons, err := lessonsOf(ctx, tx, settings, bookings, nil)
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(bookings, func(a, b domain.Booking) int {
		return cmp.Compare(fam.rank[a.PersonID], fam.rank[b.PersonID])
	})

	byLesson := map[domain.LessonID]*feedEvent{}

	var events []*feedEvent

	for _, b := range bookings {
		e, ok := byLesson[b.LessonID]
		if !ok {
			sl, found := lessons[b.LessonID]
			if !found {
				continue // the store lists only bookings with their lesson
			}

			e = &feedEvent{ScheduledLesson: sl}
			byLesson[b.LessonID] = e
			events = append(events, e)
		}

		e.names = append(e.names, fullName(fam.person(b.PersonID)))
	}

	out := make([]feedEvent, len(events))
	for i, e := range events {
		out[i] = *e
	}

	slices.SortFunc(out, func(a, b feedEvent) int { return byStart(a.Lesson, b.Lesson) })

	return out, nil
}

// teachingEvents returns the lessons in the school dates [from, until) that
// p teaches or accompanies through their active staff records, canceled and
// hidden ones included; none unless p is staff.
func teachingEvents(
	ctx context.Context, tx store.Tx, p Principal, from, until domain.Date, loc *time.Location,
) ([]feedEvent, error) {
	if !p.Role.Staff() {
		return nil, nil
	}

	own, err := ownStaff(ctx, tx, p.PersonID)
	if err != nil || len(own) == 0 {
		return nil, err
	}

	staffIDs := make([]domain.StaffID, len(own))
	for i, st := range own {
		staffIDs[i] = st.ID
	}

	lessons, err := tx.Lessons().ListByStaff(ctx, staffIDs, from.At(domain.TimeOfDay{}, loc), until.At(domain.TimeOfDay{}, loc))
	if err != nil {
		return nil, fmt.Errorf("lessons: %w", err)
	}

	return resolveEvents(ctx, tx, from, until, lessons, nil, func(sl ScheduledLesson) feedEvent {
		_, role := roleIn(sl.Lesson, own)

		return feedEvent{ScheduledLesson: sl, role: role}
	})
}

// scheduleEvents returns the lessons in the school dates [from, until) that
// p may see in the schedule at now (ADR 0003), of the location unless it is
// zero.
func scheduleEvents(
	ctx context.Context, tx store.Tx, settings domain.Settings, p Principal, location domain.LocationID,
	from, until domain.Date, loc *time.Location, now time.Time,
) ([]feedEvent, error) {
	v := viewerOf(p, settings, loc, now)
	w := ComputeReleaseWindow(settings, loc, now)

	lessons, err := tx.Lessons().ListRange(ctx, from.At(domain.TimeOfDay{}, loc), until.At(domain.TimeOfDay{}, loc))
	if err != nil {
		return nil, fmt.Errorf("lessons: %w", err)
	}

	lessons = slices.DeleteFunc(lessons, func(l domain.Lesson) bool { return !v.sees(l, w) })

	return resolveEvents(ctx, tx, from, until, lessons, func(_ domain.Lesson, room domain.Room) bool {
		return location.IsZero() || room.LocationID == location
	}, func(sl ScheduledLesson) feedEvent { return feedEvent{ScheduledLesson: sl} })
}

// resolveEvents resolves the lessons, which start in the school dates
// [from, until), that want keeps (all for a nil want), by the lesson and its
// room, before anything else is loaded for them, and makes them events.
func resolveEvents(
	ctx context.Context, tx store.Tx, from, until domain.Date, lessons []domain.Lesson,
	want func(domain.Lesson, domain.Room) bool, event func(ScheduledLesson) feedEvent,
) ([]feedEvent, error) {
	if len(lessons) == 0 {
		return nil, nil
	}

	refs, err := loadRangeRefs(ctx, tx, from, until)
	if err != nil {
		return nil, err
	}

	out := make([]feedEvent, 0, len(lessons))

	for _, l := range lessons {
		if want != nil && !want(l, refs.rooms[l.RoomID]) {
			continue
		}

		if err := refs.loadClass(ctx, tx, l.ClassID); err != nil {
			return nil, err
		}

		sl, err := refs.resolve(l)
		if err != nil {
			return nil, err
		}

		out = append(out, event(sl))
	}

	return out, nil
}

// event is the iCalendar event of e in a feed of scope, whose UIDs end in
// uidSuffix, worded with labels.
func (s *CalendarFeedService) event(
	e feedEvent, scope domain.FeedScope, uidSuffix string, labels feedLabels, loc *time.Location,
) ical.Event {
	l := e.Lesson

	who := e.Instructor.DisplayName
	if scope == domain.FeedBookings {
		who = strings.Join(e.names, ", ")
	}

	summary := e.CourseType.Name + " – " + who
	if e.role == TeachInstructor || e.role == TeachAccompanist {
		summary += " (" + labels.role(e.role) + ")"
	}

	status := ical.StatusConfirmed
	if l.Status == domain.LessonCanceled {
		status = ical.StatusCancelled
		summary = labels.canceled + ": " + summary
	}

	place := e.Location.Name + ", " + e.Room.Name

	where := place
	if addr := oneLine(e.Location.Address); addr != "" {
		where += ", " + addr
	}

	var desc []string

	if l.Status == domain.LessonCanceled {
		desc = append(desc, cmp.Or(l.CancelNote, labels.canceledNote))
	}

	if l.Kind != domain.LessonRegular {
		desc = append(desc, labels.kind(l.Kind))
	}

	if scope == domain.FeedBookings {
		desc = append(desc, labels.booked+": "+who)
	}

	desc = append(desc, labels.instructor+": "+e.Instructor.DisplayName)
	if !l.AccompanistID.IsZero() {
		desc = append(desc, labels.accompanist+": "+e.Accompanist.DisplayName)
	}

	desc = append(desc, labels.room+": "+place)

	return ical.Event{
		UID:   l.ID.String() + uidSuffix,
		Start: l.StartsAt, End: l.EndsAt, Summary: summary, Location: where,
		Description: strings.Join(desc, "\n"), URL: s.lessonURL(l, loc), Status: status,
	}
}

// uidSuffix is what follows the lesson ID in the UIDs of feed's events: its
// subject, so a lesson in two feeds (the school's and a location's
// schedule, say) is two events, and the host of the app's URL.
func (s *CalendarFeedService) uidSuffix(feed domain.CalendarFeed) string {
	host := "studio-schedule"
	if u, err := url.Parse(s.appURL); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}

	subject := "-" + string(feed.Scope)
	if !feed.LocationID.IsZero() {
		subject += "-" + feed.LocationID.String()
	}

	return subject + "@" + host
}

// lessonURL links to l in the app's schedule; empty without the app's URL.
func (s *CalendarFeedService) lessonURL(l domain.Lesson, loc *time.Location) string {
	if s.appURL == "" {
		return ""
	}

	q := url.Values{"from": {domain.DateOf(l.StartsAt, loc).String()}, "lesson": {l.ID.String()}}

	return s.appURL + "schedule?" + q.Encode()
}

// oneLine joins the lines of s with commas.
func oneLine(s string) string {
	var parts []string

	for line := range strings.Lines(s) {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}

	return strings.Join(parts, ", ")
}

// slug returns s in lower case with every run of characters other than
// ASCII letters and digits replaced by one hyphen, for a file name.
func slug(s string) string {
	var b strings.Builder

	hyphen := false

	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}

			b.WriteRune(r)

			hyphen = false
		} else {
			hyphen = true
		}
	}

	return cmp.Or(b.String(), "calendar")
}
