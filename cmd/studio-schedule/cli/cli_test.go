package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// The demo accounts (seed.DemoLogins).
const (
	demoMember     = "clara.amberly@example.org"
	demoInstructor = "alina.starling@example.net"
	demoOffice     = "olga.ledgerby@example.net"
	demoAdmin      = "arno.keystone@example.net"
)

// testNow is the fixed time of the runs: 22:30 UTC is already the next day
// in the demo school's zone (Europe/Berlin).
var testNow = time.Date(2026, 9, 22, 22, 30, 0, 0, time.UTC)

// record is what the test commands saw and did.
type record struct {
	ran       bool
	changed   bool
	principal service.Principal
	line      string
	// fail is returned by the command fail.
	fail error
	// openErr, pdf and config are what the command env reported.
	openErr error
	pdf     bool
	cfg     config.Config
	// services reports whether the command got services.
	services bool
	// jobs are the names of the jobs the command env built, delivery its
	// senders.
	jobs     []string
	delivery Delivery
}

// testCommands are the commands of the test tree, built on a.
func testCommands(a *App, rec *record) []*cobra.Command {
	var only string // --only of validate

	whoami := func(access httpapi.Access) *cobra.Command {
		return a.Command(Spec{
			Use: "whoami-" + string(access), Short: "print the account of --as", Access: access,
			Run: func(_ context.Context, env *Env, _ []string) error {
				rec.ran, rec.principal = true, env.Principal

				return env.Out.Print(map[string]string{"email": env.Principal.Email}, func(t *Text) {
					t.Line("%s (%s)", env.Principal.Email, env.Principal.Role)
				})
			},
		})
	}

	return []*cobra.Command{
		whoami(httpapi.AccessSignedIn), whoami(httpapi.AccessStaff), whoami(httpapi.AccessOffice),
		whoami(httpapi.AccessAdmin),
		a.Command(Spec{
			Use: "destroy", Short: "change data after a confirmation", Store: true, Destructive: true,
			Run: func(_ context.Context, env *Env, _ []string) error {
				rec.ran = true
				if err := env.Confirm("Destroy everything?"); err != nil {
					return err
				}

				rec.changed = true

				return nil
			},
		}),
		a.Command(Spec{
			Use: "read", Short: "read a line from stdin",
			Run: func(_ context.Context, env *Env, _ []string) error {
				line, err := env.ReadLine("Search term", "search term")
				rec.line = line

				return err
			},
		}),
		a.Command(Spec{
			Use: "when", Short: "print the time in the school's zone", Store: true,
			Run: func(_ context.Context, env *Env, _ []string) error {
				rec.services = env.Services.Settings != nil

				return env.Out.Print(nil, func(t *Text) { t.Line("%s", t.Time(env.Clock.Now())) })
			},
		}),
		a.Command(Spec{
			Use: "fail", Short: "return an error", Args: cobra.MaximumNArgs(1),
			Fields: map[string]string{"email": "--email"},
			Flags:  func(cmd *cobra.Command) { cmd.Flags().String("email", "", "an email address") },
			Run: func(context.Context, *Env, []string) error {
				rec.ran = true

				return rec.fail
			},
		}),
		a.Command(Spec{
			Use: "validate", Short: "check --only before the database opens", Store: true,
			Fields: map[string]string{"checks": "--only"},
			Flags:  func(cmd *cobra.Command) { cmd.Flags().StringVar(&only, "only", "", "a check") },
			Validate: func([]string) error {
				if only != "" && only != "known" {
					return &domain.FieldError{Field: "checks", Msg: fmt.Sprintf("unknown check %q", only)}
				}

				return nil
			},
			Run: func(context.Context, *Env, []string) error { rec.ran = true; return nil },
		}),
		a.Command(Spec{
			Use: "table", Short: "print a table",
			Run: func(_ context.Context, env *Env, _ []string) error {
				rec.services = env.Services.Settings != nil

				return env.Out.Print([]string{"Olga Ledgerby"}, func(t *Text) {
					t.Table("Name", "Role")
					t.Row("Olga Ledgerby", t.Paint(Green, "office"))
				})
			},
		}),
		a.Command(Spec{
			Use: "env", Short: "use the env's configuration, database and renderer",
			Run: func(ctx context.Context, env *Env, _ []string) error {
				cfg, err := env.Config(config.Flags{})
				if err != nil {
					return err
				}

				rec.cfg = cfg

				svc, release, err := env.Open(ctx, cfg)
				if rec.openErr = err; err == nil {
					rec.services = svc.Settings != nil
					release()
				}

				if r, stop, ok := env.PDF(); ok {
					rec.pdf = r != nil
					stop()
				}

				_, err = fmt.Fprintln(env.Stderr(), "env done")

				return err
			},
		}),
		{
			Use: "legacy", Short: "a command not built from a Spec",
			RunE: func(cmd *cobra.Command, _ []string) error {
				rec.ran = true

				_, err := fmt.Fprintln(cmd.OutOrStdout(), "legacy ran")

				return err
			},
		},
		a.Command(Spec{
			Use: "jobs", Short: "list the jobs and run one", Store: true, Args: cobra.MaximumNArgs(1),
			Run: func(ctx context.Context, env *Env, args []string) error {
				runner, release, err := env.Jobs()
				if err != nil {
					return err
				}

				defer release()

				for _, j := range runner.Jobs() {
					rec.jobs = append(rec.jobs, j.Name)
				}

				if len(args) == 1 {
					_, err = runner.RunNow(ctx, args[0])
				}

				return err
			},
		}),
		a.Command(Spec{
			Use: "jobs-without-store", Short: "ask for the jobs without the database",
			Run: func(_ context.Context, env *Env, _ []string) error {
				_, _, err := env.Jobs()

				return err
			},
		}),
		a.Command(Spec{
			Use: "delivery", Short: "build the senders",
			Run: func(_ context.Context, env *Env, _ []string) error {
				var err error

				rec.delivery, err = env.Delivery(config.Config{})

				return err
			},
		}),
		Group("grp", "a group", a.Command(Spec{
			Use: "sub", Short: "a subcommand",
			Run: func(context.Context, *Env, []string) error { rec.ran = true; return nil },
		})),
	}
}

