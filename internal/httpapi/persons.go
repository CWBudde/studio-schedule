package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// registerPersons registers the office's persons view (ADR 0008): the
// search, a person's detail with its family links, account, staff links and
// class memberships, edits of every person field, the deletion of a person
// nothing refers to, the guardian links, and an account for an existing
// person.
func registerPersons(api huma.API, h *handlers) {
	write := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"admin"}

		return withConflict(api, op)
	}

	register(api, AccessOffice, huma.Operation{
		OperationID: "listPersons",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/persons",
		Summary:     "Search persons",
		Description: "Returns the persons whose given name, family name, email or member number contains every " +
			"word of `q`, ignoring case, ordered by family and given name; at most 50; merged persons are left " +
			"out. For the office's pickers, e.g. linking a staff member to a person.",
		Tags: []string{"admin"},
	}, h.listPersons)

	register(api, AccessOffice, huma.Operation{
		OperationID: "getPerson",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/persons/{personId}",
		Summary:     "Get a person",
		Description: "Returns every field of a person with its guardians, dependents, account, linked staff " +
			"members and class memberships.",
		Tags: []string{"admin"},
	}, h.getPerson)

	register(api, AccessOffice, write(huma.Operation{
		OperationID:   "createPerson",
		Method:        http.MethodPost,
		Path:          BasePath + "/admin/persons",
		Summary:       "Create a person",
		Description:   "Adds a person without an account, e.g. a second parent or a staff member's record.",
		DefaultStatus: http.StatusCreated,
	}), h.createPerson)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "updatePerson",
		Method:      http.MethodPut,
		Path:        BasePath + "/admin/persons/{personId}",
		Summary:     "Update a person",
		Description: "Replaces every field of a person, including the email and the office's notes. The " +
			"email of the person's account, its login name, stays as it is.",
	}), h.updatePerson)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "deletePerson",
		Method:      http.MethodDelete,
		Path:        BasePath + "/admin/persons/{personId}",
		Summary:     "Delete a person",
		Description: "Deletes a person, e.g. one created by mistake. While anything refers to it (an account, a " +
			"staff link, bookings, class memberships, passes they own or paid for, or a guardian link in either " +
			"direction) that is a 409 " +
			"`in-use` and nothing changes.",
		DefaultStatus: http.StatusNoContent,
	}), h.deletePerson)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "addGuardian",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/persons/{personId}/guardians",
		Summary:     "Add a guardian",
		Description: "Links a guardian to the person, e.g. a second parent. A guardian linked already is a 409 " +
			"`duplicate`; an unknown guardian, the person itself, or one of its dependents (directly or through " +
			"others, which would make the links a cycle) is a 422 on `guardianId`.",
		DefaultStatus: http.StatusCreated,
	}), h.addGuardian)

	register(api, AccessOffice, huma.Operation{
		OperationID: "removeGuardian",
		Method:      http.MethodDelete,
		Path:        BasePath + "/admin/persons/{personId}/guardians/{guardianId}",
		Summary:     "Remove a guardian",
		Description: "Ends a guardianship. The person stays, even without a guardian left; a guardian who is " +
			"not linked is a 404.",
		Tags:          []string{"admin"},
		DefaultStatus: http.StatusNoContent,
	}, h.removeGuardian)

	register(api, AccessOffice, write(huma.Operation{
		OperationID: "createPersonAccount",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/persons/{personId}/account",
		Summary:     "Create an account for a person",
		Description: "Creates an unconfirmed account without a password for an existing person and mails it an " +
			"invitation, whose link (valid for 72 hours) sets the password and confirms the account. A " +
			"person with an account is a 409 `has-account`, an email another account has a 409 `duplicate`. " +
			"Only an admin may create an admin account; for the office that is a 403.",
		DefaultStatus: http.StatusCreated,
	}), h.createPersonAccount)
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
	MemberNo   string          `doc:"Member number, if any."   json:"memberNo,omitempty"`
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

	return &PersonMatchListOutput{Body: PersonMatchList{Items: personMatches(persons)}}, nil
}

func personMatch(p domain.Person) PersonMatch {
	return PersonMatch{ID: p.ID, GivenName: p.GivenName, FamilyName: p.FamilyName, Email: p.Email, MemberNo: p.MemberNo}
}

// PersonRecord is every field of a person, as the office edits it.
type PersonRecord struct {
	GivenName  string      `doc:"Given name."                                 json:"givenName"  maxLength:"100" minLength:"1"`
	FamilyName string      `doc:"Family name."                                json:"familyName" maxLength:"100" minLength:"1"`
	Email      string      `doc:"Contact email, if any; not the login name." json:"email,omitempty" maxLength:"254"`
	Phone      string      `doc:"Phone number, if any."                       json:"phone,omitempty" maxLength:"50"`
	BirthDate  domain.Date `doc:"Date of birth, if known; not in the future." format:"date" json:"birthDate,omitzero"`
	Notes      string      `doc:"The office's internal notes, if any."        json:"notes,omitempty" maxLength:"2000"`
	MemberNo   string      `doc:"The school's member number, if any; unique (a number another person has is a 409 duplicate). The member import finds a person by it." json:"memberNo,omitempty" maxLength:"40"`
}

