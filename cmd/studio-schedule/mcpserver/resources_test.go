package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/cwbudde/studio-schedule/docs"
	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The resources every signed-in account lists.
var schoolResources = []string{"school://glossary", "school://settings", "school://timetable"}

// The URI templates of the resources.
const (
	lessonTemplate = "lesson://{id}"
	personTemplate = "person://{id}"
)

// TestResourceRegistry: the resources have unique URIs, known access
// levels, a name, title and description, and a JSON or Markdown type;
// the templates are lesson://{id} and person://{id}, the only resources
// that return person data.
func TestResourceRegistry(t *testing.T) {
	t.Parallel()

	var uris, templates []string

	for _, r := range resources {
		if slices.Contains(uris, r.uri) {
			t.Errorf("resource %s twice", r.uri)
		}

		uris = append(uris, r.uri)

		if _, ok := r.access.MinRole(); !ok {
			t.Errorf("%s: access level %q", r.uri, r.access)
		}

		if r.name == "" || r.title == "" || r.description == "" || r.read == nil ||
			(r.mimeType != mimeJSON && r.mimeType != mimeMarkdown) {
			t.Errorf("%s: %+v", r.uri, r)
		}

		if r.template {
			templates = append(templates, r.uri)
		}

		if r.personalData != r.template {
			t.Errorf("%s: personal data %t, template %t", r.uri, r.personalData, r.template)
		}

		if c := r.callee(); c != (callee{name: r.uri, kind: "resource", access: r.access}) {
			t.Errorf("%s: callee %+v", r.uri, c)
		}
	}

	if !slices.Equal(templates, []string{lessonTemplate, personTemplate}) {
		t.Errorf("templates %v", templates)
	}
}

