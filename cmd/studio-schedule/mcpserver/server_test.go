package mcpserver

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// TestWriteGroups: the groups of --allow-writes; an unknown one is a
// field error on allowWrites.
func TestWriteGroups(t *testing.T) {
	t.Parallel()

	// No announcements group: no tool writes an announcement (ADR 0022,
	// amendment of 2026-11-03).
	want := []WriteGroup{"bookings", "lessons", "mail", "jobs"}
	if got := WriteGroups(); !slices.Equal(got, want) {
		t.Errorf("WriteGroups() = %v, want %v", got, want)
	}

	if WriteGroup("deletes").Valid() || WriteGroup("announcements").Valid() || WriteGroup("").Valid() ||
		!WriteJobs.Valid() {
		t.Error("Valid is wrong")
	}

	got, err := ParseWriteGroups([]string{"bookings", " jobs"})
	if err != nil || !slices.Equal(got, []WriteGroup{WriteBookings, WriteJobs}) {
		t.Errorf("ParseWriteGroups = %v, %v", got, err)
	}

	if got, err := ParseWriteGroups(nil); err != nil || len(got) != 0 {
		t.Errorf("ParseWriteGroups(nil) = %v, %v", got, err)
	}

	_, err = ParseWriteGroups([]string{"mail", "deletes"})

	var field *domain.FieldError
	if !errors.As(err, &field) || field.Field != "allowWrites" ||
		field.Msg != `unknown group "deletes"; the groups are bookings, lessons, mail, jobs` {
		t.Errorf("unknown group: %v", err)
	}
}

// TestNew: the doctor tool needs the command's environment, the logger
// may be left out; MCP is the SDK's server, which a client can connect to
// directly.
func TestNew(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	admin := f.principal(t, demoAdmin)

	if _, err := New(f.svc, f.actorOf(demoAdmin), admin, clock.NewFixed(testNow), Options{}, "test"); err == nil {
		t.Error("New without Checks did not fail")
	}

	s, err := New(f.svc, f.actorOf(demoAdmin), admin, clock.NewFixed(testNow), Options{Checks: checksEnv{f}}, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	clientT, serverT := mcp.NewInMemoryTransports()

	ss, err := s.MCP().Connect(t.Context(), serverT, nil)
	if err != nil {
		t.Fatal(err)
	}

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), clientT, nil)
	if err != nil {
		t.Fatal(err)
	}

	if info := cs.InitializeResult().ServerInfo; info.Name != "studio-schedule" || info.Version != "1.2.3" {
		t.Errorf("server info %+v", info)
	}

	if names := toolNames(t, cs); len(names) != 5 {
		t.Errorf("an admin's tools: %v", names)
	}

	_ = cs.Close()
	_ = ss.Wait()
}

// TestRegistry: the registry's tools have unique names and known access
// levels. A read tool reads only and touches nothing outside; a write tool
// (writeTools) has its group and says whether it is destructive and
// whether it reaches outside. Exactly the person tools (personDataTools,
// personDataWriteTools) return person data. Every input schema is a JSON
// schema object.
func TestRegistry(t *testing.T) {
	t.Parallel()

	var names, persons, writes []string

	for _, group := range registry {
		for _, tl := range group {
			if slices.Contains(names, tl.name) {
				t.Errorf("tool %s twice", tl.name)
			}

			names = append(names, tl.name)

			if _, ok := tl.access.MinRole(); !ok {
				t.Errorf("%s: access level %q", tl.name, tl.access)
			}

			a := tl.annotations
			if a.Title == "" || tl.description == "" || a.OpenWorldHint == nil {
				t.Errorf("%s: %+v", tl.name, tl)
			}

			if tl.write != "" {
				writes = append(writes, tl.name)

				if !tl.write.Valid() || a.ReadOnlyHint || a.DestructiveHint == nil {
					t.Errorf("write tool %s: %+v", tl.name, tl)
				}
			} else if !a.ReadOnlyHint || !a.IdempotentHint || *a.OpenWorldHint {
				t.Errorf("read tool %s: %+v", tl.name, tl)
			}

			if tl.personalData {
				persons = append(persons, tl.name)
			}
		}
	}

	slices.Sort(persons)
	slices.Sort(writes)

	if want := slices.Sorted(slices.Values(append(slices.Clone(personDataTools), personDataWriteTools...))); !slices.Equal(persons, want) {
		t.Errorf("the tools with person data are %v, want %v", persons, want)
	}

	if !slices.Equal(writes, writeTools) {
		t.Errorf("the write tools are %v, want %v", writes, writeTools)
	}

	f := newFixture(t)
	cs := f.session(t, demoAdmin, Options{PersonalData: true, AllowWrites: WriteGroups()}, registry)

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Tools) != len(names) {
		t.Errorf("an admin with every flag lists %d tools, the registry has %d", len(res.Tools), len(names))
	}

	for _, tl := range res.Tools {
		schema, ok := tl.InputSchema.(map[string]any)
		if !ok || schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s: input schema %v", tl.Name, tl.InputSchema)
		}

		if tl.Annotations == nil || tl.Title == "" || tl.Title != tl.Annotations.Title {
			t.Errorf("%s: title %q, annotations %+v", tl.Name, tl.Title, tl.Annotations)
		}
	}
}

