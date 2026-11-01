package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// newTermsCmd is the terms group: the terms of the timetable.
func newTermsCmd(app *cli.App) *cobra.Command {
	return cli.Group("terms", "List the terms of the timetable", newTermsListCmd(app))
}

func newTermsListCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List the terms",
		Long: "List every term by its start, as the office's class pages do: its ID, name, first and last day, " +
			"and \"current\" for the term that includes today (the one that started last if terms overlap), " +
			"which `classes list` shows by default.",
		Example: "  studio-schedule terms list --as office@example.org",
		Access:  httpapi.AccessOffice,
		JSON:    "the response body of the operation listTerms",
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			terms, err := env.Services.Timetable.Terms(ctx)
			if err != nil {
				return err
			}

			current, err := env.Services.Timetable.CurrentTerm(ctx)
			if err != nil && !errors.Is(err, service.ErrNoCurrentTerm) {
				return err
			}

			return env.Out.Print(httpapi.NewTermList(terms), func(t *cli.Text) {
				if len(terms) == 0 {
					t.Line("no terms")

					return
				}

				t.Table("ID", "Name", "From", "To", "Current")

				for _, term := range terms {
					t.Row(term.ID.String(), term.Name, t.Date(term.StartsOn), t.Date(term.EndsOn), currentText(term, current))
				}
			})
		},
	})
}

// currentText marks the current term.
func currentText(term, current domain.Term) string {
	if term.ID == current.ID {
		return "current"
	}

	return ""
}