// TestResourceListing: every account lists the school's resources; the
// templates only with PersonalData and only if the start's role reaches
// their level: lesson://{id} from staff, person://{id} from the office.
// The listing gives each its name, title, description and MIME type.
func TestResourceListing(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	for _, tc := range []struct {
		as        string
		opts      Options
		templates []string
	}{
		{demoMember, Options{}, nil},
		{demoMember, Options{PersonalData: true, AllowWrites: WriteGroups()}, nil},
		{demoInstructor, Options{}, nil},
		{demoInstructor, Options{PersonalData: true}, []string{lessonTemplate}},
		{demoOffice, Options{}, nil},
		{demoOffice, Options{PersonalData: true}, []string{lessonTemplate, personTemplate}},
		{demoAdmin, Options{}, nil},
		{demoAdmin, Options{PersonalData: true, AllowWrites: WriteGroups()}, []string{lessonTemplate, personTemplate}},
	} {
		uris, templates := resourceURIs(t, f.session(t, tc.as, tc.opts, registry))

		if !slices.Equal(uris, schoolResources) || !slices.Equal(templates, tc.templates) {
			t.Errorf("%s %+v: resources %v, templates %v; want %v", tc.as, tc.opts, uris, templates, tc.templates)
		}
	}

	cs := f.session(t, demoAdmin, Options{PersonalData: true}, registry)

	res, err := cs.ListResources(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := cs.ListResourceTemplates(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	listed := map[string][4]string{}
	for _, r := range res.Resources {
		listed[r.URI] = [4]string{r.Name, r.Title, r.Description, r.MIMEType}
	}

	for _, r := range tmpl.ResourceTemplates {
		listed[r.URITemplate] = [4]string{r.Name, r.Title, r.Description, r.MIMEType}
	}

	for _, r := range resources {
		if got := listed[r.uri]; got != [4]string{r.name, r.title, r.description, r.mimeType} {
			t.Errorf("%s listed as %q", r.uri, got)
		}
	}
}

// TestResourceUnlisted: a template not listed is unknown to the session
// (Review focus 3): reading a real person or lesson by its URI fails as
// the SDK fails any unknown resource, nothing is read and nothing audited.
func TestResourceUnlisted(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ella := demoPerson(t, f, "Ella", "Amberly")
	own, _ := demoLessons(t, f)

	for _, tc := range []struct {
		as   string
		opts Options
		uris []string
	}{
		{demoOffice, Options{}, []string{personPrefix + ella.ID.String(), lessonPrefix + own.String()}},
		{demoAdmin, Options{}, []string{personPrefix + ella.ID.String(), lessonPrefix + own.String()}},
		{demoInstructor, Options{}, []string{lessonPrefix + own.String()}},
		{demoInstructor, Options{PersonalData: true}, []string{personPrefix + ella.ID.String()}},
		{demoMember, Options{PersonalData: true}, []string{personPrefix + ella.ID.String(), lessonPrefix + own.String()}},
	} {
		cs := f.session(t, tc.as, tc.opts, registry)

		for _, uri := range tc.uris {
			c, rpcErr := readResource(t, cs, uri)
			if rpcErr == nil || rpcErr.Code != jsonrpc.CodeInvalidParams || rpcErr.Message != "Resource not found" ||
				string(rpcErr.Data) != `{"uri":"`+uri+`"}` {
				t.Errorf("%s %+v: read %s: %+v, %+v", tc.as, tc.opts, uri, c, rpcErr)
			}
		}
	}

	if calls := toolCalls(t, f.st); len(calls) != 0 {
		t.Errorf("unlisted resources were audited: %+v", calls)
	}
}

// TestResourceGlossary: school://glossary is docs/glossary.md as it is,
// Markdown, for every account; its read is audited like a tool call.
func TestResourceGlossary(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	member := f.principal(t, demoMember)

	c, rpcErr := readResource(t, f.session(t, demoMember, Options{}, registry), "school://glossary")
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}

	if c.URI != "school://glossary" || c.MIMEType != "text/markdown" || c.Text != docs.Glossary ||
		!strings.HasPrefix(c.Text, "# ") {
		t.Errorf("glossary %s %s: %.80q", c.URI, c.MIMEType, c.Text)
	}

	calls := toolCalls(t, f.st)
	if len(calls) != 1 || calls[0].ActorID != member.AccountID ||
		calls[0].Details != (domain.AuditDetails{Tool: "school://glossary", Outcome: "ok", Via: via}) {
		t.Errorf("audit events %+v", calls)
	}
}

// TestResourceTimetable: school://timetable is the body of listClasses of
// the current term, the same for every account.
func TestResourceTimetable(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	c, rpcErr := readResource(t, f.session(t, demoMember, Options{}, registry), "school://timetable")
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}

	if c.URI != "school://timetable" || c.MIMEType != "application/json" {
		t.Errorf("timetable %s %s", c.URI, c.MIMEType)
	}

	golden(t, "timetable.json", c.Text)

	// Parity: the classes of the current term, as listClasses encodes them.
	term, err := f.svc.Timetable.CurrentTerm(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	classes, err := f.svc.Timetable.Classes(t.Context(), term.ID)
	if err != nil {
		t.Fatal(err)
	}

	if want, _ := json.Marshal(httpapi.NewClassList(classes)); c.Text != string(want) || len(classes) == 0 {
		t.Errorf("timetable differs from listClasses of %s (%d classes)", term.ID, len(classes))
	}

	if text := readText(t, f.session(t, demoOffice, Options{PersonalData: true}, registry), "school://timetable"); text != c.Text {
		t.Error("the office's timetable differs from the member's")
	}

	if calls := toolCalls(t, f.st); len(calls) != 2 || calls[0].Details.Tool != "school://timetable" ||
		calls[0].Details.Outcome != "ok" {
		t.Errorf("audit events %+v", calls)
	}
}

// TestResourceTimetableNoTerm: without a term that includes today the
// timetable is not found.
func TestResourceTimetableNoTerm(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	f.svc = app.NewServices(app.Deps{
		Store: f.st, Clock: clock.NewFixed(time.Date(2031, 1, 7, 8, 0, 0, 0, time.UTC)), IDs: offsetIDs{ids.NewSequence()},
	})

	cs := f.session(t, demoMember, Options{}, registry)

	_, rpcErr := readResource(t, cs, "school://timetable")
	if rpcErr == nil || rpcErr.Code != jsonrpc.CodeInvalidParams ||
		rpcErr.Message != "current term: not found: no term includes today" ||
		string(rpcErr.Data) != `{"uri":"school://timetable"}` {
		t.Errorf("read: %+v", rpcErr)
	}

	if calls := toolCalls(t, f.st); len(calls) != 1 || calls[0].Details.Outcome != "not-found" {
		t.Errorf("audit events %+v", calls)
	}
}

// TestResourceSettings: school://settings is the body of getPublicSettings
// for a member and an instructor, every area of `settings get` for the
// office and admins; the role is the current one, so an office account
// demoted since the start reads the public settings.
func TestResourceSettings(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	member := readText(t, f.session(t, demoMember, Options{}, registry), "school://settings")
	golden(t, "settings-member.json", member)

	public, err := f.svc.Settings.Public(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if want, _ := json.Marshal(httpapi.NewPublicSettings(public)); member != string(want) {
		t.Error("a member's settings differ from getPublicSettings")
	}

	if instructor := readText(t, f.session(t, demoInstructor, Options{}, registry), "school://settings"); instructor != member {
		t.Error("an instructor's settings differ from a member's")
	}

	office := readText(t, f.session(t, demoOffice, Options{}, registry), "school://settings")
	golden(t, "settings-office.json", office)

	var areas map[string]json.RawMessage
	if err := json.Unmarshal([]byte(office), &areas); err != nil {
		t.Fatal(err)
	}

	keys := make([]string, 0, len(areas))
	for k := range areas {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	if want := []string{"bookingRules", "branding", "passes", "receipts", "regional", "school"}; !slices.Equal(keys, want) {
		t.Errorf("the office's areas %v, want %v", keys, want)
	}

	if admin := readText(t, f.session(t, demoAdmin, Options{}, registry), "school://settings"); admin != office {
		t.Error("an admin's settings differ from the office's")
	}

	demoted := f.sessionOf(t, f.principal(t, demoOffice),
		func(context.Context) (service.Principal, error) { return f.principal(t, demoMember), nil }, Options{}, registry)
	if text := readText(t, demoted, "school://settings"); text != member {
		t.Errorf("a demoted office account reads %s", text)
	}
}

// TestResourceSettingsMissing: without school settings, both the public
// settings and the office's areas are not found.
func TestResourceSettingsMissing(t *testing.T) {
	t.Parallel()

	f := fixtureOn(noSchool{newFixture(t).st})

	for _, as := range []string{demoMember, demoOffice} {
		_, rpcErr := readResource(t, f.session(t, as, Options{}, registry), "school://settings")
		if rpcErr == nil || rpcErr.Code != jsonrpc.CodeInvalidParams || !strings.Contains(rpcErr.Message, "not found") ||
			string(rpcErr.Data) != `{"uri":"school://settings"}` {
			t.Errorf("%s: %+v", as, rpcErr)
		}
	}

	calls := toolCalls(t, f.st)
	if len(calls) != 2 || calls[0].Details.Outcome != "not-found" || calls[1].Details.Outcome != "not-found" {
		t.Errorf("audit events %+v", calls)
	}
}

// TestResourceLesson: lesson://{id} is the body of listLessonParticipants:
// the instructor reads a lesson she teaches, the office the same document;
// a lesson she neither teaches nor accompanies is not found for her, but
// not for the office. Its audit event names the template, not the lesson.
func TestResourceLesson(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	own, other := demoLessons(t, f)
	teacher := f.session(t, demoInstructor, Options{PersonalData: true}, registry)

	c, rpcErr := readResource(t, teacher, lessonPrefix+own.String())
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}

	if c.URI != lessonPrefix+own.String() || c.MIMEType != "application/json" {
		t.Errorf("lesson %s %s", c.URI, c.MIMEType)
	}

	instructor := c.Text

	golden(t, "resource-lesson.json", instructor)

	var doc map[string]any
	if err := json.Unmarshal([]byte(instructor), &doc); err != nil {
		t.Fatal(err)
	}

	if bookings, _ := doc["bookings"].([]any); len(bookings) == 0 {
		t.Errorf("the lesson has no bookings: %s", instructor)
	}

	office := f.session(t, demoOffice, Options{PersonalData: true}, registry)
	if text := readText(t, office, lessonPrefix+own.String()); text != instructor {
		t.Error("the office's lesson differs from the instructor's")
	}

	// Parity: the participants as listLessonParticipants encodes them.
	lp, err := f.svc.Participants.Participants(t.Context(), f.principal(t, demoOffice), own)
	if err != nil {
		t.Fatal(err)
	}

	if want, _ := json.Marshal(httpapi.NewLessonParticipants(lp)); instructor != string(want) {
		t.Error("the lesson differs from listLessonParticipants")
	}

	_ = readText(t, office, lessonPrefix+other.String())

	_, rpcErr = readResource(t, teacher, lessonPrefix+other.String())
	if rpcErr == nil || rpcErr.Code != jsonrpc.CodeInvalidParams || !strings.Contains(rpcErr.Message, "not found") ||
		string(rpcErr.Data) != `{"uri":"`+lessonPrefix+other.String()+`"}` {
		t.Errorf("another's lesson: %+v", rpcErr)
	}

	calls := toolCalls(t, f.st)

	outcomes := make([]string, 0, len(calls))
	for _, e := range calls {
		outcomes = append(outcomes, e.Details.Outcome)

		if e.Details.Tool != lessonTemplate {
			t.Errorf("audited as %q", e.Details.Tool)
		}

		if raw, _ := json.Marshal(e); strings.Contains(string(raw), own.String()) || strings.Contains(string(raw), other.String()) {
			t.Errorf("the audit event names the lesson: %s", raw)
		}
	}

	if want := []string{"ok", "ok", "ok", "not-found"}; !slices.Equal(outcomes, want) {
		t.Errorf("outcomes %v, want %v", outcomes, want)
	}
}

// TestResourcePerson: person://{id} is the body of getPerson (`persons
// show`) without the date of birth, of a person who has one; its audit
// event names the template, not the person.
func TestResourcePerson(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ella := demoPerson(t, f, "Ella", "Amberly")
	uri := personPrefix + ella.ID.String()

	d, err := f.svc.Persons.Person(t.Context(), ella.ID)
	if err != nil {
		t.Fatal(err)
	}

	if d.Person.BirthDate.IsZero() {
		t.Fatal("the demo person has no date of birth")
	}

	c, rpcErr := readResource(t, f.session(t, demoOffice, Options{PersonalData: true}, registry), uri)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}

	if c.URI != uri || c.MIMEType != "application/json" {
		t.Errorf("person %s %s", c.URI, c.MIMEType)
	}

	golden(t, "resource-person.json", c.Text)

	for _, key := range []string{"birthDate", "dateOfBirth", d.Person.BirthDate.String()} {
		if strings.Contains(c.Text, key) {
			t.Errorf("the person has %q: %s", key, c.Text)
		}
	}

	// Parity: getPerson's body less its birthDate.
	var got, want map[string]any
	if err := json.Unmarshal([]byte(c.Text), &got); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(httpapi.NewPersonDetail(d))
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}

	if _, ok := want["birthDate"]; !ok {
		t.Errorf("getPerson has no birthDate: %s", raw)
	}

	delete(want, "birthDate")

	gotJSON, _ := json.Marshal(got)
	if wantJSON, _ := json.Marshal(want); string(gotJSON) != string(wantJSON) {
		t.Errorf("person differs from getPerson without birthDate:\n%s\n%s", gotJSON, wantJSON)
	}

	if admin := readText(t, f.session(t, demoAdmin, Options{PersonalData: true}, registry), uri); admin != c.Text {
		t.Error("an admin's person differs from the office's")
	}

	calls := toolCalls(t, f.st)
	if len(calls) != 2 || calls[0].Details != (domain.AuditDetails{Tool: personTemplate, Outcome: "ok", Via: via}) {
		t.Errorf("audit events %+v", calls)
	}

	for _, e := range calls {
		if raw, _ := json.Marshal(e); strings.Contains(string(raw), ella.ID.String()) {
			t.Errorf("the audit event names the person: %s", raw)
		}
	}
}

