package domain

import (
	"slices"
	"time"
)

// OutboxMail is an email waiting to be sent, or one that was. Mails are
// written to the outbox in the transaction that causes them and sent later
// by a background job, so a rolled-back transaction sends nothing.
type OutboxMail struct {
	ID      OutboxMailID
	To      string
	Subject string
	Text    string
	HTML    string // optional
	// Template names the template the mail was rendered from (optional).
	Template string
	// ReplyTo is the address replies go to (optional), e.g. the staff
	// member who sent a lesson notice.
	ReplyTo string
	// ReceiptID names the receipt whose PDF is attached (optional). The
	// mail refers to the stored, immutable PDF instead of copying it.
	ReceiptID ReceiptID
	CreatedAt time.Time
	// Attempts counts the delivery attempts so far.
	Attempts int
	// NextAttemptAt is when the next delivery attempt is due. It is zero
	// once the mail is sent or delivery was given up.
	NextAttemptAt time.Time
	SentAt        time.Time // optional
	LastError     string    // optional, the error of the last failed attempt
}

// MailStatus is the delivery state of an outbox mail.
type MailStatus string

// Known mail states.
const (
	MailPending MailStatus = "pending" // not sent yet, another attempt is due
	MailSent    MailStatus = "sent"
	MailFailed  MailStatus = "failed" // delivery was given up
)

// MailStatuses returns all mail states.
func MailStatuses() []MailStatus { return []MailStatus{MailPending, MailSent, MailFailed} }

// Valid reports whether s is a known mail status.
func (s MailStatus) Valid() bool { return slices.Contains(MailStatuses(), s) }

// Status derives the delivery state of the mail.
func (m OutboxMail) Status() MailStatus {
	switch {
	case !m.SentAt.IsZero():
		return MailSent
	case m.NextAttemptAt.IsZero():
		return MailFailed
	default:
		return MailPending
	}
}

// Validate checks the mail for consistency. All errors wrap ErrValidation.
func (m OutboxMail) Validate() error {
	return validate(
		rule{!m.ID.IsZero(), "mail id must be set"},
		rule{validEmail(m.To), "invalid mail recipient"},
		rule{optionalEmail(m.ReplyTo), "invalid reply-to address"},
		rule{!blank(m.Subject), "mail subject must not be empty"},
		rule{!blank(m.Text), "mail text must not be empty"},
		rule{!m.CreatedAt.IsZero(), "mail creation time must be set"},
		rule{m.Attempts >= 0, "mail attempts must not be negative"},
		rule{m.SentAt.IsZero() || m.NextAttemptAt.IsZero(), "a sent mail has no next attempt"},
		rule{m.SentAt.IsZero() || !m.SentAt.Before(m.CreatedAt), "mail cannot be sent before it is created"},
	)
}
