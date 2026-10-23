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
		rule{!blank(t.Name), "term name must not be empty"},
		rule{t.StartsOn.Valid(), "invalid term start"},
		rule{t.EndsOn.Valid(), "invalid term end"},
		rule{t.StartsOn.Compare(t.EndsOn) <= 0, "term must not end before it starts"},
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
	Level         Level
	Weekday       time.Weekday
	StartTime     TimeOfDay // school time
	Duration      time.Duration
	Capacity      int
}

// Validate checks the class for consistency. All errors wrap ErrValidation.
func (c Class) Validate() error {
	return validate(
		rule{!c.ID.IsZero(), "class id must be set"},
		rule{!c.TermID.IsZero(), "class term must be set"},
		rule{!c.CourseTypeID.IsZero(), "class course type must be set"},
		rule{!c.RoomID.IsZero(), "class room must be set"},
		rule{!c.InstructorID.IsZero(), "class instructor must be set"},
		rule{c.AccompanistID.IsZero() || c.AccompanistID != c.InstructorID, "accompanist must differ from the instructor"},
		rule{c.Level.Valid(), "invalid class level"},
		rule{validWeekday(c.Weekday), "invalid class weekday"},
		rule{c.StartTime.Valid(), "invalid class start time"},
		rule{c.Duration > 0 && c.Duration%time.Minute == 0, "class duration must be a positive number of minutes"},
		rule{c.Duration <= 24*time.Hour-c.StartTime.SinceMidnight(), "class must not run past midnight"},
		rule{c.Capacity > 0, "class capacity must be positive"},
	)
}

// ClassMember is a fixed enrolment of a person in a class. Members are
// booked into the class's lessons automatically.
type ClassMember struct {
	ClassID  ClassID
	PersonID PersonID
	Since    Date // optional
}

// Validate checks the enrolment for consistency. All errors wrap
// ErrValidation.
func (m ClassMember) Validate() error {
	return validate(
		rule{!m.ClassID.IsZero(), "class member class must be set"},
		rule{!m.PersonID.IsZero(), "class member person must be set"},
		rule{optionalDate(m.Since), "invalid class member start date"},
	)
}
