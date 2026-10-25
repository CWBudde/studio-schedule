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
	// AuditPersonAnonymized: the office anonymized the target person (ADR
	// 0016). The details record nothing: the person's data is gone.
	AuditPersonAnonymized AuditAction = "person-anonymized"
	// AuditAccountDisabled and AuditAccountEnabled: an admin disabled or
	// enabled the target account (ADR 0016).
	AuditAccountDisabled AuditAction = "account-disabled"
	AuditAccountEnabled  AuditAction = "account-enabled"
	// AuditAccountRoleChanged: an admin changed the role of the target
	// account; the details name the previous and the new role.
	AuditAccountRoleChanged AuditAction = "account-role-changed"
	// AuditAccountDeleted: an account was deleted (ADR 0016). Its target is
	// the account's person, which stays. Without an actor the account's
	// holder deleted it themself.
	AuditAccountDeleted AuditAction = "account-deleted"
)

// AuditActions returns all audit actions.
func AuditActions() []AuditAction {
	return []AuditAction{
		AuditImpersonationStarted, AuditImpersonationEnded, AuditMailRequeued, AuditPersonMerged,
		AuditPersonAnonymized, AuditAccountDisabled, AuditAccountEnabled, AuditAccountRoleChanged, AuditAccountDeleted,
	}
}

// Valid reports whether a is a known audit action.
func (a AuditAction) Valid() bool { return slices.Contains(AuditActions(), a) }

// targetsAccount reports whether an event of a acts upon an account: an
// impersonation or an account change. Such an event is created with the
// account as its target; once the account is deleted it targets the
// account's person instead (AuditEvent.Retarget).
func (a AuditAction) targetsAccount() bool {
	switch a {
	case AuditImpersonationStarted, AuditImpersonationEnded, AuditAccountDisabled, AuditAccountEnabled,
		AuditAccountRoleChanged:
		return true
	default:
		return false
	}
}

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
	// PreviousRole and Role are the roles of AuditAccountRoleChanged before
	// and after.
	PreviousRole Role `json:"previousRole,omitempty"`
	Role         Role `json:"role,omitempty"`
}

// IsZero reports whether the details record nothing.
func (d AuditDetails) IsZero() bool { return d == AuditDetails{} }

// AuditEvent records a security-relevant action of an account (the actor)
// on an account or a person (the target), or on neither. Events are only
// ever added; deleting an account turns the events that target it into
// events that target its person (Retarget, ADR 0016), and the retention job
// deletes events after three years. So an impersonation or an account
// change targets its account while the account exists, and its person
// after.
type AuditEvent struct {
	ID AuditEventID
	At time.Time
	// ActorID is the account that acted; zero only for AuditAccountDeleted
	// of an account its holder deleted.
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
		rule{!e.ActorID.IsZero() || e.Action == AuditAccountDeleted, "audit event actor must be set"},
		rule{e.Action.Valid(), "invalid audit action"},
		rule{e.TargetKind == "" || e.TargetKind.Valid(), "invalid audit target kind"},
		rule{(e.TargetKind == AuditTargetAccount) == !e.TargetID.IsZero(), "an account target needs the account"},
		rule{(e.TargetKind == AuditTargetPerson) == !e.TargetPersonID.IsZero(), "a person target needs the person"},
		rule{
			!e.Action.targetsAccount() || e.TargetKind == AuditTargetAccount || e.TargetKind == AuditTargetPerson,
			"an impersonation or an account change targets the account, or its person once the account is deleted",
		},
		rule{
			e.Action != AuditMailRequeued || (e.TargetKind == "" && !e.Details.MailID.IsZero()),
			"a requeued mail has no target and names the mail",
		},
		rule{
			e.Action != AuditPersonMerged || (e.TargetKind == AuditTargetPerson && !e.Details.MergedPersonID.IsZero() &&
				e.Details.MergedPersonID != e.TargetPersonID),
			"a merge targets the surviving person and names the merged one",
		},
		rule{
			e.Action != AuditPersonAnonymized && e.Action != AuditAccountDeleted || e.TargetKind == AuditTargetPerson,
			"an anonymization or an account deletion targets the person",
		},
		rule{
			(e.Action == AuditAccountRoleChanged) == (e.Details.Role != "" || e.Details.PreviousRole != "") &&
				(e.Action != AuditAccountRoleChanged || e.Details.Role.Valid() && e.Details.PreviousRole.Valid()),
			"a role change names both roles, and only a role change names roles",
		},
	)
}

// Retarget returns e, which targets an account that is being deleted, with
// that account's person as its target instead (ADR 0016).
func (e AuditEvent) Retarget(person PersonID) AuditEvent {
	e.TargetKind, e.TargetID, e.TargetPersonID = AuditTargetPerson, AccountID{}, person

	return e
}
