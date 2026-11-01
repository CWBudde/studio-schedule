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

// registerTimetableAdmin registers the office's writes to the terms, the
// weekly classes and their members (ADR 0007), and the office's lists of a
// term's classes and a class's members. The public reads are in
// timetable.go.
func registerTimetableAdmin(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"timetable"}

		return withConflict(api, op)
	}

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "createTerm",
		Method:        http.MethodPost,
		Path:          BasePath + "/terms",
		Summary:       "Create a term",
		Description:   "Adds a term. Terms may overlap, and their names need not be unique.",
		DefaultStatus: http.StatusCreated,
	}), h.createTerm)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updateTerm",
		Method:      http.MethodPut,
		Path:        BasePath + "/terms/{termId}",
		Summary:     "Update a term",
		Description: "Replaces the name and dates of a term. The lessons of its classes that the new dates " +
			"leave out are deleted while they lie ahead, still follow their class and have no bookings; " +
			"any other lesson left out is a 422 on `startsOn` or `endsOn`, whichever leaves it out. " +
			"Longer dates get the lessons that lie ahead.",
	}), h.updateTerm)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deleteTerm",
		Method:        http.MethodDelete,
		Path:          BasePath + "/terms/{termId}",
		Summary:       "Delete a term",
		Description:   "Deletes a term without classes; a term with classes is a 409 `in-use`.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteTerm)

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
			"edited one by one follow, with their bookings; a capacity below the class's member count or the " +
			"bookings of one of them is a 422 on `capacity`. A retired class generates no more lessons and " +
			"leaves the public timetable; its future lessons that still follow it and have no bookings but " +
			"the automatic ones of class members are deleted with those.",
	}), h.updateClass)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "deleteClass",
		Method:        http.MethodDelete,
		Path:          BasePath + "/classes/{classId}",
		Summary:       "Delete a class",
		Description:   "Deletes a class with its lessons while none of them has started or been booked (automatic bookings of former members aside) and it has no members; otherwise it is a 409 `in-use` and can be retired instead.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteClass)

	register(api, AccessOffice, huma.Operation{
		OperationID: "listClassMembers",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/classes/{classId}/members",
		Summary:     "List the members of a class",
		Description: "Returns the persons enrolled in a class, ordered by family and given name.",
		Tags:        []string{"timetable"},
	}, h.listClassMembers)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "addClassMember",
		Method:      http.MethodPost,
		Path:        BasePath + "/classes/{classId}/members",
		Summary:     "Add a class member",
		Description: "Enrolls a person in a class and books them into the class's lessons from `since` on that " +
			"have not started, canceled ones included, even beyond a lesson's capacity, unless they have a " +
			"booking for the lesson already (one canceled when an earlier membership ended aside). An unknown person is a 422 on `personId`, a person who is a " +
			"member already a 409 `duplicate`, a retired class a 422 on `path.classId`, and a class whose " +
			"members fill its capacity a 409 `full`.",
		DefaultStatus: http.StatusCreated,
	}), h.addClassMember)

	register(api, AccessOffice, huma.Operation{
		OperationID: "updateClassMember",
		Method:      http.MethodPut,
		Path:        BasePath + "/classes/{classId}/members/{personId}",
		Summary:     "Update a class member",
		Description: "Replaces the dates of a membership: `since` and the end of a pause, `pausedUntil` (left " +
			"out: not paused, which ends a pause). Then books the member into the class's lessons that have " +
			"not started and that they are active on but have no booking for, as `addClassMember` does (a " +
			"date they released stays released), after canceling their automatic bookings of those they " +
			"are no longer active on (a new pause, a later `since`), without a mail. A person who is not a " +
			"member is a 404, a retired class a 422 on `path.classId`.",
		Tags: []string{"timetable"},
	}, h.updateClassMember)

	register(api, AccessOffice, huma.Operation{
		OperationID: "removeClassMember",
		Method:      http.MethodDelete,
		Path:        BasePath + "/classes/{classId}/members/{personId}",
		Summary:     "Remove a class member",
		Description: "Ends a person's membership in a class and cancels their bookings of the class's lessons " +
			"that have not started, without a mail; a person who is not a member is a 404.",
		Tags:          []string{"timetable"},
		DefaultStatus: http.StatusNoContent,
	}, h.removeClassMember)
}

