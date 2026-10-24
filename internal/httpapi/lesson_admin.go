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

	clone := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"lessons"}
		op.Method = http.MethodPost

		return op
	}

	register(api, AccessOffice, clone(huma.Operation{
		OperationID: "previewLessonClone",
		Path:        BasePath + "/admin/lessons/clone/preview",
		Summary:     "Preview a clone of a day or a week",
		Description: "Returns what `cloneLessons` would create and skip now, without changing anything.",
	}), h.previewLessonClone)

	register(api, AccessOffice, clone(huma.Operation{
		OperationID: "cloneLessons",
		Path:        BasePath + "/admin/lessons/clone",
		Summary:     "Clone a day or a week",
		Description: "Copies the scheduled lessons of a school day, or of a week from Monday to Sunday, to " +
			"another day or week in one transaction. Each copy is a one-off lesson at the same school-local " +
			"wall-clock time, with the course type, room, staff, length, capacity, kind and visibility of its " +
			"source and no bookings. Canceled lessons are not copied. A source lesson is skipped with a " +
			"reason: `class-slot` when its class has a slot in the target week within its term (generation " +
			"makes that lesson), `started` when the copy would not start in the future, `staff-inactive` " +
			"when its instructor or accompanist is no longer active, and `room-taken` when another lesson " +
			"that is not canceled, a lesson of a class that generation has not created yet, or an earlier " +
			"copy of the same clone uses the room at an overlapping time, checked in this order. Nothing is " +
			"changed or deleted, and nobody is mailed. A week's dates " +
			"must be Mondays (422 on `body.source` or `body.target`); a target equal to the source or before " +
			"today (a week: its Monday) is a 422 on `body.target`.",
	}), h.cloneLessons)
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

// LessonRefInput is the request of deleteLesson and of the operations on a
// lesson's participants.
type LessonRefInput struct {
	LessonID domain.LessonID `doc:"The lesson." path:"lessonId"`
}

// LessonCloneData is the request body of previewLessonClone and
// cloneLessons.
type LessonCloneData struct {
	Unit   string      `doc:"Copy one school day or a week from Monday to Sunday." enum:"day,week" json:"unit"`
	Source domain.Date `doc:"School date to copy from; a Monday for a week."     format:"date"    json:"source"`
	Target domain.Date `doc:"School date to copy to; a Monday for a week. It must differ from the source and not lie before today." format:"date" json:"target"`
}

// LessonCloneInput is the request of previewLessonClone and cloneLessons.
type LessonCloneInput struct {
	Body LessonCloneData
}

// LessonClone is the response body of previewLessonClone and cloneLessons.
type LessonClone struct {
	Created []LessonCloneCopy `doc:"Copies, ordered by the start of their source." json:"created" nullable:"false"`
	Skipped []LessonCloneSkip `doc:"Scheduled source lessons that are not copied, ordered by start." json:"skipped" nullable:"false"`
}

// LessonCloneCopy is a copy a clone creates.
type LessonCloneCopy struct {
	ID       domain.LessonID `doc:"The new lesson; absent in a preview." json:"id,omitzero" required:"false"`
	Source   LessonSummary   `doc:"The lesson copied. The copy has its course type, room, staff, capacity, kind and visibility, no class, no level and no bookings." json:"source"`
	StartsAt time.Time       `doc:"Start instant of the copy (UTC)." json:"startsAt"`
	EndsAt   time.Time       `doc:"End instant of the copy (UTC)."   json:"endsAt"`
}

// LessonCloneSkip is a source lesson a clone does not copy.
type LessonCloneSkip struct {
	Source   LessonSummary        `doc:"The lesson not copied." json:"source"`
	StartsAt time.Time            `doc:"Start instant its copy would have had (UTC)." json:"startsAt"`
	EndsAt   time.Time            `doc:"End instant its copy would have had (UTC)."   json:"endsAt"`
	Reason   string               `doc:"Why it is not copied: its class makes the lesson of the target week (class-slot), the copy would not start in the future (started), its instructor or accompanist is no longer active (staff-inactive), or the room is taken (room-taken); checked in this order." enum:"class-slot,started,staff-inactive,room-taken" json:"reason"`
	Lesson   *CloneConflictLesson `doc:"The lesson that uses the room; only with reason room-taken." json:"lesson,omitempty"`
}

// CloneConflictLesson names the lesson that takes the room of a copy: a
// lesson that exists, a lesson of a class that generation has not created
// yet, or another copy of the same clone.
type CloneConflictLesson struct {
	ID         domain.LessonID `doc:"Lesson ID; absent in a preview for a copy of the same clone, and for a lesson of a class that generation has not created yet." json:"id,omitzero" required:"false"`
	ClassID    domain.ClassID  `doc:"Weekly class the lesson belongs to; absent for one-off lessons and copies. Without an ID and copyOf, the lesson is one this class's generation will create." json:"classId,omitzero" required:"false"`
	CopyOf     domain.LessonID `doc:"Set when the lesson is a copy of the same clone: the lesson it copies." json:"copyOf,omitzero" required:"false"`
	StartsAt   time.Time       `doc:"Start instant (UTC)." json:"startsAt"`
	EndsAt     time.Time       `doc:"End instant (UTC)."   json:"endsAt"`
	CourseType CourseTypeRef   `json:"courseType"`
}

// LessonCloneOutput is the response of previewLessonClone and cloneLessons.
type LessonCloneOutput struct {
	Body LessonClone
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

func (h *handlers) previewLessonClone(ctx context.Context, in *LessonCloneInput) (*LessonCloneOutput, error) {
	return h.cloneOutput(ctx)(h.deps.Lessons.PreviewClone(ctx, in.Body.service()))
}

func (h *handlers) cloneLessons(ctx context.Context, in *LessonCloneInput) (*LessonCloneOutput, error) {
	return h.cloneOutput(ctx)(h.deps.Lessons.Clone(ctx, in.Body.service()))
}

func (h *handlers) cloneOutput(ctx context.Context) func(service.CloneResult, error) (*LessonCloneOutput, error) {
	return func(r service.CloneResult, err error) (*LessonCloneOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		body := LessonClone{
			Created: make([]LessonCloneCopy, 0, len(r.Created)),
			Skipped: make([]LessonCloneSkip, 0, len(r.Skipped)),
		}

		for _, c := range r.Created {
			body.Created = append(body.Created, LessonCloneCopy{
				ID: c.ID, Source: lessonSummary(c.Source), StartsAt: c.StartsAt.UTC(), EndsAt: c.EndsAt.UTC(),
			})
		}

		for _, k := range r.Skipped {
			skip := LessonCloneSkip{
				Source: lessonSummary(k.Source), StartsAt: k.StartsAt.UTC(), EndsAt: k.EndsAt.UTC(),
				Reason: string(k.Reason),
			}

			if by := k.TakenBy; by != nil {
				skip.Lesson = &CloneConflictLesson{
					ID: by.Lesson.ID, ClassID: by.Lesson.ClassID, CopyOf: by.CopyOf, StartsAt: by.Lesson.StartsAt.UTC(),
					EndsAt:     by.Lesson.EndsAt.UTC(),
					CourseType: CourseTypeRef{ID: by.CourseType.ID, Name: by.CourseType.Name},
				}
			}

			body.Skipped = append(body.Skipped, skip)
		}

		return &LessonCloneOutput{Body: body}, nil
	}
}

func (d LessonCloneData) service() service.CloneRequest {
	return service.CloneRequest{Unit: service.CloneUnit(d.Unit), Source: d.Source, Target: d.Target}
}
