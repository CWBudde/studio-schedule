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

// AnyID is satisfied by every ID type, and not by a bare uuid.UUID.
type AnyID interface {
	~[16]byte
	String() string
	IsZero() bool
}

// ParseID parses the canonical text form of an ID
// (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx, hex digits in either case). Other
// spellings uuid.Parse would accept (braces, urn:uuid:, no dashes) and the
// nil UUID are rejected, so every ID has exactly one valid string form.
// Errors wrap ErrValidation.
func ParseID[I AnyID](s string) (I, error) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return I{}, fmt.Errorf("%w: invalid ID %q", ErrValidation, s)
	}

	u, err := uuid.Parse(s)
	if err != nil || u == uuid.Nil() {
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
	bookingEventKind  struct{}
	waitlistEntryKind struct{}
	passTypeKind      struct{}
	passKind          struct{}
	receiptKind       struct{}
	messageKind       struct{}
	calendarFeedKind  struct{}
	importRunKind     struct{}
	sessionKind       struct{}
	outboxMailKind    struct{}
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

	// BookingEventID identifies a booking event.
	BookingEventID = ID[bookingEventKind]

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

	// SessionID identifies a session.
	SessionID = ID[sessionKind]

	// OutboxMailID identifies an outbox mail.
	OutboxMailID = ID[outboxMailKind]
)
