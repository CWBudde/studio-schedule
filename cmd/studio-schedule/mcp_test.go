package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// mcpServer is a run of `mcp` in-process: the client's session, and the
// command's stderr, to read once the session is closed.
type mcpServer struct {
	*mcp.ClientSession

	stderr *bytes.Buffer
	done   chan int
}

// mcpSession runs `mcp args…` of r in a goroutine, its stdin and stdout
// pipes to a go-sdk client, and returns the connected session. Its
// cleanup closes the session, which ends the server, and fails the test
// unless the command exits with status 0.
func mcpSession(t *testing.T, r cliRun, args ...string) *mcpServer {
	t.Helper()

	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()

	s := &mcpServer{stderr: &bytes.Buffer{}, done: make(chan int, 1)}

	go func() {
		code := r.run(t, serverIn, serverOut, s.stderr, append([]string{"mcp"}, args...)...)
		_ = serverOut.Close() // the client reads EOF if the command ends first
		s.done <- code
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)

	session, err := client.Connect(t.Context(), &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatalf("connect: %v; exit %d, stderr %q", err, <-s.done, s.stderr)
	}

	s.ClientSession = session

	t.Cleanup(func() { s.close(t) })

	return s
}

// close closes the session once and checks that the command exited with
// status 0; it returns the command's stderr.
func (s *mcpServer) close(t *testing.T) string {
	t.Helper()

	if s.done == nil {
		return s.stderr.String()
	}

	_ = s.Close()

	if code := <-s.done; code != cli.ExitOK {
		t.Errorf("mcp exited with %d, stderr %q", code, s.stderr)
	}

	s.done = nil

	return s.stderr.String()
}

// toolNames lists the session's tools by name.
func (s *mcpServer) toolNames(t *testing.T) []string {
	t.Helper()

	res, err := s.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}

	slices.Sort(names)

	return names
}

// callTool calls the tool name with args and returns its result's text.
func (s *mcpServer) callTool(t *testing.T, name string, args any) (text string, isError bool) {
	t.Helper()

	res, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}

	if len(res.Content) != 1 {
		t.Fatalf("call %s: %d contents", name, len(res.Content))
	}

	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("call %s: content %T", name, res.Content[0])
	}

	return tc.Text, res.IsError
}

// document calls the tool name with args and decodes its document; an
// error result fails the test.
func (s *mcpServer) document(t *testing.T, name string, args any) any {
	t.Helper()

	text, isError := s.callTool(t, name, args)
	if isError {
		t.Fatalf("call %s: error result %q", name, text)
	}

	return decodeJSON(t, text)
}

// decodeJSON decodes a JSON document.
func decodeJSON(t *testing.T, text string) any {
	t.Helper()

	var doc any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("%v: %q", err, text)
	}

	return doc
}

// commandDocument runs a command with --output json and decodes its
// document.
func commandDocument(t *testing.T, r cliRun, args ...string) any {
	t.Helper()

	res := r.exec(t, append(args, "--output", "json")...)
	if res.code != cli.ExitOK {
		t.Fatalf("%v: exit %d, stderr %q", args, res.code, res.stderr)
	}

	return decodeJSON(t, res.stdout)
}

// toolCalls returns the mcp-tool-called audit events of st, oldest first.
func toolCalls(t *testing.T, st store.Store) []domain.AuditEvent {
	t.Helper()

	events := accountsRead(t, st, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) {
		return tx.Audit().List(ctx, 1000)
	})

	var out []domain.AuditEvent

	for _, e := range slices.Backward(events) {
		if e.Action == domain.AuditMCPToolCalled {
			out = append(out, e)
		}
	}

	return out
}

