package domain

import (
	"slices"
	"time"
)

// AuditAction is what an audit event records.
type AuditAction string

// Known audit actions.
const (
	// AuditImpersonationStarted: an admin signed in as another account
	// (the target).
	AuditImpersonationStarted AuditAction = "impersonation-started"
	// AuditImpersonationEnded: the admin returned to their own account or
	// signed out.
	AuditImpersonationEnded AuditAction = "impersonation-ended"
	// AuditMailRequeued: an admin queued a failed mail of the outbox again
	// (ADR 0014). It has no target; the details name the mail.
	AuditMailRequeued AuditAction = "mail-requeued"
	// AuditPersonMerged: the office merged a duplicate person into the
	// target person (ADR 0015); the details name the merged person, now a
	// tombstone.
	AuditPersonMerged AuditAction = "person-merged"
)

// AuditActions returns all audit actions.
func AuditActions() []AuditAction {
	return []AuditAction{AuditImpersonationStarted, AuditImpersonationEnded, AuditMailRequeued, AuditPersonMerged}
}

// Valid reports whether a is a known audit action.
func (a AuditAction) Valid() bool { return slices.Contains(AuditActions(), a) }

// AuditTargetKind tells what an audit event acted upon.
type AuditTargetKind string

// Known target kinds. The zero kind means the event has no target.
const (
	AuditTargetAccount AuditTargetKind = "account"
	AuditTargetPerson  AuditTargetKind = "person"
)

// AuditTargetKinds returns all target kinds.
func AuditTargetKinds() []AuditTargetKind {
	return []AuditTargetKind{AuditTargetAccount, AuditTargetPerson}
}

// Valid reports whether k is a known target kind.
func (k AuditTargetKind) Valid() bool { return slices.Contains(AuditTargetKinds(), k) }

// AuditDetails are what an event records beyond its target, stored as a
// JSON object. Every field is optional; an action documents the ones it
// sets.
type AuditDetails struct {
	// MailID, Recipient and Template name the mail of AuditMailRequeued.
	MailID    OutboxMailID `json:"mailId,omitzero"`
	Recipient string       `json:"recipient,omitempty"`
	Template  string       `json:"template,omitempty"`
	// MergedPersonID and MergedName name the person of AuditPersonMerged
	// that was merged into the target, with its name at the time.
	MergedPersonID PersonID `json:"mergedPersonId,omitzero"`
	MergedName     string   `json:"mergedName,omitempty"`
}

// IsZero reports whether the details record nothing.
func (d AuditDetails) IsZero() bool { return d == AuditDetails{} }

// AuditEvent records a security-relevant action of an account (the actor)
// on an account or a person (the target), or on neither. Events are only
// ever added.
type AuditEvent struct {
	ID      AuditEventID
	At      time.Time
	ActorID AccountID
	Action  AuditAction
	// TargetKind says which of TargetID and TargetPersonID is set; empty
	// for an event without a target.
	TargetKind AuditTargetKind
	// TargetID is the account acted upon, with TargetKind account.
	TargetID AccountID
	// TargetPersonID is the person acted upon, with TargetKind person.
	TargetPersonID PersonID
	Details        AuditDetails
}

// Validate checks the event for consistency. All errors wrap ErrValidation.
func (e AuditEvent) Validate() error {
	return validate(
		rule{!e.ID.IsZero(), "audit event id must be set"},
		rule{!e.At.IsZero(), "audit event time must be set"},
		rule{!e.ActorID.IsZero(), "audit event actor must be set"},
		rule{e.Action.Valid(), "invalid audit action"},
		rule{e.TargetKind == "" || e.TargetKind.Valid(), "invalid audit target kind"},
		rule{(e.TargetKind == AuditTargetAccount) == !e.TargetID.IsZero(), "an account target needs the account"},
		rule{(e.TargetKind == AuditTargetPerson) == !e.TargetPersonID.IsZero(), "a person target needs the person"},
		rule{e.Action != AuditImpersonationStarted && e.Action != AuditImpersonationEnded ||
			e.TargetKind == AuditTargetAccount, "an impersonation targets an account"},
		rule{
			e.Action != AuditMailRequeued || (e.TargetKind == "" && !e.Details.MailID.IsZero()),
			"a requeued mail has no target and names the mail",
		},
		rule{
			e.Action != AuditPersonMerged || (e.TargetKind == AuditTargetPerson && !e.Details.MergedPersonID.IsZero() &&
				e.Details.MergedPersonID != e.TargetPersonID),
			"a merge targets the surviving person and names the merged one",
		},
	)
}
