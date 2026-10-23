package httpapi

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func registerTimetable(api huma.API, h *handlers) {
	register(api, AccessPublic, huma.Operation{
		OperationID: "listTerms",
		Method:      "GET",
		Path:        BasePath + "/terms",
		Summary:     "List terms",
		Description: "Returns all terms ordered by start date.",
		Tags:        []string{"timetable"},
	}, h.listTerms)

	register(api, AccessPublic, huma.Operation{
		OperationID: "listClasses",
		Method:      "GET",
		Path:        BasePath + "/terms/{termId}/classes",
		Summary:     "List the classes of a term",
		Description: "Returns the weekly classes of a term ordered by weekday (Sunday first) and start time.",
		Tags:        []string{"timetable"},
	}, h.listClasses)
}

// Term is a date range with its own timetable.
type Term struct {
	ID       domain.TermID `doc:"Term ID."                           json:"id"`
	Name     string        `doc:"Display name."                      json:"name"`
	StartsOn domain.Date   `doc:"First day of the term (inclusive)." format:"date" json:"startsOn"`
	EndsOn   domain.Date   `doc:"Last day of the term (inclusive)."  format:"date" json:"endsOn"`
}

// TermList is the response body of listTerms.
type TermList struct {
	Items []Term `doc:"Terms ordered by start date." json:"items" nullable:"false"`
}

// TermListOutput is the response of listTerms.
type TermListOutput struct {
	Body TermList
}

func (h *handlers) listTerms(ctx context.Context, _ *struct{}) (*TermListOutput, error) {
	terms, err := h.deps.Timetable.Terms(ctx)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]Term, 0, len(terms))
	for _, t := range terms {
		items = append(items, Term{ID: t.ID, Name: t.Name, StartsOn: t.StartsOn, EndsOn: t.EndsOn})
	}

	return &TermListOutput{Body: TermList{Items: items}}, nil
}

// Class is a weekly class of a term. Its members are not exposed.
type Class struct {
	ID              domain.ClassID      `doc:"Class ID."                                    json:"id"`
	TermID          domain.TermID       `doc:"Term the class belongs to."                   json:"termId"`
	CourseTypeID    domain.CourseTypeID `doc:"Course type taught."                          json:"courseTypeId"`
	RoomID          domain.RoomID       `doc:"Room the class takes place in."               json:"roomId"`
	Instructor      StaffRef            `doc:"Instructor, also when no longer active."     json:"instructor"`
	Accompanist     *StaffRef           `doc:"Accompanist, if any."                         json:"accompanist,omitempty"`
	Level           domain.Level        `doc:"Skill level the class is aimed at."           enum:"beginner,intermediate,advanced,professional,open" json:"level"`
	Weekday         int                 `doc:"Day of the week, 0 = Sunday to 6 = Saturday." json:"weekday"                                          maximum:"6"      minimum:"0"`
	StartTime       domain.TimeOfDay    `doc:"Start time (HH:MM) in the school time zone."  example:"18:00"                                         json:"startTime" pattern:"^[0-2][0-9]:[0-5][0-9]$"`
	DurationMinutes int                 `doc:"Length of each lesson in minutes."            json:"durationMinutes"                                  minimum:"1"`
	Capacity        int                 `doc:"Maximum number of participants per lesson."   json:"capacity"                                         minimum:"1"`
}

// ClassList is the response body of listClasses.
type ClassList struct {
	Items []Class `doc:"Classes ordered by weekday (Sunday first) and start time." json:"items" nullable:"false"`
}

// ClassListInput is the request of listClasses.
type ClassListInput struct {
	TermID domain.TermID `doc:"Term ID." path:"termId"`
}

// ClassListOutput is the response of listClasses.
type ClassListOutput struct {
	Body ClassList
}

func (h *handlers) listClasses(ctx context.Context, in *ClassListInput) (*ClassListOutput, error) {
	classes, err := h.deps.Timetable.Classes(ctx, in.TermID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]Class, 0, len(classes))
	for _, c := range classes {
		items = append(items, Class{
			ID:              c.ID,
			TermID:          c.TermID,
			CourseTypeID:    c.CourseTypeID,
			RoomID:          c.RoomID,
			Instructor:      StaffRef{ID: c.Instructor.ID, DisplayName: c.Instructor.DisplayName},
			Accompanist:     accompanistRef(c.Accompanist),
			Level:           c.Level,
			Weekday:         int(c.Weekday),
			StartTime:       c.StartTime,
			DurationMinutes: int(c.Duration / time.Minute),
			Capacity:        c.Capacity,
		})
	}

	return &ClassListOutput{Body: ClassList{Items: items}}, nil
}

// accompanistRef is the reference to an accompanist, nil for none.
func accompanistRef(s domain.Staff) *StaffRef {
	if s.ID.IsZero() {
		return nil
	}

	return &StaffRef{ID: s.ID, DisplayName: s.DisplayName}
}
