package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Announcements (ADR 0014): the office writes them for an audience; the
// audience resolves when an account reads them and when they are emailed,
// so they reach whoever belongs to it then. Writing one only saves it (and
// takes its place in the daily limit); the announcements job emails it,
// once, through the outbox, from its PublishAt on, in batches (SendDue).

const (
	// lessonSpan bounds how long a lesson lasts: a lesson ends on the
	// school day it starts (class durations, lesson_admin.go), and a school
	// day lasts at most 25 hours.
	lessonSpan = 25 * time.Hour
	// announcementMailBatch is how many due announcements one job run
	// looks at.
	announcementMailBatch = 10
	// announcementRecipientBatch is how many mails one job run queues for
	// announcements at most; a larger audience is mailed over several runs
	// (a minute apart), so a run stays short, also on the demo's single
	// thread. The outbox delivers about 20 a minute anyway.
	announcementRecipientBatch = 100
)

var (
	// ErrAnnouncementExpired refuses to edit an expired announcement.
	ErrAnnouncementExpired = fmt.Errorf("%w: the announcement has expired", domain.ErrConflict)

	// errUnknownCourseType is the class's (class_admin.go).
	errUnknownClass  error = &domain.FieldError{Field: "classId", Msg: "class does not exist"}
	errUnknownLesson error = &domain.FieldError{Field: "lessonId", Msg: "lesson does not exist"}
)

// TooManyAnnouncementMailsError refuses to email an announcement on a
// school day that has domain.AnnouncementMailsPerDay emailed announcements,
// sent and scheduled, already.
type TooManyAnnouncementMailsError struct {
	// Day is the school day the announcement would be emailed on.
	Day domain.Date
}

func (e *TooManyAnnouncementMailsError) Error() string {
	return fmt.Sprintf("%v: at most %d emailed announcements per day", e.Day, domain.AnnouncementMailsPerDay)
}

// AnnouncementAudience names whom an announcement is for: the kind, and the
// one reference the kind needs.
type AnnouncementAudience struct {
	Audience     domain.Audience
	Role         domain.Role
	ClassID      domain.ClassID
	CourseTypeID domain.CourseTypeID
	LessonID     domain.LessonID
}

// AnnouncementInput is an announcement as the office writes it.
type AnnouncementInput struct {
	AnnouncementAudience

	Title string
	// Body is plain text (plainText rules, up to
	// domain.MaxAnnouncementBodyLen characters).
	Body string
	// PublishAt is when it shows; zero means now.
	PublishAt time.Time
	// ExpiresAt is when it stops showing (optional).
	ExpiresAt time.Time
	// Email mails it to its audience once, at PublishAt.
	Email bool
}

// AnnouncementEntry is an announcement as the office lists it, with what
// its author and audience refer to.
type AnnouncementEntry struct {
	Announcement domain.Announcement
	// Author is the person of the author's account, if it can be found.
	Author domain.Person
	// Class and its Term, for the class audience.
	Class domain.Class
	Term  domain.Term
	// CourseType is the course type of the audience, or of its class or
	// lesson.
	CourseType domain.CourseType
	// Lesson is the lesson of the lesson audience.
	Lesson domain.Lesson
}

// AnnouncementView is an announcement shown to an account.
type AnnouncementView struct {
	Announcement domain.Announcement
	// Dismissed tells that the account dismissed it.
	Dismissed bool
}

// AnnouncementService writes, lists and emails announcements.
type AnnouncementService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
	mails MailRenderer
	// wake asks for a run of the announcements job (SendDue) soon.
	wake func()
	// recipientBatch is announcementRecipientBatch; tests make it smaller.
	recipientBatch int
}

// NewAnnouncementService returns an AnnouncementService that renders its
// mails with mails. It calls wake (if not nil) after it saved an
// announcement that is to be emailed now, so that the announcements job
// sends it right away rather than at its next tick.
func NewAnnouncementService(
	st store.Store, clk clock.Clock, src ids.Source, mails MailRenderer, wake func(),
) *AnnouncementService {
	return &AnnouncementService{
		store: st, clock: clk, ids: src, mails: mails, wake: wake, recipientBatch: announcementRecipientBatch,
	}
}

