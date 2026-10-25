package mail

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Template names. Each has the files templates/<locale>/<name>.subject.txt,
// .txt and .html in every locale, and a data type documented with it.
const (
	// TemplateConfirmEmail asks a new account to confirm its email address.
	// Data: LinkData.
	TemplateConfirmEmail = "confirm-email"
	// TemplateResetPassword sends the link to set a new password after
	// someone asked to reset it. Data: LinkData.
	TemplateResetPassword = "reset-password"
	// TemplateChangeEmail goes to the new address of an email change and
	// asks to confirm it. Data: LinkData.
	TemplateChangeEmail = "change-email"
	// TemplateEmailChanged tells the old address that the account's email
	// address was changed. Data: EmailChangedData.
	TemplateEmailChanged = "email-changed"
	// TemplateInvite tells a person that the school created an account for
	// them and sends the link to choose its password. Data: LinkData.
	TemplateInvite = "invite"
	// TemplateLessonCanceled tells those booked for a lesson that it is
	// canceled, with the school's note. Data: LessonData.
	TemplateLessonCanceled = "lesson-canceled"
	// TemplateLessonRestored tells those booked for a canceled lesson that
	// it takes place after all. Data: LessonData.
	TemplateLessonRestored = "lesson-restored"
	// TemplateLessonChanged tells those booked for a lesson that its start
	// or room changed. Data: LessonChangedData.
	TemplateLessonChanged = "lesson-changed"
	// TemplateBookingConfirmed confirms a booking to the account that made
	// it (ADR 0009). Data: BookingData with one lesson.
	TemplateBookingConfirmed = "booking-confirmed"
	// TemplateBookingCanceled confirms the cancellation of one or more
	// bookings of a person to the account that canceled them, marking the
	// late ones (ADR 0009). Data: BookingData.
	TemplateBookingCanceled = "booking-canceled"
	// TemplateWaitlistInvite tells a person on the waiting list, through
	// the lesson's recipients (ADR 0010), that a place is reserved for them
	// until an instant, with the link to take it. Data: WaitlistInviteData.
	TemplateWaitlistInvite = "waitlist-invite"
	// TemplateWaitlistExpired tells them that the reserved place was not
	// taken in time and whether they stay on the list. Data:
	// WaitlistExpiredData.
	TemplateWaitlistExpired = "waitlist-expired"
	// TemplateLessonNotice brings a message from a lesson's instructor,
	// accompanist or the office to those booked for it (ADR 0009,
	// amendment); replies go to the sender. Data: LessonNoticeData.
	TemplateLessonNotice = "lesson-notice"
	// TemplateReceipt sends the PDF of a receipt, or of its cancellation, to
	// whoever paid (ADR 0012); the PDF is attached. Data: ReceiptData.
	TemplateReceipt = "receipt"
	// TemplateAnnouncement brings an announcement of the school to its
	// audience (ADR 0014). Data: AnnouncementData.
	TemplateAnnouncement = "announcement"
	// TemplateAccountDeleted confirms to the former address of an account
	// that its holder deleted it (ADR 0016). Data: AccountDeletedData.
	TemplateAccountDeleted = "account-deleted"
)

// TemplateNames returns all template names.
func TemplateNames() []string {
	return []string{
		TemplateConfirmEmail, TemplateResetPassword, TemplateChangeEmail, TemplateEmailChanged, TemplateInvite,
		TemplateLessonCanceled, TemplateLessonRestored, TemplateLessonChanged, TemplateBookingConfirmed,
		TemplateBookingCanceled, TemplateWaitlistInvite, TemplateWaitlistExpired, TemplateLessonNotice,
		TemplateReceipt, TemplateAnnouncement, TemplateAccountDeleted,
	}
}

// SecretLink reports whether mails of the template carry a secret,
// single-use link: whoever reads such a mail can take over what the link
// grants (an account, a password, a reserved place). The mail log never
// shows their bodies and never queues them again (ADR 0014).
func SecretLink(template string) bool {
	switch template {
	case TemplateConfirmEmail, TemplateChangeEmail, TemplateResetPassword, TemplateInvite, TemplateWaitlistInvite:
		return true
	default:
		return false
	}
}

// LinkData is the data of the templates that send a single-use link:
// TemplateConfirmEmail, TemplateResetPassword, TemplateChangeEmail and
// TemplateInvite.
type LinkData struct {
	// SchoolName is the school the account belongs to.
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
	// Link is the absolute URL including the token.
	Link string
	// ExpiresHours is how long the link stays valid, in hours.
	ExpiresHours int
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d LinkData) Validate() error {
	switch {
	case strings.TrimSpace(d.SchoolName) == "":
		return fmt.Errorf("%w: school name must not be empty", domain.ErrValidation)
	case !absoluteHTTPURL(d.Link):
		return fmt.Errorf("%w: link must be an absolute http(s) URL", domain.ErrValidation)
	case d.ExpiresHours <= 0:
		return fmt.Errorf("%w: link expiry must be positive", domain.ErrValidation)
	}

	return nil
}

