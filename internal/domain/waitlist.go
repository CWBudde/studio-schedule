package domain

import (
	"slices"
	"time"
)

// WaitlistStatus is the state of a waiting-list entry.
type WaitlistStatus string

// Known waiting-list statuses.
const (
	WaitlistWaiting   WaitlistStatus = "waiting"
	WaitlistInvited   WaitlistStatus = "invited"
	WaitlistAccepted  WaitlistStatus = "accepted"
	WaitlistExpired   WaitlistStatus = "expired"
	WaitlistWithdrawn WaitlistStatus = "withdrawn"
)

// WaitlistStatuses returns all waiting-list statuses.
func WaitlistStatuses() []WaitlistStatus {
	return []WaitlistStatus{WaitlistWaiting, WaitlistInvited, WaitlistAccepted, WaitlistExpired, WaitlistWithdrawn}
}

// Valid reports whether s is a known waiting-list status.
func (s WaitlistStatus) Valid() bool { return slices.Contains(WaitlistStatuses(), s) }

// WaitlistEntry is a person waiting for a place in a full lesson. Entries
// are invited one at a time, in JoinedAt order, with a timed invite.
type WaitlistEntry struct {
	ID       WaitlistEntryID
	LessonID LessonID
	PersonID PersonID
	Status   WaitlistStatus
	JoinedAt time.Time
	// The invite: the SHA-256 of a random token sent to the person, and its
	// validity window. All three are zero while the entry is waiting.
	InviteTokenHash [32]byte
	InvitedAt       time.Time
	ExpiresAt       time.Time
}

// Validate checks the entry for consistency. All errors wrap ErrValidation.
func (w WaitlistEntry) Validate() error {
	hasToken := w.InviteTokenHash != [32]byte{}
	hasInvite := hasToken || !w.InvitedAt.IsZero() || !w.ExpiresAt.IsZero()
	completeInvite := hasToken && !w.InvitedAt.IsZero() && w.ExpiresAt.After(w.InvitedAt)
	needsInvite := w.Status == WaitlistInvited || w.Status == WaitlistAccepted || w.Status == WaitlistExpired

	return validate(
		rule{!w.ID.IsZero(), "waitlist entry id must be set"},
		rule{!w.LessonID.IsZero(), "waitlist entry lesson must be set"},
		rule{!w.PersonID.IsZero(), "waitlist entry person must be set"},
		rule{w.Status.Valid(), "invalid waitlist status"},
		rule{!w.JoinedAt.IsZero(), "waitlist join time must be set"},
		rule{w.Status != WaitlistWaiting || !hasInvite, "a waiting entry has no invite"},
		rule{!needsInvite || hasInvite, "entry status requires an invite"},
		rule{!hasInvite || completeInvite, "invite needs a token, an invite time and a later expiry"},
		rule{notBefore(w.InvitedAt, w.JoinedAt), "entry cannot be invited before it joined"},
	)
}
