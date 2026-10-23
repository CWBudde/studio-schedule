package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func registerFamily(api huma.API, h *handlers) {
	register(api, AccessSignedIn, huma.Operation{
		OperationID: "getMyFamily",
		Method:      http.MethodGet,
		Path:        BasePath + "/family",
		Summary:     "Get my family",
		Description: "Returns the signed-in person and their dependents (children without their own login), " +
			"each with its other guardians.",
		Tags: []string{"family"},
	}, h.getMyFamily)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "updateMyPerson",
		Method:      http.MethodPut,
		Path:        BasePath + "/family/me",
		Summary:     "Update my person data",
		Description: "Replaces the signed-in person's name, phone and birth date.",
		Tags:        []string{"family"},
	}, h.updateMyPerson)

	register(api, AccessSignedIn, huma.Operation{
		OperationID:   "addDependent",
		Method:        http.MethodPost,
		Path:          BasePath + "/family/dependents",
		Summary:       "Add a dependent",
		Description:   "Creates a person (a child without their own login) with the signed-in person as guardian.",
		Tags:          []string{"family"},
		DefaultStatus: http.StatusCreated,
	}, h.addDependent)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "updateDependent",
		Method:      http.MethodPut,
		Path:        BasePath + "/family/dependents/{personId}",
		Summary:     "Update a dependent",
		Description: "Replaces the name, phone and birth date of one of the signed-in person's dependents. " +
			"Anyone else's person is a 404.",
		Tags: []string{"family"},
	}, h.updateDependent)

	register(api, AccessSignedIn, huma.Operation{
		OperationID: "removeDependent",
		Method:      http.MethodDelete,
		Path:        BasePath + "/family/dependents/{personId}",
		Summary:     "Remove a dependent",
		Description: "Ends the signed-in person's guardianship. A dependent left without a guardian is " +
			"deleted; while it has bookings or class memberships, that is a 409 and nothing changes. " +
			"Anyone else's person is a 404.",
		Tags:          []string{"family"},
		DefaultStatus: http.StatusNoContent,
	}, h.removeDependent)
}

// PersonData is the person data a guardian edits.
type PersonData struct {
	GivenName  string      `doc:"Given name."                                json:"givenName"  maxLength:"100" minLength:"1"`
	FamilyName string      `doc:"Family name."                               json:"familyName" maxLength:"100" minLength:"1"`
	Phone      string      `doc:"Phone number, if any."                      json:"phone,omitempty" maxLength:"50"`
	BirthDate  domain.Date `doc:"Date of birth, if known; not in the future." format:"date" json:"birthDate,omitzero"`
}

// FamilyPerson is the signed-in person or one of their dependents.
type FamilyPerson struct {
	ID domain.PersonID `doc:"Person ID." json:"id"`
	PersonData
}

// GuardianName names another guardian of a dependent.
type GuardianName struct {
	GivenName  string `doc:"Given name."  json:"givenName"`
	FamilyName string `doc:"Family name." json:"familyName"`
}

// FamilyDependent is a dependent of the signed-in person.
type FamilyDependent struct {
	FamilyPerson
	Guardians []GuardianName `doc:"The dependent's other guardians." json:"guardians" nullable:"false"`
}

// Family is the response body of getMyFamily.
type Family struct {
	Me         FamilyPerson      `doc:"The signed-in person."                         json:"me"`
	Dependents []FamilyDependent `doc:"Dependents ordered by family and given name." json:"dependents" nullable:"false"`
}

// FamilyOutput is the response of getMyFamily.
type FamilyOutput struct {
	Body Family
}

// PersonDataInput is the request of updateMyPerson and addDependent.
type PersonDataInput struct {
	Body PersonData
}

// DependentInput is the request of updateDependent.
type DependentInput struct {
	PersonID domain.PersonID `doc:"The dependent." path:"personId"`
	Body     PersonData
}

// DependentRefInput is the request of removeDependent.
type DependentRefInput struct {
	PersonID domain.PersonID `doc:"The dependent." path:"personId"`
}

// FamilyPersonOutput is the response of updateMyPerson, addDependent and
// updateDependent.
type FamilyPersonOutput struct {
	Body FamilyPerson
}

func (h *handlers) getMyFamily(ctx context.Context, _ *struct{}) (*FamilyOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	f, err := h.deps.Family.Family(ctx, p)
	if err != nil {
		return nil, h.familyError(ctx, err)
	}

	out := Family{Me: familyPerson(f.Me), Dependents: make([]FamilyDependent, 0, len(f.Dependents))}
	for _, d := range f.Dependents {
		guardians := make([]GuardianName, 0, len(d.Guardians))
		for _, g := range d.Guardians {
			guardians = append(guardians, GuardianName{GivenName: g.GivenName, FamilyName: g.FamilyName})
		}

		out.Dependents = append(out.Dependents, FamilyDependent{FamilyPerson: familyPerson(d.Person), Guardians: guardians})
	}

	return &FamilyOutput{Body: out}, nil
}

func (h *handlers) updateMyPerson(ctx context.Context, in *PersonDataInput) (*FamilyPersonOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	return h.familyPerson(ctx, func() (domain.Person, error) {
		return h.deps.Family.UpdatePerson(ctx, p, in.Body.service())
	})
}

func (h *handlers) addDependent(ctx context.Context, in *PersonDataInput) (*FamilyPersonOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	return h.familyPerson(ctx, func() (domain.Person, error) {
		return h.deps.Family.AddDependent(ctx, p, in.Body.service())
	})
}

func (h *handlers) updateDependent(ctx context.Context, in *DependentInput) (*FamilyPersonOutput, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	return h.familyPerson(ctx, func() (domain.Person, error) {
		return h.deps.Family.UpdateDependent(ctx, p, in.PersonID, in.Body.service())
	})
}

func (h *handlers) removeDependent(ctx context.Context, in *DependentRefInput) (*struct{}, error) {
	p, _ := principalFrom(ctx) // AccessSignedIn

	if err := h.deps.Family.RemoveDependent(ctx, p, in.PersonID); err != nil {
		return nil, h.familyError(ctx, err)
	}

	return &struct{}{}, nil
}

// familyPerson runs change and returns the person it wrote.
func (h *handlers) familyPerson(
	ctx context.Context, change func() (domain.Person, error),
) (*FamilyPersonOutput, error) {
	person, err := change()
	if err != nil {
		return nil, h.familyError(ctx, err)
	}

	return &FamilyPersonOutput{Body: familyPerson(person)}, nil
}

func (h *handlers) familyError(ctx context.Context, err error) error {
	if errors.Is(err, service.ErrDependentInUse) {
		return huma.Error409Conflict("the dependent has bookings or class memberships")
	}

	return h.toHTTPError(ctx, err)
}

func (d PersonData) service() service.PersonData {
	return service.PersonData{GivenName: d.GivenName, FamilyName: d.FamilyName, Phone: d.Phone, BirthDate: d.BirthDate}
}

func familyPerson(p domain.Person) FamilyPerson {
	return FamilyPerson{ID: p.ID, PersonData: PersonData{
		GivenName: p.GivenName, FamilyName: p.FamilyName, Phone: p.Phone, BirthDate: p.BirthDate,
	}}
}