// harness runs the test tree in-process.
type harness struct {
	// env is the environment; DATABASE_URL is set to a dummy unless it
	// has the key (an empty value removes it).
	env      map[string]string
	stdin    string
	terminal bool
	// st is the store every command opens; open replaces the opener.
	st       store.Store
	open     Opener
	pdf      func() (service.ReceiptRenderer, func())
	delivery DeliveryFunc
}

// outcome is what a run printed and its exit status.
type outcome struct {
	code           int
	stdout, stderr string
}

func (h harness) run(t *testing.T, rec *record, args ...string) outcome {
	t.Helper()

	env := map[string]string{config.EnvDatabaseURL: "postgres://memory.example.test/studio"}
	for k, v := range h.env {
		if v == "" {
			delete(env, k)
		} else {
			env[k] = v
		}
	}

	open := h.open
	if open == nil {
		st := h.st
		open = func(context.Context, string) (store.Store, func(), error) { return st, func() {}, nil }
	}

	a := NewApp(System{
		Lookup:         func(key string) (string, bool) { v, ok := env[key]; return v, ok },
		StdinTerminal:  h.terminal,
		StdoutTerminal: h.terminal,
		OSUser:         "operator",
		Open:           open,
		PDF:            h.pdf,
		Delivery:       h.delivery,
		Clock:          clock.NewFixed(testNow),
		IDs:            ids.NewSequence(),
	})

	var stdout, stderr bytes.Buffer

	// The output is set before Wire: cobra's completion command keeps the
	// stdout of the moment it is added.
	root := &cobra.Command{Use: "studio-schedule"}
	root.SetArgs(args)
	root.SetIn(strings.NewReader(h.stdin))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.AddCommand(testCommands(a, rec)...)
	a.Wire(root)

	code := Execute(root, &stderr)

	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
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

// changeAccount changes the account of email in st.
func changeAccount(t *testing.T, st store.Store, email string, change func(a *domain.Account)) {
	t.Helper()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().GetByEmail(ctx, email)
		if err != nil {
			return err
		}

		change(&acc)

		return tx.Accounts().Update(ctx, acc)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestActor: a command with an access level acts as the account of --as
// (or STUDIO_AS), whose role must reach the level; an account that cannot
// act exits with 3, a role too low with 4, both before the command runs.
func TestActor(t *testing.T) {
	t.Parallel()

	st := demoStore(t)

	// Two further member accounts of the demo: one becomes unconfirmed,
	// one disabled.
	const unconfirmed, disabled = "kai.lanternby@example.org", "oskar.nettlebury@example.org"

	changeAccount(t, st, unconfirmed, func(a *domain.Account) { a.ConfirmedAt = time.Time{} })
	changeAccount(t, st, disabled, func(a *domain.Account) { a.DisabledAt = testNow.Add(-time.Hour) })

	// Before, both can act.
	for _, email := range []string{unconfirmed, disabled} {
		if got := (harness{st: demoStore(t)}).run(t, &record{}, "whoami-signedIn", "--as", email); got.code != ExitOK {
			t.Fatalf("%s: exit %d, stderr %q", email, got.code, got.stderr)
		}
	}

	h := harness{st: st}

	for name, tc := range map[string]struct {
		env    map[string]string
		args   []string
		code   int
		stderr string
	}{
		"no --as": {
			args: []string{"whoami-office"}, code: ExitUsage,
			stderr: "error: studio-schedule whoami-office acts as an account: give --as <email> or set STUDIO_AS\n",
		},
		"unknown": {
			args: []string{"whoami-office", "--as", "nobody@example.org"}, code: ExitNotFound,
			stderr: "error: --as nobody@example.org: not found: no account has this email\n",
		},
		"unconfirmed": {
			args: []string{"whoami-signedIn", "--as", unconfirmed}, code: ExitNotFound,
			stderr: "error: --as " + unconfirmed + ": not found: the account is not confirmed or is disabled\n",
		},
		"disabled": {
			args: []string{"whoami-signedIn", "--as", disabled}, code: ExitNotFound,
			stderr: "error: --as " + disabled + ": not found: the account is not confirmed or is disabled\n",
		},
		"member on staff": {
			args: []string{"whoami-staff", "--as", demoMember}, code: ExitConflict,
			stderr: "error: --as " + demoMember + ": forbidden: the account's role member is below instructor, " +
				"which this command needs\n",
		},
		"instructor on office": {args: []string{"whoami-office", "--as", demoInstructor}, code: ExitConflict},
		"office on admin":      {args: []string{"whoami-admin", "--as", demoOffice}, code: ExitConflict},
		"STUDIO_AS on office": {
			env: map[string]string{config.EnvAs: demoOffice}, args: []string{"whoami-office"},
			code: ExitOK,
		},
		"--as over STUDIO_AS": {
			env:  map[string]string{config.EnvAs: demoMember},
			args: []string{"whoami-admin", "--as", demoAdmin}, code: ExitOK,
		},
		"member signed in": {args: []string{"whoami-signedIn", "--as", demoMember}, code: ExitOK},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var rec record

			h := h
			h.env = tc.env

			got := h.run(t, &rec, tc.args...)
			if got.code != tc.code || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("%v: exit %d, stderr %q; want exit %d, stderr %q", tc.args, got.code, got.stderr, tc.code,
					tc.stderr)
			}

			if rec.ran != (tc.code == ExitOK) {
				t.Errorf("%v: the command ran: %t", tc.args, rec.ran)
			}
		})
	}
}

