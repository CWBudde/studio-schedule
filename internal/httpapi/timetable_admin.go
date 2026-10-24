package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerTimetableAdmin registers the office's writes to the weekly
// classes (ADR 0007), and the office's list of a term's classes. The public
// reads are in timetable.go.
func registerTimetableAdmin(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"timetable"}

		return withConflict(api, op)
	}

	register(api, AccessOffice, huma.Operation{
		OperationID: "listAllClasses",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/terms/{termId}/classes",
		Summary:     "List all classes of a term",
		Description: "Returns the weekly classes of a term like `listClasses`, including retired classes, " +
			"which the public list leaves out.",
		Tags: []string{"timetable"},
	}, h.listAllClasses)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "createClass",
		Method:      http.MethodPost,
		Path:        BasePath + "/terms/{termId}/classes",
		Summary:     "Create a class",
		Description: "Adds a weekly class to a term and generates its lessons that lie ahead. An unknown " +
			"course type, room or staff member is a 422 on the field that names it.",
		DefaultStatus: http.StatusCreated,
	}), h.createClass)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateClass",
		Method:      http.MethodPut,
		Path:        BasePath + "/classes/{classId}",
		Summary:     "Update a class",
		Description: "Replaces the fields of a class; its term stays. The future lessons that were not " +
			"edited one by one follow, with their bookings; a capacity below the bookings of one of them is " +
			"a 422 on `capacity`. A retired class generates no more lessons and leaves the public timetable; " +
			"its future lessons that still follow it and have no bookings are deleted.",
	}), h.updateClass)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deleteClass",
		Method:        http.MethodDelete,
		Path:          BasePath + "/classes/{classId}",
		Summary:       "Delete a class",
		Description:   "Deletes a class with its lessons while none of them has started or been booked and it has no members; otherwise it is a 409 `in-use` and can be retired instead.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteClass)
}

// ClassData is the editable part of a class.
type ClassData struct {
	CourseTypeID    domain.CourseTypeID `doc:"Course type taught."                          json:"courseTypeId"`
	RoomID          domain.RoomID       `doc:"Room the class takes place in."               json:"roomId"`
	InstructorID    domain.StaffID      `doc:"Instructor."                                  json:"instructorId"`
	AccompanistID   domain.StaffID      `doc:"Accompanist, if any."                         json:"accompanistId,omitzero" required:"false"`
	Level           domain.Level        `doc:"Skill level the class is aimed at."           enum:"beginner,intermediate,advanced,professional,open" json:"level"`
	Weekday         int                 `doc:"Day of the week, 0 = Sunday to 6 = Saturday." json:"weekday"                                          maximum:"6"      minimum:"0"`
	StartTime       domain.TimeOfDay    `doc:"Start time (HH:MM) in the school time zone."  example:"18:00"                                         json:"startTime" pattern:"^[0-2][0-9]:[0-5][0-9]$"`
	DurationMinutes int                 `doc:"Length of each lesson in minutes."            json:"durationMinutes"                                  maximum:"1440"   minimum:"1"`
	Capacity        int                 `doc:"Maximum number of participants per lesson."   json:"capacity"                                         maximum:"1000"   minimum:"1"`
	Retired         bool                `doc:"Whether the class is retired: it generates no more lessons and leaves the public timetable." json:"retired,omitempty" required:"false"`
}

// ClassEntry is a class in the office's list, with what the public Class
// leaves out: whether it is retired.
type ClassEntry struct {
	Class

	Retired bool `doc:"Whether the class is retired." json:"retired"`
}

// ClassEntryList is the response body of listAllClasses.
type ClassEntryList struct {
	Items []ClassEntry `doc:"Classes ordered by weekday (Sunday first) and start time." json:"items" nullable:"false"`
}

// ClassEntryListOutput is the response of listAllClasses.
type ClassEntryListOutput struct {
	Body ClassEntryList
}

// ClassCreateInput is the request of createClass.
type ClassCreateInput struct {
	TermID domain.TermID `doc:"Term ID." path:"termId"`
	Body   ClassData
}

// ClassUpdateInput is the request of updateClass.
type ClassUpdateInput struct {
	ClassID domain.ClassID `doc:"The class." path:"classId"`
	Body    ClassData
}

// ClassRefInput is the request of deleteClass.
type ClassRefInput struct {
	ClassID domain.ClassID `doc:"The class." path:"classId"`
}

// ClassOutput is the response of createClass and updateClass.
type ClassOutput struct {
	Body ClassEntry
}

func (h *handlers) listAllClasses(ctx context.Context, in *ClassListInput) (*ClassEntryListOutput, error) {
	classes, err := h.deps.Timetable.AllClasses(ctx, in.TermID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]ClassEntry, 0, len(classes))
	for _, c := range classes {
		items = append(items, ClassEntry{Class: class(c), Retired: c.Retired})
	}

	return &ClassEntryListOutput{Body: ClassEntryList{Items: items}}, nil
}

func (h *handlers) createClass(ctx context.Context, in *ClassCreateInput) (*ClassOutput, error) {
	return h.classOutput(ctx)(h.deps.Timetable.CreateClass(ctx, in.TermID, in.Body.service()))
}

func (h *handlers) updateClass(ctx context.Context, in *ClassUpdateInput) (*ClassOutput, error) {
	return h.classOutput(ctx)(h.deps.Timetable.UpdateClass(ctx, in.ClassID, in.Body.service()))
}

func (h *handlers) deleteClass(ctx context.Context, in *ClassRefInput) (*struct{}, error) {
	if err := h.deps.Timetable.DeleteClass(ctx, in.ClassID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) classOutput(ctx context.Context) func(service.TimetableClass, error) (*ClassOutput, error) {
	return func(c service.TimetableClass, err error) (*ClassOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &ClassOutput{Body: ClassEntry{Class: class(c), Retired: c.Retired}}, nil
	}
}

func (d ClassData) service() service.ClassData {
	return service.ClassData{
		CourseTypeID: d.CourseTypeID, RoomID: d.RoomID, InstructorID: d.InstructorID, AccompanistID: d.AccompanistID,
		Level: d.Level, Weekday: time.Weekday(d.Weekday), StartTime: d.StartTime,
		Duration: time.Duration(d.DurationMinutes) * time.Minute, Capacity: d.Capacity, Retired: d.Retired,
	}
}