// wakeIfDue wakes the announcements job if a is to be emailed at now.
func (s *AnnouncementService) wakeIfDue(a domain.Announcement, now time.Time) {
	if s.wake != nil && a.MailPending() && !a.PublishAt.After(now) {
		s.wake()
	}
}

// List returns every announcement, newest first, for the office.
func (s *AnnouncementService) List(ctx context.Context) ([]AnnouncementEntry, error) {
	var out []AnnouncementEntry

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		as, err := tx.Announcements().List(ctx)
		if err != nil {
			return err
		}

		out, err = announcementEntries(ctx, tx, as)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list announcements: %w", err)
	}

	return out, nil
}

// Create publishes an announcement by p (the admin while impersonating). If
// it asks for email, it takes its place in the daily limit at once
// (*TooManyAnnouncementMailsError beyond it), and the announcements job
// emails it from PublishAt on: right after this returns if it publishes
// now.
func (s *AnnouncementService) Create(ctx context.Context, p Principal, in AnnouncementInput) (AnnouncementEntry, error) {
	var (
		out AnnouncementEntry
		now time.Time
	)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		now = s.clock.Now()
		a := domain.Announcement{
			ID: ids.New[domain.AnnouncementID](s.ids), AuthorID: actorOf(p), CreatedAt: now, UpdatedAt: now,
		}

		var err error

		out, err = s.write(ctx, tx, now, a, in, tx.Announcements().Create)

		return err
	})
	if err != nil {
		return AnnouncementEntry{}, fmt.Errorf("create announcement: %w", err)
	}

	s.wakeIfDue(out.Announcement, now)

	return out, nil
}

// Update changes an announcement that has not expired
// (ErrAnnouncementExpired otherwise). One whose mailing has begun (emailed,
// or being sent) stays emailed and is not emailed again, whatever in.Email
// says; one that is not, is emailed like a new one. It locks the
// announcement against a concurrent mailing, and an edit never changes
// its mail state.
func (s *AnnouncementService) Update(
	ctx context.Context, id domain.AnnouncementID, in AnnouncementInput,
) (AnnouncementEntry, error) {
	var (
		out AnnouncementEntry
		now time.Time
	)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// The order of SendDue: the daily limit, then the announcement.
		if err := tx.Announcements().LockMail(ctx); err != nil {
			return err
		}

		a, err := tx.Announcements().Lock(ctx, id)
		if err != nil {
			return err
		}

		now = s.clock.Now()
		if a.ExpiredAt(now) {
			return ErrAnnouncementExpired
		}

		if a.MailStarted() {
			in.Email = true
		}

		a.UpdatedAt = now
		out, err = s.write(ctx, tx, now, a, in, tx.Announcements().Update)

		return err
	})
	if err != nil {
		return AnnouncementEntry{}, fmt.Errorf("update announcement %v: %w", id, err)
	}

	s.wakeIfDue(out.Announcement, now)

	return out, nil
}

// errExpiresPast refuses an announcement that would have expired already.
var errExpiresPast error = &domain.FieldError{Field: "expiresAt", Msg: "the expiry must lie in the future"}