// probe returns a tool of the test registry: it runs ran and returns doc.
func probe(name string, access httpapi.Access, personalData bool, write WriteGroup, ran *int) tool {
	return tool{
		name: name, description: "a test tool", access: access, personalData: personalData, write: write,
		annotations: readOnly(name),
		add: handler(func(context.Context, *Server, service.Principal, struct{}) (any, error) {
			if ran != nil {
				*ran++
			}

			return map[string]string{"tool": name}, nil
		}),
	}
}

// TestFilters: a session lists the tools the start's role reaches; a tool
// that returns person data only with PersonalData, a write tool only with
// its group. A tool not listed is unknown: calling it by name fails and
// runs nothing.
func TestFilters(t *testing.T) {
	t.Parallel()

	var ran int

	tools := [][]tool{{
		probe("public_read", httpapi.AccessPublic, false, "", &ran),
		probe("staff_persons", httpapi.AccessStaff, true, "", &ran),
		probe("office_read", httpapi.AccessOffice, false, "", &ran),
	}, {
		probe("book", httpapi.AccessOffice, true, WriteBookings, &ran),
		probe("cancel_lesson", httpapi.AccessOffice, false, WriteLessons, &ran),
		probe("run_job", httpapi.AccessAdmin, false, WriteJobs, &ran),
	}}

	f := newFixture(t)

	for _, tc := range []struct {
		as    string
		opts  Options
		tools []string
	}{
		{demoMember, Options{}, []string{"public_read"}},
		{demoMember, Options{PersonalData: true, AllowWrites: WriteGroups()}, []string{"public_read"}},
		{demoInstructor, Options{}, []string{"public_read"}},
		{demoInstructor, Options{PersonalData: true}, []string{"public_read", "staff_persons"}},
		{demoOffice, Options{}, []string{"office_read", "public_read"}},
		{demoOffice, Options{AllowWrites: []WriteGroup{WriteBookings}}, []string{"office_read", "public_read"}},
		{
			demoOffice,
			Options{PersonalData: true, AllowWrites: []WriteGroup{WriteBookings}},
			[]string{"book", "office_read", "public_read", "staff_persons"},
		},
		{
			demoOffice,
			Options{AllowWrites: []WriteGroup{WriteLessons, WriteJobs}},
			[]string{"cancel_lesson", "office_read", "public_read"},
		},
		{demoAdmin, Options{AllowWrites: []WriteGroup{WriteJobs}}, []string{"office_read", "public_read", "run_job"}},
		{
			demoAdmin,
			Options{PersonalData: true, AllowWrites: WriteGroups()},
			[]string{"book", "cancel_lesson", "office_read", "public_read", "run_job", "staff_persons"},
		},
	} {
		cs := f.session(t, tc.as, tc.opts, tools)

		if got := toolNames(t, cs); !slices.Equal(got, tc.tools) {
			t.Errorf("%s %+v: tools %v, want %v", tc.as, tc.opts, got, tc.tools)
		}
	}

	// Unlisted means uncallable: a write tool without its group, a person
	// tool without the flag.
	cs := f.session(t, demoOffice, Options{AllowWrites: []WriteGroup{WriteLessons}}, tools)

	for _, name := range []string{"book", "staff_persons", "run_job"} {
		_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name})
		if err == nil || !strings.Contains(err.Error(), `unknown tool "`+name+`"`) {
			t.Errorf("call %s: %v", name, err)
		}
	}

	if ran != 0 {
		t.Errorf("an unlisted tool ran %d times", ran)
	}

	if calls := toolCalls(t, f.st); len(calls) != 0 {
		t.Errorf("unknown tools were audited: %+v", calls)
	}

	// A listed one runs.
	if doc := document(t, cs, "cancel_lesson", nil); doc["tool"] != "cancel_lesson" || ran != 1 {
		t.Errorf("cancel_lesson: %v, ran %d", doc, ran)
	}
}
