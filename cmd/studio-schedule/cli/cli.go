// Package cli is the command layer of the studio-schedule binary (ADR 0020):
// a command is a Spec (its use, flags, access level, whether it is
// destructive) whose Run calls one service method through the Env and
// presents the result with the Env's Output. The layer loads the
// configuration, opens the database, resolves the account of --as, checks
// its role against the command's access level, maps errors to messages and
// exit codes, and asks for confirmation. There is no SQL in it, and no
// command reaches the store directly.
//
// The package is server-only: it opens PostgreSQL and reads the OS user.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/app"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The global flags (ADR 0020).
const (
	FlagOutput = "output"
	FlagYes    = "yes"
)

// DatabaseURLUsage is the help text of --database-url.
const DatabaseURLUsage = "PostgreSQL connection URL or key=value DSN; prefer the env var, flags are " +
	"visible in the process list (env " + config.EnvDatabaseURL + ")"

// Annotations of a Spec's command, read by the reference (docs/cli.md).
const (
	// AnnotationAccess is the access level of the command; absent for an
	// operator command.
	AnnotationAccess = "studio-schedule/access"
	// AnnotationDestructive is set on a command that asks for
	// confirmation.
	AnnotationDestructive = "studio-schedule/destructive"
	// AnnotationJSON documents what the command prints with --output json.
	AnnotationJSON = "studio-schedule/json"
	// annotationSpec marks a command built from a Spec.
	annotationSpec = "studio-schedule/spec"
)

// Opener opens the store of a database URL after checking its schema, as
// serve does; release closes it.
type Opener func(ctx context.Context, databaseURL string) (st store.Store, release func(), err error)

// System is what a command needs of the process: the environment, whether
// stdin and stdout are terminals, the OS user, the database, the clock and
// the ID source. Tests fill it in; OS returns the process's.
type System struct {
	// Lookup reads environment variables; nil is an empty environment.
	Lookup config.LookupFunc
	// StdinTerminal and StdoutTerminal report whether stdin and stdout are
	// terminals: a terminal gets prompts, a header line and color.
	StdinTerminal, StdoutTerminal bool
	// OSUser is the user who runs the command, recorded in the audit
	// events of an account acting through the command line.
	OSUser string
	// Open opens the database of a Spec with Store.
	Open Opener
	// PDF starts the receipt renderer (typstwasm on the server); release
	// stops it. Nil: there is none.
	PDF func() (r service.ReceiptRenderer, release func())
	// Clock defaults to clock.System, IDs to ids.V7.
	Clock clock.Clock
	IDs   ids.Source
}

// OS returns the System of the process, with the database from open.
func OS(open Opener) System {
	name := "unknown"
	if u, err := user.Current(); err == nil {
		name = u.Username
	}

	return System{
		Lookup:         os.LookupEnv,
		StdinTerminal:  isTerminal(os.Stdin),
		StdoutTerminal: isTerminal(os.Stdout),
		OSUser:         name,
		Open:           open,
	}
}

// isTerminal reports whether f is a character device, a terminal.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()

	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// App holds the System and the global flags of one command tree.
type App struct {
	sys         System
	databaseURL string
	output      string
	as          string
	yes         bool
}

// NewApp returns the App of sys.
func NewApp(sys System) *App {
	if sys.Lookup == nil {
		sys.Lookup = func(string) (string, bool) { return "", false }
	}

	if sys.Clock == nil {
		sys.Clock = clock.System{}
	}

	if sys.IDs == nil {
		sys.IDs = ids.V7{}
	}

	return &App{sys: sys}
}

// Lookup reads an environment variable of the System.
func (a *App) Lookup(key string) (string, bool) { return a.sys.Lookup(key) }

