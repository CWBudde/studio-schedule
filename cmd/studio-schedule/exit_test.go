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

			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMainProcess$")
			// Only the variable that selects main(), no DATABASE_URL of the
			// developer's shell; under -cover, a directory for the child's
			// coverage data, or it warns on stderr.
			cmd.Env = []string{envMainArgs + "=" + tc.args, "GOCOVERDIR=" + t.TempDir()}

			var stdout, stderr bytes.Buffer

			cmd.Stdout, cmd.Stderr = &stdout, &stderr

			err := cmd.Run()

			code := 0
			if exit, ok := errors.AsType[*exec.ExitError](err); ok {
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}

			stderrOK := stderr.String() == tc.stderr
			if tc.prefix {
				stderrOK = strings.HasPrefix(stderr.String(), tc.stderr)
			}

			if code != tc.code || stdout.String() != tc.stdout || !stderrOK {
				t.Errorf("studio-schedule %s: exit %d, stdout %q, stderr %q; want exit %d, stdout %q, stderr %q",
					tc.args, code, stdout.String(), stderr.String(), tc.code, tc.stdout, tc.stderr)
			}
		})
	}
}
