package domain

import "slices"

// Location is a site of the school with one or more rooms.
type Location struct {
	ID      LocationID
	Name    string
	Address string // optional
}

// Validate checks the location for consistency. All errors wrap
// ErrValidation.
func (l Location) Validate() error {
	return validate(
		rule{!l.ID.IsZero(), "location id must be set"},
		rule{!blank(l.Name), "location name must not be empty"},
	)
}

// Room is a studio at a location. Capacity is the default lesson capacity.
type Room struct {
	ID         RoomID
	LocationID LocationID
	Name       string
	Capacity   int
}

// Validate checks the room for consistency. All errors wrap ErrValidation.
func (r Room) Validate() error {
	return validate(
		rule{!r.ID.IsZero(), "room id must be set"},
		rule{!r.LocationID.IsZero(), "room location must be set"},
		rule{!blank(r.Name), "room name must not be empty"},
		rule{r.Capacity > 0, "room capacity must be positive"},
	)
}

// StaffKind is the role of a staff member in a lesson.
type StaffKind string

// Known staff kinds.
const (
	StaffInstructor  StaffKind = "instructor"
	StaffAccompanist StaffKind = "accompanist"
)

// StaffKinds returns all staff kinds.
func StaffKinds() []StaffKind { return []StaffKind{StaffInstructor, StaffAccompanist} }

// Valid reports whether k is a known staff kind.
func (k StaffKind) Valid() bool { return slices.Contains(StaffKinds(), k) }

// Staff is an instructor or accompanist who can be assigned to lessons.
type Staff struct {
	ID          StaffID
	PersonID    PersonID // optional: staff without a person record are allowed
	DisplayName string
	Kind        StaffKind
	Active      bool
}

// Validate checks the staff member for consistency. All errors wrap
// ErrValidation.
func (s Staff) Validate() error {
	return validate(
		rule{!s.ID.IsZero(), "staff id must be set"},
		rule{!blank(s.DisplayName), "staff display name must not be empty"},
		rule{s.Kind.Valid(), "invalid staff kind"},
	)
}

// Level is the skill level a class is aimed at.
type Level string

// Known levels, from least to most advanced; LevelOpen is for everyone.
const (
	LevelBeginner     Level = "beginner"
	LevelIntermediate Level = "intermediate"
	LevelAdvanced     Level = "advanced"
	LevelProfessional Level = "professional"
	LevelOpen         Level = "open"
)

// Levels returns all levels.
func Levels() []Level {
	return []Level{LevelBeginner, LevelIntermediate, LevelAdvanced, LevelProfessional, LevelOpen}
}

// Valid reports whether l is a known level.
func (l Level) Valid() bool { return slices.Contains(Levels(), l) }

// CourseType is a catalog entry (dance style) with a description.
type CourseType struct {
	ID          CourseTypeID
	Name        string
	Description string // optional
}

// Validate checks the course type for consistency. All errors wrap
// ErrValidation.
func (c CourseType) Validate() error {
	return validate(
		rule{!c.ID.IsZero(), "course type id must be set"},
		rule{!blank(c.Name), "course type name must not be empty"},
	)
}
