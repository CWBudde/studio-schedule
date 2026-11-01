// Command studio-schedule is the server binary of Studio Schedule.
package main

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	sys := cli.OS(openDatabase)
	sys.PDF = startPDF

	os.Exit(runMain(newRoot(sys), os.Stderr))
}

// runMain executes root and returns the exit status of ADR 0020 (0 ok, 1
// failed, 2 usage, 3 not found, 4 conflict), after printing an error to
// stderr as "error: <err>", or as a problem document with --output json.
func runMain(root *cobra.Command, stderr io.Writer) int {
	return cli.Execute(root, stderr)
}