// write sets a's fields from in, checks them and, if a is to be emailed and
// its mailing has not begun, the daily limit, and saves a with save. It
// sends nothing: SendDue does.
func (s *AnnouncementService) write(
	ctx context.Context, tx store.Tx, now time.Time, a domain.Announcement, in AnnouncementInput,
	save func(context.Context, domain.Announcement) error,
) (AnnouncementEntry, error) {
	title, err := plainText(in.Title, "title", domain.MaxAnnouncementTitleLen)
	if err != nil {
		return AnnouncementEntry{}, err
	}

	if strings.Contains(title, "\n") {
		return AnnouncementEntry{}, &domain.FieldError{Field: "title", Msg: "title must be one line"}
	}

	body, err := plainText(in.Body, "body", domain.MaxAnnouncementBodyLen)
	if err != nil {
		return AnnouncementEntry{}, err
	}

	a.Title, a.Body, a.Email = title, body, in.Email
	a.Audience, a.Role, a.ClassID, a.CourseTypeID, a.LessonID = in.Audience, in.Role, in.ClassID, in.CourseTypeID,
		in.LessonID
	a.PublishAt, a.ExpiresAt = cmp.Or(in.PublishAt, now), in.ExpiresAt

	if err := a.Validate(); err != nil {
		return AnnouncementEntry{}, err
	}

	if a.ExpiredAt(now) {
		return AnnouncementEntry{}, errExpiresPast
	}

	if err := checkAudienceRefs(ctx, tx, in.AnnouncementAudience); err != nil {
		return AnnouncementEntry{}, err
	}

	_, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return AnnouncementEntry{}, err
	}

	if a.Email && !a.MailStarted() {
		if err := announcementMailLimit(ctx, tx, loc, now, a); err != nil {
			return AnnouncementEntry{}, err
		}
	}

	if err := save(ctx, a); err != nil {
		return AnnouncementEntry{}, err
	}

	entries, err := announcementEntries(ctx, tx, []domain.Announcement{a})
	if err != nil {
		return AnnouncementEntry{}, err
	}

	return entries[0], nil
}

// checkAudienceRefs reports an unknown class, course type or lesson of the
// audience as a *domain.FieldError on its field.
func checkAudienceRefs(ctx context.Context, tx store.Tx, aud AnnouncementAudience) error {
	check := func(err, unknown error) error {
		if errors.Is(err, domain.ErrNotFound) {
			return unknown
		}

		return err
	}

	switch aud.Audience {
	case domain.AudienceClass:
		_, err := tx.Classes().Get(ctx, aud.ClassID)

		return check(err, errUnknownClass)
	case domain.AudienceCourseType:
		_, err := tx.CourseTypes().Get(ctx, aud.CourseTypeID)

		return check(err, errUnknownCourseType)
	case domain.AudienceLesson:
		_, err := tx.Lessons().Get(ctx, aud.LessonID)

		return check(err, errUnknownLesson)
	default:
		return nil
	}
}

// announcementMailLimit checks that a may be emailed on its school day: the
// day of its PublishAt, or today if that has passed. It locks the limit
// until the transaction ends.
func announcementMailLimit(
	ctx context.Context, tx store.Tx, loc *time.Location, now time.Time, a domain.Announcement,
) error {
	if err := tx.Announcements().LockMail(ctx); err != nil {
		return err
	}

	day := domain.DateOf(maxTime(a.PublishAt, now), loc)
	from, to := day.At(domain.TimeOfDay{}, loc), day.AddDays(1).At(domain.TimeOfDay{}, loc)

	sent, err := tx.Announcements().ListMailings(ctx, from, to)
	if err != nil {
		return err
	}

	scheduled, err := tx.Announcements().CountScheduledForMail(ctx, from, to, a.ID)
	if err != nil {
		return err
	}

	if len(sent)+scheduled >= domain.AnnouncementMailsPerDay {
		return &TooManyAnnouncementMailsError{Day: day}
	}

	return nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

// Delete deletes an announcement with its dismissals. Its mailings stay
// and keep counting for the daily limit; one being sent stops, and what it
// sent so far is recorded as its mailing.
func (s *AnnouncementService) Delete(ctx context.Context, id domain.AnnouncementID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := tx.Announcements().LockMail(ctx); err != nil {
			return err
		}

		a, err := tx.Announcements().Lock(ctx, id)
		if err != nil {
			return err
		}

		if a.EmailedAt.IsZero() && a.EmailRecipients > 0 {
			if err := s.recordMailing(ctx, tx, s.clock.Now(), a); err != nil {
				return err
			}
		}

		return tx.Announcements().Delete(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("delete announcement %v: %w", id, err)
	}

	return nil
}

// CountRecipients returns how many addresses an announcement for the
// audience would be emailed to now, for the confirmation before emailing.
func (s *AnnouncementService) CountRecipients(ctx context.Context, aud AnnouncementAudience) (int, error) {
	a := domain.Announcement{
		Audience: aud.Audience, Role: aud.Role, ClassID: aud.ClassID, CourseTypeID: aud.CourseTypeID,
		LessonID: aud.LessonID,
	}

	var n int

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		if err := checkAudience(a); err != nil {
			return err
		}

		if err := checkAudienceRefs(ctx, tx, aud); err != nil {
			return err
		}

		_, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		rs, err := announcementRecipients(ctx, tx, loc, s.clock.Now(), a)
		n = len(rs)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("count announcement recipients: %w", err)
	}

	return n, nil
}

