package httpapi

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func registerSchedule(api huma.API, h *handlers) {
	register(api, AccessPublic, huma.Operation{
		OperationID: "getSchedule",
		Method:      "GET",
		Path:        BasePath + "/schedule",
		Summary:     "Published schedule",
		Description: "Returns the lessons that start in a range of school dates, as the caller may see " +
			"them. Visitors who are not signed in see public lessons that are released, plus workshops " +
			"and private lessons, which are published early. Members also see lessons for " +
			"members, and regular lessons up to visibleUntil (early access). Staff see every lesson, " +
			"hidden and unreleased ones included. Canceled lessons are included with their status. Each lesson " +
			"comes with its free places and, for a signed-in caller, whom of their family it has booked.",
		Tags: []string{"schedule"},
	}, h.getSchedule)

	register(api, AccessPublic, huma.Operation{
		OperationID: "getLesson",
		Method:      "GET",
		Path:        BasePath + "/lessons/{lessonId}",
		Summary:     "Published lesson",
		Description: "Returns one lesson the caller may see (see getSchedule), with its free places and, for a signed-in caller, whom of their family it has booked. Lessons that do not exist or are not visible are reported alike as not found. Office and admin accounts also get `admin`: the detached flag, the active bookings and the cancellation details.",
		Tags:        []string{"schedule"},
	}, h.getLesson)
}

// ScheduleInput are the parameters of getSchedule.
type ScheduleInput struct {
	From domain.Date `doc:"First school date of the range." format:"date" query:"from" required:"true"`
	To   domain.Date `doc:"School date after the last day of the range (exclusive); at most 42 days after from." format:"date" query:"to" required:"true"`

	Location   domain.LocationID   `doc:"Only lessons at this location." query:"location"`
	Instructor domain.StaffID      `doc:"Only lessons with this staff member as instructor or accompanist." query:"instructor"`
	CourseType domain.CourseTypeID `doc:"Only lessons of this course type." query:"courseType"`
	Level      domain.LevelID      `doc:"Only lessons of classes for this level: a span of levels that includes it, or all levels. Classes without a level, one-off lessons and an unknown level match nothing." query:"level"`
}

// Schedule is the response body of getSchedule.
type Schedule struct {
	Timezone      string           `doc:"IANA time zone of the school; school dates and times are local to it." json:"timezone"`
	Now           time.Time        `doc:"Server time the response was computed at."                             json:"now"`
	From          domain.Date      `doc:"First school date of the range."                format:"date"          json:"from"`
	To            domain.Date      `doc:"School date after the last day of the range." format:"date"            json:"to"`
	ReleasedUntil time.Time        `doc:"Regular lessons that start before this instant are released."          json:"releasedUntil"`
	NextReleaseAt time.Time        `doc:"Instant of the next weekly release."                                  json:"nextReleaseAt"`
	VisibleUntil  time.Time        `doc:"Regular lessons that start before this instant are visible to the caller: releasedUntil for guests, later for members with early access; absent for staff, who see every lesson." json:"visibleUntil,omitzero"`
	Lessons       []ScheduleLesson `doc:"Visible lessons, ordered by start."                                   json:"lessons"       nullable:"false"`
}

// ScheduleLesson is a lesson in the schedule with its availability.
type ScheduleLesson struct {
	LessonSummary
	Availability
}

// Availability is how many places a lesson has left and whom of the
// caller's family it has booked (ADR 0009).
type Availability struct {
	FreePlaces int           `doc:"Places left: the capacity minus the active bookings and the places that waiting-list invites reserve, at least 0. A lesson without one is full: members may join its waiting list." json:"freePlaces" minimum:"0"`
	BookedFor  []BookedFor   `doc:"The signed-in person and their dependents with an active booking for the lesson, the signed-in person first; absent if there is none or the caller is not signed in (and in the responses of the lesson writes)." json:"bookedFor,omitempty" nullable:"false"`
	Waitlist   []WaitlistFor `doc:"The signed-in person and their dependents on the lesson's waiting list, the signed-in person first, with their place or invite; absent if there is none or the caller is not signed in (and in the responses of the lesson writes)." json:"waitlist,omitempty" nullable:"false"`
	// The cutoffs are not secret: everyone gets them, so the UI can show a
	// closed booking and warn of a late cancellation without a request.
	BookingClosesAt time.Time `doc:"Booking closes at this instant: the booking cutoff before the start (UTC)." json:"bookingClosesAt"`
	LateCancelFrom  time.Time `doc:"From this instant on a cancellation is late: the cancellation cutoff before the start (UTC); absent for a lesson the school canceled, which is never canceled late." json:"lateCancelFrom,omitzero"`
}