// Spec is a command of the layer.
type Spec struct {
	// Use, Short, Long and Example are the cobra command's.
	Use, Short, Long, Example string
	// Args validates the positional arguments; nil means none.
	Args cobra.PositionalArgs
	// Access is the level the command needs (the levels of
	// httpapi/access.go): the level of the HTTP operation it mirrors. The
	// command then acts as the account of --as (env STUDIO_AS), whose role
	// must reach the level. Empty: an operator command, which takes no
	// --as.
	Access httpapi.Access
	// Store opens the database (Env.Services). A command with Access opens
	// it in any case.
	Store bool
	// Destructive marks a command that deletes, anonymizes, mails, cancels
	// or merges: it calls Env.Confirm before it changes anything.
	Destructive bool
	// JSON says what the command prints with --output json, for the
	// reference: "the response body of <operationId>" or the schema of a
	// command without an operation.
	JSON string
	// Flags binds the command's own flags.
	Flags func(cmd *cobra.Command)
	// Fields name the flags of the fields a service's field error names
	// (domain.FieldError), e.g. "email": "--email".
	Fields map[string]string
	// Run does the work: it calls one service method and presents the
	// result with env.Out.
	Run func(ctx context.Context, env *Env, args []string) error
}

// Command returns the cobra command of spec.
func (a *App) Command(spec Spec) *cobra.Command {
	if spec.Access != "" {
		if _, ok := spec.Access.MinRole(); !ok {
			panic("cli: command " + spec.Use + " has an unknown access level " + string(spec.Access))
		}
	}

	args := spec.Args
	if args == nil {
		args = cobra.NoArgs
	}

	cmd := &cobra.Command{
		Use:     spec.Use,
		Short:   spec.Short,
		Long:    spec.Long,
		Example: spec.Example,
		Args:    args,
		Annotations: map[string]string{
			annotationSpec: "true",
			AnnotationJSON: spec.JSON,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return a.run(ctx, cmd, spec, args)
		},
	}

	if spec.Access != "" {
		cmd.Annotations[AnnotationAccess] = string(spec.Access)
	}

	if spec.Destructive {
		cmd.Annotations[AnnotationDestructive] = "true"
	}

	if spec.Flags != nil {
		spec.Flags(cmd)
	}

	return cmd
}

// Group returns a command that groups cmds, e.g. `config check`. Without a
// subcommand it prints its help; an unknown one is a usage error.
func Group(use, short string, cmds ...*cobra.Command) *cobra.Command {
	group := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return Usagef("unknown command %q for %q", args[0], cmd.CommandPath())
			}

			return cmd.Help()
		},
	}
	group.AddCommand(cmds...)

	return group
}

// Env is what a command's Run gets: the presenter, the services, the
// account it acts as and the clock.
type Env struct {
	// Out presents the result on stdout.
	Out *Output
	// Services are the application's services (app.NewServices), the same
	// as the HTTP API's; zero unless the Spec opens the database.
	Services app.Services
	// Principal is the account of --as, with Via naming the command line
	// and the OS user; zero for an operator command.
	Principal service.Principal
	// Clock is the System's clock.
	Clock clock.Clock

	app *App
	cmd *cobra.Command
	in  *bufio.Reader
}

// Stderr is where prompts and messages beside the result go.
func (e *Env) Stderr() io.Writer { return e.cmd.ErrOrStderr() }

// Config loads the configuration as serve does (config.Load: the
// environment, flags, the defaults), with the global --database-url and
// --as; flags are the command's own explicit flags. Nothing is opened.
func (e *Env) Config(flags config.Flags) (config.Config, error) {
	flags.DatabaseURL, flags.As = e.app.globalFlags(e.cmd)

	return config.Load(e.app.sys.Lookup, flags)
}

// Open opens the database of cfg (Env.Config) with the System's Opener
// and returns its services, for a command that opens it itself, such as
// doctor, which reports a database it cannot open as a finding.
func (e *Env) Open(ctx context.Context, cfg config.Config) (app.Services, func(), error) {
	st, release, err := e.app.sys.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return app.Services{}, nil, err
	}

	return e.app.services(st, cfg), release, nil
}

// PDF starts the System's receipt renderer; ok is false without one.
func (e *Env) PDF() (r service.ReceiptRenderer, release func(), ok bool) {
	if e.app.sys.PDF == nil {
		return nil, nil, false
	}

	r, release = e.app.sys.PDF()

	return r, release, true
}

// ErrNotConfirmed is the answer "no" to Env.Confirm.
var ErrNotConfirmed = Usage(errors.New("not confirmed; nothing was changed"))

