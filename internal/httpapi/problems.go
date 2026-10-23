package httpapi

import (
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// Reasons of a Conflict (ADR 0007).
const (
	reasonInUse     = "in-use"
	reasonDuplicate = "duplicate"
)

// Conflict is the problem document of a 409 the UI can explain: Reason
// says why, without internals (ADR 0007).
type Conflict struct {
	huma.ErrorModel

	Reason string `doc:"in-use: something still refers to the record; duplicate: the name is taken." enum:"in-use,duplicate" json:"reason"`
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
