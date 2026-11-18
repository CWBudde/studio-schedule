package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// newEventsCmd is the office's events group (ADR 0019, ADR 0020).
func newEventsCmd(app *cli.App) *cobra.Command {
	return cli.Group("events", "List events and registrations and record payments",
		newEventsListCmd(app), newEventsRegistrationsCmd(app), newEventsMarkPaidCmd(app))
}

func newEventsListCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List all events, including drafts",
		Long: "List all events, including drafts. Each row shows the slug, title, status, the start of the first " +
			"session in the school's time zone, registered places and capacity, and the price in the school's currency.",
		Example: "  studio-schedule events list --as office@example.org",
		Access:  httpapi.AccessOffice,
		JSON:    "the response body of the operation listAdminEvents",
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			events, err := env.Services.Events.List(ctx)
			if err != nil {
				return err
			}

			var currency string

			if len(events) > 0 {
				currency, err = schoolCurrency(ctx, env)
				if err != nil {
					return err
				}
			}

			body := httpapi.NewAdminEventList(events)

			return env.Out.Print(body, func(t *cli.Text) {
				if len(body.Items) == 0 {
					t.Line("no events")

					return
				}

				t.Table("Slug", "Title", "Status", "First session", "Registered / Capacity", "Price")

				for _, e := range body.Items {
					first := ""
					if len(e.Sessions) > 0 {
						first = t.Time(e.Sessions[0].StartsAt)
					}

					t.Row(e.Slug, oneLine(e.Title), e.Status, first, fmt.Sprintf("%d/%d", e.Registered, e.Capacity),
						cli.Money(domain.Money(e.Price), currency))
				}
			})
		},
	})
}

func newEventsRegistrationsCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "registrations <slug>",
		Short: "List an event's registrations",
		Long: "List the event's registrations, including canceled registrations and the waiting list. Each row " +
			"shows the person or guest name, registration status, payment status, and receipt number if issued. " +
			"Use the event slug from events list and find registration IDs in the JSON output.",
		Example: "  studio-schedule events registrations contemporary-weekend-intensive --as office@example.org",
		Args:    cobra.ExactArgs(1),
		Access:  httpapi.AccessOffice,
		JSON:    "the response body of the operation listEventRegistrations",
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			report, err := env.Services.Events.ListRegistrationsBySlug(ctx, args[0])
			if err != nil {
				return err
			}

			body := httpapi.NewEventRegistrationList(report.Registrations)

			return env.Out.Print(body, func(t *cli.Text) {
				if len(report.Registrations) == 0 {
					t.Line("no registrations")

					return
				}

				t.Table("Name", "Status", "Payment", "Receipt")

				for _, view := range report.Registrations {
					r := view.Registration
					t.Row(oneLine(shownName(view.Name, view.Anonymized)), string(r.Status), string(r.Payment),
						report.ReceiptNumbers[r.ReceiptID])
				}
			})
		},
	})
}

func newEventsMarkPaidCmd(app *cli.App) *cobra.Command {
	var (
		method, payer string
		id            domain.EventRegistrationID
		payment       service.EventPayment
	)

	return app.Command(cli.Spec{
		Use:   "mark-paid <registration-id>",
		Short: "Record payment for a registration and issue its receipt",
		Long: "Record an event fee paid at the desk. The command issues a receipt, prints its number, and " +
			"queues receipt mail when a recipient is available. Find the registration ID in events registrations " +
			"with --output json. Confirm the payment with --yes.",
		Example:     "  studio-schedule events mark-paid <registration-id> --as office@example.org --method cash --yes",
		Args:        cobra.ExactArgs(1),
		Access:      httpapi.AccessOffice,
		Destructive: true,
		JSON:        "the response body of the operation markEventRegistrationPaid",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&method, "method", "", "payment method (cash, transfer, or card)")
			_ = cmd.MarkFlagRequired("method") // the flag exists
			cmd.Flags().StringVar(&payer, "payer", "", "ID of the person who paid, as persons search prints it")
			cmd.Flags().StringVar(&payment.Recipient.Name, "recipient-name", "", "name printed on the receipt")
			cmd.Flags().StringVar(&payment.Recipient.Email, "recipient-email", "", "receipt email for a guest or payer without an account")
			cmd.Flags().StringVar(&payment.Recipient.Address, "recipient-address", "", "address printed on the receipt")
		},
		Fields: map[string]string{
			"paymentMethod": "--method", "payerId": "--payer", "recipientName": "--recipient-name",
			"recipientEmail": "--recipient-email", "recipientAddress": "--recipient-address",
		},
		Validate: func(args []string) error {
			var err error

			id, err = domain.ParseID[domain.EventRegistrationID](args[0])
			if err != nil {
				return cli.Usagef("%q is no registration ID", args[0])
			}

			payment.Method = domain.PaymentMethod(method)
			if !payment.Method.Valid() {
				return cli.Usagef("--method: %q is no payment method (cash, transfer, or card)", method)
			}

			if payer != "" {
				payment.PayerID, err = personArg(payer)
				if err != nil {
					return cli.Usagef("--payer: %q is no person ID", payer)
				}
			}

			return nil
		},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			if err := env.RequireYes(); err != nil {
				return err
			}

			paid, err := env.Services.Events.MarkPaid(ctx, env.Principal, id, payment)
			if err != nil {
				return err
			}

			body := httpapi.NewEventPayment(paid)

			return env.Out.Print(body, func(t *cli.Text) { t.Line("%s", body.Receipt.Number) })
		},
	})
}