// checkAudience validates the audience of a alone, with the field errors
// of domain.Announcement.Validate.
func checkAudience(a domain.Announcement) error {
	a.ID, a.AuthorID, a.Title, a.Body = domain.AnnouncementID{1}, domain.AccountID{1}, "x", "x"
	a.PublishAt, a.CreatedAt, a.UpdatedAt = time.Unix(1, 0), time.Unix(1, 0), time.Unix(1, 0)

	return a.Validate()
}

// ForAccount returns the announcements shown now that reach p's account,
// newest first, each with whether the account dismissed it.
//
// An announcement reaches an account if it is for everyone, for its role,
// or for persons among which are the account's person or one of its
// dependents: the members of a class (or of the classes of a course type
// whose term has not ended) and the persons with an active booking of one
// of its lessons that is not over, or the persons with an active booking of
// the lesson.
func (s *AnnouncementService) ForAccount(ctx context.Context, p Principal) ([]AnnouncementView, error) {
	var out []AnnouncementView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		now := s.clock.Now()

		shown, err := tx.Announcements().ListShown(ctx, now)
		if err != nil || len(shown) == 0 {
			return err
		}

		reach, err := s.reachOf(ctx, tx, p, now, shown)
		if err != nil {
			return err
		}

		var mine []domain.Announcement

		for _, a := range shown {
			if reach.reaches(p, a) {
				mine = append(mine, a)
			}
		}

		dismissed, err := tx.Announcements().ListDismissed(ctx, p.AccountID, announcementIDs(mine))
		if err != nil {
			return err
		}

		out = make([]AnnouncementView, len(mine))
		for i, a := range mine {
			out[i] = AnnouncementView{Announcement: a, Dismissed: slices.Contains(dismissed, a.ID)}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("announcements of %v: %w", p.AccountID, err)
	}

	return out, nil
}

// Dismiss records that p's account dismissed an announcement that is shown
// and reaches it (domain.ErrNotFound otherwise). While an admin
// impersonates the account, nothing is recorded.
func (s *AnnouncementService) Dismiss(ctx context.Context, p Principal, id domain.AnnouncementID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		a, err := tx.Announcements().Get(ctx, id)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if !a.ShownAt(now) {
			return domain.ErrNotFound
		}

		reach, err := s.reachOf(ctx, tx, p, now, []domain.Announcement{a})
		if err != nil {
			return err
		}

		switch {
		case !reach.reaches(p, a):
			return domain.ErrNotFound
		case !p.ImpersonatorID.IsZero():
			return nil
		}

		d := domain.AnnouncementDismissal{AnnouncementID: a.ID, AccountID: p.AccountID, DismissedAt: now}
		if err := d.Validate(); err != nil {
			return err
		}

		return tx.Announcements().Dismiss(ctx, d)
	})
	if err != nil {
		return fmt.Errorf("dismiss announcement %v: %w", id, err)
	}

	return nil
}

// reach is what a reader's account belongs to among the audiences of some
// announcements.
type reach struct {
	classes     map[domain.ClassID]bool
	courseTypes map[domain.CourseTypeID]bool
	lessons     map[domain.LessonID]bool
}

