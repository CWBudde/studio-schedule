package domain

import (
	"slices"
	"time"
	"unicode/utf8"
)

// LessonStatus tells whether a lesson takes place.
type LessonStatus string

// Known lesson statuses.
const (
	LessonScheduled LessonStatus = "scheduled"
	LessonCanceled  LessonStatus = "canceled"
)

// LessonStatuses returns all lesson statuses.
func LessonStatuses() []LessonStatus { return []LessonStatus{LessonScheduled, LessonCanceled} }

// Valid reports whether s is a known lesson status.
func (s LessonStatus) Valid() bool { return slices.Contains(LessonStatuses(), s) }

// LessonVisibility controls who sees a lesson in the schedule.
type LessonVisibility string

// Known lesson visibilities.
const (
	VisibilityPublic  LessonVisibility = "public"  // everyone, including guests
	VisibilityMembers LessonVisibility = "members" // logged-in accounts only
	VisibilityHidden  LessonVisibility = "hidden"  // staff only
)

// LessonVisibilities returns all lesson visibilities.
func LessonVisibilities() []LessonVisibility {
	return []LessonVisibility{VisibilityPublic, VisibilityMembers, VisibilityHidden}
}

// Valid reports whether v is a known lesson visibility.
func (v LessonVisibility) Valid() bool { return slices.Contains(LessonVisibilities(), v) }

// LessonKind distinguishes regular lessons from special formats.
type LessonKind string

// Known lesson kinds.
const (
	LessonRegular  LessonKind = "regular"
	LessonWorkshop LessonKind = "workshop"
	LessonPrivate  LessonKind = "private"
	// LessonRental blocks a room for an outside renter (ADR 0007,
	// amendment 2026-10-31): a one-off, hidden lesson without instructor,
	// accompanist, capacity or bookings, named by its Renter; its course
	// type is optional.
	LessonRental LessonKind = "rental"
)

// LessonKinds returns all lesson kinds.
func LessonKinds() []LessonKind {
	return []LessonKind{LessonRegular, LessonWorkshop, LessonPrivate, LessonRental}
}

// Valid reports whether k is a known lesson kind.
func (k LessonKind) Valid() bool { return slices.Contains(LessonKinds(), k) }

// Bookable reports whether lessons of kind k take bookings and a waiting
// list: every kind but a room rental.
func (k LessonKind) Bookable() bool { return k != LessonRental }

// Lesson is a concrete, dated occurrence, generated from a class or created
// as a one-off.
type Lesson struct {
	ID      LessonID
	ClassID ClassID // optional: zero for one-off lessons
	// SlotDate is the school date of the class slot the lesson was
	// generated for, set exactly when ClassID is. It stays when the lesson
	// moves, so generation does not create the slot again (ADR 0003).
	SlotDate      Date
	CourseTypeID  CourseTypeID // optional only for a room rental
	RoomID        RoomID
	InstructorID  StaffID
	AccompanistID StaffID // optional
	StartsAt      time.Time
	EndsAt        time.Time
	Status        LessonStatus
	Visibility    LessonVisibility
	Kind          LessonKind
	Capacity      int // 0 for a room rental
	// Renter names who rents the room, for staff only; set exactly on a
	// room rental.
	Renter string
	// Detached marks a lesson of a class that the office edited one by
	// one: it no longer follows edits of its class or term (ADR 0007).
	// Only a lesson of a class can be detached.
	Detached bool
	// CancelNote, CanceledAt and CanceledBy record the office's
	// cancellation (ADR 0007); all optional, and only on a canceled
	// lesson.
	CancelNote string
	CanceledAt time.Time
	CanceledBy AccountID
	// EventID names the event whose session the lesson is (ADR 0019); zero
	// for a lesson of no event. A session is a one-off workshop.
	EventID EventID
}

// MaxCancelNoteLength is the longest cancellation note, in characters.
const MaxCancelNoteLength = 500

// MaxRenterLength is the longest renter of a room rental, in characters.
const MaxRenterLength = 100

// Validate checks the lesson for consistency. All errors wrap ErrValidation.
// A room rental has no instructor, accompanist or capacity, is hidden, is
// one-off and names its renter, and its course type is optional; every other
// kind needs a course type, an instructor and a positive capacity and has no
// renter.
func (l Lesson) Validate() error {
	if err := l.validateKind(); err != nil {
		return err
	}

	return validate(
		rule{!l.ID.IsZero(), "lesson id must be set"},
		rule{l.ClassID.IsZero() == l.SlotDate.IsZero(), "a lesson has a slot date exactly when it has a class"},
		rule{optionalDate(l.SlotDate), "invalid lesson slot date"},
		on("roomId", !l.RoomID.IsZero(), "lesson room must be set"),
		on("accompanistId", l.AccompanistID.IsZero() || l.AccompanistID != l.InstructorID,
			"accompanist must differ from the instructor"),
		rule{!l.StartsAt.IsZero(), "lesson start must be set"},
		rule{l.EndsAt.After(l.StartsAt), "lesson must end after it starts"},
		rule{l.Status.Valid(), "invalid lesson status"},
		on("visibility", l.Visibility.Valid(), "invalid lesson visibility"),
		rule{!l.Detached || !l.ClassID.IsZero(), "only a lesson of a class can be detached"},
		rule{
			l.Status == LessonCanceled || (l.CancelNote == "" && l.CanceledAt.IsZero() && l.CanceledBy.IsZero()),
			"only a canceled lesson has cancellation details",
		},
		on("note", l.CancelNote == "" || !blank(l.CancelNote), "the note must not be blank"),
		on("note", utf8.RuneCountInString(l.CancelNote) <= MaxCancelNoteLength, "the note is too long"),
		on("kind", l.EventID.IsZero() || l.Kind == LessonWorkshop, "an event's session is a workshop"),
		on("eventId", l.EventID.IsZero() || l.ClassID.IsZero(), "an event's session belongs to no class"),
	)
}

// validateKind checks the fields that depend on the kind of l.
func (l Lesson) validateKind() error {
	if l.Kind != LessonRental {
		return validate(
			on("kind", l.Kind.Valid(), "invalid lesson kind"),
			on("courseTypeId", !l.CourseTypeID.IsZero(), "lesson course type must be set"),
			on("instructorId", !l.InstructorID.IsZero(), "lesson instructor must be set"),
			on("capacity", l.Capacity > 0, "lesson capacity must be positive"),
			on("renter", l.Renter == "", "only a room rental has a renter"),
		)
	}

	return validate(
		on("kind", l.ClassID.IsZero(), "a lesson of a class cannot be a room rental"),
		on("instructorId", l.InstructorID.IsZero(), "a room rental has no instructor"),
		on("accompanistId", l.AccompanistID.IsZero(), "a room rental has no accompanist"),
		on("capacity", l.Capacity == 0, "a room rental has no capacity"),
		on("visibility", l.Visibility == VisibilityHidden, "a room rental is visible to staff only"),
		on("renter", !blank(l.Renter), "the renter must be set"),
		on("renter", utf8.RuneCountInString(l.Renter) <= MaxRenterLength, "the renter is too long"),
	)
}
