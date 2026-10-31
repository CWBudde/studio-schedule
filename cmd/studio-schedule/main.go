// Command studio-schedule is the server binary of Studio Schedule.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(runMain(newRootCmd(os.LookupEnv), os.Stderr))
}

// runMain executes root and returns the exit status: 0, or 1 after printing
// the error to stderr.
func runMain(root *cobra.Command, stderr io.Writer) int {
	if err := root.Execute(); err != nil {
		fmt.Fprintln(stderr, "error:", err)

		return 1
	}

	return 0
}