// TestActorPrincipal: the principal is the account's, without a session,
// acting through the command line as the OS user.
func TestActorPrincipal(t *testing.T) {
	t.Parallel()

	var rec record

	got := harness{st: demoStore(t)}.run(t, &rec, "whoami-office", "--as", " "+strings.ToUpper(demoAdmin)+" ")
	if got.code != ExitOK || got.stdout != demoAdmin+" (admin)\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	p := rec.principal
	if p.Email != demoAdmin || p.Role != domain.RoleAdmin || p.AccountID.IsZero() || !p.SessionID.IsZero() ||
		p.Via != (domain.AuditVia{Channel: domain.ChannelCLI, OSUser: "operator"}) {
		t.Errorf("principal = %+v", p)
	}

	// --output json: the document on stdout.
	got = harness{st: demoStore(t)}.run(t, &rec, "whoami-office", "--as", demoOffice, "-o", "json")
	if got.code != ExitOK || got.stdout != "{\n  \"email\": \""+demoOffice+"\"\n}\n" {
		t.Errorf("JSON: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}
}

// TestFlagsOfCommands: --as on an operator command, and --as, --yes or
// --output json on a command not built from a Spec, are usage errors that
// run nothing; so is an --output that is neither text nor json.
func TestFlagsOfCommands(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args   []string
		stderr string
	}{
		"--as on an operator Spec": {
			args:   []string{"table", "--as", demoOffice},
			stderr: "error: --as: studio-schedule table acts as no account\n",
		},
		"--as on legacy": {
			args:   []string{"legacy", "--as", demoOffice},
			stderr: "error: --as: studio-schedule legacy does not take it\n",
		},
		"--yes on legacy": {
			args:   []string{"legacy", "--yes"},
			stderr: "error: --yes: studio-schedule legacy does not take it\n",
		},
		"--output json on legacy": {args: []string{"legacy", "--output", "json"}},
		"invalid --output": {
			args:   []string{"table", "--output", "yaml"},
			stderr: "error: --output: \"yaml\" is not text or json\n",
		},
		"invalid --output, legacy": {
			args:   []string{"legacy", "-o", "yaml"},
			stderr: "error: --output yaml: studio-schedule legacy prints text only\n",
		},
		"completion --as": {args: []string{"completion", "bash", "--as", demoOffice}},
		"unknown command": {
			args:   []string{"nonsense"},
			stderr: "error: unknown command \"nonsense\" for \"studio-schedule\"\n",
		},
		"unknown flag": {args: []string{"table", "--loud"}, stderr: "error: unknown flag: --loud\n"},
		"extra args":   {args: []string{"table", "x"}},
		"group, unknown subcommand": {
			args:   []string{"grp", "nonsense"},
			stderr: "error: unknown command \"nonsense\" for \"studio-schedule grp\"\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var rec record

			got := harness{st: memory.New()}.run(t, &rec, tc.args...)
			if got.code != ExitUsage || got.stdout != "" || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit 2, stderr %q", tc.args, got.code, got.stdout,
					got.stderr, tc.stderr)
			}

			if rec.ran {
				t.Errorf("%v ran", tc.args)
			}
		})
	}

	// --output json on a usage error: the problem document on stderr.
	got := harness{st: memory.New()}.run(t, &record{}, "legacy", "--output", "json")
	if got.code != ExitUsage || !strings.Contains(got.stderr, `"status": 400`) ||
		!strings.Contains(got.stderr, `"detail": "--output json: studio-schedule legacy prints text only"`) {
		t.Errorf("legacy --output json: exit %d, stderr %s", got.code, got.stderr)
	}
}

