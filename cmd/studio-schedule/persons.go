package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

// newPersonsCmd is the group `persons` (ADR 0020): the office's person
// search and detail, the data export and anonymization (ADR 0016), and the
// duplicates and their merge (ADR 0015).
func newPersonsCmd(app *cli.App) *cobra.Command {
	return cli.Group("persons", "Find, export, anonymize and merge persons",
		newPersonsSearchCmd(app), newPersonsShowCmd(app), newPersonsExportCmd(app), newPersonsAnonymizeCmd(app),
		newPersonsDuplicatesCmd(app), newPersonsMergeCmd(app))
}

func newPersonsSearchCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "search",
		Short: "Search persons by name, email or member number (the term from stdin)",
		Long: "Search the persons whose given name, family name, email or member number contains every word " +
			"of the search term, ignoring case, as the office's person search does; at most 50, ordered by " +
			"family and given name; merged and anonymized persons are left out. The term is read from stdin, " +
			"after a prompt on a terminal, never from an argument, so it stays out of the shell history and " +
			"the process list (ASVS 8.3.1). It prints each person's ID, name, member number and email.",
		Example: "  studio-schedule persons search --as office@example.org\n" +
			"  echo 'erika example' | studio-schedule persons search --as office@example.org --output json",
		Access: httpapi.AccessOffice,
		JSON:   "the response body of the operation searchPersons",
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			query, err := env.ReadLine("Search", "search term")
			if err != nil {
				return err
			}

			persons, err := env.Services.Persons.SearchPersons(ctx, query)
			if err != nil {
				return err
			}

			body := httpapi.NewPersonMatchList(persons)

			return env.Out.Print(body, func(t *cli.Text) {
				t.Table("ID", "Name", "Member no.", "Email")

				for _, p := range body.Items {
					t.Row(p.ID.String(), matchName(p), p.MemberNo, p.Email)
				}
			})
		},
	})
}

func newPersonsShowCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "show <person-id>",
		Short: "Show a person with their family, account, staff links and classes",
		Long: "Show every field of a person with their guardians, dependents, account, linked staff members, " +
			"class memberships and the persons merged into them, as the office's person page does. A merged " +
			"or anonymized person is shown too, with the person it was merged into or when it was anonymized.",
		Example: "  studio-schedule persons show --as office@example.org 01890a5d-ac96-774b-bcce-b302099a8057",
		Args:    cobra.ExactArgs(1),
		Access:  httpapi.AccessOffice,
		JSON:    "the response body of the operation getPerson",
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			id, err := personArg(args[0])
			if err != nil {
				return err
			}

			d, err := env.Services.Persons.Person(ctx, id)
			if err != nil {
				return err
			}

			body := httpapi.NewPersonDetail(d)

			return env.Out.Print(body, func(t *cli.Text) { showPerson(t, body) })
		},
	})
}

// showPerson writes the detail of a person: its fields, then a table for
// each of its links that is not empty.
func showPerson(t *cli.Text, d httpapi.PersonDetail) {
	name := d.GivenName + " " + d.FamilyName
	if !d.AnonymizedAt.IsZero() {
		name = deletedPersonName
	}

	t.Table("Field", "Value")
	t.Row("ID", d.ID.String())
	t.Row("Name", name)
	t.Row("Email", d.Email)
	t.Row("Phone", d.Phone)
	t.Row("Birth date", t.Date(d.BirthDate))
	t.Row("Member no.", d.MemberNo)
	t.Row("Notes", oneLine(d.Notes))

	account := ""
	if a := d.Account; a != nil {
		account = fmt.Sprintf("%s (%s, %s)", a.Email, a.Role, a.Status)
	}

	t.Row("Account", account)

	if m := d.MergedInto; m != nil {
		t.Row("Merged into", matchName(*m)+" ("+m.ID.String()+")")
	}

	if !d.AnonymizedAt.IsZero() {
		t.Row("Anonymized", t.Time(d.AnonymizedAt))
	}

	matches := func(title string, persons []httpapi.PersonMatch) {
		if len(persons) == 0 {
			return
		}

		t.Line("%s:", title)
		t.Table("ID", "Name", "Member no.", "Email")

		for _, p := range persons {
			t.Row(p.ID.String(), matchName(p), p.MemberNo, p.Email)
		}
	}

	matches("Guardians", d.Guardians)
	matches("Dependents", d.Dependents)

	if len(d.Staff) > 0 {
		t.Line("Staff:")
		t.Table("ID", "Name", "Kind", "Active")

		for _, s := range d.Staff {
			t.Row(s.ID.String(), s.DisplayName, string(s.Kind), yesNo(s.Active))
		}
	}

	if len(d.Classes) > 0 {
		t.Line("Classes:")
		t.Table("ID", "Term", "Course type", "Day", "Time", "Since", "Retired")

		for _, c := range d.Classes {
			t.Row(c.ClassID.String(), c.TermName, c.CourseTypeName, time.Weekday(c.Weekday).String(),
				c.StartTime.String(), t.Date(c.Since), yesNo(c.Retired))
		}
	}

	matches("Merged", d.Merged)
}

// deletedPersonName is the name of an anonymized person, as the web shows
// it (web/src/lib/personName.ts).
const deletedPersonName = "Deleted person"

// matchName is the name of a person the way the web shows it: "Given
// Family", or "Deleted person" for an anonymized one.
func matchName(p httpapi.PersonMatch) string {
	if p.Anonymized {
		return deletedPersonName
	}

	return p.GivenName + " " + p.FamilyName
}

// personName is matchName of a person.
func personName(p domain.Person) string {
	if p.Anonymized() {
		return deletedPersonName
	}

	return p.GivenName + " " + p.FamilyName
}

// personArg parses the person ID of an argument; an invalid one is a
// usage error (domain.ErrValidation).
func personArg(s string) (domain.PersonID, error) {
	return domain.ParseID[domain.PersonID](s)
}

// oneLine is s with its lines and runs of spaces joined by single spaces,
// for a table cell.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
