package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// newDBCmd is the group `db` (ADR 0020). Its commands open the database
// with open themselves.
func newDBCmd(app *cli.App, open cli.Opener) *cobra.Command {
	return cli.Group("db", "Check the database", newDBCheckCmd(app, open))
}

// dbCheckJSON documents what `db check --output json` prints.
const dbCheckJSON = `one object: ok (bool, false when a check found something), checks (an array of the ` +
	`names of the checks that ran, in their order) and findings (an array, empty without any, of {check, ` +
	`entity, id, message}: the check that found it; the entity pass, booking, lesson, person or ` +
	`receipt-year; the record's ID, or the year of a receipt counter; and what is inconsistent)`

func newDBCheckCmd(app *cli.App, open cli.Opener) *cobra.Command {
	var (
		only []string
		cmd  *cobra.Command
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
		Flags: func(c *cobra.Command) {
			cmd = c
			c.Flags().StringSliceVar(&only, "only", nil, "run only these checks (repeatable or comma-separated): "+
				integrityCheckNames())
		},
		Fields: map[string]string{"checks": "--only"},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			url, err := databaseFlag{}.load(cmd, app.Lookup)
			if err != nil {
				return err
			}

			checks := make([]service.IntegrityCheck, len(only))
			for i, name := range only {
				checks[i] = service.IntegrityCheck(name)
			}

			// An unknown check is refused before the database is opened.
			if checks, err = service.SelectIntegrityChecks(checks...); err != nil {
				return err
			}

			return runDBCheck(ctx, env, open, url, checks)
		},
	})
}

// runDBCheck runs checks (service.SelectIntegrityChecks) on the database of url and
// prints the findings; findings are an error after the report, so the
// command exits with status 1.
func runDBCheck(ctx context.Context, env *cli.Env, open cli.Opener, url string, checks []service.IntegrityCheck) error {
	st, release, err := open(ctx, url)
	if err != nil {
		return err
	}

	defer release()

	findings, err := service.NewIntegrityService(st).Check(ctx, checks...)
	if err != nil {
		return err
	}

	report := newDBCheckReport(checks, findings)

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
	if len(findings) == 1 {
		noun = "finding"
	}

	return fmt.Errorf("%d %s", len(findings), noun)
}

// dbCheckReport is the JSON of db check.
type dbCheckReport struct {
	OK       bool             `json:"ok"`
	Checks   []string         `json:"checks"`
	Findings []dbCheckFinding `json:"findings"`
}

type dbCheckFinding struct {
	Check   string `json:"check"`
	Entity  string `json:"entity"`
	ID      string `json:"id"`
	Message string `json:"message"`
}

// newDBCheckReport is the report of the findings of the checks that ran.
func newDBCheckReport(ran []service.IntegrityCheck, findings []service.IntegrityFinding) dbCheckReport {
	r := dbCheckReport{OK: len(findings) == 0, Checks: make([]string, len(ran)), Findings: []dbCheckFinding{}}
	for i, c := range ran {
		r.Checks[i] = string(c)
	}

	for _, f := range findings {
		r.Findings = append(r.Findings, dbCheckFinding{
			Check: string(f.Check), Entity: string(f.Entity), ID: f.ID, Message: f.Message,
		})
	}

	return r
}

// integrityCheckNames lists the checks, for the help.
func integrityCheckNames() string {
	names := make([]string, 0, len(service.IntegrityChecks()))
	for _, c := range service.IntegrityChecks() {
		names = append(names, string(c))
	}

	return strings.Join(names, ", ")
}
