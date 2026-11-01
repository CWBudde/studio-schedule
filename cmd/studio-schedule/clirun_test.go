package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// update rewrites the golden files of testdata/ (go test
// ./cmd/studio-schedule -update), like internal/service's feeds.
var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// The demo accounts the commands act as (seed.DemoLogins).
const (
	demoMember     = "clara.amberly@example.org"
	demoInstructor = "alina.starling@example.net"
	demoOffice     = "olga.ledgerby@example.net"
	demoAdmin      = "arno.keystone@example.net"
)

// testOSUser is the OS user of the in-process runs.
const testOSUser = "operator"

// testNow is the fixed time of the in-process runs: in the demo term.
var testNow = time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)

// cliRun is a run of the command tree in-process (cobra Execute with
// buffers, through runMain), with an injected environment, stdin and
// terminal check, a fixed clock and deterministic IDs.
type cliRun struct {
	// env is the environment; DATABASE_URL is set to a dummy when st is.
	env map[string]string
	// stdin is the input; terminal makes stdin and stdout terminals.
	stdin    string
	terminal bool
	// st is the store every command opens (the memory store); nil opens
	// DATABASE_URL on PostgreSQL as the binary does.
	st store.Store
	// open, if set, replaces the opener (a database that fails, say).
	open cli.Opener
	// pdf is the receipt renderer (cli.System.PDF); nil: none.
	pdf func() (service.ReceiptRenderer, func())
}

// result is what a run printed and its exit status.
type result struct {
	code           int
	stdout, stderr string
}

// exec runs the command tree with args.
func (r cliRun) exec(t *testing.T, args ...string) result {
	t.Helper()

	env := map[string]string{}
	for k, v := range r.env {
		env[k] = v
	}

	open := cli.Opener(openDatabase)
	if r.st != nil {
		st := r.st
		open = func(context.Context, string) (store.Store, func(), error) { return st, func() {}, nil }

		if _, ok := env["DATABASE_URL"]; !ok {
			env["DATABASE_URL"] = "postgres://memory.example.test/studio"
		}
	}

	if r.open != nil {
		open = r.open
	}

	root := newRoot(cli.System{
		Lookup:         func(key string) (string, bool) { v, ok := env[key]; return v, ok },
		StdinTerminal:  r.terminal,
		StdoutTerminal: r.terminal,
		OSUser:         testOSUser,
		Open:           open,
		PDF:            r.pdf,
		Clock:          clock.NewFixed(testNow),
		IDs:            idsOf(t),
	})

	var stdout, stderr bytes.Buffer

	root.SetArgs(args)
	root.SetIn(strings.NewReader(r.stdin))
	root.SetOut(&stdout)
	root.SetErr(&stderr)

	code := runMain(root, &stderr)

	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// runIDs hold the ID source of each test's runs (idsOf), by test name.
var runIDs sync.Map

// cliIDs are the IDs of a test's runs: a sequence that goes on across the
// runs of the test, so a second run does not reuse the first's IDs, apart
// from the demo data's: seed's sequence has a zero timestamp, these one of
// 1 ms. They are deterministic, so golden files can show them.
type cliIDs struct{ seq *ids.Sequence }

func (s cliIDs) NewUUID() uuid.UUID {
	u := s.seq.NewUUID()
	u[5] = 1 // the lowest byte of the timestamp

	return u
}

// idsOf returns the ID source of the runs of t.
func idsOf(t *testing.T) ids.Source {
	src, _ := runIDs.LoadOrStore(t.Name(), cliIDs{ids.NewSequence()})

	return src.(cliIDs) //nolint:forcetypeassert // only cliIDs are stored
}

// demoStore returns a memory store with the demo data.
func demoStore(t *testing.T) *memory.Store {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	return st
}

// demoDatabase returns the URL of a PostgreSQL test database with the demo
// data.
func demoDatabase(t *testing.T) string {
	t.Helper()

	url := newTestDatabase(t)
	if err := seed.LoadDemo(t.Context(), openTestStore(t, url)); err != nil {
		t.Fatal(err)
	}

	return url
}

// stores runs fn on the memory store and on PostgreSQL, each with the demo
// data: the cliRun with its store set, and one opening the database by
// DATABASE_URL.
func stores(t *testing.T, fn func(t *testing.T, r cliRun)) {
	t.Helper()

	t.Run("memory", func(t *testing.T) {
		t.Parallel()
		fn(t, cliRun{st: demoStore(t)})
	})
	t.Run("postgres", func(t *testing.T) {
		t.Parallel()
		fn(t, cliRun{env: map[string]string{"DATABASE_URL": demoDatabase(t)}})
	})
}

// golden compares got with testdata/name, or writes it there with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./cmd/studio-schedule -update)", err)
	}

	if got != string(want) {
		t.Errorf("%s differs (go test ./cmd/studio-schedule -update rewrites it):\n--- got\n%s\n--- want\n%s",
			path, got, want)
	}
}
