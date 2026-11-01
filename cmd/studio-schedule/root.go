package main

import (
	"context"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// newRootCmd builds the command tree on the database of DATABASE_URL, with
// env as the environment (nil means empty) and neither stdin nor stdout a
// terminal, so tests never touch the process environment.
func newRootCmd(env config.LookupFunc) *cobra.Command {
	return newRoot(cli.System{Lookup: env, Open: openDatabase})
}

// newRoot builds the command tree on sys (cli.OS in main). A command group
// of the command layer (ADR 0020) lives in a file of its own and is added
// here with one line.
func newRoot(sys cli.System) *cobra.Command {
	app := cli.NewApp(sys)
	env := app.Lookup

	root := &cobra.Command{
		Use:   "studio-schedule",
		Short: "Schedule and booking system for dance and ballet schools",
	}

	root.AddCommand(newServeCmd(env), newMigrateCmd(env), newSeedCmd(env), newOpenAPICmd(), newVersionCmd())
	root.AddCommand(newInitCmd(env))
	root.AddCommand(newImportCmd(app))
	root.AddCommand(newHealthcheckCmd(env))
	root.AddCommand(newVAPIDKeysCmd())

	return app.Wire(root)
}

// openDatabase opens the PostgreSQL store of databaseURL after checking its
// schema (openPostgres).
func openDatabase(ctx context.Context, databaseURL string) (store.Store, func(), error) {
	st, err := openPostgres(ctx, databaseURL)
	if err != nil {
		return nil, nil, err
	}

	return st, st.Close, nil
}

// newLogger builds the logger of the configured format and level.
func newLogger(w io.Writer, cfg config.Log) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}
	if cfg.JSON {
		return slog.New(slog.NewJSONHandler(w, opts))
	}

	return slog.New(slog.NewTextHandler(w, opts))
}