func (r reach) reaches(p Principal, a domain.Announcement) bool {
	switch a.Audience {
	case domain.AudienceEveryone:
		return true
	case domain.AudienceRole:
		return p.Role == a.Role
	case domain.AudienceClass:
		return r.classes[a.ClassID]
	case domain.AudienceCourseType:
		return r.courseTypes[a.CourseTypeID]
	case domain.AudienceLesson:
		return r.lessons[a.LessonID]
	default:
		return false
	}
}

// reachOf returns what p's person and its dependents belong to at now, as
// far as the announcements as ask; see ForAccount. It costs a few queries
// whatever the number of announcements.
func (s *AnnouncementService) reachOf(
	ctx context.Context, tx store.Tx, p Principal, now time.Time, as []domain.Announcement,
) (reach, error) {
	r := reach{
		classes: map[domain.ClassID]bool{}, courseTypes: map[domain.CourseTypeID]bool{},
		lessons: map[domain.LessonID]bool{},
	}

	var askLessons []domain.LessonID

	byPersons := false

	for _, a := range as {
		switch a.Audience {
		case domain.AudienceLesson:
			askLessons = append(askLessons, a.LessonID)
		case domain.AudienceClass, domain.AudienceCourseType:
			byPersons = true
		}
	}

	if (!byPersons && len(askLessons) == 0) || p.PersonID.IsZero() {
		return r, nil
	}

	dependents, err := tx.Persons().ListDependents(ctx, p.PersonID)
	if err != nil {
		return r, err
	}

	persons := []domain.PersonID{p.PersonID}
	for _, d := range dependents {
		persons = append(persons, d.ID)
	}

	if len(askLessons) > 0 {
		booked, err := tx.Bookings().ListActiveByLessonsAndPersons(ctx, askLessons, persons)
		if err != nil {
			return r, err
		}

		for _, b := range booked {
			r.lessons[b.LessonID] = true
		}
	}

	if !byPersons {
		return r, nil
	}

	_, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return r, err
	}

	today := domain.DateOf(now, loc)

	// The classes they are members of; their course types count while the
	// class's term has not ended.
	var memberOf []domain.ClassID

	for _, person := range persons {
		ms, err := tx.Classes().ListMemberships(ctx, person)
		if err != nil {
			return r, err
		}

		for _, m := range ms {
			r.classes[m.ClassID] = true
			memberOf = append(memberOf, m.ClassID)
		}
	}

	classes, err := tx.Classes().ListByIDs(ctx, memberOf)
	if err != nil {
		return r, err
	}

	terms, err := termsByID(ctx, tx, classes)
	if err != nil {
		return r, err
	}

	for _, c := range classes {
		if t, ok := terms[c.TermID]; ok && t.EndsOn.Compare(today) >= 0 {
			r.courseTypes[c.CourseTypeID] = true
		}
	}

	// The lessons not over they are booked for.
	bookings, err := tx.Bookings().ListByPersons(ctx, store.BookingQuery{Persons: persons, Active: true, EndsAfter: now})
	if err != nil {
		return r, err
	}

	lessonIDs := make([]domain.LessonID, len(bookings))
	for i, b := range bookings {
		lessonIDs[i] = b.LessonID
	}

	lessons, err := tx.Lessons().ListByIDs(ctx, lessonIDs)
	if err != nil {
		return r, err
	}

	for _, l := range lessons {
		r.courseTypes[l.CourseTypeID] = true
		if !l.ClassID.IsZero() {
			r.classes[l.ClassID] = true
		}
	}

	return r, nil
}

// termsByID returns the terms of the classes by ID.
func termsByID(ctx context.Context, tx store.Tx, classes []domain.Class) (map[domain.TermID]domain.Term, error) {
	out := map[domain.TermID]domain.Term{}

	for _, c := range classes {
		if _, ok := out[c.TermID]; ok {
			continue
		}

		t, err := tx.Terms().Get(ctx, c.TermID)
		if err != nil {
			return nil, fmt.Errorf("term %v: %w", c.TermID, refErr(err))
		}

		out[t.ID] = t
	}

	return out, nil
}