// TestMCPTools: a session lists the tools whose access level the role of
// --as reaches; every signed-in role reaches the schedule, so no role
// starts without a tool. --personal-data adds the person tools the role
// reaches: the staff's lesson, the office's person search, a person's
// bookings and passes, and the receipts, and the admins' mail log.
func TestMCPTools(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		for _, tc := range []struct {
			as       string
			tools    []string
			personal []string
		}{
			{demoMember, []string{"schedule"}, []string{"schedule"}},
			{demoInstructor, []string{"free_rooms", "schedule"}, []string{"free_rooms", "lesson", "schedule"}},
			{
				demoOffice,
				[]string{"free_rooms", "schedule", "statistics"},
				[]string{
					"free_rooms", "lesson", "person_bookings", "person_passes", "receipts", "schedule", "search_persons",
					"statistics",
				},
			},
			{
				demoAdmin,
				[]string{"db_check", "doctor", "free_rooms", "schedule", "statistics"},
				[]string{
					"db_check", "doctor", "free_rooms", "lesson", "mail_log", "person_bookings", "person_passes", "receipts",
					"schedule", "search_persons", "statistics",
				},
			},
		} {
			for _, flags := range [][]string{nil, {"--personal-data"}, {"--allow-writes", "bookings,jobs"}} {
				s := mcpSession(t, r, append([]string{"--as", tc.as}, flags...)...)

				want := tc.tools
				if slices.Contains(flags, "--personal-data") {
					want = tc.personal
				}

				if got := s.toolNames(t); !slices.Equal(got, want) {
					t.Errorf("%s %v: tools %v, want %v", tc.as, flags, got, want)
				}

				s.close(t)
			}
		}
	})
}

// wantToolCall fails unless e is the audit event of a call of tool by the
// account acc through MCP with outcome: no target, never the arguments.
func wantToolCall(t *testing.T, e domain.AuditEvent, acc domain.AccountID, tool, outcome string) {
	t.Helper()

	want := domain.AuditDetails{
		Tool: tool, Outcome: outcome, Via: domain.AuditVia{Channel: domain.ChannelMCP, OSUser: testOSUser},
	}

	if e.Action != domain.AuditMCPToolCalled || e.ActorID != acc || !e.TargetID.IsZero() ||
		!e.TargetPersonID.IsZero() || !reflect.DeepEqual(e.Details, want) {
		t.Errorf("audit event %+v, want %s by %s with %+v", e, domain.AuditMCPToolCalled, acc, want)
	}
}

// TestMCPAudit: every call writes one mcp-tool-called event, actor the
// account of --as, via MCP with the OS user, with the tool and the
// outcome, and no target.
func TestMCPAudit(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		office, _ := accountOf(t, st, demoOffice)
		s := mcpSession(t, r, "--as", demoOffice)

		s.document(t, "schedule", nil)

		if _, isError := s.callTool(t, "statistics", map[string]any{"range": "decade"}); !isError {
			t.Error("an unknown range is no error result")
		}

		calls := toolCalls(t, st)
		if len(calls) != 2 {
			t.Fatalf("%d tool calls audited, want 2", len(calls))
		}

		wantToolCall(t, calls[0], office.ID, "schedule", "ok")
		wantToolCall(t, calls[1], office.ID, "statistics", "invalid")
	})
}

// TestMCPPrompts: the office lists the prompts, waitlist_invitations only
// with --personal-data; a get renders this week in the school's language
// and is audited like a tool call.
func TestMCPPrompts(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		office, _ := accountOf(t, st, demoOffice)

		for _, tc := range []struct {
			flags   []string
			prompts []string
		}{
			{nil, []string{"canceled_lesson_announcement", "weekly_overview"}},
			{[]string{"--personal-data"}, []string{"canceled_lesson_announcement", "waitlist_invitations", "weekly_overview"}},
		} {
			s := mcpSession(t, r, append([]string{"--as", demoOffice}, tc.flags...)...)

			res, err := s.ListPrompts(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}

			names := make([]string, 0, len(res.Prompts))
			for _, p := range res.Prompts {
				names = append(names, p.Name)
			}

			slices.Sort(names)

			if !slices.Equal(names, tc.prompts) {
				t.Errorf("%v: prompts %v, want %v", tc.flags, names, tc.prompts)
			}

			s.close(t)
		}

		s := mcpSession(t, r, "--as", demoOffice)

		got, err := s.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "weekly_overview"})
		if err != nil {
			t.Fatal(err)
		}

		if len(got.Messages) != 1 {
			t.Fatalf("messages %+v", got.Messages)
		}

		text, _ := got.Messages[0].Content.(*mcp.TextContent)
		if text == nil || !strings.HasPrefix(text.Text,
			"Give me an overview of the school's week from Monday 2026-09-21 through Sunday 2026-09-27.") ||
			!strings.Contains(text.Text, "Europe/Berlin") {
			t.Errorf("weekly_overview: %+v", got.Messages[0].Content)
		}

		calls := toolCalls(t, st)
		if len(calls) != 1 {
			t.Fatalf("%d calls audited, want 1", len(calls))
		}

		wantToolCall(t, calls[0], office.ID, "weekly_overview", "ok")
	})
}