// ClassData is the editable part of a class.
type ClassData struct {
	CourseTypeID    domain.CourseTypeID `doc:"Course type taught."                          json:"courseTypeId"`
	RoomID          domain.RoomID       `doc:"Room the class takes place in."               json:"roomId"`
	InstructorID    domain.StaffID      `doc:"Instructor."                                  json:"instructorId"`
	AccompanistID   domain.StaffID      `doc:"Accompanist, if any."                         json:"accompanistId,omitzero" required:"false"`
	Level           domain.SkillLevel        `doc:"Skill level the class is aimed at."           enum:"beginner,intermediate,advanced,professional,open" json:"level"`
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

	return &ClassEntryListOutput{Body: NewClassEntryList(classes)}, nil
}

// NewClassEntryList is the response body of listAllClasses for classes; the
// command line's `classes list --output json` prints it too (ADR 0020).
func NewClassEntryList(classes []service.TimetableClass) ClassEntryList {
	items := make([]ClassEntry, 0, len(classes))
	for _, c := range classes {
		items = append(items, ClassEntry{Class: class(c), Retired: c.Retired})
	}

	return ClassEntryList{Items: items}
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

// TermData is the editable part of a term.
type TermData struct {
	Name     string      `doc:"Display name."                      json:"name"     maxLength:"100" minLength:"1"`
	StartsOn domain.Date `doc:"First day of the term (inclusive)." format:"date" json:"startsOn"`
	EndsOn   domain.Date `doc:"Last day of the term (inclusive)."  format:"date" json:"endsOn"`
}

// TermDataInput is the request of createTerm.
type TermDataInput struct {
	Body TermData
}

// TermUpdateInput is the request of updateTerm.
type TermUpdateInput struct {
	TermID domain.TermID `doc:"The term." path:"termId"`
	Body   TermData
}

// TermRefInput is the request of deleteTerm.
type TermRefInput struct {
	TermID domain.TermID `doc:"The term." path:"termId"`
}

// TermOutput is the response of createTerm and updateTerm.
type TermOutput struct {
	Body Term
}

func (h *handlers) createTerm(ctx context.Context, in *TermDataInput) (*TermOutput, error) {
	return h.termOutput(ctx)(h.deps.Timetable.CreateTerm(ctx, in.Body.service()))
}

func (h *handlers) updateTerm(ctx context.Context, in *TermUpdateInput) (*TermOutput, error) {
	return h.termOutput(ctx)(h.deps.Timetable.UpdateTerm(ctx, in.TermID, in.Body.service()))
}

func (h *handlers) deleteTerm(ctx context.Context, in *TermRefInput) (*struct{}, error) {
	if err := h.deps.Timetable.DeleteTerm(ctx, in.TermID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) termOutput(ctx context.Context) func(domain.Term, error) (*TermOutput, error) {
	return func(t domain.Term, err error) (*TermOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &TermOutput{Body: term(t)}, nil
	}
}

func (d TermData) service() service.TermData {
	return service.TermData{Name: d.Name, StartsOn: d.StartsOn, EndsOn: d.EndsOn}
}

// ClassMemberData is the request body of addClassMember.
type ClassMemberData struct {
	PersonID domain.PersonID `doc:"The person to enroll."           json:"personId"`
	Since    domain.Date     `doc:"Member since, if known."        format:"date" json:"since,omitzero"`
}

// ClassMember is a person enrolled in a class.
type ClassMember struct {
	Person      PersonMatch `doc:"The member."             json:"person"`
	Since       domain.Date `doc:"Member since, if known." format:"date" json:"since,omitzero"`
	PausedUntil domain.Date `doc:"The last day of the member's pause while it lasts (today or later): they are not booked automatically into lessons up to it." format:"date" json:"pausedUntil,omitzero"`
}

// ClassMemberUpdateData is the request body of updateClassMember.
type ClassMemberUpdateData struct {
	Since       domain.Date `doc:"Member since, if known."                          format:"date" json:"since,omitzero"       required:"false"`
	PausedUntil domain.Date `doc:"The last day of the pause; left out: not paused." format:"date" json:"pausedUntil,omitzero" required:"false"`
}

// ClassMemberUpdateInput is the request of updateClassMember.
type ClassMemberUpdateInput struct {
	ClassID  domain.ClassID  `doc:"The class."  path:"classId"`
	PersonID domain.PersonID `doc:"The member." path:"personId"`
	Body     ClassMemberUpdateData
}

// ClassMemberList is the response body of listClassMembers.
type ClassMemberList struct {
	Items []ClassMember `doc:"Members ordered by family and given name." json:"items" nullable:"false"`
}

// ClassMemberListOutput is the response of listClassMembers.
type ClassMemberListOutput struct {
	Body ClassMemberList
}

// ClassMemberInput is the request of addClassMember.
type ClassMemberInput struct {
	ClassID domain.ClassID `doc:"The class." path:"classId"`
	Body    ClassMemberData
}

// ClassMemberRefInput is the request of removeClassMember.
type ClassMemberRefInput struct {
	ClassID  domain.ClassID  `doc:"The class."  path:"classId"`
	PersonID domain.PersonID `doc:"The member." path:"personId"`
}

// ClassMemberOutput is the response of addClassMember.
type ClassMemberOutput struct {
	Body ClassMember
}

func (h *handlers) listClassMembers(ctx context.Context, in *ClassRefInput) (*ClassMemberListOutput, error) {
	members, err := h.deps.Timetable.ClassMembers(ctx, in.ClassID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]ClassMember, 0, len(members))
	for _, m := range members {
		items = append(items, classMember(m))
	}

	return &ClassMemberListOutput{Body: ClassMemberList{Items: items}}, nil
}

func (h *handlers) addClassMember(ctx context.Context, in *ClassMemberInput) (*ClassMemberOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	m, err := h.deps.Timetable.AddClassMember(ctx, p, domain.ClassMember{
		ClassID: in.ClassID, PersonID: in.Body.PersonID, Since: in.Body.Since,
	})
	if err != nil {
		return nil, h.memberError(ctx, err)
	}

	return &ClassMemberOutput{Body: classMember(m)}, nil
}

// memberError maps the errors of the class member writes: a full class to
// the 409 `full`, a retired one to a 422 on `path.classId`, and everything
// else like toHTTPError.
func (h *handlers) memberError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrClassFull):
		return conflict(reasonFull, "the class is full")
	case errors.Is(err, service.ErrClassRetired):
		return huma.Error422UnprocessableEntity("validation failed", &huma.ErrorDetail{
			Message: "the class is retired", Location: "path.classId",
		})
	}

	return h.toHTTPError(ctx, err)
}

func (h *handlers) updateClassMember(ctx context.Context, in *ClassMemberUpdateInput) (*ClassMemberOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	m, err := h.deps.Timetable.UpdateClassMember(ctx, p, domain.ClassMember{
		ClassID: in.ClassID, PersonID: in.PersonID, Since: in.Body.Since, PausedUntil: in.Body.PausedUntil,
	})
	if err != nil {
		return nil, h.memberError(ctx, err)
	}

	return &ClassMemberOutput{Body: classMember(m)}, nil
}

func (h *handlers) removeClassMember(ctx context.Context, in *ClassMemberRefInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	if err := h.deps.Timetable.RemoveClassMember(ctx, p, in.ClassID, in.PersonID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func classMember(m service.ClassMemberEntry) ClassMember {
	return ClassMember{Person: personMatch(m.Person), Since: m.Since, PausedUntil: m.PausedUntil}
}
