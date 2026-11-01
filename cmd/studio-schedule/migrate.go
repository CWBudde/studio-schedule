package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/store/postgres"
)

func newMigrateCmd(env config.LookupFunc) *cobra.Command {
	var db databaseFlag

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage the database schema",
		Long: "Apply, roll back or list the schema migrations embedded in the binary. The database " +
			"comes from " + config.EnvDatabaseURL + " or --database-url.",
		Args: cobra.NoArgs,
	}

	// withMigrator runs fn with a migrator for the configured database.
	withMigrator := func(cmd *cobra.Command, fn func(ctx context.Context, m *postgres.Migrator, out io.Writer) error) error {
		databaseURL, err := db.load(cmd, env)
		if err != nil {
			return err
		}

		m, err := postgres.NewMigrator(databaseURL)
		if err != nil {
			return err
		}

		defer func() { _ = m.Close() }()

		// An interrupted migration rolls back its transaction.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		return fn(ctx, m, cmd.OutOrStdout())
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Apply all pending migrations",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return withMigrator(cmd, migrateUp)
			},
		},
		&cobra.Command{
			Use:   "down",
			Short: "Roll back the most recent migration",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return withMigrator(cmd, migrateDown)
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "List the migrations and whether they are applied",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return withMigrator(cmd, migrateStatus)
			},
		},
	)

	return cmd
}

func migrateUp(ctx context.Context, m *postgres.Migrator, out io.Writer) error {
	res, err := m.Up(ctx)
	for _, r := range res {
		printResult(out, r)
	}

	if err != nil {
		return err
	}

	if len(res) == 0 {
		_, err = fmt.Fprintln(out, "schema is up to date")
	}

	return err
}

func migrateDown(ctx context.Context, m *postgres.Migrator, out io.Writer) error {
	r, err := m.Down(ctx)
	if err != nil {
		return err
	}

	printResult(out, r)

	return nil
}

func migrateStatus(ctx context.Context, m *postgres.Migrator, out io.Writer) error {
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "VERSION\tSTATE\tAPPLIED AT\tNAME")

	for _, s := range status {
		state, appliedAt := "pending", "-"
		if s.Applied {
			state, appliedAt = "applied", s.AppliedAt.Format(time.RFC3339)
		}

		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", s.Version, state, appliedAt, s.Name)
	}

	return w.Flush()
}

func printResult(out io.Writer, r postgres.MigrationResult) {
	_, _ = fmt.Fprintf(out, "%-4s %s (%s)\n", r.Direction, r.Name, r.Duration.Round(time.Microsecond))
}
