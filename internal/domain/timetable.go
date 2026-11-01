package domain

import "time"

// Term is a date range with its own timetable, e.g. a summer timetable.
// Both dates are inclusive.
type Term struct {
	ID       TermID
	Name     string
	StartsOn Date
	EndsOn   Date
}

// Validate checks the term for consistency. All errors wrap ErrValidation.
func (t Term) Validate() error {
	return validate(
		rule{!t.ID.IsZero(), "term id must be set"},
		on("name", !blank(t.Name), "term name must not be empty"),
		on("startsOn", t.StartsOn.Valid(), "invalid term start"),
		on("endsOn", t.EndsOn.Valid(), "invalid term end"),
		on("endsOn", t.StartsOn.Compare(t.EndsOn) <= 0, "term must not end before it starts"),
	)
}

// Class is a weekly template within a term that generates lessons.
type Class struct {
	ID            ClassID
	TermID        TermID
	CourseTypeID  CourseTypeID
	RoomID        RoomID
	InstructorID  StaffID
	AccompanistID StaffID // optional
	// Who the class is for (ADR 0021): a span of the school's levels or all
	// levels (never both), a stage (0 for none, else 1–MaxStage) and an age
	// range in months (either end 0 for none).
	Levels    LevelRange
	AllLevels bool
	Stage     int
	AgeFrom   AgeMonths
	AgeTo     AgeMonths
	Weekday   time.Weekday
	StartTime TimeOfDay // school time
	Duration  time.Duration
	Capacity  int
	// Retired classes generate no more lessons and leave the public
	// timetable; their lessons stay (ADR 0007).
	Retired bool
}

// Validate checks the class for consistency. All errors wrap ErrValidation.
func (c Class) Validate() error {
	return validate(
		rule{!c.ID.IsZero(), "class id must be set"},
		rule{!c.TermID.IsZero(), "class term must be set"},
		on("courseTypeId", !c.CourseTypeID.IsZero(), "class course type must be set"),
		on("roomId", !c.RoomID.IsZero(), "class room must be set"),
		on("instructorId", !c.InstructorID.IsZero(), "class instructor must be set"),
		on("accompanistId", c.AccompanistID.IsZero() || c.AccompanistID != c.InstructorID,
			"accompanist must differ from the instructor"),
		on("levelToId", c.Levels.From.IsZero() == c.Levels.To.IsZero(), "a level range needs both ends"),
		on("allLevels", !c.AllLevels || c.Levels.IsZero(), "all levels excludes a level range"),
		on("stage", c.Stage == 0 || 1 <= c.Stage && c.Stage <= MaxStage, "stage must be between 1 and 9"),
		on("ageFromMonths", 0 <= c.AgeFrom && c.AgeFrom <= MaxAgeMonths, "age must be between 1 and 1200 months"),
		on("ageToMonths", 0 <= c.AgeTo && c.AgeTo <= MaxAgeMonths, "age must be between 1 and 1200 months"),
		on("ageToMonths", c.AgeTo == 0 || c.AgeTo >= c.AgeFrom, "the age range ends before it starts"),
		on("weekday", validWeekday(c.Weekday), "invalid class weekday"),
		on("startTime", c.StartTime.Valid(), "invalid class start time"),
		on("durationMinutes", c.Duration > 0 && c.Duration%time.Minute == 0,
			"class duration must be a positive number of minutes"),
		on("durationMinutes", c.Duration <= 24*time.Hour-c.StartTime.SinceMidnight(),
			"class must not run past midnight"),
		on("capacity", c.Capacity > 0, "class capacity must be positive"),
	)
}

// ClassMember is a fixed enrolment of a person in a class. Members are
// booked into the class's lessons automatically.
type ClassMember struct {
	ClassID  ClassID
	PersonID PersonID
	Since    Date // optional
	// PausedUntil is the last school date of a pause (ADR 0009): the member
	// is not booked automatically into lessons up to it. Zero for none.
	PausedUntil Date
}

// ActiveOn reports whether the membership has begun on the school date d
// and is not paused then: its Since is empty or not after d, and its pause,
// if any, ended before d. Members are booked into the lessons of
// the slot dates they are active on (ADR 0009).
func (m ClassMember) ActiveOn(d Date) bool {
	return (m.Since.IsZero() || m.Since.Compare(d) <= 0) && (m.PausedUntil.IsZero() || m.PausedUntil.Compare(d) < 0)
}

// Validate checks the enrolment for consistency. All errors wrap
// ErrValidation.
func (m ClassMember) Validate() error {
	return validate(
		rule{!m.ClassID.IsZero(), "class member class must be set"},
		rule{!m.PersonID.IsZero(), "class member person must be set"},
		on("since", optionalDate(m.Since), "invalid class member start date"),
		on("pausedUntil", optionalDate(m.PausedUntil), "invalid class member pause date"),
	)
}