// Confirm asks before a destructive action (ADR 0020): question with
// [y/N] on a terminal, and nothing without one, where --yes confirms. It
// returns nil when confirmed, ErrNotConfirmed for any answer but y or yes,
// and a usage error without a terminal and --yes.
func (e *Env) Confirm(question string) error {
	if e.app.yes {
		return nil
	}

	if !e.app.sys.StdinTerminal {
		return Usagef("%s changes data: confirm with --yes when stdin is not a terminal; nothing was changed",
			e.cmd.CommandPath())
	}

	if _, err := fmt.Fprintf(e.Stderr(), "%s [y/N] ", question); err != nil {
		return err
	}

	line, err := e.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	default:
		return ErrNotConfirmed
	}
}

// ReadLine reads one line from stdin, after prompt on stderr when stdin is
// a terminal: the way a secret or a search term reaches a command, never as
// an argument (ASVS 8.3.1). Empty input is a usage error naming what.
func (e *Env) ReadLine(prompt, what string) (string, error) {
	if e.app.sys.StdinTerminal {
		if _, err := fmt.Fprint(e.Stderr(), prompt+": "); err != nil {
			return "", err
		}
	}

	line, err := e.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	if line = strings.TrimSpace(line); line == "" {
		return "", Usagef("no %s on stdin", what)
	}

	return line, nil
}

// run runs spec: it loads what the spec needs, then spec.Run.
func (a *App) run(ctx context.Context, cmd *cobra.Command, spec Spec, args []string) error {
	out, err := a.presenter(cmd)
	if err != nil {
		return err
	}

	env := &Env{Out: out, Clock: a.sys.Clock, app: a, cmd: cmd, in: bufio.NewReader(cmd.InOrStdin())}

	if spec.Access == "" && cmd.Flags().Changed(config.FlagAs) {
		return Usagef("--as: %s acts as no account", cmd.CommandPath())
	}

	if spec.Store || spec.Access != "" {
		release, err := a.open(ctx, cmd, spec, env)
		if err != nil {
			return err
		}

		defer release()
	}

	return withFields(spec.Run(ctx, env, args), spec.Fields)
}

// presenter returns the presenter of --output.
func (a *App) presenter(cmd *cobra.Command) (*Output, error) {
	if a.output != FormatText && a.output != FormatJSON {
		return nil, Usagef("--%s: %q is not text or json", FlagOutput, a.output)
	}

	noColor, _ := a.sys.Lookup(config.EnvNoColor)

	return &Output{
		w: cmd.OutOrStdout(), json: a.output == FormatJSON, terminal: a.sys.StdoutTerminal,
		color: a.sys.StdoutTerminal && noColor == "",
	}, nil
}

// open loads the configuration, opens the database, builds the services
// and, for a command with an access level, resolves the account of --as.
func (a *App) open(ctx context.Context, cmd *cobra.Command, spec Spec, env *Env) (func(), error) {
	var flags config.Flags

	flags.DatabaseURL, flags.As = a.globalFlags(cmd)

	cfg, err := config.LoadWithoutLog(a.sys.Lookup, flags)
	if err != nil {
		return nil, err
	}

	if cfg.DatabaseURL == "" {
		return nil, ErrNoDatabaseURL
	}

	if spec.Access != "" && cfg.As == "" {
		return nil, Usagef("%s acts as an account: give --as <email> or set %s", cmd.CommandPath(), config.EnvAs)
	}

	st, release, err := a.sys.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	env.Services = a.services(st, cfg)

	if err := a.schoolTime(ctx, env); err != nil {
		release()

		return nil, err
	}

	if spec.Access != "" {
		if env.Principal, err = a.actor(ctx, env, spec.Access, cfg.As); err != nil {
			release()

			return nil, err
		}
	}

	return release, nil
}

// services are the application's services on st, with links to the app of
// cfg.
func (a *App) services(st store.Store, cfg config.Config) app.Services {
	return app.NewServices(app.Deps{Store: st, Clock: a.sys.Clock, IDs: a.sys.IDs, AppURL: AppURL(cfg)})
}

// schoolTime sets the presenter's time zone to the school's; a database
// without settings keeps UTC.
func (a *App) schoolTime(ctx context.Context, env *Env) error {
	s, err := env.Services.Settings.Public(ctx)

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return nil
	case err != nil:
		return err
	}

	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return fmt.Errorf("the school's time zone %q: %w", s.Timezone, err)
	}

	env.Out.loc = loc

	return nil
}

