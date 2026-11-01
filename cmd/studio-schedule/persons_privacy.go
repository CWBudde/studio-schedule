package main

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

func newPersonsExportCmd(app *cli.App) *cobra.Command {
	var file outputFile

	return app.Command(cli.Spec{
		Use:   "export <person-id>",
		Short: "Export everything the school stores about a person as JSON (GDPR)",
		Long: "Export the data of a person as one JSON document, as the office's export does (ADR 0016): the " +
			"person, their family links (names only), account, bookings, waiting-list entries, passes with " +
			"their redemptions, receipts (no PDFs), calendar feeds, push devices, dismissed announcements " +
			"and class memberships, without secrets. A merged or anonymized person is exported too. The " +
			"document goes to stdout, with nothing else, in either output format; with --file it goes into " +
			"that file, created with mode 0600, and an existing file is refused unless --force.",
		Example: "  studio-schedule persons export --as office@example.org <person-id> --file erika.json\n" +
			"  studio-schedule persons export --as office@example.org <person-id> > erika.json",
		Args:   cobra.ExactArgs(1),
		Access: httpapi.AccessOffice,
		JSON: "the response body of the operation exportPerson, also without --output json; with --file it " +
			"goes into the file, and stdout stays empty",
		Flags: func(cmd *cobra.Command) {
			file.bind(cmd, "the export", " instead of stdout")
		},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			id, err := personArg(args[0])
			if err != nil {
				return err
			}

			if err := file.check(); err != nil {
				return err
			}

			x, err := env.Services.Persons.Export(ctx, id)
			if err != nil {
				return err
			}

			body := httpapi.NewPersonExport(x)

			if file.name == "" {
				return env.Out.Print(body, func(t *cli.Text) {
					var doc strings.Builder

					// A strings.Builder does not fail, and the body always
					// marshals: it holds strings, numbers, IDs and times.
					_ = encodeExport(&doc, body)

					t.Line("%s", strings.TrimSuffix(doc.String(), "\n"))
				})
			}

			if err := file.write(func(w io.Writer) error { return encodeExport(w, body) }); err != nil {
				return err
			}

			if env.Out.JSON() {
				return nil
			}

			return env.Out.Print(nil, func(t *cli.Text) {
				t.Line("exported %s (%s) to %s", personName(x.Person), x.Person.ID, file.name)
			})
		},
	})
}

// encodeExport writes body to w as the JSON document the presenter prints
// with --output json.
func encodeExport(w io.Writer, body httpapi.PersonExport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)

	return enc.Encode(body)
}

func newPersonsAnonymizeCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "anonymize <person-id>",
		Short: "Erase a person's personal data, keeping their history anonymous (asks first)",
		Long: "Anonymize a person as the office's privacy page does (ADR 0016): the name becomes \"Deleted " +
			"person\", and email, phone, birth date, notes and member number are cleared. Guardian links in " +
			"both directions, class memberships, open waiting-list entries and staff links are removed, " +
			"bookings of lessons that have not started are canceled on time, and the account is deleted with " +
			"its sessions, links and calendar feeds. Past bookings, passes and receipts stay. It cannot be " +
			"undone: it asks on a terminal and needs --yes otherwise. Recorded in the audit log under the " +
			"account of --as. Refused: an unknown or merged person (exit 3), an anonymized one, the only " +
			"guardian of a dependent, a person whose account receipts, the audit log, announcements or " +
			"duplicate dismissals name, the person of --as, that of an admin account unless --as is an " +
			"admin, and that of the last admin who can sign in (exit 4).",
		Example: "  studio-schedule persons anonymize --as office@example.org <person-id>\n" +
			"  studio-schedule persons anonymize --as office@example.org --yes <person-id>",
		Args:        cobra.ExactArgs(1),
		Access:      httpapi.AccessOffice,
		Destructive: true,
		JSON:        "the response body of the operation anonymizePerson",
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			id, err := personArg(args[0])
			if err != nil {
				return err
			}

			d, err := env.Services.Persons.Person(ctx, id)
			if err != nil {
				return err
			}

			name := personName(d.Person)

			if err := env.Confirm("Anonymize " + name + "?"); err != nil {
				return err
			}

			sum, err := env.Services.Persons.Anonymize(ctx, env.Principal, id)
			if err != nil {
				return err
			}

			body := httpapi.NewAnonymizeResult(sum)

			return env.Out.Print(body, func(t *cli.Text) {
				t.Line("anonymized %s (%s)", name, id)
				t.Table("What", "Count")
				t.Row("bookings canceled", strconv.Itoa(body.BookingsCanceled))
				t.Row("waiting-list entries withdrawn", strconv.Itoa(body.WaitlistWithdrawn))
				t.Row("class memberships ended", strconv.Itoa(body.MembershipsEnded))
				t.Row("guardian links removed", strconv.Itoa(body.LinksRemoved))
				t.Row("staff records unlinked", strconv.Itoa(body.StaffUnlinked))
				t.Row("account deleted", yesNo(body.AccountDeleted))
			})
		},
	})
}