// EmailChangedData is the data of TemplateEmailChanged.
type EmailChangedData struct {
	// SchoolName is the school the account belongs to.
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
	// NewEmail is the address the account uses from now on.
	NewEmail string
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d EmailChangedData) Validate() error {
	switch {
	case strings.TrimSpace(d.SchoolName) == "":
		return fmt.Errorf("%w: school name must not be empty", domain.ErrValidation)
	case strings.TrimSpace(d.NewEmail) == "":
		return fmt.Errorf("%w: new email address must not be empty", domain.ErrValidation)
	}

	return nil
}

// AccountDeletedData is the data of TemplateAccountDeleted.
type AccountDeletedData struct {
	// SchoolName is the school the account belonged to.
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d AccountDeletedData) Validate() error {
	if strings.TrimSpace(d.SchoolName) == "" {
		return fmt.Errorf("%w: school name must not be empty", domain.ErrValidation)
	}

	return nil
}

// LessonData is the data of the mails about one lesson:
// TemplateLessonCanceled and TemplateLessonRestored. The templates format
// StartsAt and EndsAt in their own location, which must be the school's
// time zone.
type LessonData struct {
	// SchoolName is the school that sends the mail.
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
	// Participants are the names of the booked persons the mail is about:
	// the recipient, their dependents, or a guest.
	Participants []string
	// CourseType names what is taught.
	CourseType string
	// StartsAt and EndsAt are the lesson's times in the school's time zone.
	StartsAt, EndsAt time.Time
	// Room and Location say where the lesson takes place.
	Room, Location string
	// Note is the school's note on a cancellation; optional.
	Note string
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d LessonData) Validate() error {
	switch {
	case strings.TrimSpace(d.SchoolName) == "":
		return fmt.Errorf("%w: school name must not be empty", domain.ErrValidation)
	case len(d.Participants) == 0:
		return fmt.Errorf("%w: a lesson mail needs a participant", domain.ErrValidation)
	case strings.TrimSpace(d.CourseType) == "" || strings.TrimSpace(d.Room) == "" ||
		strings.TrimSpace(d.Location) == "":
		return fmt.Errorf("%w: course type, room and location must not be empty", domain.ErrValidation)
	case d.StartsAt.IsZero() || !d.EndsAt.After(d.StartsAt):
		return fmt.Errorf("%w: a lesson must end after it starts", domain.ErrValidation)
	}

	return nil
}

// LessonChangedData is the data of TemplateLessonChanged: the lesson as it
// is now, and its previous start, end and room.
type LessonChangedData struct {
	LessonData

	PreviousStartsAt, PreviousEndsAt time.Time
	PreviousRoom, PreviousLocation   string
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d LessonChangedData) Validate() error {
	if err := d.LessonData.Validate(); err != nil {
		return err
	}

	if d.PreviousStartsAt.IsZero() || !d.PreviousEndsAt.After(d.PreviousStartsAt) ||
		strings.TrimSpace(d.PreviousRoom) == "" || strings.TrimSpace(d.PreviousLocation) == "" {
		return fmt.Errorf("%w: the previous start, end, room and location must be set", domain.ErrValidation)
	}

	return nil
}

// LessonNoticeData is the data of TemplateLessonNotice: the lesson, who
// wrote the message, and the message. LessonData.Note is not used.
type LessonNoticeData struct {
	LessonData

	// Canceled tells that the school canceled the lesson.
	Canceled bool
	// Sender is the name of the staff member who wrote the message.
	Sender string
	// Message is the plain text of the message. The templates keep its
	// line breaks and escape it in the HTML part.
	Message string
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d LessonNoticeData) Validate() error {
	if err := d.LessonData.Validate(); err != nil {
		return err
	}

	switch {
	case strings.TrimSpace(d.Sender) == "":
		return fmt.Errorf("%w: a notice needs a sender", domain.ErrValidation)
	case strings.TrimSpace(d.Message) == "":
		return fmt.Errorf("%w: a notice needs a message", domain.ErrValidation)
	}

	return nil
}

// BookingData is the data of TemplateBookingConfirmed and
// TemplateBookingCanceled: the bookings of one person, each in a lesson. The
// templates format the times in their own location, which must be the
// school's time zone.
type BookingData struct {
	// SchoolName is the school that sends the mail.
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
	// Participant names the booked person: the recipient or a dependent.
	Participant string
	// Guest tells that the recipient is a guest whom staff booked, who has
	// no account to cancel with: TemplateBookingConfirmed asks them to
	// contact the school instead.
	Guest bool
	// Lessons are the booked lessons, ordered by start.
	Lessons []BookedLesson
}

