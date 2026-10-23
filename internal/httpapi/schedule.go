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
			"them. Visitors who are not signed in see public lessons that are released, plus one-off " +
			"lessons and special formats, which are published early. Members also see lessons for " +
			"members, and regular lessons up to visibleUntil (early access). Staff see every lesson, " +
			"hidden and unreleased ones included. Canceled lessons are included with their status.",
		Tags: []string{"schedule"},
	}, h.getSchedule)

	register(api, AccessPublic, huma.Operation{
		OperationID: "getLesson",
		Method:      "GET",
		Path:        BasePath + "/lessons/{lessonId}",
		Summary:     "Published lesson",
		Description: "Returns one lesson the caller may see (see getSchedule). Lessons that do not exist or are not visible are reported alike as not found.",
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
	Level      string              `doc:"Only lessons of classes at this level; excludes one-off lessons." enum:"beginner,intermediate,advanced,professional,open" query:"level"`
}

// Schedule is the response body of getSchedule.
type Schedule struct {
	Timezone      string          `doc:"IANA time zone of the school; school dates and times are local to it." json:"timezone"`
	Now           time.Time       `doc:"Server time the response was computed at."                             json:"now"`
	From          domain.Date     `doc:"First school date of the range."                format:"date"          json:"from"`
	To            domain.Date     `doc:"School date after the last day of the range." format:"date"            json:"to"`
	ReleasedUntil time.Time       `doc:"Regular lessons that start before this instant are released."          json:"releasedUntil"`
	NextReleaseAt time.Time       `doc:"Instant of the next weekly release."                                  json:"nextReleaseAt"`
	VisibleUntil  time.Time       `doc:"Regular lessons that start before this instant are visible to the caller: releasedUntil for guests, later for members with early access; absent for staff, who see every lesson." json:"visibleUntil,omitzero"`
	Lessons       []LessonSummary `doc:"Visible lessons, ordered by start."                                    json:"lessons"       nullable:"false"`
}

// LessonSummary is a lesson in the schedule.
type LessonSummary struct {
	ID          domain.LessonID `doc:"Lesson ID." json:"id"`
	ClassID     domain.ClassID  `doc:"Weekly class the lesson belongs to; absent for one-off lessons." json:"classId,omitzero"`
	StartsAt    time.Time       `doc:"Start instant (UTC)."                                              json:"startsAt"`
	EndsAt      time.Time       `doc:"End instant (UTC)."                                                json:"endsAt"`
	Status      string          `doc:"Whether the lesson takes place."  enum:"scheduled,canceled"         json:"status"`
	Kind        string          `doc:"Regular lesson or special format." enum:"regular,workshop,private" json:"kind"`
	Visibility  string          `doc:"Who may see the lesson: everyone, signed-in members, or staff only." enum:"public,members,hidden" json:"visibility"`
	Level       string          `doc:"Level of the class; absent for one-off lessons." enum:"beginner,intermediate,advanced,professional,open" json:"level,omitempty"`
	Capacity    int             `doc:"Maximum number of participants." minimum:"1"                       json:"capacity"`
	CourseType  CourseTypeRef   `json:"courseType"`
	Room        RoomRef         `json:"room"`
	Location    LocationRef     `json:"location"`
	Instructor  StaffRef        `json:"instructor"`
	Accompanist *StaffRef       `doc:"Accompanist; absent if the lesson has none." json:"accompanist,omitempty"`
}

// LessonDetail is the response body of getLesson.
type LessonDetail struct {
	LessonSummary

	CourseTypeDescription string `doc:"Description of the course type." json:"courseTypeDescription,omitempty"`
	LocationAddress       string `doc:"Postal address of the location." json:"locationAddress,omitempty"`
}

// CourseTypeRef names a course type.
type CourseTypeRef struct {
	ID   domain.CourseTypeID `json:"id"`
	Name string              `json:"name"`
}

// RoomRef names a room.
type RoomRef struct {
	ID   domain.RoomID `json:"id"`
	Name string        `json:"name"`
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

	body := Schedule{
		Timezone:      s.Timezone,
		Now:           s.Now.UTC(),
		From:          s.From,
		To:            s.To,
		ReleasedUntil: s.Release.ReleasedUntil.UTC(),
		NextReleaseAt: s.Release.NextRelease.UTC(),
		VisibleUntil:  s.VisibleUntil.UTC(),
		Lessons:       make([]LessonSummary, 0, len(s.Lessons)),
	}

	for _, sl := range s.Lessons {
		body.Lessons = append(body.Lessons, lessonSummary(sl))
	}

	return &ScheduleOutput{Body: body}, nil
}

func (h *handlers) getLesson(ctx context.Context, in *LessonInput) (*LessonOutput, error) {
	p, _ := principalFrom(ctx)

	sl, err := h.deps.Schedule.Lesson(ctx, p, in.LessonID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &LessonOutput{Body: LessonDetail{
		LessonSummary:         lessonSummary(sl),
		CourseTypeDescription: sl.CourseType.Description,
		LocationAddress:       sl.Location.Address,
	}}, nil
}

// query converts the parameters into a service query.
func (in *ScheduleInput) query() service.ScheduleQuery {
	return service.ScheduleQuery{
		From: in.From, To: in.To,
		Location: in.Location, Instructor: in.Instructor, CourseType: in.CourseType,
		Level: domain.Level(in.Level),
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
		Level:      string(sl.Level),
		Capacity:   l.Capacity,
		CourseType: CourseTypeRef{ID: sl.CourseType.ID, Name: sl.CourseType.Name},
		Room:       RoomRef{ID: sl.Room.ID, Name: sl.Room.Name},
		Location:   LocationRef{ID: sl.Location.ID, Name: sl.Location.Name},
		Instructor: StaffRef{ID: sl.Instructor.ID, DisplayName: sl.Instructor.DisplayName},
	}

	if !sl.Accompanist.ID.IsZero() {
		out.Accompanist = &StaffRef{ID: sl.Accompanist.ID, DisplayName: sl.Accompanist.DisplayName}
	}

	return out
}
