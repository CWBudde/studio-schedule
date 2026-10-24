package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
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

// BookingSource tells how a booking came about (ADR 0009).
type BookingSource string

// Known booking sources.
const (
	// BookingSourceSelf is a booking a member made for themself or a
	// dependent.
	BookingSourceSelf BookingSource = "self"
	// BookingSourceMember is an automatic booking of a class member into a
	// lesson of their class, made by the auto-enrolment.
	BookingSourceMember BookingSource = "member"
	// BookingSourceStaff is a booking the office or the lesson's instructor
	// or accompanist made for a person or a guest; the booking event names
	// who.
	BookingSourceStaff BookingSource = "staff"
)

// BookingSources returns all booking sources.
func BookingSources() []BookingSource {
	return []BookingSource{BookingSourceSelf, BookingSourceMember, BookingSourceStaff}
}

// MaxTrialNoteLength is the longest note on a trial booking, in characters.
const MaxTrialNoteLength = 500

// Valid reports whether s is a known booking source.
func (s BookingSource) Valid() bool { return slices.Contains(BookingSources(), s) }

// Booking is a place in a lesson, either for a person or for a guest without
// a person record (a name and an optional email).
type Booking struct {
	ID         BookingID
	LessonID   LessonID
	PersonID   PersonID // zero for guest bookings
	GuestName  string   // guest bookings only
	GuestEmail string   // guest bookings only, optional
	Status     BookingStatus
	Source     BookingSource
	Trial      bool // a trial lesson
	// TrialNote is an optional note about a trial booking for the
	// instructor; only a trial booking has one.
	TrialNote string
	Prepaid   bool // paid in advance (pass or receipt)
	BookedAt  time.Time
	// CanceledAt is set exactly when Status is BookingCanceled.
	CanceledAt time.Time
	// Late marks a cancellation after the cancellation cutoff (ADR 0009);
	// only a canceled booking can be late.
	Late bool
	// Released marks a cancellation that gives up the date (ADR 0009): by
	// the person or their family, or staff removing the person from the
	// lesson. It keeps a class member from being booked into the lesson
	// automatically again. The office ending a membership or a member's
	// pause does not release. Only a canceled booking can be released.
	Released bool
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
		on("guest.name", !hasGuest || !blank(b.GuestName), "guest name must not be empty"),
		on("guest.name", b.GuestName == strings.TrimSpace(b.GuestName), "guest name must not start or end with spaces"),
		on("guest.email", b.GuestEmail == "" || validEmail(b.GuestEmail), "invalid guest email"),
		rule{b.Status.Valid(), "invalid booking status"},
		rule{b.Source.Valid(), "invalid booking source"},
		rule{!b.Late || b.Status == BookingCanceled, "only a canceled booking can be late"},
		rule{!b.Released || b.Status == BookingCanceled, "only a canceled booking can be released"},
		on("trialNote", b.TrialNote == "" || b.Trial, "only a trial booking has a note"),
		on("trialNote", utf8.RuneCountInString(b.TrialNote) <= MaxTrialNoteLength, "trial note is too long"),
		on("trialNote", b.TrialNote == strings.TrimSpace(b.TrialNote), "trial note must not start or end with spaces"),
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
