package mail

import (
	"fmt"
	"net/url"
	"strings"

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
)

// TemplateNames returns all template names.
func TemplateNames() []string {
	return []string{TemplateConfirmEmail, TemplateResetPassword, TemplateChangeEmail, TemplateEmailChanged}
}

// LinkData is the data of the templates that send a single-use link:
// TemplateConfirmEmail, TemplateResetPassword and TemplateChangeEmail.
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

// absoluteHTTPURL reports whether s is an absolute http or https URL with a
// host.
func absoluteHTTPURL(s string) bool {
	u, err := url.Parse(s)

	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