// AdminPerson is a person with every field.
type AdminPerson struct {
	ID domain.PersonID `doc:"Person ID." json:"id"`
	PersonRecord
}

// PersonAccount is the account of a person.
type PersonAccount struct {
	ID     domain.AccountID `doc:"Account ID."                 json:"id"`
	Email  string           `doc:"Email address (login name)." json:"email"`
	Role   string           `doc:"Role of the account." enum:"member,instructor,office,admin" json:"role"`
	Status string           `doc:"pending: the email is not confirmed yet (an invitation or a registration not completed); active: it may sign in; disabled: it may no longer sign in." enum:"pending,active,disabled" json:"status"`
}

// PersonClass is a class membership of a person.
type PersonClass struct {
	ClassID        domain.ClassID   `doc:"Class ID."                                    json:"classId"`
	TermID         domain.TermID    `doc:"Term of the class."                           json:"termId"`
	TermName       string           `doc:"Name of the term."                            json:"termName"`
	CourseTypeName string           `doc:"Name of the class's course type."             json:"courseTypeName"`
	Weekday        int              `doc:"Day of the week, 0 = Sunday to 6 = Saturday." json:"weekday" maximum:"6" minimum:"0"`
	StartTime      domain.TimeOfDay `doc:"Start time (HH:MM) in the school time zone."  example:"18:00" json:"startTime" pattern:"^[0-2][0-9]:[0-5][0-9]$"`
	Retired        bool             `doc:"Whether the class is retired."                json:"retired"`
	Since          domain.Date      `doc:"Member since, if known."                      format:"date" json:"since,omitzero"`
}

// PersonDetail is the response body of getPerson.
type PersonDetail struct {
	AdminPerson
	Guardians  []PersonMatch  `doc:"Guardians ordered by family and given name."          json:"guardians"  nullable:"false"`
	Dependents []PersonMatch  `doc:"Dependents ordered by family and given name."         json:"dependents" nullable:"false"`
	Account    *PersonAccount `doc:"The person's account, if any."                         json:"account,omitempty"`
	Staff      []StaffEntry   `doc:"Staff members linked to the person, by display name." json:"staff"      nullable:"false"`
	Classes    []PersonClass  `doc:"Class memberships ordered by term, weekday and time." json:"classes"    nullable:"false"`
	Merged     []PersonMatch  `doc:"Persons merged into this one; their receipts are listed as this person's." json:"merged" nullable:"false"`
	MergedInto *PersonMatch   `doc:"The person this one was merged into; present only for such a hidden record, which cannot be changed." json:"mergedInto,omitempty"`
}

// PersonRefInput is the request of getPerson and deletePerson.
type PersonRefInput struct {
	PersonID domain.PersonID `doc:"The person." path:"personId"`
}

// PersonRecordInput is the request of createPerson.
type PersonRecordInput struct {
	Body PersonRecord
}

// PersonUpdateInput is the request of updatePerson.
type PersonUpdateInput struct {
	PersonID domain.PersonID `doc:"The person." path:"personId"`
	Body     PersonRecord
}

// GuardianInput is the request of addGuardian.
type GuardianInput struct {
	PersonID domain.PersonID `doc:"The dependent." path:"personId"`
	Body     struct {
		GuardianID domain.PersonID `doc:"The person to link as guardian." json:"guardianId"`
	}
}

// GuardianRefInput is the request of removeGuardian.
type GuardianRefInput struct {
	PersonID   domain.PersonID `doc:"The dependent." path:"personId"`
	GuardianID domain.PersonID `doc:"The guardian."  path:"guardianId"`
}

// PersonAccountInput is the request of createPersonAccount.
type PersonAccountInput struct {
	PersonID domain.PersonID `doc:"The person." path:"personId"`
	Body     struct {
		Email string `doc:"Email address; it becomes the login name and gets the invitation." format:"email" json:"email" maxLength:"254"`
		Role  string `doc:"Role of the account; only an admin may create an admin." enum:"member,instructor,office,admin" json:"role"`
	}
}

// PersonDetailOutput is the response of getPerson.
type PersonDetailOutput struct {
	Body PersonDetail
}

// AdminPersonOutput is the response of createPerson and updatePerson.
type AdminPersonOutput struct {
	Body AdminPerson
}

// PersonMatchOutput is the response of addGuardian.
type PersonMatchOutput struct {
	Body PersonMatch
}

// PersonAccountOutput is the response of createPersonAccount.
type PersonAccountOutput struct {
	Body PersonAccount
}

