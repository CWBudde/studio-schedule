package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The instructor view (ADR 0009, amendment): the lessons a staff member
// teaches or accompanies, with their counts and attendance state, and the
// notices staff send to the participants of a lesson.

const (
	// MaxTeachDays is the longest date range one request of the instructor
	// view may cover.
	MaxTeachDays = 31
	// teachDaysBack and teachDaysAhead are the default range around today:
	// the last week, today and the next week.
	teachDaysBack, teachDaysAhead = 7, 7

	// MaxNoticeLen is the longest message of a notice, in characters.
	MaxNoticeLen = 1000
	// NoticesPerHour is how many notices an account may send in an hour.
	NoticesPerHour = 5
	// NoticesPerLessonPerDay is how many notices may be sent about one
	// lesson on one school day, by anyone.
	NoticesPerLessonPerDay = 3
)

var (
	errNoticeEmpty error = &domain.FieldError{Field: "message", Msg: "message must not be empty"}
	errNoticeLong  error = &domain.FieldError{
		Field: "message", Msg: fmt.Sprintf("message must not exceed %d characters", MaxNoticeLen),
	}
	errNoticeControl error = &domain.FieldError{
		Field: "message", Msg: "message must not contain control or invisible format characters, " +
			"other than line breaks and tabs",
	}
)

// TooManyNoticesError refuses a notice over the limits: NoticesPerHour per
// account, NoticesPerLessonPerDay per lesson. RetryAfter is when the next
// notice is allowed again.
type TooManyNoticesError struct {
	RetryAfter time.Duration
}

func (e *TooManyNoticesError) Error() string {
	return fmt.Sprintf("too many notices; try again in %v", e.RetryAfter)
}

// TeachRole is the caller's part in a lesson of the instructor view.
type TeachRole string

// The parts in a lesson.
const (
	TeachInstructor  TeachRole = "instructor"
	TeachAccompanist TeachRole = "accompanist"
	// TeachOffice: the office sees a lesson it neither teaches nor
	// accompanies.
	TeachOffice TeachRole = "office"
)

// TeachQuery selects the lessons of the instructor view. From and To are
// school dates, To exclusive; zero means the default range: From a week
// before today, To 15 days after From (today and the next week).
type TeachQuery struct {
	From, To domain.Date
	// Staff keeps the lessons this staff member teaches or accompanies;
	// optional.
	Staff domain.StaffID
}

// TeachLesson is a lesson of the instructor view.
type TeachLesson struct {
	// ScheduledLesson is the lesson with its references and FreePlaces.
	ScheduledLesson

	Role TeachRole
	// Active counts the active bookings, Held the places held by invites,
	// Waiting the waiting-list entries waiting for an invite, Trials and
	// Guests the active bookings that are trials and guests.
	Active, Held, Waiting, Trials, Guests int
	// DayOverAt is when the attendance window of its instructor and
	// accompanist closes.
	DayOverAt time.Time
	// AttendanceOpen tells that the caller may take attendance now: the
	// lesson takes place and has started, and for its instructor and
	// accompanist its day is not over.
	AttendanceOpen bool
	// AttendanceTaken tells that the lesson takes place, has started and
	// has no active booking whose attendance is unknown (status booked).
	AttendanceTaken bool
}

// TeachLessons is the instructor view of a date range.
type TeachLessons struct {
	// Timezone is the IANA time zone of the school.
	Timezone string
	// Now is the server time the view was computed at.
	Now      time.Time
	From, To domain.Date
	// Lessons are ordered by start and ID.
	Lessons []TeachLesson
}

// TeachService is the instructor view: the lessons of a staff member and
// the notices to their participants.
type TeachService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
	mails MailRenderer
}

// NewTeachService returns a TeachService that renders its notices with
// mails.
func NewTeachService(st store.Store, clk clock.Clock, src ids.Source, mails MailRenderer) *TeachService {
	return &TeachService{store: st, clock: clk, ids: src, mails: mails}
}