// actor resolves the account of email as the command line's principal and
// checks its role against access: an unknown, unconfirmed or disabled
// account is not found (exit 3), a role too low forbidden (exit 4).
func (a *App) actor(ctx context.Context, env *Env, access httpapi.Access, email string) (service.Principal, error) {
	p, err := env.Services.Auth.Actor(ctx, email, domain.AuditVia{Channel: domain.ChannelCLI, OSUser: a.sys.OSUser})
	if err != nil {
		return service.Principal{}, fmt.Errorf("--as %s: %w", email, err)
	}

	minimum, _ := access.MinRole()
	if !p.Role.AtLeast(minimum) {
		return service.Principal{}, fmt.Errorf("--as %s: %w: the account's role %s is below %s, which this command needs",
			email, domain.ErrForbidden, p.Role, minimum)
	}

	return p, nil
}

// globalFlags returns --database-url and --as if given.
func (a *App) globalFlags(cmd *cobra.Command) (databaseURL, as *string) {
	if cmd.Flags().Changed(config.FlagDatabaseURL) {
		databaseURL = &a.databaseURL
	}

	if cmd.Flags().Changed(config.FlagAs) {
		as = &a.as
	}

	return databaseURL, as
}

// withFields names the flag of a field error by fields.
func withFields(err error, fields map[string]string) error {
	var field *domain.FieldError
	if errors.As(err, &field) {
		if name, ok := fields[field.Field]; ok {
			return flagError{name: name, err: field}
		}
	}

	return err
}

// AppURL is the root of the web app that links in mails point to: BASE_URL,
// or the listen address on localhost.
func AppURL(cfg config.Config) string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL + "/"
	}

	_, port, _ := net.SplitHostPort(cfg.HTTPAddr) // validated by config.Load

	return "http://localhost:" + port + "/"
}

// Wire finishes the command tree of root: the global flags, the completion
// command, and the error handling of every command (Execute). Call it once
// all commands are added.
func (a *App) Wire(root *cobra.Command) *cobra.Command {
	flags := root.PersistentFlags()
	flags.StringVar(&a.databaseURL, config.FlagDatabaseURL, "", DatabaseURLUsage)
	flags.StringVarP(&a.output, FlagOutput, "o", FormatText, "output format: text or json")
	flags.BoolVarP(&a.yes, FlagYes, "y", false, "confirm a destructive command without a prompt")
	flags.StringVar(&a.as, config.FlagAs, "",
		"email of the account a command acts as; its role must reach the command's access level (env "+
			config.EnvAs+")")

	root.SilenceUsage, root.SilenceErrors = true, true
	root.InitDefaultCompletionCmd()

	wire(root)

	return root
}

// wire marks the errors of cmd's own code and its subcommands' (ranError),
// so that Execute tells them from cobra's usage errors, and refuses the
// global flags that a command not built from a Spec does not use.
func wire(cmd *cobra.Command) {
	if run := cmd.RunE; run != nil {
		spec := cmd.Annotations[annotationSpec] != ""

		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if !spec {
				if err := refuseGlobals(cmd); err != nil {
					return ranError{err}
				}
			}

			if err := run(cmd, args); err != nil {
				return ranError{err}
			}

			return nil
		}
	}

	for _, sub := range cmd.Commands() {
		wire(sub)
	}
}

// refuseGlobals refuses --as, --yes and --output json on a command that
// does not support them.
func refuseGlobals(cmd *cobra.Command) error {
	for _, name := range []string{config.FlagAs, FlagYes} {
		if cmd.Flags().Changed(name) {
			return Usagef("--%s: %s does not take it", name, cmd.CommandPath())
		}
	}

	if f := cmd.Flags().Lookup(FlagOutput); f != nil && f.Value.String() != FormatText {
		return Usagef("--%s %s: %s prints text only", FlagOutput, f.Value, cmd.CommandPath())
	}

	return nil
}

// Execute runs root and returns the exit status (ExitCode), after reporting
// an error on stderr (Report), as a problem document with --output json.
func Execute(root *cobra.Command, stderr io.Writer) int {
	err := root.Execute()
	if err != nil {
		f := root.PersistentFlags().Lookup(FlagOutput)
		Report(stderr, err, f != nil && f.Value.String() == FormatJSON)
	}

	return ExitCode(err)
}