// BookedFor is someone of the caller's family with an active booking.
type BookedFor struct {
	PersonID  domain.PersonID  `doc:"The booked person."                     json:"personId"`
	Name      string           `doc:"Given and family name of the person."  json:"name"`
	BookingID domain.BookingID `doc:"The active booking, e.g. to cancel it." json:"bookingId"`
}

// LessonSummary is a lesson in the schedule.
type LessonSummary struct {
	ID         domain.LessonID `doc:"Lesson ID." json:"id"`
	ClassID    domain.ClassID  `doc:"Weekly class the lesson belongs to; absent for one-off lessons." json:"classId,omitzero"`
	StartsAt   time.Time       `doc:"Start instant (UTC)."                                              json:"startsAt"`
	EndsAt     time.Time       `doc:"End instant (UTC)."                                                json:"endsAt"`
	Status     string          `doc:"Whether the lesson takes place."  enum:"scheduled,canceled"         json:"status"`
	Kind       string          `doc:"Regular lesson or special format; a room rental (only staff see one) takes no bookings." enum:"regular,workshop,private,rental" json:"kind"`
	Visibility string          `doc:"Who may see the lesson: everyone, signed-in members, or staff only." enum:"public,members,hidden" json:"visibility"`
	Audience
	Capacity    int            `doc:"Maximum number of participants; 0 for a room rental." minimum:"0"   json:"capacity"`
	CourseType  *CourseTypeRef `doc:"Course type; absent for a room rental without one." json:"courseType,omitempty"`
	Room        RoomRef        `json:"room"`
	Location    LocationRef    `json:"location"`
	Instructor  *StaffRef      `doc:"Instructor; absent for a room rental." json:"instructor,omitempty"`
	Accompanist *StaffRef      `doc:"Accompanist; absent if the lesson has none." json:"accompanist,omitempty"`
	Renter      string         `doc:"Who rents the room; only for a room rental." json:"renter,omitempty"`
	EventID     domain.EventID `doc:"The event whose session the lesson is (ADR 0019): it is not booked one by one; absent otherwise." json:"eventId,omitzero"`
}

// LessonDetail is the response body of getLesson and of the lesson writes.
type LessonDetail struct {
	LessonSummary
	Availability

	CourseTypeDescription string       `doc:"Description of the course type." json:"courseTypeDescription,omitempty"`
	LocationAddress       string       `doc:"Postal address of the location." json:"locationAddress,omitempty"`
	Admin                 *LessonAdmin `doc:"What the office sees beyond the schedule; only for office and admin accounts." json:"admin,omitempty"`
}

// LessonAdmin is what the office sees of a lesson beyond the schedule (ADR
// 0007).
type LessonAdmin struct {
	Detached       bool        `doc:"Whether the lesson was edited one by one and no longer follows its class." json:"detached"`
	ActiveBookings int         `doc:"Number of bookings that are not canceled." json:"activeBookings"`
	CancelNote     string      `doc:"Note of the cancellation, if any."          json:"cancelNote,omitempty"`
	CanceledAt     time.Time   `doc:"When the lesson was canceled, if recorded." json:"canceledAt,omitzero"`
	CanceledBy     *AccountRef `doc:"Who canceled the lesson, if recorded."      json:"canceledBy,omitempty"`
}

// AccountRef names an account by its person.
type AccountRef struct {
	ID   domain.AccountID `json:"id"`
	Name string           `doc:"Given and family name of the account's person." json:"name"`
}

// CourseTypeRef names a course type.
type CourseTypeRef struct {
	ID   domain.CourseTypeID `json:"id"`
	Name string              `json:"name"`
}

// courseTypeRef names ct, nil for the zero course type of a room rental
// without one.
func courseTypeRef(ct domain.CourseType) *CourseTypeRef {
	if ct.ID.IsZero() {
		return nil
	}

	return &CourseTypeRef{ID: ct.ID, Name: ct.Name}
}

// RoomRef names a room.
type RoomRef struct {
	ID       domain.RoomID `json:"id"`
	Name     string        `json:"name"`
	Position int           `doc:"Place among the rooms of its location, from 1." json:"position" minimum:"1"`
}

// LocationRef names a location.
type LocationRef struct {
	ID   domain.LocationID `json:"id"`
	Name string            `json:"name"`
}

// StaffRef names a staff member.
type StaffRef struct {
	ID          domain.StaffID `json:"id"`
	DisplayName string         `json:"displayName"`
}

// ScheduleOutput is the response of getSchedule.
type ScheduleOutput struct {
	Body Schedule
}

// LessonInput are the parameters of getLesson.
type LessonInput struct {
	LessonID domain.LessonID `doc:"Lesson ID." path:"lessonId"`
}

