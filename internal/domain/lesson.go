package domain

import (
	"slices"
	"time"
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
	ID            LessonID
	ClassID       ClassID // optional: zero for one-off lessons
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
}

// Validate checks the lesson for consistency. All errors wrap ErrValidation.
func (l Lesson) Validate() error {
	return validate(
		rule{!l.ID.IsZero(), "lesson id must be set"},
		rule{!l.CourseTypeID.IsZero(), "lesson course type must be set"},
		rule{!l.RoomID.IsZero(), "lesson room must be set"},
		rule{!l.InstructorID.IsZero(), "lesson instructor must be set"},
		rule{l.AccompanistID.IsZero() || l.AccompanistID != l.InstructorID, "accompanist must differ from the instructor"},
		rule{!l.StartsAt.IsZero(), "lesson start must be set"},
		rule{l.EndsAt.After(l.StartsAt), "lesson must end after it starts"},
		rule{l.Status.Valid(), "invalid lesson status"},
		rule{l.Visibility.Valid(), "invalid lesson visibility"},
		rule{l.Kind.Valid(), "invalid lesson kind"},
		rule{l.Capacity > 0, "lesson capacity must be positive"},
	)
}
