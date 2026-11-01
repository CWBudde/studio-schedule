package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func newPersonsDuplicatesCmd(app *cli.App) *cobra.Command {
	cmd := app.Command(cli.Spec{
		Use:   "duplicates",
		Short: "List pairs of persons that may be the same",
		Long: fmt.Sprintf("List the pairs of persons that may be the same, as the office's duplicates page "+
			"does (ADR 0015), at most %d: strong ones (the same email, a person's or its account's, ignoring "+
			"case; or the same name and birth date) first, then weak ones (the same name, a birth date "+
			"unknown). A guardian and its dependent, or two dependents of one guardian, do not match by the "+
			"email alone. Dismissed pairs and merged persons are left out. Merge a pair with persons merge, "+
			"or dismiss it with persons duplicates dismiss.", service.MaxDuplicatePairs),
		Example: "  studio-schedule persons duplicates --as office@example.org",
		Access:  httpapi.AccessOffice,
		JSON:    "the response body of the operation listDuplicates",
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			pairs, err := env.Services.Persons.Duplicates(ctx)
			if err != nil {
				return err
			}

			body := httpapi.NewDuplicateList(pairs)

			return env.Out.Print(body, func(t *cli.Text) {
				t.Table("Strength", "Reasons", "Person A", "Name A", "Person B", "Name B")

				for _, p := range body.Items {
					t.Row(p.Strength, strings.Join(p.Reasons, ","), p.A.ID.String(), p.A.GivenName+" "+p.A.FamilyName,
						p.B.ID.String(), p.B.GivenName+" "+p.B.FamilyName)
				}
			})
		},
	})
	cmd.AddCommand(newPersonsDismissCmd(app))

	return cmd
}

// dismissJSON documents what `persons duplicates dismiss --output json`
// prints.
const dismissJSON = "nothing: the operation dismissDuplicate answers without a body"

func newPersonsDismissCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "dismiss <person-a> <person-b>",
		Short: "Record that two persons are not the same, so the pair is no longer listed",
		Long: "Dismiss a pair of persons persons duplicates lists, as the office's duplicates page does (ADR " +
			"0015): it records that the two are not the same, so the pair is no longer listed. Dismissing a " +
			"pair again changes nothing; an unknown or merged person exits with status 3.",
		Example: "  studio-schedule persons duplicates dismiss --as office@example.org <person-a> <person-b>",
		Args:    cobra.ExactArgs(2),
		Access:  httpapi.AccessOffice,
		JSON:    dismissJSON,
		Fields:  map[string]string{"personB": "<person-b>"},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			a, err := personArg(args[0])
			if err != nil {
				return err
			}

			b, err := personArg(args[1])
			if err != nil {
				return err
			}

			if err := env.Services.Persons.DismissDuplicate(ctx, env.Principal, a, b); err != nil {
				return err
			}

			if env.Out.JSON() {
				return nil
			}

			return env.Out.Print(nil, func(t *cli.Text) { t.Line("dismissed the pair %s and %s", a, b) })
		},
	})
}

// mergeFields are the values of --take: the fields of MergeTake by their
// JSON names, the way the API spells them.
var mergeFields = []string{"name", "email", "phone", "birthDate", "memberNo"}

