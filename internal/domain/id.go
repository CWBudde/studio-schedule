package domain

import (
	"fmt"

	"uuid"
)

// ID is a UUID that identifies an entity of kind K. Each kind gets its own
// ID type (PersonID, LessonID, ...), so mixing up IDs of different entities
// is a compile error. IDs are comparable and can be used as map keys; their
// text and JSON form is the canonical UUID string.
//
// New IDs are UUIDv7 and come from an injected ids.Source, never from the
// uuid package directly.
type ID[K any] uuid.UUID

// AnyID is satisfied by every ID type.
type AnyID interface{ ~[16]byte }

// ParseID parses the canonical text form of an ID. Errors wrap
// ErrValidation.
func ParseID[I AnyID](s string) (I, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return I{}, fmt.Errorf("%w: invalid ID %q", ErrValidation, s)
	}

	return I(u), nil
}

// String returns the canonical UUID form, e.g.
// "01890a5d-ac96-774b-bcce-b302099a8057".
func (id ID[K]) String() string { return uuid.UUID(id).String() }

// IsZero reports whether id is the zero (nil) UUID, i.e. unset.
func (id ID[K]) IsZero() bool { return id == ID[K]{} }

// MarshalText implements encoding.TextMarshaler.
func (id ID[K]) MarshalText() ([]byte, error) { return uuid.UUID(id).MarshalText() }

// UnmarshalText implements encoding.TextUnmarshaler. Errors wrap
// ErrValidation.
func (id *ID[K]) UnmarshalText(b []byte) error {
	parsed, err := ParseID[ID[K]](string(b))
	if err != nil {
		return err
	}

	*id = parsed

	return nil
}

// Kind markers: they only distinguish the ID types below.
type (
	personKind        struct{}
	accountKind       struct{}
	locationKind      struct{}
	roomKind          struct{}
	staffKind         struct{}
	courseTypeKind    struct{}
	termKind          struct{}
	classKind         struct{}
	lessonKind        struct{}
	bookingKind       struct{}
	waitlistEntryKind struct{}
	passTypeKind      struct{}
	passKind          struct{}
	receiptKind       struct{}
	messageKind       struct{}
	calendarFeedKind  struct{}
	importRunKind     struct{}
)

// ID types, one per entity of the domain model.
type (
	// PersonID identifies a person.
	PersonID = ID[personKind]

	// AccountID identifies an account.
	AccountID = ID[accountKind]

	// LocationID identifies a location.
	LocationID = ID[locationKind]

	// RoomID identifies a room.
	RoomID = ID[roomKind]

	// StaffID identifies a staff.
	StaffID = ID[staffKind]

	// CourseTypeID identifies a course type.
	CourseTypeID = ID[courseTypeKind]

	// TermID identifies a term.
	TermID = ID[termKind]

	// ClassID identifies a class.
	ClassID = ID[classKind]

	// LessonID identifies a lesson.
	LessonID = ID[lessonKind]

	// BookingID identifies a booking.
	BookingID = ID[bookingKind]

	// WaitlistEntryID identifies a waitlist entry.
	WaitlistEntryID = ID[waitlistEntryKind]

	// PassTypeID identifies a pass type.
	PassTypeID = ID[passTypeKind]

	// PassID identifies a pass.
	PassID = ID[passKind]

	// ReceiptID identifies a receipt.
	ReceiptID = ID[receiptKind]

	// MessageID identifies a message.
	MessageID = ID[messageKind]

	// CalendarFeedID identifies a calendar feed.
	CalendarFeedID = ID[calendarFeedKind]

	// ImportRunID identifies an import run.
	ImportRunID = ID[importRunKind]
)