// TestResourceUnknownID: an ID of no record is not found, with the URI as
// the error's data and the service's message; so is an empty ID, which
// reads the zero ID. An ID that is no ID is invalid, and neither the log
// nor an audit event repeats it (Review focus 2).
func TestResourceUnknownID(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	cs := f.session(t, demoOffice, Options{PersonalData: true}, registry)
	unknown := "00000000-0000-7000-8000-00000000ffff"
	zero := "00000000-0000-0000-0000-000000000000"

	cases := []struct {
		uri, tool, message, outcome string
	}{
		{personPrefix + unknown, personTemplate, "person " + unknown + ": person: not found", "not-found"},
		{lessonPrefix + unknown, lessonTemplate, "participants of lesson " + unknown + ": lesson: not found", "not-found"},
		{personPrefix, personTemplate, "person " + zero + ": person: not found", "not-found"},
		{lessonPrefix, lessonTemplate, "participants of lesson " + zero + ": lesson: not found", "not-found"},
		{personPrefix + "Zebulon", personTemplate, "validation failed: id is not an ID", "invalid"},
		{lessonPrefix + "Zebulon", lessonTemplate, "validation failed: id is not an ID", "invalid"},
	}

	want := make([]string, 0, len(cases))

	for _, tc := range cases {
		data := ""
		if tc.outcome == "not-found" {
			data = `{"uri":"` + tc.uri + `"}`
		}

		_, rpcErr := readResource(t, cs, tc.uri)
		if rpcErr == nil || rpcErr.Code != jsonrpc.CodeInvalidParams || rpcErr.Message != tc.message ||
			string(rpcErr.Data) != data {
			t.Errorf("read %s: %+v", tc.uri, rpcErr)
		}

		want = append(want, tc.tool+" "+tc.outcome)
	}

	calls := toolCalls(t, f.st)

	outcomes := make([]string, 0, len(calls))
	for _, e := range calls {
		outcomes = append(outcomes, e.Details.Tool+" "+e.Details.Outcome)

		if raw, _ := json.Marshal(e); strings.Contains(string(raw), "Zebulon") || strings.Contains(string(raw), "ffff") {
			t.Errorf("the audit event has the ID: %s", raw)
		}
	}

	if !slices.Equal(outcomes, want) {
		t.Errorf("audit events %v, want %v", outcomes, want)
	}

	if log := f.log.String(); strings.Contains(log, "Zebulon") || strings.Contains(log, "level=ERROR") {
		t.Errorf("log %q", log)
	}
}

