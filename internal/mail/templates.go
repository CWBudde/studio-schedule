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
)

// TemplateNames returns all template names.
func TemplateNames() []string {
	return []string{
		TemplateConfirmEmail, TemplateResetPassword, TemplateChangeEmail, TemplateEmailChanged, TemplateInvite,
		TemplateLessonCanceled, TemplateLessonRestored, TemplateLessonChanged,
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

// absoluteHTTPURL reports whether s is an absolute http or https URL with a
// host.
func absoluteHTTPURL(s string) bool {
	u, err := url.Parse(s)

	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
