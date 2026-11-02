package main

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/mcpserver"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

// The flags of mcp.
const (
	flagPersonalData = "personal-data"
	flagAllowWrites  = "allow-writes"
)

// newMCPCmd is the MCP server (ADR 0022): an assistant's client starts it
// and speaks MCP with it on stdin and stdout.
func newMCPCmd(app *cli.App) *cobra.Command {
	var (
		personalData bool
		allowWrites  []string
		groups       []mcpserver.WriteGroup
		cmd          *cobra.Command
	)

	names := make([]string, 0, len(mcpserver.WriteGroups()))
	for _, g := range mcpserver.WriteGroups() {
		names = append(names, string(g))
	}

	return app.Command(cli.Spec{
		Use:   "mcp",
		Short: "Serve the Model Context Protocol on stdin and stdout for an assistant",
		Long: "Run an MCP server over stdio (ADR 0022), for an assistant's client such as Claude Desktop or " +
			"Claude Code, which starts the command and speaks MCP on its stdin and stdout; over SSH, the " +
			"client runs it on the school's host. Its tools and resources act as the account of --as with that " +
			"account's rights: a session lists those whose access level the account's role reaches, and every " +
			"call or read resolves the account again, so an account disabled or demoted since the start is " +
			"refused on its next call. Every call and read is audited as mcp-tool-called with the tool's name " +
			"or the resource's URI template and its outcome, never with its arguments or a record's ID. Without " +
			"--personal-data neither tools nor resources return person data; write tools " +
			"are listed only for the groups of --allow-writes (" + strings.Join(names, ", ") + "). Nothing " +
			"but MCP goes to stdout; warnings and errors go to stderr. The command takes no --output json and " +
			"no --yes: the client asks its user before a write.",
		Example: "  studio-schedule mcp --as office@example.org\n" +
			"  claude mcp add studio-schedule -- studio-schedule mcp --as office@example.org\n" +
			"  ssh school.example.org studio-schedule mcp --as office@example.org --personal-data",
		Access:  httpapi.AccessSignedIn,
		Channel: domain.ChannelMCP,
		JSON:    "no JSON document: stdout carries the MCP messages, so --output json is refused",
		Flags: func(c *cobra.Command) {
			cmd = c
			c.Flags().BoolVar(&personalData, flagPersonalData, false,
				"list the tools and resources that return person data; the assistant's provider receives what they return")
			c.Flags().StringSliceVar(&allowWrites, flagAllowWrites, nil,
				"list the write tools of these groups (repeatable or comma-separated): "+strings.Join(names, ", "))
		},
		Fields: map[string]string{"allowWrites": "--" + flagAllowWrites},
		Validate: func([]string) error {
			if cmd.Flags().Changed(cli.FlagYes) {
				return cli.Usagef("--%s: %s asks no confirmation; the client approves a write", cli.FlagYes,
					cmd.CommandPath())
			}

			if output := cmd.Flags().Lookup(cli.FlagOutput).Value.String(); output != cli.FormatText {
				return cli.Usagef("--%s %s: %s prints MCP messages only", cli.FlagOutput, output, cmd.CommandPath())
			}

			var err error

			groups, err = mcpserver.ParseWriteGroups(allowWrites)

			return err
		},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			logger := slog.New(slog.NewTextHandler(env.Stderr(), &slog.HandlerOptions{Level: slog.LevelWarn}))

			server, err := mcpserver.New(env.Services, env.Actor, env.Principal, env.Clock, mcpserver.Options{
				PersonalData: personalData, AllowWrites: groups, Checks: env, Logger: logger,
			}, version)
			if err == nil { // New fails only without Checks
				err = server.Run(ctx, &mcp.IOTransport{
					Reader: io.NopCloser(env.Stdin()), Writer: nopWriteCloser{env.Stdout()},
				})
			}

			return err
		},
	})
}

// nopWriteCloser is stdout for the MCP transport, which closes its writer
// at the end of the session: stdout stays open, as the SDK's stdio
// transport keeps it.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
