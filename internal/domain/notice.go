package domain

import "time"

// LessonNotice records that a lesson's staff sent a message to its
// participants (ADR 0009, amendment): who sent it, when, and to how many
// addresses. The mails themselves are in the outbox; the notices are what
// the rate limits of the notices count.
type LessonNotice struct {
	ID LessonNoticeID
	// LessonID is the lesson the notice was about; zero once the lesson is
	// deleted. The notice stays: it still counts for its sender's limit.
	LessonID LessonID
	// SenderID is the account that sent the notice: the admin, not the
	// account they impersonated.
	SenderID AccountID
	SentAt   time.Time
	// Recipients is how many addresses the notice was mailed to.
	Recipients int
}

// Validate checks the notice for consistency. All errors wrap
// ErrValidation.
func (n LessonNotice) Validate() error {
	return validate(
		rule{!n.ID.IsZero(), "notice id must be set"},
		rule{!n.LessonID.IsZero(), "notice lesson must be set"},
		rule{!n.SenderID.IsZero(), "notice sender must be set"},
		rule{!n.SentAt.IsZero(), "notice time must be set"},
		rule{n.Recipients > 0, "a notice needs a recipient"},
	)
}
