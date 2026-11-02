package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/diagnose"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// newDBCmd is the group `db` (ADR 0020).
func newDBCmd(app *cli.App) *cobra.Command {
	return cli.Group("db", "Check the database", newDBCheckCmd(app))
}

// dbCheckJSON documents what `db check --output json` prints.
const dbCheckJSON = diagnose.DBCheckJSON

func newDBCheckCmd(app *cli.App) *cobra.Command {
	var (
		only   []string
		checks []service.IntegrityCheck
	)

	return app.Command(cli.Spec{
		Use:   "check",
		Short: "Check the data for inconsistencies the database cannot rule out",
		Long: "Run the integrity checks on the database from " + config.EnvDatabaseURL + " or --database-url " +
			"and print one line per finding: passes (a visit pass's used visits equal the bookings it paid " +
			"for, and no pass paid for a booking that costs no visit: one canceled in time, or one of a " +
			"lesson the school canceled), capacity (no lesson has more active bookings than places, but for " +
			"those booked beyond the capacity on purpose), tombstones (nothing still refers to a merged or " +
			"anonymized person that the merge would have moved or anonymizing ended) and receipt-counters " +
			"(each year's receipt counter is at least the highest receipt number issued that year). --only " +
			"runs some of them. A finding names records by their IDs, never by personal data. The command " +
			"only reads, in one read-only transaction, so it is safe on a live database. It exits with " +
			"status 0 without a finding, 1 with findings or when the database cannot be opened, and 2 for an " +
			"unknown check or without a database URL. With --output json it prints " + dbCheckJSON + ".",
		Example: "  studio-schedule db check\n  studio-schedule db check --only passes,capacity --output json",
		JSON:    dbCheckJSON,
		Store:   true,
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringSliceVar(&only, "only", nil, "run only these checks (repeatable or comma-separated): "+
				integrityCheckNames())
		},
		Fields: map[string]string{"checks": "--only"},
		// An unknown check is refused before the database is opened.
		Validate: func([]string) error {
			names := make([]service.IntegrityCheck, len(only))
			for i, name := range only {
				names[i] = service.IntegrityCheck(name)
			}

			var err error

			checks, err = service.SelectIntegrityChecks(names...)

			return err
		},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			return runDBCheck(ctx, env, checks)
		},
	})
}

// runDBCheck runs checks (service.SelectIntegrityChecks) and prints the
// findings (diagnose.DBCheck); findings are a reported failure after the
// report, so the command exits with status 1 (cli.Reported).
func runDBCheck(ctx context.Context, env *cli.Env, checks []service.IntegrityCheck) error {
	report, err := diagnose.DBCheck(ctx, env.Services.Integrity, checks)
	if err != nil {
		return err
	}

	err = env.Out.Print(report, func(t *cli.Text) {
		if report.OK {
			t.Line("no findings: %s", strings.Join(report.Checks, ", "))

			return
		}

		t.Table("Check", "Entity", "ID", "Message")

		for _, f := range report.Findings {
			t.Row(f.Check, f.Entity, f.ID, f.Message)
		}
	})
	if err != nil || report.OK {
		return err
	}

	noun := "findings"
	if len(report.Findings) == 1 {
		noun = "finding"
	}

	return cli.Reported(fmt.Errorf("%d %s", len(report.Findings), noun))
}

// dbCheckReport is the JSON of db check (diagnose.DBCheck).
type dbCheckReport = diagnose.DBCheckReport

// integrityCheckNames lists the checks, for the help.
func integrityCheckNames() string {
	names := make([]string, 0, len(service.IntegrityChecks()))
	for _, c := range service.IntegrityChecks() {
		names = append(names, string(c))
	}

	return strings.Join(names, ", ")
}
