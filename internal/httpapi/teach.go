package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerTeach registers the instructor view (ADR 0009, amendment): the
// lessons a staff member teaches or accompanies, and the notices to the
// participants of a lesson.
func registerTeach(api huma.API, h *handlers) {
	register(api, AccessStaff, huma.Operation{
		OperationID: "listTeachLessons",
		Method:      http.MethodGet,
		Path:        BasePath + "/teach",
		Summary:     "List the lessons I teach",
		Description: "Returns the lessons that start in a range of school dates, by default the week before " +
			"today, today and the week after (at most 31 days), with their counts and attendance state. An " +
			"instructor or accompanist gets the lessons their active staff records (linked to their person) " +
			"teach or accompany, none without a link; the office every lesson. `staffId` keeps one staff " +
			"member's lessons (for an instructor, only their own). Canceled and hidden lessons are included.",
		Tags: []string{"participants"},
	}, h.listTeachLessons)

	register(api, AccessStaff, huma.Operation{
		OperationID: "notifyParticipants",
		Method:      http.MethodPost,
		Path:        BasePath + "/lessons/{lessonId}/notify",
		Summary:     "Message a lesson's participants",
		Description: "Mails the plain-text `message` (line breaks normalized to `\\n`, trimmed, 1 to 1000 " +
			"characters, no control characters but line breaks and tabs, no invisible format characters but " +
			"the zero-width joiners; a 422 on `body.message` otherwise) to everyone booked for the lesson: the " +
			"mail recipients of every active booking and the guests with an email, each address once, each in " +
			"its language. The mail names the sender as the schedule does if they teach or accompany the " +
			"lesson, else by their name, and replies go to the sender's address (the admin's while signed in " +
			"as someone else). Same access as `listLessonParticipants` (a 404 on others' lessons); lessons " +
			"that are over or canceled too. An account sends at most 5 notices an hour, and a lesson gets at " +
			"most 3 a school day, from anyone: beyond that a 429 with `Retry-After`. Returns how many " +
			"addresses were mailed; a lesson without any counts no notice.",
		Tags: []string{"participants"},
	}, h.notifyParticipants)
}

// TeachInput are the parameters of listTeachLessons.
type TeachInput struct {
	From    domain.Date    `doc:"First school date of the range; default: a week before today." format:"date" query:"from"`
	To      domain.Date    `doc:"School date after the last day of the range (exclusive); at most 31 days after from; default: 15 days after from." format:"date" query:"to"`
	StaffID domain.StaffID `doc:"Only lessons with this staff member as instructor or accompanist." query:"staffId"`
}

// TeachLesson is a lesson of the instructor view.
type TeachLesson struct {
	LessonSummary

	Role            string    `doc:"The caller's part: instructor or accompanist of the lesson, or office for a lesson of others." enum:"instructor,accompanist,office" json:"role"`
	Active          int       `doc:"Active (not canceled) bookings; more than the capacity after an override." json:"active"`
	Held            int       `doc:"Places held by open waiting-list invites."                                 json:"held"`
	FreePlaces      int       `doc:"Capacity minus active bookings and held places, at least 0."              json:"freePlaces"`
	Waiting         int       `doc:"Waiting-list entries waiting for an invite."                              json:"waiting"`
	Trials          int       `doc:"Active bookings that are trial lessons."                                  json:"trials"`
	Guests          int       `doc:"Active bookings of guests."                                               json:"guests"`
	DayOverAt       time.Time `doc:"When the attendance window of instructors and accompanists ends (see listLessonParticipants)." json:"dayOverAt"`
	AttendanceOpen  bool      `doc:"Whether the caller may take attendance now: the lesson takes place and has started, and for instructors and accompanists its day is not over." json:"attendanceOpen"`
	AttendanceTaken bool      `doc:"Whether the lesson takes place, has started and has no active booking whose attendance is unknown (booked)." json:"attendanceTaken"`
}

// TeachLessons is the response body of listTeachLessons.
type TeachLessons struct {
	Timezone string        `doc:"IANA time zone of the school; school dates and times are local to it." json:"timezone"`
	Now      time.Time     `doc:"Server time the response was computed at."                             json:"now"`
	From     domain.Date   `doc:"First school date of the range."              format:"date"            json:"from"`
	To       domain.Date   `doc:"School date after the last day of the range." format:"date"            json:"to"`
	Lessons  []TeachLesson `doc:"The lessons, ordered by start."                                        json:"lessons" nullable:"false"`
}

// TeachLessonsOutput is the response of listTeachLessons.
type TeachLessonsOutput struct {
	Body TeachLessons
}

// NotifyData is the request body of notifyParticipants.
type NotifyData struct {
	// The service checks the length of the normalized message; maxLength
	// only bounds the input.
	Message string `doc:"The message, plain text; line breaks are kept. At most 1000 characters once its line breaks are normalized and the space around it is trimmed." json:"message" maxLength:"4000" minLength:"1"`
}

// NotifyInput is the request of notifyParticipants.
type NotifyInput struct {
	LessonID domain.LessonID `doc:"The lesson." path:"lessonId"`
	Body     NotifyData
}

// NotifyOutput is the response of notifyParticipants.
type NotifyOutput struct {
	Body struct {
		Recipients int `doc:"How many addresses the message was mailed to." json:"recipients" minimum:"0"`
	}
}

func (h *handlers) listTeachLessons(ctx context.Context, in *TeachInput) (*TeachLessonsOutput, error) {
	p, _ := principalFrom(ctx) // AccessStaff

	tl, err := h.deps.Teach.Lessons(ctx, p, service.TeachQuery{From: in.From, To: in.To, Staff: in.StaffID})
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	body := TeachLessons{
		Timezone: tl.Timezone, Now: tl.Now.UTC(), From: tl.From, To: tl.To,
		Lessons: make([]TeachLesson, 0, len(tl.Lessons)),
	}

	for _, l := range tl.Lessons {
		body.Lessons = append(body.Lessons, TeachLesson{
			LessonSummary: lessonSummary(l.ScheduledLesson), Role: string(l.Role), Active: l.Active, Held: l.Held,
			FreePlaces: l.FreePlaces, Waiting: l.Waiting, Trials: l.Trials, Guests: l.Guests,
			DayOverAt: l.DayOverAt.UTC(), AttendanceOpen: l.AttendanceOpen, AttendanceTaken: l.AttendanceTaken,
		})
	}

	return &TeachLessonsOutput{Body: body}, nil
}

func (h *handlers) notifyParticipants(ctx context.Context, in *NotifyInput) (*NotifyOutput, error) {
	p, _ := principalFrom(ctx) // AccessStaff

	n, err := h.deps.Teach.Notify(ctx, p, in.LessonID, in.Body.Message)

	var limited *service.TooManyNoticesError

	switch {
	case errors.As(err, &limited):
		return nil, tooMany("notices", limited.RetryAfter)
	case err != nil:
		return nil, h.toHTTPError(ctx, err)
	}

	out := &NotifyOutput{}
	out.Body.Recipients = n

	return out, nil
}