// TestMCPAccountChanges (review focus 1): an account disabled or demoted
// after the server started is refused on its next call: an error result,
// the tool does not run, and one audit event records the refusal.
func TestMCPAccountChanges(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		office, _ := accountOf(t, st, demoOffice)
		s := mcpSession(t, r, "--as", demoOffice)

		s.document(t, "statistics", nil)

		wantOK(t, "disable", r.exec(t, "accounts", "disable", "--as", demoAdmin, "--yes", demoOffice))

		text, isError := s.callTool(t, "statistics", nil)
		if !isError || text != "--as "+demoOffice+": not found: the account is not confirmed or is disabled" {
			t.Errorf("disabled: result %q (error %t)", text, isError)
		}

		calls := toolCalls(t, st)
		if len(calls) != 2 {
			t.Fatalf("%d tool calls audited, want 2", len(calls))
		}

		wantToolCall(t, calls[1], office.ID, "statistics", "forbidden")

		// Enabled again, then demoted to member: the office's tool is
		// refused, the schedule, which members reach, still runs.
		wantOK(t, "enable", r.exec(t, "accounts", "enable", "--as", demoAdmin, demoOffice))
		wantOK(t, "demote", r.exec(t, "accounts", "set-role", "--as", demoAdmin, demoOffice, "member"))

		text, isError = s.callTool(t, "statistics", nil)
		if !isError || text != "forbidden: the account's role member is below office, which the tool statistics needs" {
			t.Errorf("demoted: result %q (error %t)", text, isError)
		}

		s.document(t, "schedule", nil)

		calls = toolCalls(t, st)
		if len(calls) != 4 {
			t.Fatalf("%d tool calls audited, want 4", len(calls))
		}

		wantToolCall(t, calls[2], office.ID, "statistics", "forbidden")
		wantToolCall(t, calls[3], office.ID, "schedule", "ok")
	})
}

// TestMCPArgumentsStayPrivate (review focus 2): a call's arguments reach
// neither the log (the command's stderr) nor an audit event, not even
// when the call fails on them.
func TestMCPArgumentsStayPrivate(t *testing.T) {
	t.Parallel()

	const secret = "Zebulon"

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		s := mcpSession(t, r, "--as", demoAdmin)

		for _, call := range []struct {
			tool string
			args map[string]any
		}{
			{"schedule", map[string]any{"location": secret}},
			{"schedule", map[string]any{"from": secret}},
			{"schedule", map[string]any{secret: "2026-09-21"}},
			{"statistics", map[string]any{"range": secret}},
			{"db_check", map[string]any{"only": []string{secret}}},
			{"free_rooms", map[string]any{"date": secret, "start": "18:00", "durationMinutes": 60}},
			{"free_rooms", map[string]any{"date": "2026-09-22", "start": secret, "durationMinutes": 60}},
			{"free_rooms", map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": secret}},
			{"free_rooms", map[string]any{"date": "2026-09-22", "start": "18:00", "durationMinutes": 60, "locationId": secret}},
		} {
			if text, isError := s.callTool(t, call.tool, call.args); !isError {
				t.Errorf("%s %v: no error result but %q", call.tool, call.args, text)
			}
		}

		if stderr := s.close(t); strings.Contains(stderr, secret) {
			t.Errorf("the log has the argument: %q", stderr)
		}

		calls := toolCalls(t, st)
		if len(calls) != 9 {
			t.Fatalf("%d tool calls audited, want 9", len(calls))
		}

		for _, e := range calls {
			if b, err := json.Marshal(e); err != nil || bytes.Contains(b, []byte(secret)) {
				t.Errorf("audit event %s (%v) has the argument", b, err)
			}

			if e.Details.Outcome != "invalid" {
				t.Errorf("outcome %q, want invalid", e.Details.Outcome)
			}
		}
	})
}

