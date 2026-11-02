package mcpserver

import (
	"context"
	"strconv"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// personTools are the office's person tools: the search, and a person's
// bookings and passes. None returns a date of birth (ADR 0022).
func personTools() []tool {
	return []tool{{
		name: "search_persons",
		description: "Search the persons whose given name, family name, email or member number contains every " +
			"word of term, ignoring case, as the office's person search does (searchPersons; `persons " +
			"search`): at most " + strconv.Itoa(service.MaxPersonMatches) + ", ordered by family and given " +
			"name, each with its ID, name, email and member number. Merged and anonymized persons are left " +
			"out. An empty term lists the first persons. The term is neither logged nor audited. The IDs are " +
			"what person_bookings and person_passes take.",
		access:       httpapi.AccessOffice,
		personalData: true,
		annotations:  readOnly("Search persons"),
		add:          handler(searchPersons),
	}, {
		name: "person_bookings",
		description: "The bookings and waiting-list entries of one person, as the office's data export lists " +
			"them (exportPerson; `persons export`): every booking, canceled ones included, by lesson start, " +
			"with its lesson (start, end, course type), status, source, trial and late cancellation, and " +
			"every waiting-list entry, ended ones included. person names the person (ID, name, email, member " +
			"number); an anonymized person is marked anonymized, with the fixed name. A merged person is " +
			"found too; its bookings moved to the person it was merged into.",
		access:       httpapi.AccessOffice,
		personalData: true,
		annotations:  readOnly("Person bookings"),
		add:          handler(personBookings),
	}, {
		name: "person_passes",
		description: "The passes of one person (listPersonPasses; `passes list --person`): the usable ones " +
			"first, then the others, each newest sale first, with the type, the visits used and left, the " +
			"school dates it is valid from and until, its status, whether it pays for a visit today, who " +
			"paid for it if not the owner, and the bookings it paid for.",
		access:       httpapi.AccessOffice,
		personalData: true,
		annotations:  readOnly("Person passes"),
		add:          handler(personPasses),
	}}
}

// searchPersonsInput are the arguments of the search_persons tool.
type searchPersonsInput struct {
	Term string `json:"term,omitempty" jsonschema:"Words of a name, an email or a member number; every word must match."`
}

// searchPersons is the search_persons tool: the body of searchPersons.
func searchPersons(ctx context.Context, s *Server, _ service.Principal, in searchPersonsInput) (any, error) {
	persons, err := s.svc.Persons.SearchPersons(ctx, in.Term)
	if err != nil {
		return nil, err
	}

	return httpapi.NewPersonMatchList(persons), nil
}

// personInput are the arguments of the person tools.
type personInput struct {
	PersonID string `json:"personId" jsonschema:"The person (its ID, as search_persons returns it)."`
}

// personBookingsBody is the result of person_bookings: the person, and
// the bookings and waiting-list entries of its export.
type personBookingsBody struct {
	Person   httpapi.PersonMatch           `json:"person"`
	Bookings []httpapi.ExportBooking       `json:"bookings"`
	Waitlist []httpapi.ExportWaitlistEntry `json:"waitlistEntries"`
}

// personBookings is the person_bookings tool: the bookings and the
// waiting list of the person's export, without the rest of it (the date
// of birth among it).
func personBookings(ctx context.Context, s *Server, _ service.Principal, in personInput) (any, error) {
	id, err := requiredID[domain.PersonID]("personId", in.PersonID)
	if err != nil {
		return nil, err
	}

	x, err := s.svc.Persons.Export(ctx, id)
	if err != nil {
		return nil, err
	}

	body := httpapi.NewPersonExport(x)

	return personBookingsBody{
		Person: httpapi.NewPersonMatch(x.Person), Bookings: body.Bookings, Waitlist: body.Waitlist,
	}, nil
}

// personPasses is the person_passes tool: the body of listPersonPasses.
func personPasses(ctx context.Context, s *Server, _ service.Principal, in personInput) (any, error) {
	id, err := requiredID[domain.PersonID]("personId", in.PersonID)
	if err != nil {
		return nil, err
	}

	views, err := s.svc.Passes.PersonPasses(ctx, id)
	if err != nil {
		return nil, err
	}

	return httpapi.NewAdminPassList(views), nil
}
