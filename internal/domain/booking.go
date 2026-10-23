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

// Booking is a place in a lesson, either for a person or for a guest without
// an account (name and email only).
type Booking struct {
	ID         BookingID
	LessonID   LessonID
	PersonID   PersonID // zero for guest bookings
	GuestName  string   // guest bookings only
	GuestEmail string   // guest bookings only
	Status     BookingStatus
	Trial      bool // a trial lesson
	Prepaid    bool // paid in advance (pass or receipt)
	BookedAt   time.Time
	// CanceledAt is set exactly when Status is BookingCanceled.
	CanceledAt time.Time
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
}

// Validate checks the event for consistency. All errors wrap ErrValidation.
func (e BookingEvent) Validate() error {
	return validate(
		rule{!e.ID.IsZero(), "booking event id must be set"},
		rule{!e.BookingID.IsZero(), "booking event booking must be set"},
		rule{!e.At.IsZero(), "booking event time must be set"},
		rule{e.Status.Valid(), "invalid booking event status"},
	)
}