func TestLegacyAndGroup(t *testing.T) {
	t.Parallel()

	var rec record

	got := harness{}.run(t, &rec, "legacy", "--output", "text")
	if got.code != ExitOK || got.stdout != "legacy ran\n" || !rec.ran {
		t.Errorf("legacy: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	// The group without a subcommand prints its help.
	got = harness{}.run(t, &record{}, "grp")
	if got.code != ExitOK || !strings.Contains(got.stdout, "Usage:\n  studio-schedule grp [flags]") ||
		!strings.Contains(got.stdout, "sub ") || got.stderr != "" {
		t.Errorf("grp: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	rec = record{}

	got = harness{}.run(t, &rec, "grp", "sub")
	if got.code != ExitOK || !rec.ran {
		t.Errorf("grp sub: exit %d, stderr %q", got.code, got.stderr)
	}
}

// TestConfirm: a destructive command changes data only when confirmed:
// with --yes, or an answer y or yes on a terminal; without a terminal and
// --yes it refuses with exit 2.
func TestConfirm(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		terminal bool
		stdin    string
		yes      bool
		code     int
		stderr   string
	}{
		"--yes":             {yes: true, stderr: ""},
		"--yes on terminal": {terminal: true, yes: true, stderr: ""},
		"y":                 {terminal: true, stdin: "y\n", stderr: "Destroy everything? [y/N] "},
		"yes":               {terminal: true, stdin: " yes \n", stderr: "Destroy everything? [y/N] "},
		"YES at EOF":        {terminal: true, stdin: "YES", stderr: "Destroy everything? [y/N] "},
		"n": {
			terminal: true, stdin: "n\n", code: ExitUsage,
			stderr: "Destroy everything? [y/N] error: not confirmed; nothing was changed\n",
		},
		"empty": {
			terminal: true, stdin: "\n", code: ExitUsage,
			stderr: "Destroy everything? [y/N] error: not confirmed; nothing was changed\n",
		},
		"EOF": {
			terminal: true, code: ExitUsage,
			stderr: "Destroy everything? [y/N] error: not confirmed; nothing was changed\n",
		},
		"no terminal": {
			stdin: "y\n", code: ExitUsage,
			stderr: "error: studio-schedule destroy changes data: confirm with --yes when stdin is not a " +
				"terminal; nothing was changed\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			args := []string{"destroy"}
			if tc.yes {
				args = append(args, "--yes")
			}

			var rec record

			got := harness{st: memory.New(), terminal: tc.terminal, stdin: tc.stdin}.run(t, &rec, args...)
			if got.code != tc.code || got.stderr != tc.stderr || got.stdout != "" {
				t.Errorf("exit %d, stderr %q; want exit %d, stderr %q", got.code, got.stderr, tc.code, tc.stderr)
			}

			if !rec.ran || rec.changed != (tc.code == ExitOK) {
				t.Errorf("ran %t, changed %t", rec.ran, rec.changed)
			}
		})
	}

	// -y is --yes.
	var rec record

	if got := (harness{st: memory.New()}).run(t, &rec, "destroy", "-y"); got.code != ExitOK || !rec.changed {
		t.Errorf("-y: exit %d, stderr %q", got.code, got.stderr)
	}
}

// TestConfirmReadFails: an answer that cannot be read is the read error.
func TestConfirmReadFails(t *testing.T) {
	t.Parallel()

	broken := errors.New("stdin is gone")

	for name, run := range map[string]func(e *Env) error{
		"confirm":  func(e *Env) error { return e.Confirm("Destroy?") },
		"readline": func(e *Env) error { _, err := e.ReadLine("Term", "term"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := testEnv(t, failingReader{broken}, &bytes.Buffer{})
			if err := run(e); !errors.Is(err, broken) {
				t.Errorf("%v, want the read error", err)
			}

			// A prompt that cannot be written is the write error.
			e = testEnv(t, strings.NewReader("y\n"), failingWriter{broken})
			if err := run(e); !errors.Is(err, broken) {
				t.Errorf("prompt: %v, want the write error", err)
			}
		})
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// testEnv is the Env of a terminal with stdin in and stderr errOut.
func testEnv(t *testing.T, in interface{ Read([]byte) (int, error) }, errOut interface {
	Write([]byte) (int, error)
},
) *Env {
	t.Helper()

	cmd := &cobra.Command{Use: "x"}
	cmd.SetErr(errOut)

	e := &Env{app: NewApp(System{StdinTerminal: true}), cmd: cmd}
	e.in = bufio.NewReader(in)

	return e
}

// TestReadLine: a line from stdin, after a prompt only on a terminal; an
// empty one is a usage error.
func TestReadLine(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		terminal bool
		stdin    string
		line     string
		code     int
		stderr   string
	}{
		"terminal":        {terminal: true, stdin: "  Amberly  \nsecond\n", line: "Amberly", stderr: "Search term: "},
		"pipe":            {stdin: "Amberly", line: "Amberly"},
		"empty":           {stdin: " \n", code: ExitUsage, stderr: "error: no search term on stdin\n"},
		"empty, terminal": {terminal: true, code: ExitUsage, stderr: "Search term: error: no search term on stdin\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var rec record

			got := harness{terminal: tc.terminal, stdin: tc.stdin}.run(t, &rec, "read")
			if got.code != tc.code || got.stderr != tc.stderr || rec.line != tc.line {
				t.Errorf("exit %d, stderr %q, line %q; want exit %d, stderr %q, line %q", got.code, got.stderr,
					rec.line, tc.code, tc.stderr, tc.line)
			}
		})
	}
}

// TestOutputOfTerminal: the header line only on a terminal, color only on
// one without NO_COLOR.
func TestOutputOfTerminal(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		terminal bool
		env      map[string]string
		want     string
	}{
		"pipe": {want: "Olga Ledgerby  office\n"},
		"terminal": {
			terminal: true,
			want:     "\x1b[1mName\x1b[0m           \x1b[1mRole\x1b[0m\nOlga Ledgerby  \x1b[32moffice\x1b[0m\n",
		},
		"NO_COLOR": {
			terminal: true, env: map[string]string{config.EnvNoColor: "1"},
			want: "Name           Role\nOlga Ledgerby  office\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var rec record

			got := harness{terminal: tc.terminal, env: tc.env}.run(t, &rec, "table")
			if got.code != ExitOK || got.stdout != tc.want {
				t.Errorf("exit %d, stdout %q, stderr %q; want %q", got.code, got.stdout, got.stderr, tc.want)
			}

			if rec.services {
				t.Error("a command without Store got services")
			}
		})
	}

	got := harness{terminal: true}.run(t, &record{}, "table", "--output", "json")
	if got.code != ExitOK || got.stdout != "[\n  \"Olga Ledgerby\"\n]\n" {
		t.Errorf("JSON: exit %d, stdout %q", got.code, got.stdout)
	}
}

