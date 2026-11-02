package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

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
// starts without a tool. --personal-data adds no tool yet: none of these
// returns person data.
func TestMCPTools(t *testing.T) {
	t.Parallel()

	stores(t, func(t *testing.T, r cliRun) {
		for _, tc := range []struct {
			as    string
			tools []string
		}{
			{demoMember, []string{"schedule"}},
			{demoInstructor, []string{"schedule"}},
			{demoOffice, []string{"schedule", "statistics"}},
			{demoAdmin, []string{"db_check", "doctor", "schedule", "statistics"}},
		} {
			for _, flags := range [][]string{nil, {"--personal-data"}, {"--allow-writes", "bookings,jobs"}} {
				s := mcpSession(t, r, append([]string{"--as", tc.as}, flags...)...)

				if got := s.toolNames(t); !slices.Equal(got, tc.tools) {
					t.Errorf("%s %v: tools %v, want %v", tc.as, flags, got, tc.tools)
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
		} {
			if text, isError := s.callTool(t, call.tool, call.args); !isError {
				t.Errorf("%s %v: no error result but %q", call.tool, call.args, text)
			}
		}

		if stderr := s.close(t); strings.Contains(stderr, secret) {
			t.Errorf("the log has the argument: %q", stderr)
		}

		calls := toolCalls(t, st)
		if len(calls) != 5 {
			t.Fatalf("%d tool calls audited, want 5", len(calls))
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
