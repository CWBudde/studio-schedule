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

// WaitlistEntry is a person waiting for a place in a full lesson (ADR
// 0010). Waiting entries are invited in JoinedAt order (then ID), one per
// free place, with an invite that reserves the place until it expires.
//
// An entry is open while it is waiting or invited. It ends accepted (the
// person got the place), expired (the invite ran out or was declined, or the
// lesson started) or withdrawn (the person left the list, or got booked
// otherwise). An entry with StayOnList goes back to the end of the list
// (waiting, a new JoinedAt) when its invite expires or is declined instead
// of ending.
type WaitlistEntry struct {
	ID       WaitlistEntryID
	LessonID LessonID
	PersonID PersonID
	Status   WaitlistStatus
	JoinedAt time.Time
	// StayOnList keeps the entry on the list after an invite it let expire
	// or declined.
	StayOnList bool
	// JoinedBy is the account that put the person on the list (the admin
	// when impersonating), for the record; zero if unknown.
	JoinedBy AccountID
	// The invite: the SHA-256 of a random token sent to the person, and its
	// validity window. All three are zero for an entry that has never been
	// invited. An entry that ended keeps the invite it had, and one that went
	// back to waiting keeps the hash alone (no window) until a new invite
	// replaces it, so a later use of an old link can tell what happened.
	InviteTokenHash [32]byte
	InvitedAt       time.Time
	ExpiresAt       time.Time
}

// Open reports whether the entry is still on the list: waiting or invited.
func (w WaitlistEntry) Open() bool { return w.Status == WaitlistWaiting || w.Status == WaitlistInvited }

// Validate checks the entry for consistency. All errors wrap ErrValidation.
func (w WaitlistEntry) Validate() error {
	hasToken := w.InviteTokenHash != [32]byte{}
	hasWindow := !w.InvitedAt.IsZero() || !w.ExpiresAt.IsZero()
	completeInvite := hasToken && !w.InvitedAt.IsZero() && w.ExpiresAt.After(w.InvitedAt)
	needsInvite := w.Status == WaitlistInvited || w.Status == WaitlistAccepted
	waiting := w.Status == WaitlistWaiting

	return validate(
		rule{!w.ID.IsZero(), "waitlist entry id must be set"},
		rule{!w.LessonID.IsZero(), "waitlist entry lesson must be set"},
		rule{!w.PersonID.IsZero(), "waitlist entry person must be set"},
		rule{w.Status.Valid(), "invalid waitlist status"},
		rule{!w.JoinedAt.IsZero(), "waitlist join time must be set"},
		rule{!waiting || !hasWindow, "a waiting entry has no invite"},
		rule{!needsInvite || completeInvite, "entry status requires an invite"},
		rule{waiting || (!hasToken && !hasWindow) || completeInvite, "invite needs a token, an invite time and a later expiry"},
		rule{notBefore(w.InvitedAt, w.JoinedAt), "entry cannot be invited before it joined"},
	)
}
