// Package mcpserver is the MCP server of the command `studio-schedule mcp`
// (ADR 0022): tools over the application's services that an assistant
// calls as the account of --as. Each tool calls the same service method as
// the command it mirrors and returns the same document, the operation's
// response body; a test asserts that the two agree. Which tools a session
// lists is fixed at the start by the account's role and the flags; every
// call resolves the account again, checks its current role and writes an
// audit event, never with the call's arguments.
//
// The package is server-only, like the command layer: cmd/wasm must not
// depend on the MCP SDK (scripts/check-wasm-deps.sh).
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/diagnose"
	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// Options are the flags of `mcp` and what the server needs of the process.
type Options struct {
	// PersonalData lists the tools that return person data (--personal-data).
	PersonalData bool
	// AllowWrites are the write groups whose tools are listed
	// (--allow-writes), validated by ParseWriteGroups.
	AllowWrites []WriteGroup

	// Checks is not a flag: it is the command's environment, which the
	// doctor tool checks as `doctor` does. New refuses a nil one.
	Checks diagnose.Env
	// Logger is not a flag either: it gets the server's warnings and
	// errors, never a call's arguments (stderr in the command). Nil
	// discards them.
	Logger *slog.Logger
}

// WriteGroup is a group of write tools that --allow-writes lists (ADR
// 0022).
type WriteGroup string

// The write groups.
const (
	WriteBookings      WriteGroup = "bookings"
	WriteLessons       WriteGroup = "lessons"
	WriteAnnouncements WriteGroup = "announcements"
	WriteMail          WriteGroup = "mail"
	WriteJobs          WriteGroup = "jobs"
)

// WriteGroups returns the write groups in their documented order.
func WriteGroups() []WriteGroup {
	return []WriteGroup{WriteBookings, WriteLessons, WriteAnnouncements, WriteMail, WriteJobs}
}

// Valid reports whether g is a write group.
func (g WriteGroup) Valid() bool { return slices.Contains(WriteGroups(), g) }

// ParseWriteGroups returns the write groups of names, the values of
// --allow-writes. An unknown one is a field error on allowWrites, which
// the command reports as a usage error naming its flag.
func ParseWriteGroups(names []string) ([]WriteGroup, error) {
	groups := make([]WriteGroup, 0, len(names))

	for _, name := range names {
		g := WriteGroup(strings.TrimSpace(name))
		if !g.Valid() {
			all := make([]string, 0, len(WriteGroups()))
			for _, v := range WriteGroups() {
				all = append(all, string(v))
			}

			return nil, &domain.FieldError{
				Field: "allowWrites",
				Msg:   fmt.Sprintf("unknown group %q; the groups are %s", name, strings.Join(all, ", ")),
			}
		}

		groups = append(groups, g)
	}

	return groups, nil
}

// Actor resolves the account of --as again (cli.Env.Actor): before every
// call, so an account disabled or demoted since the start loses access at
// once.
type Actor func(ctx context.Context) (service.Principal, error)

// Server is the MCP server of one session of `mcp`.
type Server struct {
	mcp   *mcp.Server
	svc   app.Services
	actor Actor
	start service.Principal
	clk   clock.Clock
	opts  Options
	log   *slog.Logger
}

// instructions tell the client what the tools have in common.
const instructions = "The tools read a dance school's schedule, figures and checks as one account of " +
	"the school, with that account's rights. School dates (YYYY-MM-DD) and times are local to the " +
	"school's time zone, which the results name."

// New builds the server for start, the principal of --as resolved at the
// start: the tools whose access level start's role reaches, those that
// return person data only with opts.PersonalData, and write tools only of
// the groups of opts.AllowWrites. A tool not listed is unknown to the
// session: a call by its name fails like any unknown tool's. actor
// resolves the account again before every call; version is the binary's.
func New(
	svc app.Services, actor Actor, start service.Principal, clk clock.Clock, opts Options, version string,
) (*Server, error) {
	return newServer(svc, actor, start, clk, opts, version, registry)
}

// newServer is New with the tools of tools, so tests can filter tools that
// the registry does not have yet.
func newServer(
	svc app.Services, actor Actor, start service.Principal, clk clock.Clock, opts Options, version string,
	tools [][]tool,
) (*Server, error) {
	if opts.Checks == nil {
		return nil, errors.New("mcpserver: Options.Checks is nil")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	s := &Server{svc: svc, actor: actor, start: start, clk: clk, opts: opts, log: logger}

	// The SDK logs its sessions and transport errors, never a request's
	// parameters, so it may share the logger.
	s.mcp = mcp.NewServer(&mcp.Implementation{Name: "studio-schedule", Title: "Studio Schedule", Version: version},
		&mcp.ServerOptions{Instructions: instructions, Logger: logger})

	for _, group := range tools {
		for _, t := range group {
			if s.lists(t) {
				t.add(s, t)
			}
		}
	}

	return s, nil
}

// lists reports whether the session lists t: the start's role reaches its
// access level, and the flags allow its person data and its write group.
func (s *Server) lists(t tool) bool {
	minimum, _ := t.access.MinRole() // the registry's levels are known (TestRegistry)

	switch {
	case !s.start.Role.AtLeast(minimum):
		return false
	case t.personalData && !s.opts.PersonalData:
		return false
	case t.write != "" && !slices.Contains(s.opts.AllowWrites, t.write):
		return false
	default:
		return true
	}
}

// Run serves the session on t until the client closes it or ctx ends.
func (s *Server) Run(ctx context.Context, t mcp.Transport) error {
	return s.mcp.Run(ctx, t)
}

// MCP is the SDK's server, for tests that connect a client in memory.
func (s *Server) MCP() *mcp.Server { return s.mcp }
