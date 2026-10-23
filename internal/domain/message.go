package domain

import (
	"slices"
	"time"
)

// Audience selects who receives a message.
type Audience string

// Known audiences.
const (
	AudienceEveryone Audience = "everyone" // every account
	AudienceMembers  Audience = "members"  // member accounts
	AudienceStaff    Audience = "staff"    // instructor, office and admin accounts
)

// Audiences returns all audiences.
func Audiences() []Audience { return []Audience{AudienceEveryone, AudienceMembers, AudienceStaff} }

// Valid reports whether a is a known audience.
func (a Audience) Valid() bool { return slices.Contains(Audiences(), a) }

// Message is an announcement shown in the app and/or sent by email.
type Message struct {
	ID              MessageID
	AuthorAccountID AccountID
	Title           string
	Body            string
	Audience        Audience
	InApp           bool
	Email           bool
	PublishedAt     time.Time
	ExpiresAt       time.Time // optional
}

// Validate checks the message for consistency. All errors wrap
// ErrValidation.
func (m Message) Validate() error {
	return validate(
		rule{!m.ID.IsZero(), "message id must be set"},
		rule{!m.AuthorAccountID.IsZero(), "message author must be set"},
		rule{!blank(m.Title), "message title must not be empty"},
		rule{!blank(m.Body), "message body must not be empty"},
		rule{m.Audience.Valid(), "invalid message audience"},
		rule{m.InApp || m.Email, "message needs at least one channel"},
		rule{!m.PublishedAt.IsZero(), "message publish time must be set"},
		rule{m.ExpiresAt.IsZero() || m.ExpiresAt.After(m.PublishedAt), "message must expire after it is published"},
	)
}

// MessageRead records that an account has read a message.
type MessageRead struct {
	MessageID MessageID
	AccountID AccountID
	ReadAt    time.Time
}

// Validate checks the read receipt for consistency. All errors wrap
// ErrValidation.
func (r MessageRead) Validate() error {
	return validate(
		rule{!r.MessageID.IsZero(), "read message must be set"},
		rule{!r.AccountID.IsZero(), "read account must be set"},
		rule{!r.ReadAt.IsZero(), "read time must be set"},
	)
}