// window resolves the defaults of q at today and checks the range: To
// after From, at most MaxTeachDays (domain.ErrValidation).
func (q TeachQuery) window(today domain.Date) (domain.Date, domain.Date, error) {
	from, to := q.From, q.To
	if from.IsZero() {
		from = today.AddDays(-teachDaysBack)
	}

	if to.IsZero() {
		to = from.AddDays(teachDaysBack + 1 + teachDaysAhead)
	}

	switch {
	case !from.Valid() || !to.Valid():
		return from, to, fmt.Errorf("%w: invalid date", domain.ErrValidation)
	case to.Compare(from) <= 0:
		return from, to, fmt.Errorf("%w: to must be after from", domain.ErrValidation)
	case to.Compare(from.AddDays(MaxTeachDays)) > 0:
		return from, to, fmt.Errorf("%w: the range must not exceed %d days", domain.ErrValidation, MaxTeachDays)
	}

	return from, to, nil
}

// Lessons returns the lessons that start in the school dates of q (see
// TeachQuery) that p teaches or accompanies: for an instructor or
// accompanist, the lessons of their active staff records linked to their
// person (none without one); for the office, every lesson. q.Staff narrows
// either to that staff member's lessons. Canceled and hidden lessons are
// included. It costs the same few queries for any number of lessons.
func (s *TeachService) Lessons(ctx context.Context, p Principal, q TeachQuery) (TeachLessons, error) {
	out := TeachLessons{Now: s.clock.Now()}

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		out.Timezone, out.Lessons = settings.Timezone, nil // the callback may run more than once

		if out.From, out.To, err = q.window(domain.DateOf(out.Now, loc)); err != nil {
			return err
		}

		own, err := ownStaff(ctx, tx, p.PersonID)
		if err != nil {
			return err
		}

		lessons, err := s.lessonsOf(ctx, tx, p, own, q.Staff,
			out.From.At(domain.TimeOfDay{}, loc), out.To.At(domain.TimeOfDay{}, loc))
		if err != nil || len(lessons) == 0 {
			return err
		}

		out.Lessons, err = teachLessons(ctx, tx, p, loc, out.Now, own, out.From, out.To, lessons)

		return err
	})
	if err != nil {
		return TeachLessons{}, fmt.Errorf("teach lessons: %w", err)
	}

	return out, nil
}

// lessonsOf returns the lessons in [from, to) that p sees in the instructor
// view, narrowed to those of staff unless it is zero.
func (s *TeachService) lessonsOf(
	ctx context.Context, tx store.Tx, p Principal, own []domain.Staff, staff domain.StaffID, from, to time.Time,
) ([]domain.Lesson, error) {
	ownIDs := make([]domain.StaffID, len(own))
	for i, st := range own {
		ownIDs[i] = st.ID
	}

	switch {
	case p.Role.AtLeast(domain.RoleOffice) && staff.IsZero():
		return tx.Lessons().ListRange(ctx, from, to)
	case p.Role.AtLeast(domain.RoleOffice):
		return tx.Lessons().ListByStaff(ctx, []domain.StaffID{staff}, from, to)
	case !staff.IsZero() && !slices.Contains(ownIDs, staff):
		return nil, nil
	case !staff.IsZero():
		return tx.Lessons().ListByStaff(ctx, []domain.StaffID{staff}, from, to)
	case len(own) == 0:
		return nil, nil
	default:
		return tx.Lessons().ListByStaff(ctx, ownIDs, from, to)
	}
}

