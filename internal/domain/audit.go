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
	// AuditPasswordResetIssued: an admin issued a password-reset link for
	// the target account on the command line (ADR 0020, `accounts
	// reset-password`). The details say whether the link was mailed or
	// printed (Delivery), never the link.
	AuditPasswordResetIssued AuditAction = "password-reset-issued"
	// AuditSessionsRevoked: an admin ended the sessions of the target
	// account, or without a target those of every account (ADR 0020,
	// `accounts sessions revoke`). The details count the sessions that had
	// not ended yet (Sessions; absent for none).
	AuditSessionsRevoked AuditAction = "sessions-revoked"
	// AuditSetupLinkIssued: an operator printed a new setup link for the
	// target account, which has not confirmed, while no admin could sign in
	// (ADR 0020, `accounts setup-link`). It has no actor: the details name
	// the command line and the OS user (Via), and the delivery (printed).
	AuditSetupLinkIssued AuditAction = "setup-link-issued"
	// AuditEventRegistrationAdded: the office registered the target person,
	// or without a target a guest, for an event (ADR 0019). The details
	// name the event and the registration.
	AuditEventRegistrationAdded AuditAction = "event-registration-added"
	// AuditEventRegistrationCanceled: the office canceled the registration
	// of the target person, or without a target of a guest; the details
	// name the event, the registration and whether the fee was refunded
	// (Refund).
	AuditEventRegistrationCanceled AuditAction = "event-registration-canceled"
	// AuditEventRegistrationPaid: the office marked the registration of the
	// target person, or without a target of a guest, paid, which issued its
	// receipt; the details name the event and the registration.
	AuditEventRegistrationPaid AuditAction = "event-registration-paid"
	// AuditEventCanceled: the office canceled an event with its
	// registrations and sessions. It has no target; the details name the
	// event.
	AuditEventCanceled AuditAction = "event-canceled"
)

// AuditActions returns all audit actions.
func AuditActions() []AuditAction {
	return []AuditAction{
		AuditImpersonationStarted, AuditImpersonationEnded, AuditMailRequeued, AuditPersonMerged,
		AuditPersonAnonymized, AuditAccountDisabled, AuditAccountEnabled, AuditAccountRoleChanged, AuditAccountDeleted,
		AuditPasswordResetIssued, AuditSessionsRevoked, AuditSetupLinkIssued, AuditEventRegistrationAdded,
		AuditEventRegistrationCanceled, AuditEventRegistrationPaid, AuditEventCanceled,
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
		AuditAccountRoleChanged, AuditPasswordResetIssued, AuditSetupLinkIssued:
		return true
	default:
		return false
	}
}

// namesEvent reports whether an event of a names an event in its details
// (AuditDetails.EventID): the event actions of ADR 0019.
func (a AuditAction) namesEvent() bool {
	return a == AuditEventCanceled || a.namesRegistration()
}

// namesRegistration reports whether an event of a names a registration for
// an event in its details (AuditDetails.RegistrationID). Its target is the
// registered person, or nothing for a guest.
func (a AuditAction) namesRegistration() bool {
	switch a {
	case AuditEventRegistrationAdded, AuditEventRegistrationCanceled, AuditEventRegistrationPaid:
		return true
	default:
		return false
	}
}

// ActorOptional reports whether an event of a may have no actor: an
// account its holder deleted (AuditAccountDeleted) and a setup link an
// operator issued (AuditSetupLinkIssued). PostgreSQL's CHECK
// audit_events_actor mirrors it (migration 00029).
func (a AuditAction) ActorOptional() bool {
	return a == AuditAccountDeleted || a == AuditSetupLinkIssued
}

// LinkDelivery is how the link of an audit event reached its holder.
type LinkDelivery string

// Known deliveries.
const (
	// LinkMailed: the link was mailed to the account's address.
	LinkMailed LinkDelivery = "mailed"
	// LinkPrinted: the link was printed on the command line.
	LinkPrinted LinkDelivery = "printed"
)

// LinkDeliveries returns all deliveries.
func LinkDeliveries() []LinkDelivery { return []LinkDelivery{LinkMailed, LinkPrinted} }

