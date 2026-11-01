package main

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

// newPassesCmd is the passes group (ADR 0012, ADR 0020).
func newPassesCmd(app *cli.App) *cobra.Command {
	return cli.Group("passes", "Show the passes of a person", newPassesListCmd(app))
}

func newPassesListCmd(app *cli.App) *cobra.Command {
	var person string

	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List the passes of a person",
		Long: "List the passes of the person of --person (an ID, as persons search prints it): the usable ones " +
			"first, then the others, each newest sale first. Each line shows the pass, its type, the visits " +
			"used (for a time pass: the bookings it paid for) and left, the school dates it is valid from and " +
			"until, its status (active or voided), whether it pays for a visit today, and who paid for it if " +
			"not the owner (an anonymized person as \"" + receiptDeletedPerson + "\"). An unknown person exits " +
			"with status 3.",
		Example: "  studio-schedule passes list --as office@example.org --person <person-id>",
		Access:  httpapi.AccessOffice,
		JSON:    "the response body of the operation listPersonPasses",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&person, "person", "", "the ID of the person")
			_ = cmd.MarkFlagRequired("person") // the flag exists
		},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			id, err := domain.ParseID[domain.PersonID](person)
			if err != nil {
				return cli.Usagef("--person: %q is no person ID", person)
			}

			views, err := env.Services.Passes.PersonPasses(ctx, id)
			if err != nil {
				return err
			}

			body := httpapi.NewAdminPassList(views)

			return env.Out.Print(body, func(t *cli.Text) {
				if len(body.Items) == 0 {
					t.Line("no passes")

					return
				}

				t.Table("Pass", "Type", "Used", "Left", "Valid from", "Valid until", "Status", "Usable", "Paid by")

				for _, p := range body.Items {
					used, left := strconv.Itoa(p.UseCount), ""
					if p.Kind == string(domain.PassVisits) {
						used, left = strconv.Itoa(p.VisitsTotal-p.VisitsRemaining), strconv.Itoa(p.VisitsRemaining)
					}

					payer := ""
					if p.Payer != nil {
						payer = receiptShownName(p.Payer.Name, p.Payer.Anonymized)
					}

					t.Row(p.ID.String(), p.PassType.Name, used, left, t.Date(p.ValidFrom), t.Date(p.ValidUntil),
						p.Status, passYesNo(p.Usable), payer)
				}
			})
		},
	})
}

func passYesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