// TestMCPCommandErrors: what the command refuses before the server
// speaks, with nothing on stdout.
func TestMCPCommandErrors(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		for _, tc := range []struct {
			args   []string
			code   int
			stderr string
		}{
			{
				[]string{"--as", "nobody@example.org"},
				cli.ExitNotFound,
				"error: --as nobody@example.org: not found: no account has this email\n",
			},
			{
				[]string{"--as", demoOffice, "--allow-writes", "bookings,zebras"},
				cli.ExitUsage,
				`error: --allow-writes: unknown group "zebras"; the groups are bookings, lessons, announcements, ` +
					"mail, jobs\n",
			},
			{[]string{"--as", demoOffice, "--output", "json"}, cli.ExitUsage, ""},
			{
				[]string{"--as", demoOffice, "--output", "text", "--yes"},
				cli.ExitUsage,
				"error: --yes: studio-schedule mcp asks no confirmation; the client approves a write\n",
			},
			{nil, cli.ExitUsage, "error: studio-schedule mcp acts as an account: give --as <email> or set STUDIO_AS\n"},
		} {
			res := r.exec(t, append([]string{"mcp"}, tc.args...)...)
			if res.code != tc.code || res.stdout != "" || (tc.stderr != "" && res.stderr != tc.stderr) {
				t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.args, res.code, res.stdout,
					res.stderr, tc.code, tc.stderr)
			}
		}

		// --output json: the refusal as a problem document on stderr.
		res := r.exec(t, "mcp", "--as", demoOffice, "--output", "json")
		if !strings.Contains(res.stderr, `"detail": "--output json: studio-schedule mcp prints MCP messages only"`) {
			t.Errorf("--output json: stderr %q", res.stderr)
		}
	})
}

// without returns doc with the keys of each object in the array at path
// (keys of nested objects) removed, and how many it removed.
func without(doc any, path []string, keys ...string) (any, int) {
	removed := 0

	var walk func(v any, path []string)

	walk = func(v any, path []string) {
		obj, ok := v.(map[string]any)
		if !ok {
			return
		}

		if len(path) > 0 {
			items, _ := obj[path[0]].([]any)
			for _, item := range items {
				walk(item, path[1:])
			}

			return
		}

		for _, k := range keys {
			if _, ok := obj[k]; ok {
				delete(obj, k)
				removed++
			}
		}
	}

	walk(doc, path)

	return doc, removed
}