// teachLessons adds the references, the counts and the attendance state
// to the lessons, which start in the school dates [from, to), as p sees
// them at now.
func teachLessons(
	ctx context.Context, tx store.Tx, p Principal, loc *time.Location, now time.Time, own []domain.Staff,
	from, to domain.Date, lessons []domain.Lesson,
) ([]TeachLesson, error) {
	refs, err := loadRangeRefs(ctx, tx, from, to)
	if err != nil {
		return nil, err
	}

	wanted := lessonIDs(lessons)

	counts, err := tx.Bookings().CountsByLessons(ctx, wanted)
	if err != nil {
		return nil, fmt.Errorf("active bookings: %w", err)
	}

	open, err := tx.Waitlist().ListOpenByLessons(ctx, wanted)
	if err != nil {
		return nil, fmt.Errorf("waiting lists: %w", err)
	}

	waiting := map[domain.LessonID]int{}

	for _, e := range open {
		if e.Status == domain.WaitlistWaiting {
			waiting[e.LessonID]++
		}
	}

	held := heldPlaces(open, now)
	out := make([]TeachLesson, 0, len(lessons))

	for _, l := range lessons {
		if err := refs.loadClass(ctx, tx, l.ClassID); err != nil {
			return nil, err
		}

		sl, err := refs.resolve(l)
		if err != nil {
			return nil, err
		}

		c := counts[l.ID]
		sl.FreePlaces = max(0, l.Capacity-c.Active-held[l.ID])
		started := l.Status != domain.LessonCanceled && !now.Before(l.StartsAt)
		_, role := roleIn(l, own)

		out = append(out, TeachLesson{
			ScheduledLesson: sl, Role: role, Active: c.Active, Held: held[l.ID], Waiting: waiting[l.ID],
			Trials: c.Trials, Guests: c.Guests, DayOverAt: dayOverAt(loc, l),
			AttendanceOpen: attendable(p, loc, now, l) == nil, AttendanceTaken: started && c.Booked == 0,
		})
	}

	return out, nil
}

// noticeText normalizes the message of a notice, its line breaks (CR LF,
// CR, and the Unicode line and paragraph separators) to "\n" and without
// the space around it, and checks it: 1 to MaxNoticeLen characters, valid
// UTF-8, no control characters but line breaks and tabs, and no format
// characters (bidi overrides, zero-width spaces) but the zero-width
// joiners that emoji and some scripts need (a *domain.FieldError on
// message).
func noticeText(message string) (string, error) {
	message = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u2028", "\n", "\u2029", "\n").Replace(message)
	message = strings.TrimSpace(message)

	switch n := utf8.RuneCountInString(message); {
	case n == 0:
		return "", errNoticeEmpty
	case n > MaxNoticeLen:
		return "", errNoticeLong
	case !utf8.ValidString(message) || strings.ContainsFunc(message, hiddenRune):
		return "", errNoticeControl
	}

	return message, nil
}

// hiddenRune reports a control or format character that a notice must not
// contain (noticeText).
func hiddenRune(r rune) bool {
	switch r {
	case '\n', '\t', '\u200c', '\u200d': // ZWNJ, ZWJ
		return false
	default:
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}
}

