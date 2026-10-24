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

// registerLessonAdmin registers the office's writes to single lessons (ADR
// 0007): edit, cancel and restore, and one-off lessons. The reads are in
// schedule.go; getLesson shows the office the details these writes record.
func registerLessonAdmin(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"lessons"}

		return withConflict(api, op)
	}

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "createLesson",
		Method:      http.MethodPost,
		Path:        BasePath + "/lessons",
		Summary:     "Create a one-off lesson",
		Description: "Adds a lesson without a class, e.g. a workshop or a private lesson, which are published " +
			"early; a regular one follows the release window. An unknown course type, room or staff member is " +
			"a 422 on the field that names it. A start that is not in the future is a 409 `past` unless " +
			"`confirmPast` is set. Another lesson in the room at an overlapping time is a 409 `room-taken` " +
			"naming it, unless `confirmRoomOverlap` is set.",
		DefaultStatus: http.StatusCreated,
	}), h.createLesson)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateLesson",
		Method:      http.MethodPut,
		Path:        BasePath + "/lessons/{lessonId}",
		Summary:     "Update a lesson",
		Description: "Replaces the time, room, staff, capacity, visibility and kind of a lesson that has not " +
			"started (422 on `path.lessonId` otherwise); course type and class stay. A lesson of a class is " +
			"detached from it: class and term edits leave it alone from then on. A capacity below the active " +
			"bookings is a 422 on `capacity`. A new start in the past is a 409 `past` unless `confirmPast` is " +
			"set. A new time or room is checked like in `createLesson` (409 `room-taken`). A new start or " +
			"room of a scheduled lesson mails everyone with an active booking, unless it now starts in the past.",
	}), h.updateLesson)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "cancelLesson",
		Method:      http.MethodPost,
		Path:        BasePath + "/lessons/{lessonId}/cancel",
		Summary:     "Cancel a lesson",
		Description: "Cancels a lesson that has not started (422 on `path.lessonId` otherwise), with an " +
			"optional note, and mails everyone with an active booking. The bookings stay. A canceled lesson " +
			"stays as it is, note included, started or not.",
	}), h.cancelLesson)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "restoreLesson",
		Method:      http.MethodPost,
		Path:        BasePath + "/lessons/{lessonId}/restore",
		Summary:     "Restore a canceled lesson",
		Description: "Lets a canceled lesson that has not started take place after all (422 on " +
			"`path.lessonId` otherwise) and mails everyone with an active booking its current time and room. " +
			"Another lesson in the room at an overlapping time is a 409 `room-taken` naming it, unless " +
			"`confirmRoomOverlap` is set; the body is optional. The lesson stays detached from its class. " +
			"A scheduled lesson stays as it is, started or not.",
	}), h.restoreLesson)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "deleteLesson",
		Method:      http.MethodDelete,
		Path:        BasePath + "/lessons/{lessonId}",
		Summary:     "Delete a one-off lesson",
		Description: "Deletes a one-off lesson that has not started (422 on `path.lessonId` otherwise) and that " +
			"no booking refers to (409 `in-use` otherwise). A lesson of a class is canceled instead (409 " +
			"`generated`): generation would create it again.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteLesson)
}

// LessonData is the editable part of a lesson.
type LessonData struct {
	Date               domain.Date      `doc:"School date of the lesson."                  format:"date"   json:"date"`
	StartTime          domain.TimeOfDay `doc:"Start time (HH:MM) in the school time zone." example:"18:00" json:"startTime"          pattern:"^[0-2][0-9]:[0-5][0-9]$"`
	DurationMinutes    int              `doc:"Length in minutes; the lesson ends on its day." json:"durationMinutes" maximum:"1440" minimum:"1"`
	RoomID             domain.RoomID    `doc:"Room."                                       json:"roomId"`
	InstructorID       domain.StaffID   `doc:"Instructor."                                 json:"instructorId"`
	AccompanistID      domain.StaffID   `doc:"Accompanist, if any."                        json:"accompanistId,omitzero" required:"false"`
	Capacity           int              `doc:"Maximum number of participants."             json:"capacity"           maximum:"1000" minimum:"1"`
	Visibility         string           `doc:"Who may see the lesson."                     enum:"public,members,hidden" json:"visibility"`
	Kind               string           `doc:"Regular lesson or special format; workshops and private lessons are published early." enum:"regular,workshop,private" json:"kind"`
	ConfirmRoomOverlap bool             `doc:"Save even if another lesson uses the room at an overlapping time (skips the 409 room-taken)." json:"confirmRoomOverlap,omitempty" required:"false"`
	ConfirmPast        bool             `doc:"Save even if the lesson starts in the past (skips the 409 past)." json:"confirmPast,omitempty" required:"false"`
}

// LessonCreateData is the request body of createLesson.
type LessonCreateData struct {
	LessonData

	CourseTypeID domain.CourseTypeID `doc:"Course type taught." json:"courseTypeId"`
}

// LessonCreateInput is the request of createLesson.
type LessonCreateInput struct {
	Body LessonCreateData
}

