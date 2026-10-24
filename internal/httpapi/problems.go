package httpapi

import (
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Reasons of a Conflict (ADR 0007, ADR 0008).
const (
	reasonInUse      = "in-use"
	reasonDuplicate  = "duplicate"
	reasonManaged    = "managed"
	reasonHasAccount = "has-account"
	reasonRoomTaken  = "room-taken"
	reasonPast       = "past"
	reasonGenerated  = "generated"
)

// Conflict is the problem document of a 409 the UI can explain: Reason
// says why, without internals (ADR 0007).
type Conflict struct {
	huma.ErrorModel

	Reason string          `doc:"in-use: something still refers to the record; duplicate: the record, or a value that must be unique such as a name, exists already; managed: a dependent with its own account, a staff link or dependents of its own, which its guardians may not edit; has-account: the person has an account already; room-taken: another lesson uses the room at that time (see lesson), the request may be repeated with confirmRoomOverlap; past: the lesson would start in the past, the request may be repeated with confirmPast; generated: a lesson of a class is canceled, not deleted." enum:"in-use,duplicate,managed,has-account,room-taken,past,generated" json:"reason"`
	Lesson *ConflictLesson `doc:"The lesson that uses the room; only with reason room-taken." json:"lesson,omitempty"`
}

// ConflictLesson names the lesson of a room-taken conflict.
type ConflictLesson struct {
	ID         domain.LessonID `doc:"Lesson ID."          json:"id"`
	StartsAt   time.Time       `doc:"Start instant (UTC)." json:"startsAt"`
	EndsAt     time.Time       `doc:"End instant (UTC)."   json:"endsAt"`
	CourseType CourseTypeRef   `json:"courseType"`
}

func conflict(reason, detail string) *Conflict {
	return &Conflict{
		ErrorModel: huma.ErrorModel{Title: http.StatusText(http.StatusConflict), Status: http.StatusConflict, Detail: detail},
		Reason:     reason,
	}
}

// withConflict documents the Conflict of a write operation as its 409 and
// every other error as the default response, which Huma would otherwise
// drop once an operation lists a response of its own.
func withConflict(api huma.API, op huma.Operation) huma.Operation {
	registry := api.OpenAPI().Components.Schemas
	problem := func(t reflect.Type, description string) *huma.Response {
		return &huma.Response{
			Description: description,
			Content: map[string]*huma.MediaType{
				"application/problem+json": {Schema: registry.Schema(t, true, t.Name())},
			},
		}
	}

	op.Responses = map[string]*huma.Response{
		"409":     problem(reflect.TypeFor[Conflict](), http.StatusText(http.StatusConflict)),
		"default": problem(reflect.TypeFor[huma.ErrorModel](), "Error"),
	}

	return op
}