// TestMCPParity: each tool returns its command's document on the same
// data; without --personal-data, without the person data.
func TestMCPParity(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		week := map[string]any{"from": "2026-09-21", "to": "2026-09-27"}
		lessons := commandDocument(t, r, "lessons", "list", "--as", demoOffice, "--from", "2026-09-21", "--to",
			"2026-09-27")

		s := mcpSession(t, r, "--as", demoOffice, "--personal-data")
		if got := s.document(t, "schedule", week); !reflect.DeepEqual(got, lessons) {
			t.Errorf("schedule with --personal-data:\n%v\nlessons list:\n%v", got, lessons)
		}

		s.close(t)

		s = mcpSession(t, r, "--as", demoOffice)

		want, removed := without(lessons, []string{"lessons"}, "bookedFor", "waitlist", "renter")
		if removed == 0 {
			t.Error("the week has no person data to leave out: the test proves nothing")
		}

		if got := s.document(t, "schedule", week); !reflect.DeepEqual(got, want) {
			t.Errorf("schedule:\n%v\nlessons list without person data:\n%v", got, want)
		}

		for _, tc := range []struct {
			args map[string]any
			cmd  []string
		}{
			{map[string]any{}, nil},
			{map[string]any{"range": "term"}, []string{"--range", "term"}},
			{map[string]any{"from": "2026-09-01", "to": "2026-09-30"}, []string{"--from", "2026-09-01", "--to", "2026-09-30"}},
		} {
			stats, _ := without(commandDocument(t, r, append([]string{"stats", "--as", demoOffice}, tc.cmd...)...), nil,
				"topParticipants")
			if got := s.document(t, "statistics", tc.args); !reflect.DeepEqual(got, stats) {
				t.Errorf("statistics %v:\n%v\nstats without the top participants:\n%v", tc.args, got, stats)
			}
		}

		s.close(t)

		s = mcpSession(t, r, "--as", demoAdmin)

		if got, want := s.document(t, "doctor", nil), commandDocument(t, r, "doctor"); !reflect.DeepEqual(got, want) {
			t.Errorf("doctor:\n%v\ndoctor --output json:\n%v", got, want)
		}

		for _, tc := range []struct {
			args map[string]any
			cmd  []string
		}{
			{map[string]any{}, nil},
			{map[string]any{"only": []string{"capacity", "passes"}}, []string{"--only", "capacity,passes"}},
		} {
			want := commandDocument(t, r, append([]string{"db", "check"}, tc.cmd...)...)
			if got := s.document(t, "db_check", tc.args); !reflect.DeepEqual(got, want) {
				t.Errorf("db_check %v:\n%v\ndb check --output json:\n%v", tc.args, got, want)
			}
		}
	})
}

// resourceURIs lists the session's resources and its resource templates
// by URI.
func (s *mcpServer) resourceURIs(t *testing.T) (resources, templates []string) {
	t.Helper()

	res, err := s.ListResources(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range res.Resources {
		resources = append(resources, r.URI)
	}

	tmpl, err := s.ListResourceTemplates(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range tmpl.ResourceTemplates {
		templates = append(templates, r.URITemplate)
	}

	slices.Sort(resources)
	slices.Sort(templates)

	return resources, templates
}

// readResource reads uri and returns its one content; a failed read is
// the JSON-RPC error.
func (s *mcpServer) readResource(t *testing.T, uri string) (*mcp.ResourceContents, error) {
	t.Helper()

	res, err := s.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, err
	}

	if len(res.Contents) != 1 || res.Contents[0].URI != uri {
		t.Fatalf("read %s: contents %+v", uri, res.Contents)
	}

	return res.Contents[0], nil
}

// resourceDocument reads the JSON resource uri and decodes it; a failed
// read fails the test.
func (s *mcpServer) resourceDocument(t *testing.T, uri string) any {
	t.Helper()

	c, err := s.readResource(t, uri)
	if err != nil {
		t.Fatalf("read %s: %v", uri, err)
	}

	if c.MIMEType != "application/json" {
		t.Errorf("read %s: MIME type %q", uri, c.MIMEType)
	}

	return decodeJSON(t, c.Text)
}

// wantReadError fails unless err is the JSON-RPC error of a failed read
// with code and message.
func wantReadError(t *testing.T, uri string, err error, code int64, message string) {
	t.Helper()

	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != code || rpcErr.Message != message {
		t.Errorf("read %s: error %v, want code %d with %q", uri, err, code, message)
	}
}

// The school's resources, which every signed-in role reads, and the
// templates of the person data.
var (
	schoolResources = []string{"school://glossary", "school://settings", "school://timetable"}
	lessonTemplate  = "lesson://{id}"
	personTemplate  = "person://{id}"
)

// TestMCPResources: a session lists the school's resources for every
// signed-in role, and the templates that return person data only with
// --personal-data and only for the roles that reach them: lesson://{id}
// for staff, person://{id} for the office.
func TestMCPResources(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		for _, tc := range []struct {
			as        string
			templates []string
		}{
			{demoMember, nil},
			{demoInstructor, []string{lessonTemplate}},
			{demoOffice, []string{lessonTemplate, personTemplate}},
			{demoAdmin, []string{lessonTemplate, personTemplate}},
		} {
			for _, personal := range []bool{false, true} {
				args := []string{"--as", tc.as}
				want := []string(nil)

				if personal {
					args = append(args, "--personal-data")
					want = tc.templates
				}

				s := mcpSession(t, r, args...)

				resources, templates := s.resourceURIs(t)
				if !slices.Equal(resources, schoolResources) || !slices.Equal(templates, want) {
					t.Errorf("%s, personal data %t: resources %v, templates %v; want %v, %v", tc.as, personal,
						resources, templates, schoolResources, want)
				}

				s.close(t)
			}
		}
	})
}