// LessonUpdateInput is the request of updateLesson.
type LessonUpdateInput struct {
	LessonID domain.LessonID `doc:"The lesson." path:"lessonId"`
	Body     LessonData
}

// LessonCancelData is the request body of cancelLesson.
type LessonCancelData struct {
	// The length is checked after trimming, in the service: a raw maxLength
	// would refuse a note that only its surrounding white space makes too
	// long.
	Note string `doc:"Optional note for those booked, e.g. the reason; at most 500 characters (Unicode code points) after trimming surrounding white space, a 422 on body.note otherwise." json:"note,omitempty" required:"false"`
}

// LessonCancelInput is the request of cancelLesson.
type LessonCancelInput struct {
	LessonID domain.LessonID `doc:"The lesson." path:"lessonId"`
	Body     LessonCancelData
}

// LessonRestoreData is the optional request body of restoreLesson.
type LessonRestoreData struct {
	ConfirmRoomOverlap bool `doc:"Restore even if another lesson uses the room at an overlapping time (skips the 409 room-taken)." json:"confirmRoomOverlap,omitempty" required:"false"`
}

// LessonRestoreInput is the request of restoreLesson.
type LessonRestoreInput struct {
	LessonID domain.LessonID    `doc:"The lesson." path:"lessonId"`
	Body     *LessonRestoreData `required:"false"`
}

// LessonRefInput is the request of deleteLesson.
type LessonRefInput struct {
	LessonID domain.LessonID `doc:"The lesson." path:"lessonId"`
}

func (h *handlers) createLesson(ctx context.Context, in *LessonCreateInput) (*LessonOutput, error) {
	return h.lessonOutput(ctx)(h.deps.Lessons.CreateLesson(ctx, service.NewLesson{
		LessonData: in.Body.service(), CourseTypeID: in.Body.CourseTypeID,
	}))
}

func (h *handlers) updateLesson(ctx context.Context, in *LessonUpdateInput) (*LessonOutput, error) {
	return h.lessonOutput(ctx)(h.deps.Lessons.UpdateLesson(ctx, in.LessonID, in.Body.service()))
}

func (h *handlers) cancelLesson(ctx context.Context, in *LessonCancelInput) (*LessonOutput, error) {
	p, _ := principalFrom(ctx)

	return h.lessonOutput(ctx)(h.deps.Lessons.CancelLesson(ctx, p, in.LessonID, in.Body.Note))
}

func (h *handlers) restoreLesson(ctx context.Context, in *LessonRestoreInput) (*LessonOutput, error) {
	confirm := in.Body != nil && in.Body.ConfirmRoomOverlap

	return h.lessonOutput(ctx)(h.deps.Lessons.RestoreLesson(ctx, in.LessonID, confirm))
}

func (h *handlers) deleteLesson(ctx context.Context, in *LessonRefInput) (*struct{}, error) {
	if err := h.deps.Lessons.DeleteLesson(ctx, in.LessonID); err != nil {
		return nil, h.lessonError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) lessonOutput(ctx context.Context) func(service.ScheduledLesson, error) (*LessonOutput, error) {
	return func(sl service.ScheduledLesson, err error) (*LessonOutput, error) {
		if err != nil {
			return nil, h.lessonError(ctx, err)
		}

		return &LessonOutput{Body: lessonDetail(sl)}, nil
	}
}

// lessonError maps the conflicts of the lesson writes to their reasons and
// everything else like toHTTPError.
func (h *handlers) lessonError(ctx context.Context, err error) error {
	var taken *service.RoomTakenError

	switch {
	case errors.As(err, &taken):
		c := conflict(reasonRoomTaken, "another lesson uses the room at that time")
		c.Lesson = &ConflictLesson{
			ID: taken.Lesson.ID, StartsAt: taken.Lesson.StartsAt.UTC(), EndsAt: taken.Lesson.EndsAt.UTC(),
			CourseType: CourseTypeRef{ID: taken.CourseType.ID, Name: taken.CourseType.Name},
		}

		return c
	case errors.Is(err, service.ErrLessonStarted):
		// The lesson the path names, not a field of the body, is the
		// problem (ADR 0007).
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "the lesson has started", Location: "path.lessonId",
		})
	case errors.Is(err, service.ErrLessonPast):
		return conflict(reasonPast, "the lesson would start in the past")
	case errors.Is(err, service.ErrGeneratedLesson):
		return conflict(reasonGenerated, "a lesson of a class is canceled, not deleted")
	}

	return h.toHTTPError(ctx, err)
}

func (d LessonData) service() service.LessonData {
	return service.LessonData{
		Date: d.Date, StartTime: d.StartTime, Duration: time.Duration(d.DurationMinutes) * time.Minute,
		RoomID: d.RoomID, InstructorID: d.InstructorID, AccompanistID: d.AccompanistID, Capacity: d.Capacity,
		Visibility: domain.LessonVisibility(d.Visibility), Kind: domain.LessonKind(d.Kind),
		ConfirmRoomOverlap: d.ConfirmRoomOverlap, ConfirmPast: d.ConfirmPast,
	}
}