// SendDue emails the announcements whose PublishAt has come and that are to
// be emailed and not yet, up to announcementRecipientBatch mails, and
// returns how many mails it queued. The announcements job calls it every
// minute and when woken; it is the only place that emails announcements.
// The daily limit was checked when they were written.
func (s *AnnouncementService) SendDue(ctx context.Context) (int, error) {
	queued := 0

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		queued = 0 // the callback may run more than once

		// The mailings this writes change what the limit counts. Then the
		// due announcements, locked (skipping those another run has).
		if err := tx.Announcements().LockMail(ctx); err != nil {
			return err
		}

		now := s.clock.Now()

		due, err := tx.Announcements().ListDueForMail(ctx, now, announcementMailBatch)
		if err != nil || len(due) == 0 {
			return err
		}

		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		for _, a := range due {
			if queued == s.recipientBatch {
				break
			}

			n, err := s.sendBatch(ctx, tx, settings, loc, now, a, s.recipientBatch-queued)
			if err != nil {
				return err
			}

			queued += n
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("send announcements: %w", err)
	}

	return queued, nil
}

// sendBatch queues the mails of a, due and locked, to at most limit (> 0)
// of the recipients it has not been mailed to, and returns how many it
// queued. Its recipients resolve at now, ordered by their lowercased
// addresses, and MailCursor keeps the last one mailed, so the next run goes
// on after it and nobody gets the mail twice; someone who joins the
// audience meanwhile is mailed if their address comes later. Once every
// recipient is mailed, a is marked emailed and its mailing recorded (none
// if it reached nobody). One that expired is marked emailed without any
// more mails.
func (s *AnnouncementService) sendBatch(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, now time.Time,
	a domain.Announcement, limit int,
) (int, error) {
	var (
		batch []recipient
		rest  bool
	)

	if !a.ExpiredAt(now) {
		recipients, err := announcementRecipients(ctx, tx, loc, now, a)
		if err != nil {
			return 0, err
		}

		recipients = recipients[sort.Search(len(recipients), func(i int) bool {
			return mailKey(recipients[i].email) > a.MailCursor
		}):]

		batch, rest = recipients[:min(limit, len(recipients))], len(recipients) > limit

		err = sendMails(ctx, tx, s.ids, s.mails, settings, now, batch, mail.TemplateAnnouncement, mailOpts{},
			func(r recipient) any {
				return mail.AnnouncementData{
					SchoolName: settings.SchoolName, GivenName: r.givenName, Participants: r.participants,
					Title: a.Title, Message: a.Body,
				}
			})
		if err != nil {
			return 0, err
		}
	}

	a.EmailRecipients += len(batch)
	if rest {
		a.MailCursor = mailKey(batch[len(batch)-1].email)
	} else {
		a.EmailedAt, a.MailCursor = now, ""
	}

	if err := a.Validate(); err != nil {
		return 0, err
	}

	if err := tx.Announcements().SetMailState(ctx, a); err != nil {
		return 0, err
	}

	if !rest && a.EmailRecipients > 0 {
		if err := s.recordMailing(ctx, tx, now, a); err != nil {
			return 0, err
		}
	}

	return len(batch), nil
}

// recordMailing records that a was mailed to its EmailRecipients, at now:
// what the daily limit counts.
func (s *AnnouncementService) recordMailing(
	ctx context.Context, tx store.Tx, now time.Time, a domain.Announcement,
) error {
	m := domain.AnnouncementMailing{
		ID: ids.New[domain.AnnouncementMailingID](s.ids), AnnouncementID: a.ID, SenderID: a.AuthorID, SentAt: now,
		Recipients: a.EmailRecipients,
	}
	if err := m.Validate(); err != nil {
		return err
	}

	return tx.Announcements().CreateMailing(ctx, m)
}

// mailKey is the key an announcement's recipients are ordered and told
// apart by: the address, lowercased.
func mailKey(email string) string { return strings.ToLower(email) }