// TestSchoolTime: times are printed in the school's zone, in UTC before
// the school has settings.
func TestSchoolTime(t *testing.T) {
	t.Parallel()

	var rec record

	got := harness{st: demoStore(t)}.run(t, &rec, "when")
	if got.code != ExitOK || got.stdout != "2026-09-23 00:30\n" || !rec.services {
		t.Errorf("demo school: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	got = harness{st: memory.New()}.run(t, &rec, "when")
	if got.code != ExitOK || got.stdout != "2026-09-22 22:30\n" {
		t.Errorf("no settings: exit %d, stdout %q, stderr %q", got.code, got.stdout, got.stderr)
	}

	// A zone the memory store keeps although Go does not know it.
	st := demoStore(t)

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		s.Timezone = "Mars/Olympus_Mons"

		return tx.Settings().Put(ctx, s)
	})
	if err != nil {
		t.Fatal(err)
	}

	got = harness{st: st}.run(t, &rec, "when")
	if got.code != ExitFailed || !strings.HasPrefix(got.stderr, `error: the school's time zone "Mars/Olympus_Mons": `) {
		t.Errorf("unknown zone: exit %d, stderr %q", got.code, got.stderr)
	}
}

// brokenStore fails every transaction after the first ok ones.
type brokenStore struct {
	store.Store

	ok  int
	err error
}