// Notify mails message to everyone booked for a lesson p manages
// (domain.ErrNotFound otherwise; the office every lesson, an instructor or
// accompanist their own): the recipient chain of every active booking and
// the guests with an email, each address once, each in its language (ADR
// 0009, amendment). The sender is the account that sends (the admin while
// impersonating); replies go to its address. The mail names it as the
// lesson's instructor or accompanist if one of its staff records is (the
// name in the schedule), else by its person's name. It works on lessons
// that are over or canceled too. The message is plain text (noticeText; a
// *domain.FieldError on message otherwise). An account sends at most
// NoticesPerHour notices in an hour, and a lesson gets at most
// NoticesPerLessonPerDay on one school day, from anyone; beyond that it is
// a *TooManyNoticesError. A lesson nobody is mailed for counts no notice.
// It returns how many addresses it mailed.
func (s *TeachService) Notify(ctx context.Context, p Principal, lesson domain.LessonID, message string) (int, error) {
	message, err := noticeText(message)
	if err != nil {
		return 0, fmt.Errorf("notify lesson %v: %w", lesson, err)
	}

	sent := 0

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		sent = 0 // the callback may run more than once

		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		l, err := lockManaged(ctx, tx, p, lesson)
		if err != nil {
			return err
		}

		sender, err := tx.Accounts().Get(ctx, actorOf(p))
		if err != nil {
			return fmt.Errorf("sender: %w", refErr(err))
		}

		// The lesson lock serializes the lesson's notices, the sender's
		// person lock the sender's: their limits hold under concurrency.
		if err := tx.Persons().Lock(ctx, sender.PersonID); err != nil {
			return fmt.Errorf("sender: %w", refErr(err))
		}

		now := s.clock.Now()
		if err := noticeLimits(ctx, tx, loc, now, sender.ID, l.ID); err != nil {
			return err
		}

		recipients, err := lessonRecipients(ctx, tx, l.ID)
		if err != nil || len(recipients) == 0 {
			return err
		}

		name, err := senderName(ctx, tx, sender, l)
		if err != nil {
			return err
		}

		ct, err := tx.CourseTypes().Get(ctx, l.CourseTypeID)
		if err != nil {
			return fmt.Errorf("course type %v: %w", l.CourseTypeID, refErr(err))
		}

		room, location, err := place(ctx, tx, l.RoomID)
		if err != nil {
			return err
		}

		err = sendMails(ctx, tx, s.ids, s.mails, settings, now, recipients, mail.TemplateLessonNotice,
			mailOpts{replyTo: sender.Email}, func(r recipient) any {
				return mail.LessonNoticeData{
					LessonData: mail.LessonData{
						SchoolName: settings.SchoolName, GivenName: r.givenName, Participants: r.participants,
						CourseType: ct.Name, StartsAt: l.StartsAt.In(loc), EndsAt: l.EndsAt.In(loc),
						Room: room.Name, Location: location.Name,
					},
					Canceled: l.Status == domain.LessonCanceled, Sender: name, Message: message,
				}
			})
		if err != nil {
			return err
		}

		sent = len(recipients)
		n := domain.LessonNotice{
			ID: ids.New[domain.LessonNoticeID](s.ids), LessonID: l.ID, SenderID: sender.ID, SentAt: now,
			Recipients: sent,
		}

		if err := n.Validate(); err != nil {
			return err
		}

		return tx.Notices().Create(ctx, n)
	})
	if err != nil {
		return 0, fmt.Errorf("notify lesson %v: %w", lesson, err)
	}

	return sent, nil
}

// senderName is how a notice about lesson l names its sender: the display
// name of the sender's staff record that is l's instructor or accompanist
// (the name in the schedule), else the sender person's name.
func senderName(ctx context.Context, tx store.Tx, sender domain.Account, l domain.Lesson) (string, error) {
	own, err := ownStaff(ctx, tx, sender.PersonID)
	if err != nil {
		return "", err
	}

	if st, role := roleIn(l, own); role != TeachOffice {
		return st.DisplayName, nil
	}

	person, err := tx.Persons().Get(ctx, sender.PersonID)
	if err != nil {
		return "", fmt.Errorf("sender: %w", refErr(err))
	}

	return strings.TrimSpace(fullName(person)), nil
}

// noticeLimits checks the limits of a notice that sender sends about
// lesson at now (see Notify): a *TooManyNoticesError with the longer wait
// if one is used up.
func noticeLimits(
	ctx context.Context, tx store.Tx, loc *time.Location, now time.Time, sender domain.AccountID,
	lesson domain.LessonID,
) error {
	var wait time.Duration

	bySender, err := tx.Notices().ListBySender(ctx, sender, now.Add(-time.Hour))
	if err != nil {
		return err
	}

	if n := len(bySender); n >= NoticesPerHour {
		// The hour of the oldest notice that must leave the window.
		wait = bySender[n-NoticesPerHour].SentAt.Add(time.Hour).Sub(now)
	}

	today := domain.DateOf(now, loc)

	byLesson, err := tx.Notices().ListByLesson(ctx, lesson, today.At(domain.TimeOfDay{}, loc))
	if err != nil {
		return err
	}

	if len(byLesson) >= NoticesPerLessonPerDay {
		wait = max(wait, today.AddDays(1).At(domain.TimeOfDay{}, loc).Sub(now))
	}

	if len(bySender) < NoticesPerHour && len(byLesson) < NoticesPerLessonPerDay {
		return nil
	}

	return &TooManyNoticesError{RetryAfter: max(wait, time.Second)}
}
