package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func registerPersons(api huma.API, h *handlers) {
	register(api, AccessOffice, huma.Operation{
		OperationID: "listPersons",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/persons",
		Summary:     "Search persons",
		Description: "Returns the persons whose given name, family name or email contains every word of `q`, " +
			"ignoring case, ordered by family and given name; at most 50. For the office's pickers, e.g. " +
			"linking a staff member to a person.",
		Tags: []string{"admin"},
	}, h.listPersons)
}

// PersonSearchInput is the request of listPersons.
type PersonSearchInput struct {
	Query string `doc:"Words to look for; empty matches everyone." maxLength:"100" query:"q"`
}

// PersonMatch is a person found by listPersons, or linked to a staff
// member.
type PersonMatch struct {
	ID         domain.PersonID `doc:"Person ID."                json:"id"`
	GivenName  string          `doc:"Given name."               json:"givenName"`
	FamilyName string          `doc:"Family name."              json:"familyName"`
	Email      string          `doc:"Email address, if known." json:"email,omitempty"`
}

// PersonMatchList is the response body of listPersons.
type PersonMatchList struct {
	Items []PersonMatch `doc:"Matching persons ordered by family and given name." json:"items" nullable:"false"`
}

// PersonMatchListOutput is the response of listPersons.
type PersonMatchListOutput struct {
	Body PersonMatchList
}

func (h *handlers) listPersons(ctx context.Context, in *PersonSearchInput) (*PersonMatchListOutput, error) {
	persons, err := h.deps.Persons.SearchPersons(ctx, in.Query)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	items := make([]PersonMatch, 0, len(persons))
	for _, p := range persons {
		items = append(items, personMatch(p))
	}

	return &PersonMatchListOutput{Body: PersonMatchList{Items: items}}, nil
}

func personMatch(p domain.Person) PersonMatch {
	return PersonMatch{ID: p.ID, GivenName: p.GivenName, FamilyName: p.FamilyName, Email: p.Email}
}
