// Package mail defines outgoing email messages, the Mailer that delivers
// them, and the localized templates that produce them.
//
// The package compiles for both targets (server and WebAssembly demo), so it
// must not import net/smtp or other server-only packages. The SMTP transport
// lives in the server-only subpackage internal/mail/smtp.
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// MaxSubjectLen is the maximum subject length in characters. It keeps the
// encoded header well below the 998-byte line limit of RFC 5322.
const MaxSubjectLen = 200

// Message is one email to a single recipient. Text is required; HTML is an
// optional alternative of the same content.
type Message struct {
	// To is a bare address such as "anna@example.org" (no display name).
	To string
	// Subject is a single line.
	Subject string
	// Text is the plain-text body.
	Text string
	// HTML is the HTML body; empty means a text-only mail.
	HTML string
	// ReplyTo is a bare address replies go to; empty means the sender.
	ReplyTo string
}

// Validate checks that the message can be sent as is. All errors wrap
// domain.ErrValidation.
func (m Message) Validate() error {
	switch {
	case !bareAddress(m.To):
		return fmt.Errorf("%w: mail recipient must be a single bare address", domain.ErrValidation)
	case m.ReplyTo != "" && !bareAddress(m.ReplyTo):
		return fmt.Errorf("%w: reply-to must be a single bare address", domain.ErrValidation)
	case strings.TrimSpace(m.Subject) == "":
		return fmt.Errorf("%w: mail subject must not be empty", domain.ErrValidation)
	case strings.ContainsAny(m.Subject, "\r\n"):
		return fmt.Errorf("%w: mail subject must be a single line", domain.ErrValidation)
	case utf8.RuneCountInString(m.Subject) > MaxSubjectLen:
		return fmt.Errorf("%w: mail subject must not exceed %d characters", domain.ErrValidation, MaxSubjectLen)
	case strings.TrimSpace(m.Text) == "":
		return fmt.Errorf("%w: mail text must not be empty", domain.ErrValidation)
	}

	return nil
}

// bareAddress reports whether s is exactly one address without display name
// or surrounding spaces.
func bareAddress(s string) bool {
	a, err := mail.ParseAddress(s)

	return err == nil && a.Name == "" && a.Address == s
}

// ErrPermanent is wrapped by Mailer errors that will recur when the same
// message is sent again (an invalid message, a rejected recipient). The
// outbox gives up on such a mail instead of retrying it.
var ErrPermanent = errors.New("permanent mail delivery failure")

// Mailer delivers a message. Implementations validate the message first
// and wrap ErrPermanent around errors that a retry cannot fix.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}
