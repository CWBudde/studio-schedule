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
	"github.com/cwbudde/studio-schedule/internal/service"
)

// newLessonsCmd is the lessons group: the office's day-to-day work on the
// lessons of the timetable (ADR 0007), as the account of --as.
func newLessonsCmd(app *cli.App) *cobra.Command {
	return cli.Group("lessons", "List, generate, cancel and restore lessons",
		newLessonsListCmd(app), newLessonsGenerateCmd(app), newLessonsCancelCmd(app), newLessonsRestoreCmd(app))
}

func newLessonsListCmd(app *cli.App) *cobra.Command {
	var date, from, to string

	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List the lessons of a day or a range of days",
		Long: "List the lessons that start on a school date (--date, default today) or from --from through " +
			"--to (both inclusive, at most " + strconv.Itoa(service.MaxScheduleDays) + " days), as the office " +
			"sees the schedule: hidden, unreleased and canceled lessons included. Each line names the lesson's " +
			"ID, its date and time in the school's time zone, the class (course type and audience) or the kind of " +
			"a one-off lesson, the instructor, the room, the active bookings and the places, and its status.",
		Example: "  studio-schedule lessons list --as office@example.org\n" +
			"  studio-schedule lessons list --as office@example.org --from 2026-09-21 --to 2026-09-27",
		Access: httpapi.AccessOffice,
		JSON:   "the response body of the operation getSchedule",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&date, "date", "", "the school date (YYYY-MM-DD); default today")
			cmd.Flags().StringVar(&from, "from", "", "the first school date of a range (YYYY-MM-DD), with --to")
			cmd.Flags().StringVar(&to, "to", "", "the last school date of a range (inclusive), with --from")
			cmd.MarkFlagsMutuallyExclusive("date", "from")
			cmd.MarkFlagsMutuallyExclusive("date", "to")
			cmd.MarkFlagsRequiredTogether("from", "to")
		},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			loc := env.Out.Location()

			first, last, err := lessonDays(env, loc, date, from, to)
			if err != nil {
				return err
			}

			s, err := env.Services.Schedule.Schedule(ctx, env.Principal, service.ScheduleQuery{
				From: first, To: last.AddDays(1),
			})
			if err != nil {
				return err
			}

			return env.Out.Print(httpapi.NewSchedule(s), func(t *cli.Text) {
				if len(s.Lessons) == 0 {
					t.Line("no lessons %s", daysText(first, last))

					return
				}

				lessonTable(t)

				for _, sl := range s.Lessons {
					lessonRow(t, loc, sl)
				}
			})
		},
	})
}

// lessonDays returns the school dates the flags of `lessons list` select:
// --from through --to, --date, or today.
func lessonDays(env *cli.Env, loc *time.Location, date, from, to string) (first, last domain.Date, err error) {
	if from == "" {
		day, err := dateFlag("date", date)
		if err != nil {
			return domain.Date{}, domain.Date{}, err
		}

		if day.IsZero() {
			day = domain.DateOf(env.Clock.Now(), loc)
		}

		return day, day, nil
	}

	if first, err = dateFlag("from", from); err != nil {
		return domain.Date{}, domain.Date{}, err
	}

	if last, err = dateFlag("to", to); err != nil {
		return domain.Date{}, domain.Date{}, err
	}

	if last.Compare(first) < 0 {
		return domain.Date{}, domain.Date{}, cli.Usagef("--to: %s lies before --from %s", last, first)
	}

	return first, last, nil
}

// daysText names the school dates first through last.
func daysText(first, last domain.Date) string {
	if first == last {
		return "on " + first.String()
	}

	return fmt.Sprintf("from %s to %s", first, last)
}

// dateFlag parses the school date value of the flag name; empty is the
// zero date.
func dateFlag(name, value string) (domain.Date, error) {
	if value == "" {
		return domain.Date{}, nil
	}

	d, err := domain.ParseDate(value)
	if err != nil {
		return domain.Date{}, cli.Usagef("--%s: %q is not a date (YYYY-MM-DD)", name, value)
	}

	return d, nil
}