func (s *brokenStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	if s.ok == 0 {
		return s.err
	}

	s.ok--

	return s.Store.InTx(ctx, mode, fn)
}

// TestOpen: the database is opened for a Spec with Store or an access
// level, after the database URL is checked; what fails there runs nothing.
func TestOpen(t *testing.T) {
	t.Parallel()

	broken := errors.New("connection refused")

	for name, tc := range map[string]struct {
		h      harness
		args   []string
		code   int
		stderr string
	}{
		"no database URL": {
			h:    harness{env: map[string]string{config.EnvDatabaseURL: ""}},
			args: []string{"when"}, code: ExitUsage, stderr: "error: " + ErrNoDatabaseURL.Error() + "\n",
		},
		"no database URL, --as": {
			h:    harness{env: map[string]string{config.EnvDatabaseURL: ""}},
			args: []string{"whoami-office", "--as", demoOffice}, code: ExitUsage,
			stderr: "error: " + ErrNoDatabaseURL.Error() + "\n",
		},
		"invalid config": {
			h:    harness{st: memory.New(), env: map[string]string{config.EnvHTTPAddr: "nonsense"}},
			args: []string{"when"}, code: ExitUsage,
		},
		"open fails": {
			h: harness{open: func(context.Context, string) (store.Store, func(), error) {
				return nil, nil, broken
			}},
			args: []string{"whoami-office", "--as", demoOffice}, code: ExitFailed,
			stderr: "error: connection refused\n",
		},
		"settings fail": {
			h:    harness{st: &brokenStore{Store: demoStore(t), err: broken}},
			args: []string{"when"}, code: ExitFailed, stderr: "error: public settings: connection refused\n",
		},
		"actor fails": {
			h:    harness{st: &brokenStore{Store: demoStore(t), ok: 1, err: broken}},
			args: []string{"whoami-office", "--as", demoOffice}, code: ExitFailed,
			stderr: "error: --as " + demoOffice + ": actor: connection refused\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var rec record

			got := tc.h.run(t, &rec, tc.args...)
			if got.code != tc.code || (tc.stderr != "" && got.stderr != tc.stderr) {
				t.Errorf("exit %d, stderr %q; want exit %d, stderr %q", got.code, got.stderr, tc.code, tc.stderr)
			}

			if rec.ran {
				t.Error("the command ran")
			}
		})
	}

	// --database-url names the database without the env.
	var opened string

	h := harness{
		env: map[string]string{config.EnvDatabaseURL: ""},
		open: func(_ context.Context, url string) (store.Store, func(), error) {
			opened = url

			return memory.New(), func() {}, nil
		},
	}
	if got := h.run(t, &record{}, "when", "--database-url", "postgres://flag.example.test/studio"); got.code != ExitOK ||
		opened != "postgres://flag.example.test/studio" {
		t.Errorf("--database-url: exit %d, opened %q, stderr %q", got.code, opened, got.stderr)
	}
}

