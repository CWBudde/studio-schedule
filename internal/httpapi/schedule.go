package httpapi

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func registerSchedule(api huma.API, h *handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "getSchedule",
		Method:      "GET",
		Path:        BasePath + "/schedule",
		Summary:     "Published schedule",
		Description: "Returns the published lessons that start in a range of school dates, as a visitor " +
			"who is not signed in sees them: public lessons that are released, plus one-off lessons and " +
			"special formats, which are published early. Canceled lessons are included with their status.",
		Tags: []string{"schedule"},
	}, h.getSchedule)

	huma.Register(api, huma.Operation{
		OperationID: "getLesson",
		Method:      "GET",
		Path:        BasePath + "/lessons/{lessonId}",
		Summary:     "Published lesson",
		Description: "Returns one published lesson. Lessons that do not exist or are not visible are reported alike as not found.",
		Tags:        []string{"schedule"},
	}, h.getLesson)
}

// ScheduleInput are the parameters of getSchedule.
type ScheduleInput struct {
	From string `doc:"First school date of the range." format:"date" query:"from" required:"true"`
	To   string `doc:"School date after the last day of the range (exclusive); at most 42 days after from." format:"date" query:"to" required:"true"`

	Location   string `doc:"Only lessons at this location."                              format:"uuid" query:"location"`
	Instructor string `doc:"Only lessons with this staff member as instructor or accompanist." format:"uuid" query:"instructor"`
	CourseType string `doc:"Only lessons of this course type."                           format:"uuid" query:"courseType"`
	Level      string `doc:"Only lessons of classes at this level; excludes one-off lessons." enum:"beginner,intermediate,advanced,professional,open" query:"level"`
}

// Schedule is the response body of getSchedule.
type Schedule struct {
	Timezone      string          `doc:"IANA time zone of the school; school dates and times are local to it." json:"timezone"`
	Now           time.Time       `doc:"Server time the response was computed at."                             json:"now"`
	From          string          `doc:"First school date of the range."                format:"date"          json:"from"`
	To            string          `doc:"School date after the last day of the range." format:"date"            json:"to"`
	ReleasedUntil time.Time       `doc:"Regular lessons that start before this instant are released."          json:"releasedUntil"`
	NextReleaseAt time.Time       `doc:"Instant of the next weekly release."                                  json:"nextReleaseAt"`
	Lessons       []LessonSummary `doc:"Visible lessons, ordered by start."                                    json:"lessons"`
}

// LessonSummary is a lesson in the schedule.
type LessonSummary struct {
	ID          string        `doc:"Lesson ID."                                          format:"uuid" json:"id"`
	ClassID     string        `doc:"Weekly class the lesson belongs to; absent for one-off lessons." format:"uuid" json:"classId,omitempty"`
	StartsAt    time.Time     `doc:"Start instant (UTC)."                                              json:"startsAt"`
	EndsAt      time.Time     `doc:"End instant (UTC)."                                                json:"endsAt"`
	Status      string        `doc:"Whether the lesson takes place."  enum:"scheduled,canceled"         json:"status"`
	Kind        string        `doc:"Regular lesson or special format." enum:"regular,workshop,private" json:"kind"`
	Level       string        `doc:"Level of the class; absent for one-off lessons." enum:"beginner,intermediate,advanced,professional,open" json:"level,omitempty"`
	Capacity    int           `doc:"Maximum number of participants." minimum:"1"                       json:"capacity"`
	CourseType  CourseTypeRef `json:"courseType"`
	Room        RoomRef       `json:"room"`
	Location    LocationRef   `json:"location"`
	Instructor  StaffRef      `json:"instructor"`
	Accompanist *StaffRef     `doc:"Accompanist; absent if the lesson has none." json:"accompanist,omitempty"`
}

// LessonDetail is the response body of getLesson.
type LessonDetail struct {
	LessonSummary

	CourseTypeDescription string `doc:"Description of the course type." json:"courseTypeDescription,omitempty"`
	LocationAddress       string `doc:"Postal address of the location." json:"locationAddress,omitempty"`
}

// CourseTypeRef names a course type.
type CourseTypeRef struct {
	ID   string `format:"uuid" json:"id"`
	Name string `json:"name"`
}

// RoomRef names a room.
type RoomRef struct {
	ID   string `format:"uuid" json:"id"`
	Name string `json:"name"`
}