// TestMCPResourceAudit: a read writes one mcp-tool-called event like a
// tool call, under the resource's URI or the template, never the record's
// ID, also when the record is not found or the account is refused. Without
// --personal-data a person:// or lesson:// read fails as the SDK answers
// an unknown resource and writes no event (review focus 3).
func TestMCPResourceAudit(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)
		office, _ := accountOf(t, st, demoOffice)

		// Without --personal-data: unlisted, so not found, and not audited.
		s := mcpSession(t, r, "--as", demoOffice)

		for _, uri := range []string{"person://" + ppEllaID, "lesson://" + ppEllaID} {
			_, err := s.readResource(t, uri)
			wantReadError(t, uri, err, jsonrpc.CodeInvalidParams, "Resource not found")
		}

		s.close(t)

		if calls := toolCalls(t, st); len(calls) != 0 {
			t.Fatalf("unlisted reads audited: %+v", calls)
		}

		s = mcpSession(t, r, "--as", demoOffice, "--personal-data")

		if _, err := s.readResource(t, "school://glossary"); err != nil {
			t.Fatal(err)
		}

		s.resourceDocument(t, "person://"+ppEllaID)

		// An unknown person: invalid params, as the SDK answers an unknown
		// resource, with the service's message.
		_, err := s.readResource(t, "person://"+ppUnknownID)

		var rpcErr *jsonrpc.Error
		if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams ||
			!strings.Contains(rpcErr.Message, "not found") {
			t.Errorf("unknown person: error %v", err)
		}

		// Demoted to member: the office's resource is refused.
		wantOK(t, "demote", r.exec(t, "accounts", "set-role", "--as", demoAdmin, demoOffice, "member"))

		_, err = s.readResource(t, "person://"+ppEllaID)
		wantReadError(t, "person://"+ppEllaID, err, jsonrpc.CodeInvalidRequest,
			"forbidden: the account's role member is below office, which the resource person://{id} needs")

		calls := toolCalls(t, st)
		if len(calls) != 4 {
			t.Fatalf("%d reads audited, want 4", len(calls))
		}

		wantToolCall(t, calls[0], office.ID, "school://glossary", "ok")
		wantToolCall(t, calls[1], office.ID, personTemplate, "ok")
		wantToolCall(t, calls[2], office.ID, personTemplate, "not-found")
		wantToolCall(t, calls[3], office.ID, personTemplate, "forbidden")

		for _, e := range calls {
			if b, err := json.Marshal(e); err != nil || bytes.Contains(b, []byte(ppEllaID)) ||
				bytes.Contains(b, []byte(ppUnknownID)) {
				t.Errorf("audit event %s (%v) names the person", b, err)
			}
		}
	})
}

