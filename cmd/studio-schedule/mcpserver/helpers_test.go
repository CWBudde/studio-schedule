package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// update rewrites the golden files of testdata/ (go test
// ./cmd/studio-schedule/mcpserver -update).
var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// The demo accounts (seed.DemoLogins).
const (
	demoMember     = "clara.amberly@example.org"
	demoInstructor = "alina.starling@example.net"
	demoOffice     = "olga.ledgerby@example.net"
	demoAdmin      = "arno.keystone@example.net"
)

// testNow is the fixed time of the tests: a Tuesday in the demo term.
var testNow = time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)

// via is the channel of the tests' principals.
var via = domain.AuditVia{Channel: domain.ChannelMCP, OSUser: "operator"}

// fixture is a store with the demo data and the services on it, with the
// log of the servers built on them.
type fixture struct {
	st  store.Store
	svc app.Services
	log *syncBuffer
}

// newFixture returns the fixture on a memory store with the demo data.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	return fixtureOn(st)
}

// fixtureOn returns the fixture of st.
func fixtureOn(st store.Store) *fixture {
	return &fixture{
		st:  st,
		svc: app.NewServices(app.Deps{Store: st, Clock: clock.NewFixed(testNow), IDs: offsetIDs{ids.NewSequence()}}),
		log: &syncBuffer{},
	}
}

// offsetIDs are a sequence apart from the demo data's, whose sequence has
// a zero timestamp.
type offsetIDs struct{ seq *ids.Sequence }

func (s offsetIDs) NewUUID() uuid.UUID {
	u := s.seq.NewUUID()
	u[5] = 1

	return u
}

// principal resolves the demo account email as the MCP channel's.
func (f *fixture) principal(t *testing.T, email string) service.Principal {
	t.Helper()

	p, err := f.svc.Auth.Actor(t.Context(), email, via)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

// actorOf resolves email again on every call, as cli.Env.Actor does.
func (f *fixture) actorOf(email string) Actor {
	return func(ctx context.Context) (service.Principal, error) { return f.svc.Auth.Actor(ctx, email, via) }
}

// options are opts with the fixture's checks and logger where unset.
func (f *fixture) options(opts Options) Options {
	if opts.Checks == nil {
		opts.Checks = checksEnv{f}
	}

	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(f.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	return opts
}

// session builds the server of the registry tools for the account email
// and connects a client in memory; see sessionOf.
func (f *fixture) session(t *testing.T, email string, opts Options, tools [][]tool) *mcp.ClientSession {
	t.Helper()

	return f.sessionOf(t, f.principal(t, email), f.actorOf(email), opts, tools)
}

// sessionOf builds the server of tools for start and actor and connects a
// client in memory. Its cleanup closes the session and fails the test
// unless Run ends without an error.
func (f *fixture) sessionOf(
	t *testing.T, start service.Principal, actor Actor, opts Options, tools [][]tool,
) *mcp.ClientSession {
	t.Helper()

	s, err := newServer(f.svc, actor, start, clock.NewFixed(testNow), f.options(opts), "test", tools)
	if err != nil {
		t.Fatal(err)
	}

	clientT, serverT := mcp.NewInMemoryTransports()

	ran := make(chan error, 1)

	go func() { ran <- s.Run(context.Background(), serverT) }()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), clientT, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = cs.Close()

		if err := <-ran; err != nil {
			t.Errorf("Run: %v", err)
		}
	})

	return cs
}

// checksEnv is the environment of the doctor tool: the fixture's store,
// with a configuration of a database URL only, and no receipt renderer.
type checksEnv struct{ f *fixture }

func (c checksEnv) Config(flags config.Flags) (config.Config, error) {
	return config.LoadWithoutLog(func(key string) (string, bool) {
		if key == config.EnvDatabaseURL {
			return "postgres://memory.example.test/studio", true
		}

		return "", false
	}, flags)
}

func (c checksEnv) Open(context.Context, config.Config) (app.Services, func(), error) {
	return c.f.svc, func() {}, nil
}

func (checksEnv) PDF() (service.ReceiptRenderer, func(), bool) { return nil, nil, false }

// syncBuffer is a buffer that the server's goroutines may write while a
// test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// toolNames lists the tools of cs by name, sorted.
func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(res.Tools))
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}

	slices.Sort(names)

	return names
}

// callTool calls the tool name with args and returns the text of its
// result and whether it is an error result.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args any) (text string, isError bool) {
	t.Helper()

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
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

// document calls the tool name and decodes the JSON object of its result;
// an error result fails the test.
func document(t *testing.T, cs *mcp.ClientSession, name string, args any) map[string]any {
	t.Helper()

	text, isError := callTool(t, cs, name, args)
	if isError {
		t.Fatalf("call %s %v: error result %q", name, args, text)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("call %s: %v in %q", name, err, text)
	}

	return doc
}

// wantError calls the tool name and fails unless the result is an error
// with the text want.
func wantError(t *testing.T, cs *mcp.ClientSession, name string, args any, want string) {
	t.Helper()

	if text, isError := callTool(t, cs, name, args); !isError || text != want {
		t.Errorf("call %s %v: result %q (error %t), want the error %q", name, args, text, isError, want)
	}
}

// toolCalls returns the mcp-tool-called events of st, oldest first.
func toolCalls(t *testing.T, st store.Store) []domain.AuditEvent {
	t.Helper()

	var events []domain.AuditEvent

	err := st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		events, err = tx.Audit().List(ctx, 1000)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	var out []domain.AuditEvent

	for _, e := range slices.Backward(events) {
		if e.Action == domain.AuditMCPToolCalled {
			out = append(out, e)
		}
	}

	return out
}

// golden compares the JSON text got, indented, with testdata/name, or
// writes it there with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()

	var indented bytes.Buffer
	if err := json.Indent(&indented, []byte(got), "", "  "); err != nil {
		t.Fatal(err)
	}

	indented.WriteByte('\n')

	path := filepath.Join("testdata", name)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, indented.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./cmd/studio-schedule/mcpserver -update)", err)
	}

	if indented.String() != string(want) {
		t.Errorf("%s differs (go test ./cmd/studio-schedule/mcpserver -update rewrites it):\n--- got\n%s\n--- want\n%s",
			path, indented.String(), want)
	}
}

// faultStore fails the transactions of a mode with the error failing set
// for it.
type faultStore struct {
	store.Store

	mu   sync.Mutex
	fail map[store.TxMode]error
}

func (s *faultStore) failing(mode store.TxMode, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fail == nil {
		s.fail = map[store.TxMode]error{}
	}

	s.fail[mode] = err
}

func (s *faultStore) InTx(ctx context.Context, mode store.TxMode, fn func(ctx context.Context, tx store.Tx) error) error {
	s.mu.Lock()
	err := s.fail[mode]
	s.mu.Unlock()

	if err != nil {
		return err
	}

	return s.Store.InTx(ctx, mode, fn)
}
