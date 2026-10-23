package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

func newSeedCmd(env config.LookupFunc) *cobra.Command {
	var db databaseFlag

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Load the fictional demo data set into an empty database",
		Long: "Load the fictional demo data set (school settings, catalog, timetable, families and " +
			"accounts) into the database from " + config.EnvDatabaseURL + " or --database-url. " +
			"The schema must be up to date, and the database must be empty: seed never mixes " +
			"demo data into a school that is in use.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			databaseURL, err := db.load(cmd, env)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			st, err := openPostgres(ctx, databaseURL)
			if err != nil {
				return err
			}

			defer st.Close()

			if err := seed.LoadDemo(ctx, st); err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), "demo data loaded")

			return err
		},
	}

	db.register(cmd.Flags())

	return cmd
}