// TestMCPResourceArgumentsStayPrivate (review focus 2): the ID of a
// template's URI reaches neither the log nor an audit event, not even when
// the read fails on it; without --personal-data the read is not audited.
func TestMCPResourceArgumentsStayPrivate(t *testing.T) {
	t.Parallel()

	const secret = "Zebulon"

	stores(t, func(t *testing.T, r cliRun) {
		st := storeOf(t, r)

		s := mcpSession(t, r, "--as", demoAdmin)

		_, err := s.readResource(t, "person://"+secret)
		wantReadError(t, "person://"+secret, err, jsonrpc.CodeInvalidParams, "Resource not found")

		if stderr := s.close(t); strings.Contains(stderr, secret) {
			t.Errorf("the log has the URI: %q", stderr)
		}

		s = mcpSession(t, r, "--as", demoAdmin, "--personal-data")

		for _, uri := range []string{"person://" + secret, "lesson://" + secret} {
			_, err := s.readResource(t, uri)
			wantReadError(t, uri, err, jsonrpc.CodeInvalidParams, "validation failed: id is not an ID")
		}

		if stderr := s.close(t); strings.Contains(stderr, secret) {
			t.Errorf("the log has the URI: %q", stderr)
		}

		calls := toolCalls(t, st)
		if len(calls) != 2 {
			t.Fatalf("%d reads audited, want 2", len(calls))
		}

		for _, e := range calls {
			if b, err := json.Marshal(e); err != nil || bytes.Contains(b, []byte(secret)) {
				t.Errorf("audit event %s (%v) has the URI", b, err)
			}

			if e.Details.Outcome != "invalid" {
				t.Errorf("outcome %q, want invalid", e.Details.Outcome)
			}
		}
	})
}

// retireClass retires the class id in st, as the office does when it
// deletes a class that has lessons (no command retires one).
func retireClass(t *testing.T, st store.Store, id string) {
	t.Helper()

	classID, err := domain.ParseID[domain.ClassID](id)
	if err != nil {
		t.Fatal(err)
	}

	err = st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		c, err := tx.Classes().Get(ctx, classID)
		if err != nil {
			return err
		}

		c.Retired = true

		return tx.Classes().Update(ctx, c)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestMCPResourceParity: each resource returns the document of the
// command or operation it mirrors on the same data.
func TestMCPResourceParity(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		// The timetable is listClasses: `classes list` (listAllClasses)
		// without the retired classes and without each item's retired key.
		// The demo data has no retired class, so one is retired first, and
		// the comparison sees it left out.
		classes, _ := commandDocument(t, r, "classes", "list", "--as", demoOffice).(map[string]any)
		items, _ := classes["items"].([]any)

		first, _ := items[0].(map[string]any)
		retired, _ := first["id"].(string)

		retireClass(t, storeOf(t, r), retired)

		classes, _ = commandDocument(t, r, "classes", "list", "--as", demoOffice).(map[string]any)
		items, _ = classes["items"].([]any)

		current := []any{}

		for _, item := range items {
			c, _ := item.(map[string]any)
			if c["retired"] == true {
				if c["id"] != retired {
					t.Errorf("class %v is retired too", c["id"])
				}

				continue
			}

			delete(c, "retired")
			current = append(current, c)
		}

		if len(current) == 0 || len(current) != len(items)-1 {
			t.Fatalf("%d current classes of %d, want all but the retired one", len(current), len(items))
		}

		s := mcpSession(t, r, "--as", demoMember)

		want := map[string]any{"items": current}
		if got := s.resourceDocument(t, "school://timetable"); !reflect.DeepEqual(got, want) {
			t.Errorf("timetable:\n%v\nclasses list without retired classes:\n%v", got, want)
		}

		// The glossary is docs/glossary.md as it is, as Markdown.
		glossary, err := os.ReadFile("../../docs/glossary.md")
		if err != nil {
			t.Fatal(err)
		}

		c, err := s.readResource(t, "school://glossary")
		if err != nil || c.MIMEType != "text/markdown" || c.Text != string(glossary) {
			t.Errorf("glossary: %v, MIME type %q, text equal to docs/glossary.md: %t", err, c.MIMEType,
				err == nil && c.Text == string(glossary))
		}

		s.close(t)

		// The settings: for the office the document of `settings get`, for
		// members and instructors the public settings (getPublicSettings),
		// whose values are those of the office's areas, and no area the
		// office alone reads.
		settings, _ := commandDocument(t, r, "settings", "get", "--as", demoOffice).(map[string]any)

		s = mcpSession(t, r, "--as", demoOffice)
		if got := s.resourceDocument(t, "school://settings"); !reflect.DeepEqual(got, any(settings)) {
			t.Errorf("settings as the office:\n%v\nsettings get:\n%v", got, settings)
		}

		s.close(t)

		school, _ := settings["school"].(map[string]any)
		branding, _ := settings["branding"].(map[string]any)
		regional, _ := settings["regional"].(map[string]any)
		public := map[string]any{
			"schoolName": school["schoolName"], "legalNotice": school["legalNotice"], "theme": branding["theme"],
			"hasLogo": branding["hasLogo"], "hasBackground": branding["hasBackground"],
			"timezone": regional["timezone"], "defaultLocale": regional["defaultLocale"],
			"currency": regional["currency"],
		}

		for _, k := range []string{"accentColor", "accentColorDark"} {
			if v, ok := branding[k]; ok {
				public[k] = v
			}
		}

		for _, as := range []string{demoMember, demoInstructor} {
			s = mcpSession(t, r, "--as", as)
			if got := s.resourceDocument(t, "school://settings"); !reflect.DeepEqual(got, any(public)) {
				t.Errorf("settings as %s:\n%v\nwant the public settings:\n%v", as, got, public)
			}

			s.close(t)
		}

		// person://{id} is `persons show` without the date of birth, which
		// Ella's document has.
		person, _ := commandDocument(t, r, "persons", "show", "--as", demoOffice, ppEllaID).(map[string]any)
		if _, ok := person["birthDate"]; !ok {
			t.Fatal("persons show has no birthDate to leave out: the test proves nothing")
		}

		delete(person, "birthDate")

		s = mcpSession(t, r, "--as", demoOffice, "--personal-data")

		c, err = s.readResource(t, "person://"+ppEllaID)
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(c.Text, "birthDate") {
			t.Errorf("person has a birthDate: %s", c.Text)
		}

		if got := decodeJSON(t, c.Text); !reflect.DeepEqual(got, any(person)) {
			t.Errorf("person:\n%v\npersons show without birthDate:\n%v", got, person)
		}

		s.close(t)
	})
}