// LessonOutput is the response of getLesson.
type LessonOutput struct {
	Body LessonDetail
}

func (h *handlers) getSchedule(ctx context.Context, in *ScheduleInput) (*ScheduleOutput, error) {
	p, _ := principalFrom(ctx)

	s, err := h.deps.Schedule.Schedule(ctx, p, in.query())
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &ScheduleOutput{Body: NewSchedule(s)}, nil
}

// NewSchedule is the response body of getSchedule for s; the command line's
// `lessons list --output json` prints it too (ADR 0020).
func NewSchedule(s service.Schedule) Schedule {
	body := Schedule{
		Timezone:      s.Timezone,
		Now:           s.Now.UTC(),
		From:          s.From,
		To:            s.To,
		ReleasedUntil: s.Release.ReleasedUntil.UTC(),
		NextReleaseAt: s.Release.NextRelease.UTC(),
		VisibleUntil:  s.VisibleUntil.UTC(),
		Lessons:       make([]ScheduleLesson, 0, len(s.Lessons)),
	}

	for _, sl := range s.Lessons {
		body.Lessons = append(body.Lessons, ScheduleLesson{
			LessonSummary: lessonSummary(sl), Availability: availability(sl),
		})
	}

	return body
}

func (h *handlers) getLesson(ctx context.Context, in *LessonInput) (*LessonOutput, error) {
	p, _ := principalFrom(ctx)

	sl, err := h.deps.Schedule.Lesson(ctx, p, in.LessonID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &LessonOutput{Body: NewLessonDetail(sl)}, nil
}

// NewLessonDetail is the response body of getLesson and of the lesson
// writes for sl; the command line's `lessons cancel|restore --output json`
// prints it too (ADR 0020).
func NewLessonDetail(sl service.ScheduledLesson) LessonDetail {
	out := LessonDetail{
		LessonSummary:         lessonSummary(sl),
		Availability:          availability(sl),
		CourseTypeDescription: sl.CourseType.Description,
		LocationAddress:       sl.Location.Address,
	}

	if a := sl.Admin; a != nil {
		out.Admin = &LessonAdmin{
			Detached: a.Detached, ActiveBookings: a.ActiveBookings, CancelNote: a.CancelNote,
			CanceledAt: a.CanceledAt.UTC(),
		}

		if !a.CanceledBy.AccountID.IsZero() {
			out.Admin.CanceledBy = &AccountRef{ID: a.CanceledBy.AccountID, Name: a.CanceledBy.Name}
		}
	}

	return out
}

// availability returns the free places and the family's bookings and
// waiting-list entries of sl.
func availability(sl service.ScheduledLesson) Availability {
	a := Availability{
		FreePlaces: sl.FreePlaces, BookingClosesAt: sl.BookingClosesAt.UTC(), LateCancelFrom: sl.LateCancelFrom.UTC(),
		Waitlist: waitlistFor(sl.Waiting),
	}

	for _, b := range sl.BookedFor {
		a.BookedFor = append(a.BookedFor, BookedFor{
			PersonID: b.Person.ID, Name: b.Name, BookingID: b.BookingID,
		})
	}

	return a
}

// query converts the parameters into a service query.
func (in *ScheduleInput) query() service.ScheduleQuery {
	return service.ScheduleQuery{
		From: in.From, To: in.To,
		Location: in.Location, Instructor: in.Instructor, CourseType: in.CourseType,
		Level: in.Level,
	}
}

func lessonSummary(sl service.ScheduledLesson) LessonSummary {
	l := sl.Lesson
	out := LessonSummary{
		ID:         l.ID,
		ClassID:    l.ClassID,
		StartsAt:   l.StartsAt.UTC(),
		EndsAt:     l.EndsAt.UTC(),
		Status:     string(l.Status),
		Kind:       string(l.Kind),
		Visibility: string(l.Visibility),
		Audience:   NewAudience(sl.Audience),
		Capacity:   l.Capacity,
		CourseType: courseTypeRef(sl.CourseType),
		Room:       RoomRef{ID: sl.Room.ID, Name: sl.Room.Name, Position: sl.Room.Position},
		Location:   LocationRef{ID: sl.Location.ID, Name: sl.Location.Name},
		Renter:     l.Renter,
		EventID:    l.EventID,
	}

	if !sl.Instructor.ID.IsZero() {
		out.Instructor = &StaffRef{ID: sl.Instructor.ID, DisplayName: sl.Instructor.DisplayName}
	}

	if !sl.Accompanist.ID.IsZero() {
		out.Accompanist = &StaffRef{ID: sl.Accompanist.ID, DisplayName: sl.Accompanist.DisplayName}
	}

	return out
}
