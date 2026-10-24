package domain

import (
	"slices"
	"time"
)

// BookingStatus is the state of a booking.
type BookingStatus string

// Known booking statuses.
const (
	BookingBooked   BookingStatus = "booked"
	BookingCanceled BookingStatus = "canceled"
	BookingAttended BookingStatus = "attended"
	BookingNoShow   BookingStatus = "no_show"
)

// BookingStatuses returns all booking statuses.
func BookingStatuses() []BookingStatus {
	return []BookingStatus{BookingBooked, BookingCanceled, BookingAttended, BookingNoShow}
}

// Valid reports whether s is a known booking status.
func (s BookingStatus) Valid() bool { return slices.Contains(BookingStatuses(), s) }

// BookingSource tells how a booking came about (ADR 0009). Bookings that
// staff make for a participant get their own source with the staff actions
// (PLAN.md, Phase 4).
type BookingSource string

// Known booking sources.
const (
	// BookingSourceSelf is a booking a member made for themself or a
	// dependent.
	BookingSourceSelf BookingSource = "self"
	// BookingSourceMember is an automatic booking of a class member into a
	// lesson of their class, made by the auto-enrolment.
	BookingSourceMember BookingSource = "member"
)

// BookingSources returns all booking sources.
func BookingSources() []BookingSource {
	return []BookingSource{BookingSourceSelf, BookingSourceMember}
}

// Valid reports whether s is a known booking source.
func (s BookingSource) Valid() bool { return slices.Contains(BookingSources(), s) }

// Booking is a place in a lesson, either for a person or for a guest without
// an account (name and email only).
type Booking struct {
	ID         BookingID
	LessonID   LessonID
	PersonID   PersonID // zero for guest bookings
	GuestName  string   // guest bookings only
	GuestEmail string   // guest bookings only
	Status     BookingStatus
	Source     BookingSource
	Trial      bool // a trial lesson
	Prepaid    bool // paid in advance (pass or receipt)
	BookedAt   time.Time
	// CanceledAt is set exactly when Status is BookingCanceled.
	CanceledAt time.Time
	// Late marks a cancellation after the cancellation cutoff (ADR 0009);
	// only a canceled booking can be late.
	Late bool
}

// Validate checks the booking for consistency. All errors wrap
// ErrValidation.
func (b Booking) Validate() error {
	hasPerson := !b.PersonID.IsZero()
	hasGuest := b.GuestName != "" || b.GuestEmail != ""

	return validate(
		rule{!b.ID.IsZero(), "booking id must be set"},
		rule{!b.LessonID.IsZero(), "booking lesson must be set"},
		rule{hasPerson != hasGuest, "booking needs either a person or guest details"},
		rule{!hasGuest || !blank(b.GuestName), "guest name must not be empty"},
		rule{!hasGuest || validEmail(b.GuestEmail), "invalid guest email"},
		rule{b.Status.Valid(), "invalid booking status"},
		rule{b.Source.Valid(), "invalid booking source"},
		rule{!b.Late || b.Status == BookingCanceled, "only a canceled booking can be late"},
		rule{!b.BookedAt.IsZero(), "booking time must be set"},
		rule{(b.Status == BookingCanceled) == !b.CanceledAt.IsZero(), "cancellation time must be set exactly for canceled bookings"},
		rule{notBefore(b.CanceledAt, b.BookedAt), "booking cannot be canceled before it was made"},
	)
}

// BookingEvent is an entry in the audit trail of a booking: the status it
// changed to, when, and by whom.
type BookingEvent struct {
	ID             BookingEventID
	BookingID      BookingID
	At             time.Time
	Status         BookingStatus
	ActorAccountID AccountID // optional: zero for system actions
	Note           string    // optional
	// Late marks a cancellation after the cancellation cutoff; only with
	// status BookingCanceled.
	Late bool
	// Override records a booking beyond the lesson's capacity.
	Override bool
}

// Validate checks the event for consistency. All errors wrap ErrValidation.
func (e BookingEvent) Validate() error {
	return validate(
		rule{!e.ID.IsZero(), "booking event id must be set"},
		rule{!e.BookingID.IsZero(), "booking event booking must be set"},
		rule{!e.At.IsZero(), "booking event time must be set"},
		rule{e.Status.Valid(), "invalid booking event status"},
		rule{!e.Late || e.Status == BookingCanceled, "only a cancellation can be late"},
	)
}