// lessonTable starts the table of lessons (lessonRow).
func lessonTable(t *cli.Text) {
	t.Table("ID", "Date", "Time", "Lesson", "Instructor", "Room", "Booked", "Status")
}

// lessonRow adds the lesson sl to the table of lessonTable, with its date
// and time in the school's time zone loc.
func lessonRow(t *cli.Text, loc *time.Location, sl service.ScheduledLesson) {
	l := sl.Lesson
	start, end := l.StartsAt.In(loc), l.EndsAt.In(loc)

	booked := ""
	if l.Kind != domain.LessonRental {
		booked = fmt.Sprintf("%d/%d", sl.Booked, l.Capacity)
	}

	status := string(l.Status)
	if l.Status == domain.LessonCanceled {
		status = t.Paint(cli.Red, status)
	}

	t.Row(l.ID.String(), start.Format(time.DateOnly), start.Format("15:04")+"-"+end.Format("15:04"), lessonName(sl),
		sl.Instructor.DisplayName, sl.Room.Name, booked, status)
}

// lessonName names the lesson sl: the course type with the audience of its
// class (if it states one), or with the kind of a one-off lesson; a room
// rental by its renter.
func lessonName(sl service.ScheduledLesson) string {
	l := sl.Lesson

	switch {
	case l.Kind == domain.LessonRental:
		return "room rental: " + l.Renter
	case !l.ClassID.IsZero():
		return withAudience(sl.CourseType.Name, sl.Audience)
	default:
		return sl.CourseType.Name + " (" + string(l.Kind) + ")"
	}
}

// lessonText names the lesson sl in a sentence, with its start in the
// school's time zone loc: "Classical Ballet (Beginner) on 2026-09-23 at
// 18:00 in Studio 1".
func lessonText(loc *time.Location, sl service.ScheduledLesson) string {
	start := sl.Lesson.StartsAt.In(loc)

	return fmt.Sprintf("%s on %s at %s in %s", lessonName(sl), start.Format(time.DateOnly), start.Format("15:04"),
		sl.Room.Name)
}

// generated is what `lessons generate --output json` prints.
type generated struct {
	Created int         `json:"created"`
	Until   domain.Date `json:"until"`
}

func newLessonsGenerateCmd(app *cli.App) *cobra.Command {
	var until string

	return app.Command(cli.Spec{
		Use:   "generate",
		Short: "Generate the lessons of the weekly classes through a date",
		Long: "Create the missing lessons of the weekly classes from today through --until (inclusive), as the " +
			"hourly generate-lessons job does for its rolling horizon (ADR 0003): one lesson per class and " +
			"week within the class's term, none that has started, none of a retired class, and the class " +
			"members booked into each. A lesson that exists, was moved or canceled stays as it is, so running " +
			"it again creates nothing. It prints how many lessons it created.",
		Example: "  studio-schedule lessons generate --as office@example.org --until 2026-12-31",
		Access:  httpapi.AccessOffice,
		JSON:    "one object: created (number), the lessons created; until (date), the last school date",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&until, "until", "", "the last school date to generate lessons for (YYYY-MM-DD)")
			_ = cmd.MarkFlagRequired("until")
		},
		Fields: map[string]string{"until": "--until"},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			last, err := dateFlag("until", until)
			if err != nil {
				return err
			}

			n, err := env.Services.Generator.GenerateUntil(ctx, last)
			if err != nil {
				return err
			}

			return env.Out.Print(generated{Created: n, Until: last}, func(t *cli.Text) {
				t.Line("created %s through %s", count(n, "lesson"), last)
			})
		},
	})
}

// lessonArg parses the lesson ID of a command's argument.
func lessonArg(arg string) (domain.LessonID, error) {
	id, err := domain.ParseID[domain.LessonID](arg)
	if err != nil {
		return domain.LessonID{}, cli.Usagef("<lesson-id>: %q is not a lesson ID", arg)
	}

	return id, nil
}