// TestFailures: the error of a command's own code is mapped by its class;
// a field error names the flag of the Spec's Fields.
func TestFailures(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err    error
		code   int
		stderr string
	}{
		"ok":         {code: ExitOK},
		"not found":  {err: fmt.Errorf("person: %w", domain.ErrNotFound), code: ExitNotFound},
		"in use":     {err: domain.ErrInUse, code: ExitConflict},
		"failed":     {err: errors.New("disk full"), code: ExitFailed, stderr: "error: disk full\n"},
		"validation": {err: fmt.Errorf("%w: bad date", domain.ErrValidation), code: ExitUsage},
		"named field": {
			err: &domain.FieldError{Field: "email", Msg: "must be an email address"}, code: ExitUsage,
			stderr: "error: --email: must be an email address\n",
		},
		"other field": {
			err: &domain.FieldError{Field: "name", Msg: "must not be empty"}, code: ExitUsage,
			stderr: "error: validation failed: must not be empty\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := record{fail: tc.err}

			got := harness{}.run(t, &rec, "fail", "--email", "olga")
			if got.code != tc.code || (tc.stderr != "" && got.stderr != tc.stderr) || !rec.ran {
				t.Errorf("exit %d, stderr %q, ran %t; want exit %d, stderr %q", got.code, got.stderr, rec.ran, tc.code,
					tc.stderr)
			}
		})
	}

	// With --output json the field error is a problem document naming the
	// flag.
	rec := record{fail: &domain.FieldError{Field: "email", Msg: "must be an email address"}}

	got := harness{}.run(t, &rec, "fail", "-o", "json")
	if got.code != ExitUsage || !strings.Contains(got.stderr, `"status": 422`) ||
		!strings.Contains(got.stderr, `"location": "--email"`) {
		t.Errorf("JSON: exit %d, stderr %s", got.code, got.stderr)
	}
}

// TestEnv: a command that opens the database itself gets the
// configuration, the services of the opener and the receipt renderer.
func TestEnv(t *testing.T) {
	t.Parallel()

	var rec record

	h := harness{
		st:  memory.New(),
		env: map[string]string{config.EnvBaseURL: "https://schedule.example.org", config.EnvAs: demoOffice},
		pdf: func() (service.ReceiptRenderer, func()) { return fakeRenderer{}, func() {} },
	}

	got := h.run(t, &rec, "env", "--database-url", "postgres://flag.example.test/studio")
	if got.code != ExitOK || got.stderr != "env done\n" {
		t.Fatalf("exit %d, stderr %q", got.code, got.stderr)
	}

	if rec.cfg.DatabaseURL != "postgres://flag.example.test/studio" || rec.cfg.BaseURL != "https://schedule.example.org" ||
		rec.cfg.As != demoOffice || !rec.services || rec.openErr != nil || !rec.pdf {
		t.Errorf("record = %+v", rec)
	}

	// Without a renderer and with a database that does not open.
	broken := errors.New("connection refused")
	rec = record{}
	h = harness{open: func(context.Context, string) (store.Store, func(), error) { return nil, nil, broken }}

	if got := h.run(t, &rec, "env"); got.code != ExitOK || !errors.Is(rec.openErr, broken) || rec.pdf || rec.services {
		t.Errorf("exit %d, record %+v", got.code, rec)
	}

	// An invalid configuration is Config's error.
	h = harness{env: map[string]string{config.EnvBaseURL: "/relative"}}
	if got := h.run(t, &record{}, "env"); got.code != ExitUsage {
		t.Errorf("invalid config: exit %d, stderr %q", got.code, got.stderr)
	}
}

type fakeRenderer struct{ service.ReceiptRenderer }

// TestValidate refuses invalid input before the database is opened: an
// unknown check is a usage error naming its flag even when the database
// cannot be reached, as text and as a problem document.
func TestValidate(t *testing.T) {
	t.Parallel()

	unreachable := harness{open: func(context.Context, string) (store.Store, func(), error) {
		return nil, nil, errors.New("connection refused")
	}}

	var rec record

	got := unreachable.run(t, &rec, "validate", "--only", "bogus")
	if got.code != ExitUsage || got.stderr != "error: --only: unknown check \"bogus\"\n" || rec.ran {
		t.Errorf("unknown check: exit %d, stderr %q, ran %t", got.code, got.stderr, rec.ran)
	}

	got = unreachable.run(t, &rec, "validate", "--only", "bogus", "-o", "json")
	if got.code != ExitUsage || !strings.Contains(got.stderr, `"location": "--only"`) {
		t.Errorf("unknown check, JSON: exit %d, stderr %s", got.code, got.stderr)
	}

	// Valid input goes on to open the database.
	if got := unreachable.run(t, &rec, "validate", "--only", "known"); got.code != ExitFailed ||
		got.stderr != "error: connection refused\n" || rec.ran {
		t.Errorf("known check, unreachable: exit %d, stderr %q, ran %t", got.code, got.stderr, rec.ran)
	}

	if got := (harness{st: demoStore(t)}).run(t, &rec, "validate", "--only", "known"); got.code != ExitOK || !rec.ran {
		t.Errorf("known check: exit %d, stderr %q, ran %t", got.code, got.stderr, rec.ran)
	}
}

func TestAppURL(t *testing.T) {
	t.Parallel()

	if got := AppURL(config.Config{BaseURL: "https://schedule.example.org", HTTPAddr: ":8080"}); got !=
		"https://schedule.example.org/" {
		t.Errorf("with BASE_URL: %q", got)
	}

	if got := AppURL(config.Config{HTTPAddr: "0.0.0.0:9090"}); got != "http://localhost:9090/" {
		t.Errorf("without: %q", got)
	}
}

