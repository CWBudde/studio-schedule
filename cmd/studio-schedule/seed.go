package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
)

// anchorToday is the --anchor value for the current date in the demo
// school's time zone.
const anchorToday = "today"

func newSeedCmd(env config.LookupFunc) *cobra.Command {
	var (
		db     databaseFlag
		anchor string
		school string
	)

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Load the fictional demo data set into an empty database",
		Long: "Load the fictional demo data set (school settings, catalog, timetable, families and " +
			"accounts) into the database from " + config.EnvDatabaseURL + " or --database-url. " +
			"The schema must be up to date, and the database must be empty: seed never mixes " +
			"demo data into a school that is in use. Every demo account, admin included, signs in " +
			"with the published password " + seed.DemoPassword + ", so never seed a public installation.\n\n" +
			"The demo term and workshops lie around the anchor date: by default the fixed date " +
			seed.FixedDemoAnchor.String() + " (the data of the tests), with --anchor today the current " +
			"date in the demo school's time zone (like the web demo), or a date YYYY-MM-DD.\n\n" +
			"--school picks the demo school: " + string(seed.SchoolProgramme) + " (the default, in the app's " +
			"own look) or " + string(seed.SchoolWerkstatt) + " (the same people and timetable as a German school " +
			"in the Werkstatt look, with its own imprint, German receipts and a background image).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Checked before connecting: a typo never waits for the database.
			date, err := demoAnchor(anchor, clock.System{})
			if err != nil {
				return err
			}

			sch, err := demoSchool(school)
			if err != nil {
				return err
			}

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

			if err := seed.LoadDemoSchoolAt(ctx, st, date, sch); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(),
				"demo data (school %s) loaded around %s\nwarning: every demo account, admin included, signs in with the published password %q\n",
				sch, date, seed.DemoPassword)

			return err
		},
	}

	db.register(cmd.Flags())
	cmd.Flags().StringVar(&anchor, "anchor", seed.FixedDemoAnchor.String(),
		"date the demo term lies around: YYYY-MM-DD, or "+anchorToday+" (in the demo school's time zone)")
	cmd.Flags().StringVar(&school, "school", string(seed.SchoolProgramme),
		"demo school: "+schoolNames())

	return cmd
}

// demoAnchor parses the --anchor value: a date YYYY-MM-DD, or anchorToday
// for the date of clk's now in the demo school's time zone.
func demoAnchor(value string, clk clock.Clock) (domain.Date, error) {
	if value == anchorToday {
		loc, err := time.LoadLocation(seed.DemoSettings().Timezone)
		if err != nil {
			return domain.Date{}, fmt.Errorf("demo time zone: %w", err)
		}

		return domain.DateOf(clk.Now(), loc), nil
	}

	date, err := domain.ParseDate(value)
	if err != nil {
		return domain.Date{}, fmt.Errorf("--anchor: %q is neither %s nor a date YYYY-MM-DD", value, anchorToday)
	}

	return date, nil
}

// demoSchool parses the --school value, one of seed.Schools.
func demoSchool(value string) (seed.School, error) {
	if s := seed.School(value); s.Valid() {
		return s, nil
	}

	return "", fmt.Errorf("--school: %q is not a demo school: want %s", value, schoolNames())
}

// schoolNames lists the demo schools for help and errors: "a or b".
func schoolNames() string {
	names := make([]string, 0, len(seed.Schools()))
	for _, s := range seed.Schools() {
		names = append(names, string(s))
	}

	return strings.Join(names, " or ")
}
