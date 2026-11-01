package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
)

// newClassesCmd is the classes group: the weekly classes of a term.
func newClassesCmd(app *cli.App) *cobra.Command {
	return cli.Group("classes", "List the weekly classes of a term", newClassesListCmd(app))
}

func newClassesListCmd(app *cli.App) *cobra.Command {
	var termArg string

	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List the weekly classes of a term",
		Long: "List the weekly classes of the term of --term (an ID from `terms list`), by default the current " +
			"term, as the office's class page does: retired classes included. After a line naming the term, " +
			"each line names the class's ID, its weekday and time in the school's time zone, the course type, " +
			"the audience (levels, stage, ages), the room, the instructor and accompanist, the places, and " +
			"\"retired\" for a retired class. Without a term that includes today, give --term.",
		Example: "  studio-schedule classes list --as office@example.org\n" +
			"  studio-schedule classes list --as office@example.org --term <term-id>",
		Access: httpapi.AccessOffice,
		JSON:   "the response body of the operation listAllClasses",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&termArg, "term", "", "the term's ID; default the term that includes today")
		},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			term, err := classesTerm(ctx, env, termArg)
			if err != nil {
				return err
			}

			classes, err := env.Services.Timetable.AllClasses(ctx, term.ID)
			if err != nil {
				return err
			}

			courseTypes, err := env.Services.Catalog.CourseTypes(ctx)
			if err != nil {
				return err
			}

			rooms, err := env.Services.Catalog.Rooms(ctx, domain.LocationID{})
			if err != nil {
				return err
			}

			courseType := namesOf(courseTypes, func(c domain.CourseType) (domain.CourseTypeID, string) { return c.ID, c.Name })
			room := namesOf(rooms, func(r domain.Room) (domain.RoomID, string) { return r.ID, r.Name })

			return env.Out.Print(httpapi.NewClassEntryList(classes), func(t *cli.Text) {
				t.Line("%s: %s to %s", term.Name, t.Date(term.StartsOn), t.Date(term.EndsOn))

				if len(classes) == 0 {
					t.Line("no classes")

					return
				}

				t.Table("ID", "Day", "Time", "Course type", "Audience", "Room", "Instructor", "Accompanist", "Places",
					"Status")

				for _, c := range classes {
					status := ""
					if c.Retired {
						status = t.Paint(cli.Faint, "retired")
					}

					t.Row(c.ID.String(), c.Weekday.String()[:3], classTime(c.Class), courseType[c.CourseTypeID],
						audienceText(c.Audience), room[c.RoomID], c.Instructor.DisplayName, c.Accompanist.DisplayName,
						strconv.Itoa(c.Capacity), status)
				}
			})
		},
	})
}

// classesTerm is the term of --term, or the current term without it.
func classesTerm(ctx context.Context, env *cli.Env, arg string) (domain.Term, error) {
	if arg == "" {
		term, err := env.Services.Timetable.CurrentTerm(ctx)
		if err != nil {
			return domain.Term{}, fmt.Errorf("%w; give --term (see terms list)", err)
		}

		return term, nil
	}

	id, err := domain.ParseID[domain.TermID](arg)
	if err != nil {
		return domain.Term{}, cli.Usagef("--term: %q is not a term ID", arg)
	}

	terms, err := env.Services.Timetable.Terms(ctx)
	if err != nil {
		return domain.Term{}, err
	}

	for _, t := range terms {
		if t.ID == id {
			return t, nil
		}
	}

	return domain.Term{}, fmt.Errorf("--term: term %s: %w", id, domain.ErrNotFound)
}

// namesOf maps the IDs of items to their names.
func namesOf[I comparable, T any](items []T, of func(T) (I, string)) map[I]string {
	names := make(map[I]string, len(items))
	for _, item := range items {
		id, name := of(item)
		names[id] = name
	}

	return names
}

// classTime is the start and end of the lessons of c, school-local.
func classTime(c domain.Class) string {
	end := c.StartTime.SinceMidnight() + c.Duration

	return fmt.Sprintf("%s-%02d:%02d", c.StartTime, int(end/time.Hour), int(end%time.Hour/time.Minute))
}