func (h *handlers) getPerson(ctx context.Context, in *PersonRefInput) (*PersonDetailOutput, error) {
	d, err := h.deps.Persons.Person(ctx, in.PersonID)
	if err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	out := PersonDetail{
		AdminPerson: adminPerson(d.Person),
		Guardians:   personMatches(d.Guardians),
		Dependents:  personMatches(d.Dependents),
		Staff:       make([]StaffEntry, 0, len(d.Staff)),
		Classes:     make([]PersonClass, 0, len(d.Classes)),
		Merged:      personMatches(d.Merged),
	}

	if d.Person.Merged() {
		into := personMatch(d.MergedInto)
		out.MergedInto = &into
	}

	if d.Account != nil {
		acc := personAccount(*d.Account)
		out.Account = &acc
	}

	for _, s := range d.Staff {
		out.Staff = append(out.Staff, staffEntry(service.StaffEntry{Staff: s, Person: d.Person}))
	}

	for _, c := range d.Classes {
		out.Classes = append(out.Classes, PersonClass{
			ClassID: c.Class.ID, TermID: c.Term.ID, TermName: c.Term.Name, CourseTypeName: c.CourseType.Name,
			Weekday: int(c.Class.Weekday), StartTime: c.Class.StartTime, Retired: c.Class.Retired, Since: c.Since,
		})
	}

	return &PersonDetailOutput{Body: out}, nil
}

func (h *handlers) createPerson(ctx context.Context, in *PersonRecordInput) (*AdminPersonOutput, error) {
	return h.adminPerson(ctx)(h.deps.Persons.CreatePerson(ctx, in.Body.service()))
}

func (h *handlers) updatePerson(ctx context.Context, in *PersonUpdateInput) (*AdminPersonOutput, error) {
	return h.adminPerson(ctx)(h.deps.Persons.UpdatePerson(ctx, in.PersonID, in.Body.service()))
}

func (h *handlers) deletePerson(ctx context.Context, in *PersonRefInput) (*struct{}, error) {
	if err := h.deps.Persons.DeletePerson(ctx, in.PersonID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) addGuardian(ctx context.Context, in *GuardianInput) (*PersonMatchOutput, error) {
	g, err := h.deps.Persons.AddGuardian(ctx, in.PersonID, in.Body.GuardianID)

	switch {
	case errors.Is(err, service.ErrGuardianLinked):
		return nil, conflict(reasonDuplicate, "the guardian is linked already")
	case err != nil:
		return nil, h.toHTTPError(ctx, err)
	}

	return &PersonMatchOutput{Body: personMatch(g)}, nil
}

func (h *handlers) removeGuardian(ctx context.Context, in *GuardianRefInput) (*struct{}, error) {
	if err := h.deps.Persons.RemoveGuardian(ctx, in.PersonID, in.GuardianID); err != nil {
		return nil, h.toHTTPError(ctx, err)
	}

	return &struct{}{}, nil
}

func (h *handlers) createPersonAccount(ctx context.Context, in *PersonAccountInput) (*PersonAccountOutput, error) {
	p, _ := principalFrom(ctx) // AccessOffice

	acc, err := h.deps.Auth.CreateAccount(ctx, p, in.PersonID, service.NewAccount{
		Email: in.Body.Email, Role: domain.Role(in.Body.Role),
	})

	switch {
	case errors.Is(err, service.ErrPersonHasAccount):
		return nil, conflict(reasonHasAccount, "the person has an account already")
	case errors.Is(err, service.ErrEmailInUse):
		return nil, conflict(reasonDuplicate, "the email address belongs to another account")
	case err != nil:
		return nil, h.toHTTPError(ctx, err)
	}

	return &PersonAccountOutput{Body: personAccount(acc)}, nil
}

// adminPerson returns a function that maps the result of a person write to
// its response.
func (h *handlers) adminPerson(ctx context.Context) func(domain.Person, error) (*AdminPersonOutput, error) {
	return func(p domain.Person, err error) (*AdminPersonOutput, error) {
		if err != nil {
			return nil, h.toHTTPError(ctx, err)
		}

		return &AdminPersonOutput{Body: adminPerson(p)}, nil
	}
}

func (r PersonRecord) service() service.PersonRecord {
	return service.PersonRecord{
		GivenName: r.GivenName, FamilyName: r.FamilyName, Email: r.Email,
		Phone: r.Phone, BirthDate: r.BirthDate, Notes: r.Notes, MemberNo: r.MemberNo,
	}
}

func adminPerson(p domain.Person) AdminPerson {
	return AdminPerson{ID: p.ID, PersonRecord: PersonRecord{
		GivenName: p.GivenName, FamilyName: p.FamilyName, Email: p.Email,
		Phone: p.Phone, BirthDate: p.BirthDate, Notes: p.Notes, MemberNo: p.MemberNo,
	}}
}

func personMatches(persons []domain.Person) []PersonMatch {
	out := make([]PersonMatch, 0, len(persons))
	for _, p := range persons {
		out = append(out, personMatch(p))
	}

	return out
}

func personAccount(a domain.Account) PersonAccount {
	status := "active"

	switch {
	case !a.DisabledAt.IsZero():
		status = "disabled"
	case a.ConfirmedAt.IsZero():
		status = "pending"
	}

	return PersonAccount{ID: a.ID, Email: a.Email, Role: string(a.Role), Status: status}
}
