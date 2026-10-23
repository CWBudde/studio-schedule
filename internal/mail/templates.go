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
	// Data: ConfirmEmailData.
	TemplateConfirmEmail = "confirm-email"
)

// TemplateNames returns all template names.
func TemplateNames() []string { return []string{TemplateConfirmEmail} }

// ConfirmEmailData is the data of TemplateConfirmEmail.
type ConfirmEmailData struct {
	// SchoolName is the school the account belongs to.
	SchoolName string
	// GivenName greets the recipient; empty means a greeting without name.
	GivenName string
	// Link is the absolute confirmation URL including the token.
	Link string
	// ExpiresHours is how long the link stays valid, in hours.
	ExpiresHours int
}

// Validate checks the data before rendering. All errors wrap
// domain.ErrValidation.
func (d ConfirmEmailData) Validate() error {
	switch {
	case strings.TrimSpace(d.SchoolName) == "":
		return fmt.Errorf("%w: school name must not be empty", domain.ErrValidation)
	case !absoluteHTTPURL(d.Link):
		return fmt.Errorf("%w: confirmation link must be an absolute http(s) URL", domain.ErrValidation)
	case d.ExpiresHours <= 0:
		return fmt.Errorf("%w: link expiry must be positive", domain.ErrValidation)
	}

	return nil
}

// absoluteHTTPURL reports whether s is an absolute http or https URL with a
// host.
func absoluteHTTPURL(s string) bool {
	u, err := url.Parse(s)

	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
