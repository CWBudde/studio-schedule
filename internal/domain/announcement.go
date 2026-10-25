package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Announcements (ADR 0014): what the office tells everyone, a role, the
// members of a class or a course type, or the participants of one lesson,
// shown in the app from PublishAt until ExpiresAt and optionally emailed
// once.

// Bounds of announcements.
const (
	// MaxAnnouncementTitleLen is the longest title, in characters.
	MaxAnnouncementTitleLen = 120
	// MaxAnnouncementBodyLen is the longest text, in characters.
	MaxAnnouncementBodyLen = 4000
	// AnnouncementMailsPerDay is how many announcements the school emails
	// on one school day, sent and scheduled together.
	AnnouncementMailsPerDay = 3
)

// Audience selects whom an announcement is for.
type Audience string

// Known audiences.
const (
	// AudienceEveryone is every account.
	AudienceEveryone Audience = "everyone"
	// AudienceRole is the accounts of one role (Announcement.Role).
	AudienceRole Audience = "role"
	// AudienceClass is the members of one class and the persons booked for
	// one of its lessons that is not over (Announcement.ClassID).
	AudienceClass Audience = "class"
	// AudienceCourseType is the same for every class and lesson of one
	// course type (Announcement.CourseTypeID).
	AudienceCourseType Audience = "course-type"
	// AudienceLesson is the persons booked for one lesson
	// (Announcement.LessonID).
	AudienceLesson Audience = "lesson"
)

// Audiences returns all audiences.
func Audiences() []Audience {
	return []Audience{AudienceEveryone, AudienceRole, AudienceClass, AudienceCourseType, AudienceLesson}
}

// Valid reports whether a is a known audience.
func (a Audience) Valid() bool { return slices.Contains(Audiences(), a) }

