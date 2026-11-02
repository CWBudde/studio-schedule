package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/docs"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// resource is one entry of the resource registry: a resource with a fixed
// URI, or a template whose URI names a record by its ID. A session lists
// it like a tool (access level and personalData); a read runs through
// Server.run, so it is checked and audited like a tool call, under the
// URI or the template, never the concrete URI of a template.
type resource struct {
	// uri is the resource's URI, or a template's URI template.
	uri      string
	template bool

	name, title, description string
	// mimeType is the type of the document read returns: JSON, or
	// Markdown for a markdown document.
	mimeType string

	access       httpapi.Access
	personalData bool

	// read returns the document of uri, the URI the client read, as p.
	read func(ctx context.Context, s *Server, p service.Principal, uri string) (any, error)
}

// The MIME types of the resources.
const (
	mimeJSON     = "application/json"
	mimeMarkdown = "text/markdown"
)

// The URI templates' prefixes, before the ID.
const (
	lessonPrefix = "lesson://"
	personPrefix = "person://"
)

// resources holds the resources, in the order of docs/mcp.md: by access
// level, then by URI.
var resources = []resource{{
	uri: "school://glossary", name: "glossary", title: "Glossary",
	description: "The school's words (docs/glossary.md): each domain term of the schedule as the code, the " +
		"English and the German UI name it, with notes. Use these words when you talk about the school.",
	mimeType: mimeMarkdown,
	access:   httpapi.AccessPublic,
	read: func(context.Context, *Server, service.Principal, string) (any, error) {
		return markdown(docs.Glossary), nil
	},
}, {
	uri: "school://timetable", name: "timetable", title: "Timetable",
	description: "The weekly classes of the current term, the one that includes today, as the public timetable " +
		"shows them (listClasses): weekday (0 = Sunday), start time in the school's time zone, duration, " +
		"the course type's and the room's IDs, instructor and accompanist, audience (levels, stage, ages) " +
		"and places; retired classes are left out.",
	mimeType: mimeJSON,
	access:   httpapi.AccessPublic,
	read:     timetable,
}, {
	uri: "school://settings", name: "settings", title: "School settings",
	description: "The school's settings as the account's role may read them: for the office and admins every " +
		"area of `settings get --output json` (school, branding, regional with time zone, currency and " +
		"languages, bookingRules, receipts, passes); for members and instructors the public settings " +
		"(getPublicSettings: name, legal notice, branding, time zone, language, currency).",
	mimeType: mimeJSON,
	access:   httpapi.AccessSignedIn,
	read:     settings,
}, {
	uri: lessonPrefix + "{id}", template: true, name: "lesson", title: "Lesson with participants",
	description: "A lesson's participants and waiting list, as the participants page shows them " +
		"(listLessonParticipants): every booking, canceled ones included, with name, contact data, status, " +
		"trial and payment, the waiting list in its order, and the counts and free places. The office reads " +
		"every lesson, an instructor or accompanist only the lessons they teach or accompany; any other " +
		"lesson is not found. {id} is a lesson ID from the schedule tool.",
	mimeType:     mimeJSON,
	access:       httpapi.AccessStaff,
	personalData: true,
	read:         lessonParticipants,
}, {
	uri: personPrefix + "{id}", template: true, name: "person", title: "Person",
	description: "A person with their guardians, dependents, account, linked staff members, class memberships " +
		"and the persons merged into them, as `persons show --output json` (getPerson), without the date of " +
		"birth. A merged or anonymized person is shown too, with the person it was merged into or when it " +
		"was anonymized. {id} is a person ID.",
	mimeType:     mimeJSON,
	access:       httpapi.AccessOffice,
	personalData: true,
	read:         person,
}}

// callee is the callee of a read of r: its URI or URI template.
func (r resource) callee() callee { return callee{name: r.uri, kind: "resource", access: r.access} }

// add registers r on s.
func (r resource) add(s *Server) {
	handler := func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI

		text, outcome, err := s.run(ctx, r.callee(), func(ctx context.Context, p service.Principal) (any, error) {
			return r.read(ctx, s, p, uri)
		})
		if err != nil {
			return nil, readError(uri, outcome, err)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: r.mimeType, Text: string(text)}},
		}, nil
	}

	if r.template {
		s.mcp.AddResourceTemplate(&mcp.ResourceTemplate{
			URITemplate: r.uri, Name: r.name, Title: r.title, Description: r.description, MIMEType: r.mimeType,
		}, handler)

		return
	}

	s.mcp.AddResource(&mcp.Resource{
		URI: r.uri, Name: r.name, Title: r.title, Description: r.description, MIMEType: r.mimeType,
	}, handler)
}