func newPersonsMergeCmd(app *cli.App) *cobra.Command {
	var (
		take  []string
		apply bool
	)

	return app.Command(cli.Spec{
		Use:   "merge <survivor> <duplicate>",
		Short: "Merge a duplicate person into another (a dry run unless --apply)",
		Long: "Merge the duplicate person into the survivor, as the office's duplicates page does (ADR 0015): " +
			"its bookings, waiting-list entries, class memberships, passes (owned and paid for), guardian " +
			"links, staff links and account move to the survivor. Where both have a booking of the same " +
			"lesson that has not started, the duplicate's is canceled (a visit it cost comes back); of a " +
			"lesson that has started, both stay as history. Where both are members of a class, the " +
			"survivor's membership stays. The survivor keeps its fields but those of --take, and the notes " +
			"of both; the duplicate stays as a hidden record for its receipts. Without --apply it is a dry " +
			"run that prints what would happen and writes nothing; with it, it asks on a terminal and needs " +
			"--yes otherwise, and the merge is recorded in the audit log under the account of --as. Refused: " +
			"an unknown, merged or anonymized person (exit 3), both with an account, and a merge after which " +
			"a guardian would also be a dependent of the survivor (exit 4).",
		Example: "  studio-schedule persons merge --as office@example.org <survivor> <duplicate>\n" +
			"  studio-schedule persons merge --as office@example.org --take email,phone --apply <survivor> <duplicate>",
		Args:        cobra.ExactArgs(2),
		Access:      httpapi.AccessOffice,
		Destructive: true,
		JSON:        "the response body of the operation mergePersons",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringSliceVar(&take, "take", nil, "the fields the survivor takes from the duplicate: "+
				strings.Join(mergeFields, ", ")+" (comma-separated or repeatable)")
			cmd.Flags().BoolVar(&apply, "apply", false, "merge; without it, a dry run that writes nothing")
		},
		Fields: map[string]string{"mergedId": "<duplicate>"},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			survivor, err := personArg(args[0])
			if err != nil {
				return err
			}

			merged, err := personArg(args[1])
			if err != nil {
				return err
			}

			t, err := mergeTake(take)
			if err != nil {
				return err
			}

			req := service.MergeRequest{SurvivorID: survivor, MergedID: merged, Take: t}

			// The preview: what the merge does, and the names to ask with.
			sum, err := env.Services.Persons.Merge(ctx, env.Principal, req, false)
			if err != nil {
				return err
			}

			if apply {
				question := fmt.Sprintf("Merge %s (%s) into %s (%s)?", personName(sum.Merged), merged,
					personName(sum.Survivor), survivor)
				if err := env.Confirm(question); err != nil {
					return err
				}

				if sum, err = env.Services.Persons.Merge(ctx, env.Principal, req, true); err != nil {
					return err
				}
			}

			body := httpapi.NewMergeSummary(sum, apply)

			return env.Out.Print(body, func(t *cli.Text) { showMerge(t, sum.Merged, body) })
		},
	})
}

// mergeTake is the MergeTake of the values of --take.
func mergeTake(values []string) (service.MergeTake, error) {
	var t service.MergeTake

	for _, v := range values {
		switch v {
		case "name":
			t.Name = true
		case "email":
			t.Email = true
		case "phone":
			t.Phone = true
		case "birthDate":
			t.BirthDate = true
		case "memberNo":
			t.MemberNo = true
		default:
			return t, cli.Usagef("--take: %q is not one of %s", v, strings.Join(mergeFields, ", "))
		}
	}

	return t, nil
}

// showMerge writes what the merge of merged did, or would do.
func showMerge(t *cli.Text, merged domain.Person, s httpapi.MergeSummary) {
	verb := "would merge"
	if s.Applied {
		verb = "merged"
	}

	t.Line("%s %s (%s) into %s %s (%s)", verb, personName(merged), merged.ID, s.Survivor.GivenName,
		s.Survivor.FamilyName, s.Survivor.ID)

	t.Table("What", "Count")
	t.Row("bookings moved", strconv.Itoa(s.BookingsMoved))
	t.Row("bookings canceled", strconv.Itoa(len(s.Canceled)))
	t.Row("bookings kept as history", strconv.Itoa(s.History))
	t.Row("waiting-list entries moved", strconv.Itoa(s.WaitlistMoved))
	t.Row("waiting-list entries withdrawn", strconv.Itoa(s.WaitlistWithdrawn))
	t.Row("class memberships moved", strconv.Itoa(s.MembershipsMoved))
	t.Row("class memberships ended", strconv.Itoa(len(s.MembershipsDropped)))
	t.Row("passes moved", strconv.Itoa(s.PassesMoved))
	t.Row("paid passes moved", strconv.Itoa(s.PaidPassesMoved))
	t.Row("guardian links moved", strconv.Itoa(s.LinksMoved))
	t.Row("guardian links dropped", strconv.Itoa(s.LinksDropped))
	t.Row("staff links moved", strconv.Itoa(s.StaffMoved))
	t.Row("account moved", yesNo(s.AccountMoved))
	t.Row("receipts listed under the survivor", strconv.Itoa(s.Receipts))

	if len(s.Canceled) > 0 {
		t.Line("Canceled bookings:")
		t.Table("Lesson", "Starts", "Course type", "Reason")

		for _, c := range s.Canceled {
			t.Row(c.LessonID.String(), t.Time(c.StartsAt), c.CourseTypeName, c.Reason)
		}
	}

	if len(s.MembershipsDropped) > 0 {
		t.Line("Ended class memberships:")
		t.Table("Class", "Term", "Course type", "Day", "Time")

		for _, c := range s.MembershipsDropped {
			t.Row(c.ClassID.String(), c.TermName, c.CourseTypeName, time.Weekday(c.Weekday).String(),
				c.StartTime.String())
		}
	}

	if !s.Applied {
		t.Line("dry run: nothing was written; run again with --apply to merge")
	}
}
