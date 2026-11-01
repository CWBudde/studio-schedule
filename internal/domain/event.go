package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Events are workshops, intensives and other special dates sold as one thing
// at their own price (ADR 0019). An event groups its sessions, lessons of
// kind LessonWorkshop with Lesson.EventID set, and people register for the
// whole event (EventRegistration). Its image is stored apart from it, like
// the school's background (store.EventRepo.Image).

// EventStatus tells who sees an event.
type EventStatus string

// Known event statuses.
const (
	// EventDraft events are seen by the office only.
	EventDraft EventStatus = "draft"
	// EventPublished events are public, with their sessions.
	EventPublished EventStatus = "published"
	// EventCanceled events were canceled by the school.
	EventCanceled EventStatus = "canceled"
)

// EventStatuses returns all event statuses.
func EventStatuses() []EventStatus { return []EventStatus{EventDraft, EventPublished, EventCanceled} }

// Valid reports whether s is a known event status.
func (s EventStatus) Valid() bool { return slices.Contains(EventStatuses(), s) }

// Bounds of events.
const (
	// MaxEventSlugLength is the longest slug, in characters.
	MaxEventSlugLength = 80
	// MaxEventTitleLength is the longest title, in characters.
	MaxEventTitleLength = 200
	// MaxEventDescriptionLength is the longest description, in characters.
	MaxEventDescriptionLength = 10_000
	// MaxEventCapacity is the most places of an event.
	MaxEventCapacity = 10_000
	// MaxEventPrice is the highest price of an event, in minor units, the
	// bound of a pass's price.
	MaxEventPrice = MaxPassPrice
	// MaxGuestNameLength is the longest name of a guest registered for an
	// event, in characters.
	MaxGuestNameLength = 200
)

// Event is a workshop, an intensive or another special date (ADR 0019).
type Event struct {
	ID EventID
	// Slug names the event in its URL (/events/$slug): unique, lowercase
	// letters and digits in groups joined by single hyphens.
	Slug         string
	Title        string
	Description  string // optional
	CourseTypeID CourseTypeID
	// Capacity is the number of places, at least 1.
	Capacity int
	// Price is the gross price in minor units, 0 for a free event; VATRate
	// its VAT rate in basis points (1900 = 19 %), as for pass types.
	Price   Money
	VATRate int
	Status  EventStatus
	// SignupOpensAt and SignupClosesAt bound the sign-up window, both
	// optional: from publishing on, and until the first session starts.
	SignupOpensAt  time.Time
	SignupClosesAt time.Time
	// CancelUntil is the deadline for a member's own cancellation, optional:
	// the sign-up close.
	CancelUntil time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// CreatedBy is the office account that created the event; zero if
	// unknown or deleted.
	CreatedBy AccountID
}

// ValidEventSlug reports whether s is a slug an event can have: 1 to
// MaxEventSlugLength characters, groups of lowercase ASCII letters and
// digits joined by single hyphens ("ballet-intensive-2026").
func ValidEventSlug(s string) bool {
	if s == "" || len(s) > MaxEventSlugLength {
		return false
	}

	for group := range strings.SplitSeq(s, "-") {
		if group == "" {
			return false
		}

		for _, c := range group {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				return false
			}
		}
	}

	return true
}