// TestMCPLessonResource: lesson://{id} is the lesson's participants for
// the office and for the lesson's instructor, the same document; another
// instructor's lesson is not found, as on the participants page.
func TestMCPLessonResource(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		week, _ := commandDocument(t, r, "lessons", "list", "--as", demoOffice, "--from", "2026-09-21", "--to",
			"2026-09-27").(map[string]any)
		lessons, _ := week["lessons"].([]any)

		var own, other string

		for _, l := range lessons {
			lesson, _ := l.(map[string]any)
			instructor, _ := lesson["instructor"].(map[string]any)
			id, _ := lesson["id"].(string)

			switch {
			case instructor["displayName"] == "Alina Starling" && own == "":
				own = id
			case instructor["displayName"] != "Alina Starling" && other == "":
				other = id
			}
		}

		if own == "" || other == "" {
			t.Fatalf("the week has no lesson of Alina Starling (%q) or none of another (%q)", own, other)
		}

		s := mcpSession(t, r, "--as", demoOffice, "--personal-data")
		want := s.resourceDocument(t, "lesson://"+own)
		s.close(t)

		if doc, _ := want.(map[string]any); doc["lessonId"] != own {
			t.Errorf("lesson://%s is the participants of %v", own, doc["lessonId"])
		}

		s = mcpSession(t, r, "--as", demoInstructor, "--personal-data")

		if got := s.resourceDocument(t, "lesson://"+own); !reflect.DeepEqual(got, want) {
			t.Errorf("own lesson as the instructor:\n%v\nas the office:\n%v", got, want)
		}

		_, err := s.readResource(t, "lesson://"+other)

		var rpcErr *jsonrpc.Error
		if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams ||
			!strings.Contains(rpcErr.Message, "not found") {
			t.Errorf("another instructor's lesson: error %v", err)
		}
	})
}
