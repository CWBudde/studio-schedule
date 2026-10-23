package domain

import (
	"slices"
	"time"
)

// AuditAction is what an audit event records.
type AuditAction string

// Known audit actions.
const (
	// AuditImpersonationStarted: an admin signed in as another account.
	AuditImpersonationStarted AuditAction = "impersonation-started"
	// AuditImpersonationEnded: the admin returned to their own account or
	// signed out.
	AuditImpersonationEnded AuditAction = "impersonation-ended"
)

// AuditActions returns all audit actions.
func AuditActions() []AuditAction {
	return []AuditAction{AuditImpersonationStarted, AuditImpersonationEnded}
}

// Valid reports whether a is a known audit action.
func (a AuditAction) Valid() bool { return slices.Contains(AuditActions(), a) }

// AuditEvent records a security-relevant action of an account (the actor)
// on another account (the target). Events are only ever added.
type AuditEvent struct {
	ID       AuditEventID
	At       time.Time
	ActorID  AccountID
	Action   AuditAction
	TargetID AccountID
}

// Validate checks the event for consistency. All errors wrap ErrValidation.
func (e AuditEvent) Validate() error {
	return validate(
		rule{!e.ID.IsZero(), "audit event id must be set"},
		rule{!e.At.IsZero(), "audit event time must be set"},
		rule{!e.ActorID.IsZero(), "audit event actor must be set"},
		rule{e.Action.Valid(), "invalid audit action"},
		rule{!e.TargetID.IsZero(), "audit event target must be set"},
	)
}
