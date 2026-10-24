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
)

// LessonKinds returns all lesson kinds.
func LessonKinds() []LessonKind { return []LessonKind{LessonRegular, LessonWorkshop, LessonPrivate} }

// Valid reports whether k is a known lesson kind.
func (k LessonKind) Valid() bool { return slices.Contains(LessonKinds(), k) }

// Lesson is a concrete, dated occurrence, generated from a class or created
// as a one-off.
type Lesson struct {
	ID      LessonID
	ClassID ClassID // optional: zero for one-off lessons
	// SlotDate is the school date of the class slot the lesson was
	// generated for, set exactly when ClassID is. It stays when the lesson
	// moves, so generation does not create the slot again (ADR 0003).
	SlotDate      Date
	CourseTypeID  CourseTypeID
	RoomID        RoomID
	InstructorID  StaffID
	AccompanistID StaffID // optional
	StartsAt      time.Time
	EndsAt        time.Time
	Status        LessonStatus
	Visibility    LessonVisibility
	Kind          LessonKind
	Capacity      int
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
}

// MaxCancelNoteLength is the longest cancellation note, in characters.
const MaxCancelNoteLength = 500

// Validate checks the lesson for consistency. All errors wrap ErrValidation.
func (l Lesson) Validate() error {
	return validate(
		rule{!l.ID.IsZero(), "lesson id must be set"},
		rule{l.ClassID.IsZero() == l.SlotDate.IsZero(), "a lesson has a slot date exactly when it has a class"},
		rule{optionalDate(l.SlotDate), "invalid lesson slot date"},
		on("courseTypeId", !l.CourseTypeID.IsZero(), "lesson course type must be set"),
		on("roomId", !l.RoomID.IsZero(), "lesson room must be set"),
		on("instructorId", !l.InstructorID.IsZero(), "lesson instructor must be set"),
		on("accompanistId", l.AccompanistID.IsZero() || l.AccompanistID != l.InstructorID,
			"accompanist must differ from the instructor"),
		rule{!l.StartsAt.IsZero(), "lesson start must be set"},
		rule{l.EndsAt.After(l.StartsAt), "lesson must end after it starts"},
		rule{l.Status.Valid(), "invalid lesson status"},
		on("visibility", l.Visibility.Valid(), "invalid lesson visibility"),
		on("kind", l.Kind.Valid(), "invalid lesson kind"),
		on("capacity", l.Capacity > 0, "lesson capacity must be positive"),
		rule{!l.Detached || !l.ClassID.IsZero(), "only a lesson of a class can be detached"},
		rule{
			l.Status == LessonCanceled || (l.CancelNote == "" && l.CanceledAt.IsZero() && l.CanceledBy.IsZero()),
			"only a canceled lesson has cancellation details",
		},
		on("note", l.CancelNote == "" || !blank(l.CancelNote), "the note must not be blank"),
		on("note", utf8.RuneCountInString(l.CancelNote) <= MaxCancelNoteLength, "the note is too long"),
	)
}