func newLessonsCancelCmd(app *cli.App) *cobra.Command {
	var note string

	return app.Command(cli.Spec{
		Use:   "cancel <lesson-id>",
		Short: "Cancel a lesson and mail everyone booked on it",
		Long: "Cancel a lesson that has not started, e.g. when its instructor is ill, as the office's lesson page " +
			"does (ADR 0007): everyone with an active booking is mailed, with the --note if given (the mails " +
			"wait in the outbox for the server's deliver-mail job). The bookings stay but cost nothing: their " +
			"passes get the visits back. The waiting list's invites go back to waiting. A lesson canceled " +
			"already stays as it is, and nobody is mailed again. It asks for confirmation (--yes without a " +
			"terminal). There is no way to cancel without the mails.",
		Example:     "  studio-schedule lessons cancel --as office@example.org --note \"The teacher is ill.\" <lesson-id>",
		Args:        cobra.ExactArgs(1),
		Access:      httpapi.AccessOffice,
		Destructive: true,
		JSON:        "the response body of the operation cancelLesson",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&note, "note", "", "a note for those booked, e.g. the reason (at most "+
				strconv.Itoa(domain.MaxCancelNoteLength)+" characters)")
		},
		Fields: map[string]string{"note": "--note"},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			return setLessonStatus(ctx, env, args[0], domain.LessonCanceled, "Cancel", "canceled",
				func(id domain.LessonID) (service.ScheduledLesson, error) {
					return env.Services.Lessons.CancelLesson(ctx, env.Principal, id, note)
				})
		},
	})
}

func newLessonsRestoreCmd(app *cli.App) *cobra.Command {
	var overlap bool

	return app.Command(cli.Spec{
		Use:   "restore <lesson-id>",
		Short: "Restore a canceled lesson and mail everyone booked on it",
		Long: "Let a canceled lesson that has not started take place after all, as the office's lesson page " +
			"does (ADR 0007): everyone with an active booking is mailed its time and room, its bookings use " +
			"their passes again, and its free places go to the waiting list. Another lesson in the room at an " +
			"overlapping time is a conflict (exit 4) unless --confirm-room-overlap. A scheduled lesson stays as " +
			"it is. It asks for confirmation (--yes without a terminal).",
		Example:     "  studio-schedule lessons restore --as office@example.org <lesson-id>",
		Args:        cobra.ExactArgs(1),
		Access:      httpapi.AccessOffice,
		Destructive: true,
		JSON:        "the response body of the operation restoreLesson",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().BoolVar(&overlap, "confirm-room-overlap", false,
				"restore even if another lesson uses the room at an overlapping time")
		},
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			return setLessonStatus(ctx, env, args[0], domain.LessonScheduled, "Restore", "restored",
				func(id domain.LessonID) (service.ScheduledLesson, error) {
					return env.Services.Lessons.RestoreLesson(ctx, id, overlap)
				})
		},
	})
}

// setLessonStatus gives the lesson of arg the status with write, after
// asking "<verb> <lesson> and mail …?", and prints it, saying it is done. A lesson that has
// the status already is printed as it is, without the question: write
// changes nothing and mails nobody.
func setLessonStatus(
	ctx context.Context, env *cli.Env, arg string, status domain.LessonStatus, verb, done string,
	write func(domain.LessonID) (service.ScheduledLesson, error),
) error {
	id, err := lessonArg(arg)
	if err != nil {
		return err
	}

	loc := env.Out.Location()

	sl, err := env.Services.Schedule.Lesson(ctx, env.Principal, id)
	if err != nil {
		return err
	}

	changes := sl.Lesson.Status != status
	if changes {
		question := fmt.Sprintf("%s %s and mail those booked on it (%s)?", verb, lessonText(loc, sl),
			count(sl.Admin.ActiveBookings, "active booking"))
		if err := env.Confirm(question); err != nil {
			return err
		}
	}

	sl, err = write(id)
	if err != nil {
		return err
	}

	return env.Out.Print(httpapi.NewLessonDetail(sl), func(t *cli.Text) {
		lessonTable(t)
		lessonRow(t, loc, sl)

		if !changes {
			t.Line("the lesson is %s already: nothing changed, nobody was mailed", status)

			return
		}

		t.Line("%s; mails to those booked on it (%s) are queued", done, count(sl.Admin.ActiveBookings, "active booking"))
	})
}

// count counts n things: "1 lesson", "3 lessons".
func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}

	return strconv.Itoa(n) + " " + thing + "s"
}