// LocationRef names a location.
type LocationRef struct {
	ID   string `format:"uuid" json:"id"`
	Name string `json:"name"`
}

// StaffRef names a staff member.
type StaffRef struct {
	ID          string `format:"uuid" json:"id"`
	DisplayName string `json:"displayName"`
}

// ScheduleOutput is the response of getSchedule.
type ScheduleOutput struct {
	Body Schedule
}

// LessonInput are the parameters of getLesson.
type LessonInput struct {
	LessonID string `doc:"Lesson ID." format:"uuid" path:"lessonId"`
}

// LessonOutput is the response of getLesson.
type LessonOutput struct {
	Body LessonDetail
}

func (h *handlers) getSchedule(ctx context.Context, in *ScheduleInput) (*ScheduleOutput, error) {
	q, err := in.query()
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	s, err := h.deps.Schedule.Schedule(ctx, q)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	body := Schedule{
		Timezone:      s.Timezone,
		Now:           s.Now.UTC(),
		From:          s.From.String(),
		To:            s.To.String(),
		ReleasedUntil: s.Release.ReleasedUntil.UTC(),
		NextReleaseAt: s.Release.NextRelease.UTC(),
		Lessons:       make([]LessonSummary, 0, len(s.Lessons)),
	}

	for _, sl := range s.Lessons {
		body.Lessons = append(body.Lessons, lessonSummary(sl))
	}

	return &ScheduleOutput{Body: body}, nil
}

func (h *handlers) getLesson(ctx context.Context, in *LessonInput) (*LessonOutput, error) {
	id, err := domain.ParseID[domain.LessonID](in.LessonID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	sl, err := h.deps.Schedule.Lesson(ctx, id)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &LessonOutput{Body: LessonDetail{
		LessonSummary:         lessonSummary(sl),
		CourseTypeDescription: sl.CourseType.Description,
		LocationAddress:       sl.Location.Address,
	}}, nil
}

// query converts the parameters into a service query. Errors wrap
// domain.ErrValidation.
func (in *ScheduleInput) query() (service.ScheduleQuery, error) {
	q := service.ScheduleQuery{Level: domain.Level(in.Level)}

	var err error

	if q.From, err = domain.ParseDate(in.From); err != nil {
		return q, err
	}

	if q.To, err = domain.ParseDate(in.To); err != nil {
		return q, err
	}

	if q.Location, err = optionalQueryID[domain.LocationID](in.Location); err != nil {
		return q, err
	}

	if q.Instructor, err = optionalQueryID[domain.StaffID](in.Instructor); err != nil {
		return q, err
	}

	if q.CourseType, err = optionalQueryID[domain.CourseTypeID](in.CourseType); err != nil {
		return q, err
	}

	return q, nil
}

// optionalQueryID parses an ID parameter; an empty string is the zero ID.
func optionalQueryID[I domain.AnyID](s string) (I, error) {
	if s == "" {
		return I{}, nil
	}

	return domain.ParseID[I](s)
}

// idOrEmpty returns the text form of an optional ID, empty if unset.
func idOrEmpty[I domain.AnyID](id I) string {
	if id.IsZero() {
		return ""
	}

	return id.String()
}

func lessonSummary(sl service.ScheduledLesson) LessonSummary {
	l := sl.Lesson
	out := LessonSummary{
		ID:         l.ID.String(),
		ClassID:    idOrEmpty(l.ClassID),
		StartsAt:   l.StartsAt.UTC(),
		EndsAt:     l.EndsAt.UTC(),
		Status:     string(l.Status),
		Kind:       string(l.Kind),
		Level:      string(sl.Level),
		Capacity:   l.Capacity,
		CourseType: CourseTypeRef{ID: sl.CourseType.ID.String(), Name: sl.CourseType.Name},
		Room:       RoomRef{ID: sl.Room.ID.String(), Name: sl.Room.Name},
		Location:   LocationRef{ID: sl.Location.ID.String(), Name: sl.Location.Name},
		Instructor: StaffRef{ID: sl.Instructor.ID.String(), DisplayName: sl.Instructor.DisplayName},
	}

	if !sl.Accompanist.ID.IsZero() {
		out.Accompanist = &StaffRef{ID: sl.Accompanist.ID.String(), DisplayName: sl.Accompanist.DisplayName}
	}

	return out
}