// TestResourceRefused: an account disabled or demoted since the start is
// refused on its next read: an invalid request naming the resource's
// template, the read does not run, and the refusal is audited as the
// start's principal.
func TestResourceRefused(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		actor   func(member service.Principal) (service.Principal, error)
		message string
	}{
		"disabled": {
			actor: func(service.Principal) (service.Principal, error) {
				return service.Principal{}, service.ErrActorInactive
			},
			message: service.ErrActorInactive.Error(),
		},
		"demoted": {
			actor:   func(member service.Principal) (service.Principal, error) { return member, nil },
			message: "forbidden: the account's role member is below office, which the resource person://{id} needs",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := &countingStore{Store: newFixture(t).st}
			f := fixtureOn(st)
			office, member := f.principal(t, demoOffice), f.principal(t, demoMember)
			ella := demoPerson(t, f, "Ella", "Amberly")

			cs := f.sessionOf(t, office, func(context.Context) (service.Principal, error) { return tc.actor(member) },
				Options{PersonalData: true}, registry)

			before := st.reads()

			_, rpcErr := readResource(t, cs, personPrefix+ella.ID.String())
			if rpcErr == nil || rpcErr.Code != jsonrpc.CodeInvalidRequest || rpcErr.Message != tc.message ||
				len(rpcErr.Data) != 0 {
				t.Errorf("read: %+v", rpcErr)
			}

			if n := st.reads() - before; n != 0 {
				t.Errorf("the refused read ran %d read-only transactions", n)
			}

			calls := toolCalls(t, f.st)
			if len(calls) != 1 || calls[0].ActorID != office.AccountID ||
				calls[0].Details != (domain.AuditDetails{Tool: personTemplate, Outcome: "forbidden", Via: via}) {
				t.Errorf("audit events %+v", calls)
			}
		})
	}
}