// Valid reports whether d is a known delivery.
func (d LinkDelivery) Valid() bool { return slices.Contains(LinkDeliveries(), d) }

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

// AuditChannel is how an actor reached the app, when not through the API.
type AuditChannel string

// ChannelCLI is the command line (ADR 0020): a command run as an account
// with --as.
const ChannelCLI AuditChannel = "cli"

// AuditChannels returns all channels.
func AuditChannels() []AuditChannel { return []AuditChannel{ChannelCLI} }

// Valid reports whether c is a known channel.
func (c AuditChannel) Valid() bool { return slices.Contains(AuditChannels(), c) }

// AuditVia is the channel an actor acted through and the OS user who ran
// it (ADR 0020). The zero value is the API: a request with a session.
type AuditVia struct {
	Channel AuditChannel `json:"channel,omitempty"`
	OSUser  string       `json:"osUser,omitempty"`
}

// IsZero reports whether v is the API.
func (v AuditVia) IsZero() bool { return v == AuditVia{} }

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
	// Delivery is how the link of AuditPasswordResetIssued or
	// AuditSetupLinkIssued reached its holder; the link itself is never
	// recorded.
	Delivery LinkDelivery `json:"delivery,omitempty"`
	// Sessions is how many running sessions AuditSessionsRevoked ended.
	Sessions int `json:"sessions,omitempty"`
	// EventID names the event of the event actions (ADR 0019),
	// RegistrationID the registration of the registration actions.
	EventID        EventID             `json:"eventId,omitzero"`
	RegistrationID EventRegistrationID `json:"registrationId,omitzero"`
	// Refund tells that AuditEventRegistrationCanceled refunded the fee by
	// a Storno of its receipt.
	Refund bool `json:"refund,omitempty"`
	// Via is the channel the actor acted through, for any action; zero for
	// the API.
	Via AuditVia `json:"via,omitzero"`
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
	// of an account its holder deleted and for AuditSetupLinkIssued, which
	// an operator issues (ActorOptional).
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
		rule{!e.ActorID.IsZero() || e.Action.ActorOptional(), "audit event actor must be set"},
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
		rule{
			e.Details.Via.IsZero() || e.Details.Via.Channel.Valid(),
			"an audit event's channel must be known",
		},
		rule{
			(e.Action == AuditPasswordResetIssued || e.Action == AuditSetupLinkIssued) == e.Details.Delivery.Valid() &&
				(e.Details.Delivery == "" || e.Details.Delivery.Valid()),
			"an issued link says how it was delivered, and only an issued link does",
		},
		rule{
			e.Action != AuditSetupLinkIssued || e.ActorID.IsZero() && e.Details.Via.Channel == ChannelCLI &&
				e.Details.Delivery == LinkPrinted,
			"a setup link is printed by an operator on the command line, without an actor",
		},
		rule{
			e.Details.Sessions >= 0 && (e.Details.Sessions == 0 || e.Action == AuditSessionsRevoked),
			"only revoked sessions are counted, and not below zero",
		},
		rule{
			e.Action.namesEvent() == !e.Details.EventID.IsZero(),
			"an event action names the event, and only an event action does",
		},
		rule{
			e.Action.namesRegistration() == !e.Details.RegistrationID.IsZero(),
			"a registration action names the registration, and only a registration action does",
		},
		rule{
			!e.Action.namesRegistration() || e.TargetKind == "" || e.TargetKind == AuditTargetPerson,
			"a registration action targets the registered person, or nothing for a guest",
		},
		rule{e.Action != AuditEventCanceled || e.TargetKind == "", "a canceled event has no target"},
		rule{
			!e.Details.Refund || e.Action == AuditEventRegistrationCanceled,
			"only a canceled registration records a refund",
		},
	)
}

// Retarget returns e, which targets an account that is being deleted, with
// that account's person as its target instead (ADR 0016).
func (e AuditEvent) Retarget(person PersonID) AuditEvent {
	e.TargetKind, e.TargetID, e.TargetPersonID = AuditTargetPerson, AccountID{}, person

	return e
}