// announcementRecipients returns whom a is emailed to at now, each address
// once, ignoring case, ordered by mailKey: for everyone or a role, the
// confirmed accounts (of the role) that are not disabled, in their
// language; for persons, the recipients of the mail chain of each
// (recipientsOf), with the persons they get it for. Guests without an
// account are not reached.
func announcementRecipients(
	ctx context.Context, tx store.Tx, loc *time.Location, now time.Time, a domain.Announcement,
) ([]recipient, error) {
	var set recipientSet

	switch a.Audience {
	case domain.AudienceEveryone, domain.AudienceRole:
		rs, err := accountRecipients(ctx, tx, a.Role)
		if err != nil {
			return nil, err
		}

		for _, r := range rs {
			set.add(r)
		}
	default:
		persons, err := audiencePersons(ctx, tx, loc, now, a)
		if err != nil {
			return nil, err
		}

		chains, err := recipientsOf(ctx, tx, persons)
		if err != nil {
			return nil, err
		}

		for _, person := range persons {
			for _, r := range chains[person] {
				set.add(r, r.participants...)
			}
		}
	}

	return slices.SortedFunc(slices.Values(set.list), func(a, b recipient) int {
		return strings.Compare(mailKey(a.email), mailKey(b.email))
	}), nil
}

// accountRecipients returns the confirmed accounts that are not disabled,
// of the role (any role if empty), with their persons' given names, in the
// order of AccountRepo.List.
func accountRecipients(ctx context.Context, tx store.Tx, role domain.Role) ([]recipient, error) {
	accounts, err := tx.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}

	accounts = slices.DeleteFunc(accounts, func(acc domain.Account) bool {
		return acc.ConfirmedAt.IsZero() || !acc.DisabledAt.IsZero() || (role != "" && acc.Role != role)
	})

	personIDs := make([]domain.PersonID, len(accounts))
	for i, acc := range accounts {
		personIDs[i] = acc.PersonID
	}

	persons, err := tx.Persons().ListByIDs(ctx, personIDs)
	if err != nil {
		return nil, err
	}

	names := byID(persons)
	out := make([]recipient, len(accounts))

	for i, acc := range accounts {
		out[i] = recipient{email: acc.Email, locale: acc.Locale, givenName: names[acc.PersonID].GivenName, account: true, accountID: acc.ID}
	}

	return out, nil
}

// audiencePersons returns the persons of a class, course type or lesson
// audience at now, each once, in a stable order (see ForAccount): the
// members first, then the persons booked, a few queries for any number of
// lessons.
func audiencePersons(
	ctx context.Context, tx store.Tx, loc *time.Location, now time.Time, a domain.Announcement,
) ([]domain.PersonID, error) {
	var (
		persons []domain.PersonID
		lessons []domain.LessonID
	)

	seen := map[domain.PersonID]bool{}
	add := func(id domain.PersonID) {
		if !id.IsZero() && !seen[id] {
			seen[id] = true
			persons = append(persons, id)
		}
	}

	addMembers := func(class domain.ClassID) error {
		ms, err := tx.Classes().ListMembers(ctx, class)
		for _, m := range ms {
			add(m.PersonID)
		}

		return err
	}

	// The lessons not over at now: they started after now minus the
	// longest a lesson lasts, and end after now.
	notOver := func(ls []domain.Lesson, err error) error {
		for _, l := range ls {
			if l.EndsAt.After(now) {
				lessons = append(lessons, l.ID)
			}
		}

		return err
	}

	switch a.Audience {
	case domain.AudienceLesson:
		lessons = []domain.LessonID{a.LessonID}
	case domain.AudienceClass:
		if err := addMembers(a.ClassID); err != nil {
			return nil, err
		}

		if err := notOver(tx.Lessons().ListByClassStartingAfter(ctx, a.ClassID, now.Add(-lessonSpan))); err != nil {
			return nil, err
		}
	case domain.AudienceCourseType:
		today := domain.DateOf(now, loc)

		terms, err := tx.Terms().List(ctx)
		if err != nil {
			return nil, err
		}

		for _, t := range terms {
			if t.EndsOn.Compare(today) < 0 {
				continue
			}

			classes, err := tx.Classes().ListByTerm(ctx, t.ID)
			if err != nil {
				return nil, err
			}

			for _, c := range classes {
				if c.CourseTypeID != a.CourseTypeID {
					continue
				}

				if err := addMembers(c.ID); err != nil {
					return nil, err
				}
			}
		}

		err = notOver(tx.Lessons().ListByCourseTypeStartingAfter(ctx, a.CourseTypeID, now.Add(-lessonSpan)))
		if err != nil {
			return nil, err
		}
	}

	if len(lessons) == 0 {
		return persons, nil
	}

	bookings, err := tx.Bookings().ListActiveByLessons(ctx, lessons)
	if err != nil {
		return nil, err
	}

	for _, b := range bookings {
		add(b.PersonID)
	}

	return persons, nil
}