// TestResourceStoreFails: a store that fails is an internal error for the
// client, and the log has the error under the resource's URI, for each
// read: the actor, the public and the office's settings, the timetable's
// term and its classes, a lesson and a person.
func TestResourceStoreFails(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		as  string
		uri string
		// static resolves the actor without the store, so the failure
		// reaches the read; the nth read-only transaction of the call
		// fails, without static the first is the actor's.
		static bool
		nth    int
	}{
		"actor":           {as: demoMember, uri: "school://glossary", nth: 1},
		"public settings": {as: demoMember, uri: "school://settings", static: true, nth: 1},
		"office settings": {as: demoOffice, uri: "school://settings", static: true, nth: 1},
		"term":            {as: demoMember, uri: "school://timetable", static: true, nth: 1},
		"classes":         {as: demoMember, uri: "school://timetable", static: true, nth: 2},
		"lesson":          {as: demoOffice, uri: lessonTemplate, static: true, nth: 1},
		"person":          {as: demoOffice, uri: personTemplate, static: true, nth: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := &countingStore{Store: newFixture(t).st}
			f := fixtureOn(st)
			p := f.principal(t, tc.as)
			own, _ := demoLessons(t, f)
			ella := demoPerson(t, f, "Ella", "Amberly")

			actor := f.actorOf(tc.as)
			if tc.static {
				actor = func(context.Context) (service.Principal, error) { return p, nil }
			}

			cs := f.sessionOf(t, p, actor, Options{PersonalData: true}, registry)

			uri := strings.NewReplacer(lessonTemplate, lessonPrefix+own.String(),
				personTemplate, personPrefix+ella.ID.String()).Replace(tc.uri)

			st.failNth(tc.nth, errors.New("connection reset"))

			_, rpcErr := readResource(t, cs, uri)
			if rpcErr == nil || rpcErr.Code != jsonrpc.CodeInternalError || rpcErr.Message != "internal error" ||
				len(rpcErr.Data) != 0 {
				t.Errorf("read %s: %+v", uri, rpcErr)
			}

			if log := f.log.String(); !strings.Contains(log, "level=ERROR") ||
				!strings.Contains(log, `msg="mcp: a resource call failed" resource=`+tc.uri) ||
				!strings.Contains(log, "connection reset") {
				t.Errorf("log %q", log)
			}

			calls := toolCalls(t, f.st)
			if len(calls) != 1 || calls[0].Details != (domain.AuditDetails{Tool: tc.uri, Outcome: "error", Via: via}) {
				t.Errorf("audit events %+v", calls)
			}
		})
	}
}

