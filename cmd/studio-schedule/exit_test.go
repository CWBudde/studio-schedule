package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mainRun runs runMain on the root command with args and an empty
// environment; the command's error output and runMain's go to stderr, as
// in the binary, where both are the process's stderr.
func mainRun(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer

	root := newRootCmd(func(string) (string, bool) { return "", false })
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errOut)

	code = runMain(root, &errOut)

	return code, out.String(), errOut.String()
}

// TestRunMainExitStatus pins the exit status contract of the binary (ADR
// 0020): 0 on success; on a failure "error: <err>" on stderr (and nothing
// else, the usage is silenced) with 1 when the command failed and 2 for a
// usage error: an unknown command or flag, a missing database URL.
func TestRunMainExitStatus(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := mainRun("version")
	if code != 0 || stdout != version+"\n" || stderr != "" {
		t.Errorf("version: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	// Nothing listens on port 1, so connecting fails.
	unreachable := "postgres://app@127.0.0.1:1/studio?connect_timeout=2"

	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"unknown command": {
			args: []string{"nonsense"}, code: 2,
			stderr: "error: unknown command \"nonsense\" for \"studio-schedule\"\n",
		},
		"unknown flag": {args: []string{"version", "--loud"}, code: 2, stderr: "error: unknown flag: --loud\n"},
		"extra argument": {
			args: []string{"version", "x"}, code: 2,
			stderr: "error: unknown command \"x\" for \"studio-schedule version\"\n",
		},
		"seed needs a database": {
			args: []string{"seed"}, code: 2,
			stderr: "error: " + errNoDatabaseURL.Error() + "\n",
		},
		"invalid configuration": {args: []string{"serve", "--addr", "nonsense"}, code: 2},
		"no database to reach":  {args: []string{"seed", "--database-url", unreachable}, code: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := mainRun(tc.args...)
			if code != tc.code || stdout != "" || (tc.stderr != "" && stderr != tc.stderr) ||
				!strings.HasPrefix(stderr, "error: ") {
				t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit %d, stderr %q", tc.args, code, stdout, stderr,
					tc.code, tc.stderr)
			}
		})
	}
}

// TestCommandsOffTheLayerRefuseGlobals: the commands not built on the
// command layer refuse --as, --yes and --output json as a usage error
// (exit 2, ADR 0020) before they run, so nothing reaches stdout.
func TestCommandsOffTheLayerRefuseGlobals(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		args   []string
		stderr string
	}{
		{[]string{"version", "--yes"}, "--yes: studio-schedule version does not take it"},
		{[]string{"openapi", "--output", "json"}, "--output json: studio-schedule openapi prints text only"},
		{
			[]string{"migrate", "status", "--as", "x@example.org"},
			"--as: studio-schedule migrate status does not take it",
		},
		{[]string{"vapid-keys", "-o", "json"}, "--output json: studio-schedule vapid-keys prints text only"},
		{[]string{"completion", "bash", "--yes"}, "--yes: studio-schedule completion bash does not take it"},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			t.Parallel()

			got := cliRun{}.exec(t, c.args...)
			if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, c.stderr) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit 2 with %q", got.code, got.stdout, got.stderr,
					c.stderr)
			}
		})
	}
}

// TestCompletion prints the completion script of each shell cobra knows,
// in the real process: cobra's completion commands write to the stdout of
// the moment Wire adds them, the process's, not to the buffers of an
// in-process run.
func TestCompletion(t *testing.T) {
	t.Parallel()

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := mainProcess(t, "completion\n"+shell)
			if code != 0 || stderr != "" || !strings.Contains(stdout, "studio-schedule") {
				t.Errorf("exit %d, stderr %q, stdout of %d bytes; want a script for studio-schedule", code, stderr,
					len(stdout))
			}
		})
	}

	// --yes is refused before the script is written, on the real stdout too.
	code, stdout, stderr := mainProcess(t, "completion\nbash\n--yes")
	if code != 2 || stdout != "" || stderr != "error: --yes: studio-schedule completion bash does not take it\n" {
		t.Errorf("--yes: exit %d, stdout of %d bytes, stderr %q", code, len(stdout), stderr)
	}
}

// envMainArgs makes TestMainProcess run main() with these arguments
// (separated by newlines) instead of testing.
const envMainArgs = "STUDIO_SCHEDULE_TEST_MAIN_ARGS"

// TestMainProcess runs the test binary again as the real command: main()
// with the process's arguments, environment and exit status.
func TestMainProcess(t *testing.T) {
	if args, ok := os.LookupEnv(envMainArgs); ok {
		os.Args = append([]string{"studio-schedule"}, strings.Split(args, "\n")...)

		main()

		return // not reached: main exits
	}

	t.Parallel()

	for name, tc := range map[string]struct {
		args           string
		code           int
		stdout, stderr string
		// prefix: stderr starts with stderr.
		prefix bool
	}{
		"success": {args: "version", stdout: version + "\n"},
		"usage":   {args: "seed", code: 2, stderr: "error: " + errNoDatabaseURL.Error() + "\n"},
		// The driver's message differs between machines: its start.
		"failure": {
			args: "seed\n--database-url\npostgres://app@127.0.0.1:1/studio?connect_timeout=2", code: 1,
			stderr: "error: postgres: check schema: ", prefix: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			code, stdout, stderr := mainProcess(t, tc.args)

			stderrOK := stderr == tc.stderr
			if tc.prefix {
				stderrOK = strings.HasPrefix(stderr, tc.stderr)
			}

			if code != tc.code || stdout != tc.stdout || !stderrOK {
				t.Errorf("studio-schedule %s: exit %d, stdout %q, stderr %q; want exit %d, stdout %q, stderr %q",
					tc.args, code, stdout, stderr, tc.code, tc.stdout, tc.stderr)
			}
		})
	}
}

// mainProcess runs the test binary again as the real command with args
// (separated by newlines; TestMainProcess) and returns its exit status and
// output.
func mainProcess(t *testing.T, args string) (code int, stdout, stderr string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMainProcess$")
	// Only the variable that selects main(), no DATABASE_URL of the
	// developer's shell; under -cover, a directory for the child's
	// coverage data, or it warns on stderr.
	cmd.Env = []string{envMainArgs + "=" + args, "GOCOVERDIR=" + t.TempDir()}

	var out, errOut bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &errOut

	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}

	return code, out.String(), errOut.String()
}