// BookedLesson is one lesson of a BookingData.
type BookedLesson struct {
	// CourseType names what is taught.
	CourseType string
	// StartsAt and EndsAt are the lesson's times in the school's time zone.
	StartsAt, EndsAt time.Time
	// Room and Location say where the lesson takes place.
	Room, Location string
	// Late marks a cancellation after the cancellation cutoff; only in
	// TemplateBookingCanceled.
	Late bool
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d BookingData) Validate() error {
	switch {
	case strings.TrimSpace(d.SchoolName) == "":
		return fmt.Errorf("%w: school name must not be empty", domain.ErrValidation)
	case strings.TrimSpace(d.Participant) == "":
		return fmt.Errorf("%w: a booking mail needs a participant", domain.ErrValidation)
	case len(d.Lessons) == 0:
		return fmt.Errorf("%w: a booking mail needs a lesson", domain.ErrValidation)
	}

	for _, l := range d.Lessons {
		switch {
		case strings.TrimSpace(l.CourseType) == "" || strings.TrimSpace(l.Room) == "" ||
			strings.TrimSpace(l.Location) == "":
			return fmt.Errorf("%w: course type, room and location must not be empty", domain.ErrValidation)
		case l.StartsAt.IsZero() || !l.EndsAt.After(l.StartsAt):
			return fmt.Errorf("%w: a lesson must end after it starts", domain.ErrValidation)
		}
	}

	return nil
}

// WaitlistData is the part the waiting-list mails share: a person on the
// list of one lesson. The templates format the times in their own location,
// which must be the school's time zone.
type WaitlistData struct {
	// SchoolName is the school that sends the mail.
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
	// Participant names the person on the list: the recipient or a
	// dependent.
	Participant string
	// CourseType names what is taught.
	CourseType string
	// StartsAt and EndsAt are the lesson's times in the school's time zone.
	StartsAt, EndsAt time.Time
	// Room and Location say where the lesson takes place.
	Room, Location string
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d WaitlistData) Validate() error {
	switch {
	case strings.TrimSpace(d.SchoolName) == "":
		return fmt.Errorf("%w: school name must not be empty", domain.ErrValidation)
	case strings.TrimSpace(d.Participant) == "":
		return fmt.Errorf("%w: a waiting-list mail needs a participant", domain.ErrValidation)
	case strings.TrimSpace(d.CourseType) == "" || strings.TrimSpace(d.Room) == "" ||
		strings.TrimSpace(d.Location) == "":
		return fmt.Errorf("%w: course type, room and location must not be empty", domain.ErrValidation)
	case d.StartsAt.IsZero() || !d.EndsAt.After(d.StartsAt):
		return fmt.Errorf("%w: a lesson must end after it starts", domain.ErrValidation)
	}

	return nil
}

// WaitlistInviteData is the data of TemplateWaitlistInvite.
type WaitlistInviteData struct {
	WaitlistData

	// Link is the absolute URL of the page that accepts the invite,
	// including its token.
	Link string
	// ExpiresAt is when the reservation ends, in the school's time zone.
	ExpiresAt time.Time
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d WaitlistInviteData) Validate() error {
	if err := d.WaitlistData.Validate(); err != nil {
		return err
	}

	switch {
	case !absoluteHTTPURL(d.Link):
		return fmt.Errorf("%w: link must be an absolute http(s) URL", domain.ErrValidation)
	case d.ExpiresAt.IsZero():
		return fmt.Errorf("%w: the invite needs an expiry", domain.ErrValidation)
	}

	return nil
}

// WaitlistExpiredData is the data of TemplateWaitlistExpired.
type WaitlistExpiredData struct {
	WaitlistData

	// StillWaiting tells that the person went back to the end of the list
	// (they chose to stay on it) rather than leaving it.
	StillWaiting bool
}

// absoluteHTTPURL reports whether s is an absolute http or https URL with a
// host.
func absoluteHTTPURL(s string) bool {
	u, err := url.Parse(s)

	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// ReceiptData is the data of TemplateReceipt.
type ReceiptData struct {
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
	// Number is the receipt's number, e.g. "SD-2026-0001".
	Number string
	// Cancellation tells a cancellation receipt; Cancels is then the number
	// of the receipt it cancels.
	Cancellation bool
	Cancels      string
	// Amount is the amount paid or refunded, formatted with its currency.
	Amount string
	// Item describes what was paid, e.g. "10-visit pass for Lea Example,
	// valid …".
	Item string
}

// AnnouncementData is the data of TemplateAnnouncement.
type AnnouncementData struct {
	// SchoolName is the school that sends the mail.
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
	// Participants are the persons the recipient gets the mail for, when
	// the audience is the members or participants of classes or a lesson:
	// the recipient, their dependents; empty for everyone or a role.
	Participants []string
	// Title is the announcement's title.
	Title string
	// Message is its plain text. The templates keep its line breaks and
	// escape it in the HTML part.
	Message string
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d AnnouncementData) Validate() error {
	switch {
	case strings.TrimSpace(d.SchoolName) == "":
		return fmt.Errorf("%w: school name must not be empty", domain.ErrValidation)
	case strings.TrimSpace(d.Title) == "":
		return fmt.Errorf("%w: an announcement needs a title", domain.ErrValidation)
	case strings.TrimSpace(d.Message) == "":
		return fmt.Errorf("%w: an announcement needs a message", domain.ErrValidation)
	}

	return nil
}