func announcementIDs(as []domain.Announcement) []domain.AnnouncementID {
	out := make([]domain.AnnouncementID, len(as))
	for i, a := range as {
		out[i] = a.ID
	}

	return out
}

// announcementEntries resolves what the announcements refer to, a few
// queries for any number of them. A reference that cannot be found is
// broken data; the author's person only stays empty.
func announcementEntries(ctx context.Context, tx store.Tx, as []domain.Announcement) ([]AnnouncementEntry, error) {
	authorIDs := make([]domain.AccountID, len(as))

	var (
		classIDs  []domain.ClassID
		lessonIDs []domain.LessonID
	)

	for i, a := range as {
		authorIDs[i] = a.AuthorID
		if !a.ClassID.IsZero() {
			classIDs = append(classIDs, a.ClassID)
		}

		if !a.LessonID.IsZero() {
			lessonIDs = append(lessonIDs, a.LessonID)
		}
	}

	authors, err := auditAccounts(ctx, tx, authorIDs)
	if err != nil {
		return nil, err
	}

	classList, err := tx.Classes().ListByIDs(ctx, classIDs)
	if err != nil {
		return nil, err
	}

	lessonList, err := tx.Lessons().ListByIDs(ctx, lessonIDs)
	if err != nil {
		return nil, err
	}

	terms, err := termsByID(ctx, tx, classList)
	if err != nil {
		return nil, err
	}

	courseTypes, err := tx.CourseTypes().List(ctx)
	if err != nil {
		return nil, err
	}

	classes := make(map[domain.ClassID]domain.Class, len(classList))
	for _, c := range classList {
		classes[c.ID] = c
	}

	lessons := make(map[domain.LessonID]domain.Lesson, len(lessonList))
	for _, l := range lessonList {
		lessons[l.ID] = l
	}

	courseType := func(id domain.CourseTypeID) (domain.CourseType, error) {
		i := slices.IndexFunc(courseTypes, func(c domain.CourseType) bool { return c.ID == id })
		if i < 0 {
			return domain.CourseType{}, fmt.Errorf("course type %v: %w", id, errBrokenReference)
		}

		return courseTypes[i], nil
	}

	out := make([]AnnouncementEntry, len(as))

	for i, a := range as {
		e := AnnouncementEntry{Announcement: a, Author: authors[a.AuthorID].Person}
		ct := a.CourseTypeID

		var ok bool

		switch a.Audience {
		case domain.AudienceClass:
			if e.Class, ok = classes[a.ClassID]; !ok {
				return nil, fmt.Errorf("class %v: %w", a.ClassID, errBrokenReference)
			}

			e.Term, ct = terms[e.Class.TermID], e.Class.CourseTypeID
		case domain.AudienceLesson:
			if e.Lesson, ok = lessons[a.LessonID]; !ok {
				return nil, fmt.Errorf("lesson %v: %w", a.LessonID, errBrokenReference)
			}

			ct = e.Lesson.CourseTypeID
		}

		if !ct.IsZero() {
			if e.CourseType, err = courseType(ct); err != nil {
				return nil, err
			}
		}

		out[i] = e
	}

	return out, nil
}