// readError is the JSON-RPC error of a failed read of uri: a read has no
// error result, unlike a tool call. A record that is not found is invalid
// params with the URI as its data, as the SDK answers an unlisted resource
// (SEP-2164), with the error's message; an argument that does not fit is
// invalid params; an unexpected error (errInternal) an internal error; a
// refusal an invalid request.
func readError(uri, outcome string, err error) error {
	switch outcome {
	case outcomeNotFound:
		data, _ := json.Marshal(map[string]string{"uri": uri}) // a string map always encodes

		return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error(), Data: data}
	case outcomeInvalid:
		return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: err.Error()}
	case outcomeError:
		return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: err.Error()}
	default:
		return &jsonrpc.Error{Code: jsonrpc.CodeInvalidRequest, Message: err.Error()}
	}
}

// timetable is school://timetable: the body of listClasses for the
// current term.
func timetable(ctx context.Context, s *Server, _ service.Principal, _ string) (any, error) {
	term, err := s.svc.Timetable.CurrentTerm(ctx)
	if err != nil {
		return nil, err
	}

	classes, err := s.svc.Timetable.Classes(ctx, term.ID)
	if err != nil {
		return nil, err
	}

	return httpapi.NewClassList(classes), nil
}

// settingsDocument is the document of `settings get --output json`: every
// area's response body by its field.
type settingsDocument struct {
	School       httpapi.SchoolSettings      `json:"school"`
	Branding     httpapi.BrandingSettings    `json:"branding"`
	Regional     httpapi.RegionalSettings    `json:"regional"`
	BookingRules httpapi.BookingRules        `json:"bookingRules"`
	Receipts     httpapi.ReceiptSettingsBody `json:"receipts"`
	Passes       httpapi.PassSettings        `json:"passes"`
}

// settings is school://settings: the document of `settings get` for the
// office (its access level), the body of getPublicSettings below it. The
// role is the current one, so a demoted account reads the public settings.
func settings(ctx context.Context, s *Server, p service.Principal, _ string) (any, error) {
	if office, _ := httpapi.AccessOffice.MinRole(); !p.Role.AtLeast(office) {
		public, err := s.svc.Settings.Public(ctx)
		if err != nil {
			return nil, err
		}

		return httpapi.NewPublicSettings(public), nil
	}

	school, schoolErr := s.svc.Settings.School(ctx)
	branding, brandingErr := s.svc.Settings.Branding(ctx)
	regional, regionalErr := s.svc.Settings.Regional(ctx)
	rules, rulesErr := s.svc.Settings.BookingRules(ctx)
	receipts, receiptsErr := s.svc.Receipts.Settings(ctx)
	mode, modeErr := s.svc.Passes.RedemptionMode(ctx)

	if err := errors.Join(schoolErr, brandingErr, regionalErr, rulesErr, receiptsErr, modeErr); err != nil {
		return nil, err
	}

	return settingsDocument{
		School:       httpapi.NewSchoolSettings(school),
		Branding:     httpapi.NewBrandingSettings(branding),
		Regional:     httpapi.NewRegionalSettings(regional),
		BookingRules: httpapi.NewBookingRules(rules),
		Receipts:     httpapi.NewReceiptSettings(receipts),
		Passes:       httpapi.NewPassSettings(mode),
	}, nil
}

// lessonParticipants is lesson://{id}: the body of listLessonParticipants,
// with the service's check that p manages the lesson (another lesson is
// not found).
func lessonParticipants(ctx context.Context, s *Server, p service.Principal, uri string) (any, error) {
	id, err := parseID[domain.LessonID]("id", strings.TrimPrefix(uri, lessonPrefix))
	if err != nil {
		return nil, err
	}

	lp, err := s.svc.Participants.Participants(ctx, p, id)
	if err != nil {
		return nil, err
	}

	return httpapi.NewLessonParticipants(lp), nil
}

// personBody is the body of getPerson without the date of birth, which no
// result of the server has (ADR 0022). Its BirthDate hides the embedded
// field of the same JSON name: encoding/json writes the less nested one,
// which omitempty leaves out, so the key is absent.
type personBody struct {
	httpapi.PersonDetail

	BirthDate *domain.Date `json:"birthDate,omitempty"`
}

// person is person://{id}: the document of `persons show`, without the
// date of birth.
func person(ctx context.Context, s *Server, _ service.Principal, uri string) (any, error) {
	id, err := parseID[domain.PersonID]("id", strings.TrimPrefix(uri, personPrefix))
	if err != nil {
		return nil, err
	}

	d, err := s.svc.Persons.Person(ctx, id)
	if err != nil {
		return nil, err
	}

	return personBody{PersonDetail: httpapi.NewPersonDetail(d)}, nil
}