// TestResourceNotAudited: a read whose audit event cannot be written
// fails as an internal error and returns nothing of the document.
func TestResourceNotAudited(t *testing.T) {
	t.Parallel()

	st := &faultStore{Store: newFixture(t).st}
	f := fixtureOn(st)
	cs := f.session(t, demoMember, Options{}, registry)

	st.failing(store.ReadWrite, errors.New("disk full"))

	_, rpcErr := readResource(t, cs, "school://glossary")
	if rpcErr == nil || rpcErr.Code != jsonrpc.CodeInternalError || rpcErr.Message != "internal error" {
		t.Errorf("read: %+v", rpcErr)
	}

	if log := f.log.String(); !strings.Contains(log, `msg="mcp: a resource call could not be audited" resource=school://glossary`) ||
		!strings.Contains(log, "disk full") {
		t.Errorf("log %q", log)
	}
}

// demoPerson returns the demo person of the name.
func demoPerson(t *testing.T, f *fixture, given, family string) domain.Person {
	t.Helper()

	var persons []domain.Person

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		persons, err = tx.Persons().List(ctx)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(persons, func(p domain.Person) bool { return p.GivenName == given && p.FamilyName == family })
	if i < 0 {
		t.Fatalf("no demo person %s %s", given, family)
	}

	return persons[i]
}

// demoLessons returns two lessons of the fortnights around testNow that have
// bookings: own, which the instructor login teaches, and other, which she
// neither teaches nor accompanies.
func demoLessons(t *testing.T, f *fixture) (own, other domain.LessonID) {
	t.Helper()

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		staff, err := tx.Staff().List(ctx)
		if err != nil {
			return err
		}

		i := slices.IndexFunc(staff, func(s domain.Staff) bool { return s.DisplayName == "Alina Starling" })
		if i < 0 {
			return errors.New("no staff Alina Starling")
		}

		alina := staff[i].ID

		lessons, err := tx.Lessons().ListRange(ctx, testNow.AddDate(0, 0, -14), testNow.AddDate(0, 0, 14))
		if err != nil {
			return err
		}

		for _, l := range lessons {
			bookings, err := tx.Bookings().ListByLesson(ctx, l.ID)
			if err != nil {
				return err
			}

			switch {
			case len(bookings) == 0:
			case l.InstructorID == alina && own.IsZero():
				own = l.ID
			case l.InstructorID != alina && l.AccompanistID != alina && other.IsZero():
				other = l.ID
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if own.IsZero() || other.IsZero() {
		t.Fatalf("demo lessons %v, %v", own, other)
	}

	return own, other
}

// countingStore counts the read-only transactions begun and fails the one
// failNth names.
type countingStore struct {
	store.Store

	mu       sync.Mutex
	readOnly int
	failAt   int
	err      error
}

// failNth fails the nth read-only transaction from now with err.
func (s *countingStore) failNth(n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failAt, s.err = s.readOnly+n, err
}

// reads returns the number of read-only transactions begun.
func (s *countingStore) reads() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readOnly
}

func (s *countingStore) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	if mode == store.ReadOnly {
		s.mu.Lock()
		s.readOnly++
		fail := s.readOnly == s.failAt
		s.mu.Unlock()

		if fail {
			return s.err
		}
	}

	return s.Store.InTx(ctx, mode, fn)
}