// Announcement is a message of the school to an audience (ADR 0014). Its
// audience resolves when it is read and when it is emailed, so it reaches
// whoever belongs to it then.
type Announcement struct {
	ID AnnouncementID
	// AuthorID is the account that wrote it: the admin, not the account
	// they impersonated.
	AuthorID AccountID
	Title    string
	// Body is plain text with line breaks.
	Body     string
	Audience Audience
	// Role, ClassID, CourseTypeID and LessonID name the audience; only the
	// one its kind needs is set.
	Role         Role
	ClassID      ClassID
	CourseTypeID CourseTypeID
	LessonID     LessonID
	// PublishAt is when it shows; it may lie in the future.
	PublishAt time.Time
	// ExpiresAt is when it stops showing (optional).
	ExpiresAt time.Time
	// Email tells that it is emailed to its audience once, at PublishAt.
	Email bool
	// EmailedAt is when it was emailed to its last recipient (or found to
	// reach nobody, or to have expired before it was sent); zero until
	// then. It is set once, so an announcement is emailed once.
	EmailedAt time.Time
	// EmailRecipients is how many addresses it was emailed to, so far while
	// it is being sent.
	EmailRecipients int
	// MailCursor is, while it is being sent in batches, the last address
	// it was mailed to, lowercased: its recipients are mailed in the order
	// of their lowercased addresses. Empty before the first batch and once
	// it is emailed.
	MailCursor string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Validate checks the announcement for consistency. All errors wrap
// ErrValidation.
func (a Announcement) Validate() error {
	title, body := utf8.RuneCountInString(a.Title), utf8.RuneCountInString(a.Body)

	return validate(
		rule{!a.ID.IsZero(), "announcement id must be set"},
		rule{!a.AuthorID.IsZero(), "announcement author must be set"},
		on("title", !blank(a.Title), "title must not be empty"),
		on("title", title <= MaxAnnouncementTitleLen, "title must not exceed 120 characters"),
		on("title", a.Title == strings.TrimSpace(a.Title) && !strings.ContainsAny(a.Title, "\r\n"),
			"title must be one line without spaces around it"),
		on("body", !blank(a.Body), "text must not be empty"),
		on("body", body <= MaxAnnouncementBodyLen, "text must not exceed 4000 characters"),
		on("audience", a.Audience.Valid(), "invalid audience"),
		on("role", (a.Audience == AudienceRole) == (a.Role != ""), "a role is needed exactly for the role audience"),
		on("role", a.Role == "" || (a.Role.Valid() && a.Role != RoleGuest), "invalid role"),
		on("classId", (a.Audience == AudienceClass) == !a.ClassID.IsZero(),
			"a class is needed exactly for the class audience"),
		on("courseTypeId", (a.Audience == AudienceCourseType) == !a.CourseTypeID.IsZero(),
			"a course type is needed exactly for the course type audience"),
		on("lessonId", (a.Audience == AudienceLesson) == !a.LessonID.IsZero(),
			"a lesson is needed exactly for the lesson audience"),
		on("publishAt", !a.PublishAt.IsZero(), "publish time must be set"),
		on("expiresAt", a.ExpiresAt.IsZero() || a.ExpiresAt.After(a.PublishAt), "must expire after it is published"),
		rule{a.EmailedAt.IsZero() || a.Email, "only an emailed announcement has a mail time"},
		rule{a.EmailRecipients >= 0, "email recipients must not be negative"},
		rule{a.MailCursor == "" || (a.Email && a.EmailedAt.IsZero()), "only an announcement being sent has a mail cursor"},
		rule{
			a.EmailRecipients == 0 || !a.EmailedAt.IsZero() || a.MailCursor != "",
			"recipients are counted while and when it is emailed",
		},
		rule{!a.CreatedAt.IsZero(), "announcement creation time must be set"},
		rule{!a.UpdatedAt.Before(a.CreatedAt), "announcement cannot be updated before it is created"},
	)
}

// ShownAt reports whether the announcement shows at now: published and not
// expired.
func (a Announcement) ShownAt(now time.Time) bool {
	return !a.PublishAt.After(now) && !a.ExpiredAt(now)
}

// MailPending reports whether the announcement is to be emailed and not
// (completely) emailed yet: scheduled, or being sent.
func (a Announcement) MailPending() bool { return a.Email && a.EmailedAt.IsZero() }

// MailStarted reports whether mailing the announcement has begun: it is
// emailed, or being sent.
func (a Announcement) MailStarted() bool { return !a.EmailedAt.IsZero() || a.MailCursor != "" }

// ExpiredAt reports whether the announcement has expired at now.
func (a Announcement) ExpiredAt(now time.Time) bool {
	return !a.ExpiresAt.IsZero() && !a.ExpiresAt.After(now)
}

// AnnouncementDismissal records that an account dismissed an announcement:
// it no longer shows in that account's banner, but still in its list.
type AnnouncementDismissal struct {
	AnnouncementID AnnouncementID
	AccountID      AccountID
	DismissedAt    time.Time
}

// Validate checks the dismissal for consistency. All errors wrap
// ErrValidation.
func (d AnnouncementDismissal) Validate() error {
	return validate(
		rule{!d.AnnouncementID.IsZero(), "dismissed announcement must be set"},
		rule{!d.AccountID.IsZero(), "dismissing account must be set"},
		rule{!d.DismissedAt.IsZero(), "dismissal time must be set"},
	)
}

// AnnouncementMailing records that an announcement was emailed: what the
// daily limit of emailed announcements counts. It outlives its
// announcement with a zero AnnouncementID, so deleting an announcement
// does not free its place in the limit.
type AnnouncementMailing struct {
	ID AnnouncementMailingID
	// AnnouncementID is zero once the announcement is deleted.
	AnnouncementID AnnouncementID
	// SenderID is the announcement's author.
	SenderID AccountID
	SentAt   time.Time
	// Recipients is how many addresses it was mailed to.
	Recipients int
}

// Validate checks the mailing for consistency. All errors wrap
// ErrValidation.
func (m AnnouncementMailing) Validate() error {
	return validate(
		rule{!m.ID.IsZero(), "mailing id must be set"},
		rule{!m.AnnouncementID.IsZero(), "mailed announcement must be set"},
		rule{!m.SenderID.IsZero(), "mailing sender must be set"},
		rule{!m.SentAt.IsZero(), "mailing time must be set"},
		rule{m.Recipients > 0, "a mailing needs a recipient"},
	)
}
