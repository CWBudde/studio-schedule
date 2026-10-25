package main

import (
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/config"
)

// newRootCmd builds the command tree. env looks up environment variables
// (os.LookupEnv in main), so tests never touch the process environment.
func newRootCmd(env config.LookupFunc) *cobra.Command {
	root := &cobra.Command{
		Use:           "studio-schedule",
		Short:         "Schedule and booking system for dance and ballet schools",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newServeCmd(env), newMigrateCmd(env), newSeedCmd(env), newOpenAPICmd(), newVersionCmd())
	root.AddCommand(newInitCmd(env))
	root.AddCommand(newImportCmd(env))
	root.AddCommand(newHealthcheckCmd(env))
	root.AddCommand(newVAPIDKeysCmd())

	return root
}

// newLogger builds the logger of the configured format and level.
func newLogger(w io.Writer, cfg config.Log) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}
	if cfg.JSON {
		return slog.New(slog.NewJSONHandler(w, opts))
	}

	return slog.New(slog.NewTextHandler(w, opts))
}