// TestOS: the System of the process reads its environment and names its
// user.
func TestOS(t *testing.T) {
	t.Setenv("STUDIO_SCHEDULE_CLI_TEST", "set")

	opened := false
	sys := OS(func(context.Context, string) (store.Store, func(), error) { opened = true; return nil, nil, nil })

	if v, ok := sys.Lookup("STUDIO_SCHEDULE_CLI_TEST"); !ok || v != "set" {
		t.Errorf("Lookup = %q, %t", v, ok)
	}

	if sys.OSUser == "" {
		t.Error("no OS user")
	}

	if _, _, _ = sys.Open(t.Context(), ""); !opened {
		t.Error("Open is not the opener")
	}

	if sys.PDF != nil || sys.Clock != nil || sys.IDs != nil {
		t.Errorf("OS sets more than the process's: %+v", sys)
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}

	if isTerminal(f) {
		t.Error("a file is a terminal")
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if isTerminal(f) {
		t.Error("a closed file is a terminal")
	}
}

// TestNewAppDefaults: without a Lookup the environment is empty; the clock
// and the IDs default to the system's.
func TestNewAppDefaults(t *testing.T) {
	t.Parallel()

	a := NewApp(System{})
	if _, ok := a.Lookup(config.EnvDatabaseURL); ok {
		t.Error("an empty System has an environment")
	}

	if _, ok := a.sys.Clock.(clock.System); !ok {
		t.Errorf("clock = %T", a.sys.Clock)
	}

	if _, ok := a.sys.IDs.(ids.V7); !ok {
		t.Errorf("IDs = %T", a.sys.IDs)
	}
}

// TestWire: the global flags and a completion command for each shell.
func TestWire(t *testing.T) {
	t.Parallel()

	// cobra adds the completion command to a root with subcommands.
	a := NewApp(System{})
	root := &cobra.Command{Use: "studio-schedule"}
	root.AddCommand(testCommands(a, &record{})...)
	a.Wire(root)

	for _, name := range []string{config.FlagDatabaseURL, FlagOutput, FlagYes, config.FlagAs} {
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("no global --%s", name)
		}
	}

	completion, _, err := root.Find([]string{"completion"})
	if err != nil || completion.Name() != "completion" {
		t.Fatalf("completion: %v", err)
	}

	var shells []string
	for _, c := range completion.Commands() {
		shells = append(shells, c.Name())
	}

	slices.Sort(shells)

	if want := []string{"bash", "fish", "powershell", "zsh"}; !slices.Equal(shells, want) {
		t.Errorf("completion shells = %v, want %v", shells, want)
	}

	// Each prints its script.
	for _, shell := range shells {
		var rec record

		got := harness{}.run(t, &rec, "completion", shell)
		if got.code != ExitOK || !strings.Contains(got.stdout, "studio-schedule") {
			t.Errorf("completion %s: exit %d, stderr %q", shell, got.code, got.stderr)
		}
	}
}

// TestCommandAnnotations: the reference reads the access level, whether
// the command is destructive and its JSON from the annotations.
func TestCommandAnnotations(t *testing.T) {
	t.Parallel()

	a := NewApp(System{})

	cmd := a.Command(Spec{
		Use: "anonymize", Access: httpapi.AccessOffice, Destructive: true,
		JSON: "the response body of anonymizePerson",
	})
	if cmd.Annotations[AnnotationAccess] != "office" || cmd.Annotations[AnnotationDestructive] != "true" ||
		cmd.Annotations[AnnotationJSON] != "the response body of anonymizePerson" {
		t.Errorf("annotations = %v", cmd.Annotations)
	}

	cmd = a.Command(Spec{Use: "check"})
	if _, ok := cmd.Annotations[AnnotationAccess]; ok {
		t.Errorf("an operator command has an access level: %v", cmd.Annotations)
	}

	if _, ok := cmd.Annotations[AnnotationDestructive]; ok {
		t.Errorf("a command is destructive: %v", cmd.Annotations)
	}

	if err := cmd.Args(cmd, []string{"x"}); err == nil {
		t.Error("a Spec without Args takes arguments")
	}
}

func TestCommandUnknownAccess(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), `has an unknown access level nobody`) {
			t.Errorf("recovered %v, want the unknown level", r)
		}
	}()

	NewApp(System{}).Command(Spec{Use: "x", Access: "nobody"})
}
