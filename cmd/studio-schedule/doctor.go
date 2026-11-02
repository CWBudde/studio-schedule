package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/diagnose"
	"github.com/cwbudde/studio-schedule/internal/config"
)

// errChecksFailed makes doctor exit with status 1 after its report.
var errChecksFailed = errors.New("a check failed")

// doctorJSON documents what `doctor --output json` prints.
const doctorJSON = diagnose.DoctorJSON

func newDoctorCmd(app *cli.App) *cobra.Command {
	var (
		flags serveFlags
		cmd   *cobra.Command
	)

	return app.Command(cli.Spec{
		Use:   "doctor",
		Short: "Check the configuration, the database, the mail server and the receipt module",
		Long: "Run every check of a deployment and print one line per check with its result (ok, fail, warn " +
			"or skip) and a detail: config (the configuration as serve loads it, from the environment and " +
			"serve's flags), database (it connects), schema (exactly the binary's migrations are applied), " +
			"settings (the school settings exist, with a time zone and currency the app accepts), smtp (a " +
			"connection and login to " + config.EnvSMTPHost + ", nothing is sent; skipped without it), vapid " +
			"(the web push key pair; skipped without it), base-url (https; plain http on localhost or no " +
			config.EnvBaseURL + " is a warning, for local development only, elsewhere a failure) and " +
			"receipts (a fictional sample receipt renders as a PDF; skipped in a build without the receipt " +
			"renderer). A check that needs a failed one is skipped. It exits with status 1 when a check " +
			"fails; warnings and skipped checks do not fail. With --output json it prints " + doctorJSON + ".",
		Example: "  studio-schedule doctor\n  studio-schedule doctor --output json",
		JSON:    doctorJSON,
		Flags:   func(c *cobra.Command) { cmd = c; flags.register(c) },
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			report := diagnose.Doctor(ctx, env, env.Clock, flags.explicit(cmd))

			err := env.Out.Print(report, func(t *cli.Text) {
				t.Table("Check", "Result", "Detail")

				for _, c := range report.Checks {
					t.Row(c.Name, t.Paint(resultStyle[c.Result], c.Result), c.Detail)
				}
			})

			if err != nil || report.OK {
				return err
			}

			return cli.Reported(errChecksFailed)
		},
	})
}

// resultStyle colors a result on a color terminal.
var resultStyle = map[string]cli.Style{
	diagnose.CheckOK: cli.Green, diagnose.CheckFail: cli.Red, diagnose.CheckWarn: cli.Faint, diagnose.CheckSkip: cli.Faint,
}

// doctorReport is the JSON of doctor (diagnose.Doctor), doctorCheck one of
// its checks.
type (
	doctorReport = diagnose.DoctorReport
	doctorCheck  = diagnose.DoctorCheck
)