// Validate checks the event for consistency. All errors wrap ErrValidation.
func (e Event) Validate() error {
	return validate(
		rule{!e.ID.IsZero(), "event id must be set"},
		on("slug", ValidEventSlug(e.Slug),
			"the slug must be lowercase letters and digits joined by single hyphens, at most 80 characters"),
		on("title", !blank(e.Title), "the title must not be empty"),
		on("title", e.Title == strings.TrimSpace(e.Title), "the title must not start or end with spaces"),
		on("title", utf8.RuneCountInString(e.Title) <= MaxEventTitleLength, "the title is too long"),
		on("description", e.Description == strings.TrimSpace(e.Description),
			"the description must not start or end with spaces"),
		on("description", utf8.RuneCountInString(e.Description) <= MaxEventDescriptionLength,
			"the description is too long"),
		on("courseTypeId", !e.CourseTypeID.IsZero(), "the course type must be set"),
		on("capacity", e.Capacity >= 1 && e.Capacity <= MaxEventCapacity, "capacity must be between 1 and 10000"),
		on("price", e.Price >= 0 && e.Price <= MaxEventPrice, "price out of range"),
		on("vatRate", e.VATRate >= 0 && e.VATRate <= MaxVATRate, "VAT rate must be between 0 and 10000 basis points"),
		on("status", e.Status.Valid(), "invalid event status"),
		on("signupClosesAt", e.SignupOpensAt.IsZero() || e.SignupClosesAt.IsZero() ||
			e.SignupClosesAt.After(e.SignupOpensAt), "the sign-up must close after it opens"),
		rule{!e.CreatedAt.IsZero(), "event creation time must be set"},
		rule{!e.UpdatedAt.IsZero(), "event update time must be set"},
		rule{notBefore(e.UpdatedAt, e.CreatedAt), "an event cannot be updated before it was created"},
	)
}

// EventRegistrationStatus is the state of a registration for an event.
type EventRegistrationStatus string

// Known registration statuses. The waiting list of an event is its
// registrations in EventWaiting and EventInvited (ADR 0019, ADR 0010).
const (
	// EventWaiting registrations wait for a place.
	EventWaiting EventRegistrationStatus = "waiting"
	// EventInvited registrations hold a freed place until their invite
	// expires.
	EventInvited EventRegistrationStatus = "invited"
	// EventRegistered registrations have a place.
	EventRegistered EventRegistrationStatus = "registered"
	// EventRegistrationCanceled registrations ended: canceled by the
	// member, the office or with the event.
	EventRegistrationCanceled EventRegistrationStatus = "canceled"
)

// EventRegistrationStatuses returns all registration statuses.
func EventRegistrationStatuses() []EventRegistrationStatus {
	return []EventRegistrationStatus{EventWaiting, EventInvited, EventRegistered, EventRegistrationCanceled}
}

// Valid reports whether s is a known registration status.
func (s EventRegistrationStatus) Valid() bool { return slices.Contains(EventRegistrationStatuses(), s) }

// EventPayment is whether a registration's fee was paid.
type EventPayment string

// Known payment states.
const (
	// EventUnpaid registrations owe their fee.
	EventUnpaid EventPayment = "unpaid"
	// EventPaid registrations paid at the desk, with a receipt; a free one
	// is paid from the start, without one.
	EventPaid EventPayment = "paid"
	// EventRefunded registrations got their fee back by a Storno of the
	// receipt.
	EventRefunded EventPayment = "refunded"
)

// EventPayments returns all payment states.
func EventPayments() []EventPayment { return []EventPayment{EventUnpaid, EventPaid, EventRefunded} }

// Valid reports whether p is a known payment state.
func (p EventPayment) Valid() bool { return slices.Contains(EventPayments(), p) }

// EventRegistration is a person's or a guest's registration for a whole
// event (ADR 0019). A person has at most one open (waiting, invited or
// registered) registration per event; the store enforces that.
type EventRegistration struct {
	ID      EventRegistrationID
	EventID EventID
	// PersonID is the registered person; zero for a guest.
	PersonID PersonID
	// GuestName and GuestEmail name a guest the office registered: both
	// required for a guest, both empty for a person. Once the guest's data
	// is anonymized (ADR 0016) the name is AnonymousGuestName and the email
	// is empty.
	GuestName  string
	GuestEmail string
	Status     EventRegistrationStatus
	Payment    EventPayment
	// Price and VATRate are the event's gross price and VAT rate when the
	// registration was created; a later change of the event's price does
	// not touch them.
	Price   Money
	VATRate int
	// ReceiptID is the receipt of the payment, CancellationReceiptID its
	// Storno; both optional. A paid registration has a receipt unless it is
	// free, a refunded one has both.
	ReceiptID             ReceiptID
	CancellationReceiptID ReceiptID
	// The invite of the waiting list: the SHA-256 of a random token sent to
	// the person and its validity window, set exactly while the
	// registration is EventInvited.
	InviteTokenHash [32]byte
	InvitedAt       time.Time
	ExpiresAt       time.Time
	// RegisteredBy is the account that registered (the member, or the
	// office at the desk); zero if unknown or deleted.
	RegisteredBy AccountID
	// CreatedAt is when the registration was made, its place in the waiting
	// list.
	CreatedAt time.Time
	UpdatedAt time.Time
	// CanceledAt is set exactly when Status is EventRegistrationCanceled.
	CanceledAt time.Time
}

