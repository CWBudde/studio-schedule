package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

func newReceiptsListCmd(app *cli.App) *cobra.Command {
	var from, to string

	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List the receipts issued in a range of dates",
		Long: "List the receipts issued on the school dates from --from to --to, both inclusive, newest first: " +
			"number, issue date, kind (receipt, or storno for a cancellation), the recipient printed on it, the " +
			"gross amount, the payment method, and the receipt a storno cancels or the storno that canceled a " +
			"receipt. Without --to it ends today (in the school's time zone), without --from it starts on " +
			"January 1 of the year of --to.",
		Example: "  studio-schedule receipts list --as office@example.org\n" +
			"  studio-schedule receipts list --as office@example.org --from 2026-01-01 --to 2026-03-31",
		Access: httpapi.AccessOffice,
		JSON: "the response body of the operation searchReceipts: every receipt of the range in items, newest " +
			"first, with total their number",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&from, "from", "", "first issue date, YYYY-MM-DD (default: January 1 of the year of --to)")
			cmd.Flags().StringVar(&to, "to", "", "last issue date, YYYY-MM-DD (default: today)")
		},
		Fields: map[string]string{"to": "--to"},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			first, err := receiptDateFlag("from", from)
			if err != nil {
				return err
			}

			last, err := receiptDateFlag("to", to)
			if err != nil {
				return err
			}

			page, err := env.Services.Receipts.Issued(ctx, first, last)
			if err != nil {
				return err
			}

			body := httpapi.NewReceiptList(page)

			return env.Out.Print(body, func(t *cli.Text) {
				if len(body.Items) == 0 {
					t.Line("no receipts")

					return
				}

				t.Table("Number", "Date", "Kind", "Recipient", "Gross", "Payment", "Note")

				for _, r := range body.Items {
					note := ""

					switch {
					case r.Cancels != nil:
						note = "cancels " + r.Cancels.Number
					case r.CanceledBy != nil:
						note = "canceled by " + r.CanceledBy.Number
					}

					t.Row(r.Number, t.Date(r.IssueDate), receiptKind(r.Kind), r.RecipientName,
						cli.Money(domain.Money(r.Gross), r.Currency), r.PaymentMethod, note)
				}
			})
		},
	})
}

// receiptDateFlag parses the date of the flag name; empty is the zero
// date, an invalid one a usage error on the flag.
func receiptDateFlag(name, value string) (domain.Date, error) {
	if value == "" {
		return domain.Date{}, nil
	}

	d, err := domain.ParseDate(value)
	if err != nil {
		return domain.Date{}, cli.Usagef("--%s: %q is no date of the form YYYY-MM-DD", name, value)
	}

	return d, nil
}