// Open reports whether the registration holds or waits for a place:
// waiting, invited or registered.
func (r EventRegistration) Open() bool {
	return r.Status == EventWaiting || r.Status == EventInvited || r.Status == EventRegistered
}

// Validate checks the registration for consistency. All errors wrap
// ErrValidation.
func (r EventRegistration) Validate() error {
	hasPerson := !r.PersonID.IsZero()
	hasGuest := r.GuestName != "" || r.GuestEmail != ""
	anonymizedGuest := r.GuestName == AnonymousGuestName && r.GuestEmail == ""
	invited := r.Status == EventInvited
	hasInvite := r.InviteTokenHash != [32]byte{} || !r.InvitedAt.IsZero() || !r.ExpiresAt.IsZero()
	completeInvite := r.InviteTokenHash != [32]byte{} && !r.InvitedAt.IsZero() && r.ExpiresAt.After(r.InvitedAt)
	hasReceipt := !r.ReceiptID.IsZero()
	hasStorno := !r.CancellationReceiptID.IsZero()

	return validate(
		rule{!r.ID.IsZero(), "registration id must be set"},
		rule{!r.EventID.IsZero(), "registration event must be set"},
		rule{hasPerson != hasGuest, "registration needs either a person or guest details"},
		on("guest.name", !hasGuest || !blank(r.GuestName), "guest name must not be empty"),
		on("guest.name", r.GuestName == strings.TrimSpace(r.GuestName), "guest name must not start or end with spaces"),
		on("guest.name", utf8.RuneCountInString(r.GuestName) <= MaxGuestNameLength, "guest name is too long"),
		on("guest.email", !hasGuest || validEmail(r.GuestEmail) || anonymizedGuest, "a guest needs a valid email"),
		rule{r.Status.Valid(), "invalid registration status"},
		rule{r.Payment.Valid(), "invalid payment state"},
		rule{r.Price >= 0 && r.Price <= MaxEventPrice, "registration price out of range"},
		rule{r.VATRate >= 0 && r.VATRate <= MaxVATRate, "registration VAT rate out of range"},
		rule{r.Price > 0 || r.Payment == EventPaid, "a free registration is paid"},
		rule{r.Price > 0 || !hasReceipt, "a free registration has no receipt"},
		rule{r.Payment != EventPaid || r.Price == 0 || hasReceipt, "a paid registration needs a receipt"},
		rule{r.Payment != EventUnpaid || !hasReceipt, "an unpaid registration has no receipt"},
		rule{r.Payment != EventRefunded || (hasReceipt && hasStorno), "a refunded registration needs its receipt and Storno"},
		rule{r.Payment == EventRefunded || !hasStorno, "only a refunded registration has a Storno"},
		rule{!hasStorno || r.CancellationReceiptID != r.ReceiptID, "the Storno is another receipt"},
		rule{!invited || completeInvite, "an invited registration needs a token, an invite time and a later expiry"},
		rule{invited || !hasInvite, "only an invited registration has an invite"},
		rule{!r.CreatedAt.IsZero(), "registration creation time must be set"},
		rule{!r.UpdatedAt.IsZero(), "registration update time must be set"},
		rule{notBefore(r.UpdatedAt, r.CreatedAt), "a registration cannot be updated before it was created"},
		rule{notBefore(r.InvitedAt, r.CreatedAt), "a registration cannot be invited before it was created"},
		rule{
			(r.Status == EventRegistrationCanceled) == !r.CanceledAt.IsZero(),
			"cancellation time must be set exactly for canceled registrations",
		},
		rule{notBefore(r.CanceledAt, r.CreatedAt), "a registration cannot be canceled before it was created"},
	)
}
