package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// flagStatsParticipants lists the top participants in the text of stats.
const flagStatsParticipants = "participants"

func newStatsCmd(app *cli.App) *cobra.Command {
	var (
		statsRange, from, to string
		participants         bool
	)

	ranges := make([]string, 0, len(service.StatisticsRanges()))
	for _, r := range service.StatisticsRanges() {
		ranges = append(ranges, string(r))
	}

	return app.Command(cli.Spec{
		Use:   "stats",
		Short: "Print the statistics of a range of school dates",
		Long: "Print the school's figures of a range of school dates, as the office's statistics page does: " +
			"the totals, by week (by month beyond 26 weeks), by class, room, course type and instructor. " +
			"--range picks the current week and the 3 or 11 before it (last4Weeks, the default; last12Weeks) " +
			"or the current term up to today (term); --from and --to (both inclusive, at most " +
			strconv.Itoa(service.MaxStatisticsDays) + " days) give a custom range. The text leaves the top " +
			strconv.Itoa(service.TopParticipantsLimit) + " participants out, as they name persons, unless " +
			"--participants; --output json prints the whole response body, as the API gives it to the office.",
		Example: "  studio-schedule stats --as office@example.org\n" +
			"  studio-schedule stats --as office@example.org --from 2026-09-01 --to 2026-09-30 --participants",
		Access: httpapi.AccessOffice,
		JSON:   "the response body of the operation getStatistics",
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringVar(&statsRange, "range", "", "the range: "+strings.Join(ranges, ", ")+
				"; default "+string(service.StatisticsLast4Weeks)+", custom with --from and --to")
			cmd.Flags().StringVar(&from, "from", "", "the first school date of a custom range (YYYY-MM-DD), with --to")
			cmd.Flags().StringVar(&to, "to", "", "the last school date of a custom range (inclusive), with --from")
			cmd.Flags().BoolVar(&participants, flagStatsParticipants, false,
				"list the top participants in the text, with their names")
			cmd.MarkFlagsRequiredTogether("from", "to")
		},
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			q, err := statsQuery(statsRange, from, to)
			if err != nil {
				return err
			}

			st, err := env.Services.Statistics.Statistics(ctx, q)
			if err != nil {
				return err
			}

			return env.Out.Print(httpapi.NewStatistics(st), func(t *cli.Text) {
				statsText(t, st, participants)
			})
		},
	})
}

// statsQuery is the query of the flags of stats: no --range is
// last4Weeks, and --from with --to is a custom range.
func statsQuery(statsRange, from, to string) (service.StatisticsQuery, error) {
	q := service.StatisticsQuery{Range: service.StatisticsRange(statsRange)}

	switch {
	case from == "" && q.Range == "":
		q.Range = service.StatisticsLast4Weeks
	case from == "":
	case q.Range == "" || q.Range == service.StatisticsCustom:
		var err error

		q.Range = service.StatisticsCustom

		if q.From, err = dateFlag("from", from); err != nil {
			return q, err
		}

		q.To, err = dateFlag("to", to)

		return q, err
	default:
		return q, cli.Usagef("--range %s: --from and --to give a custom range", statsRange)
	}

	return q, nil
}

// statsText writes the statistics st as text: the top participants only
// with participants.
func statsText(t *cli.Text, st service.Statistics, participants bool) {
	title := fmt.Sprintf("statistics from %s to %s (%s", t.Date(st.From), t.Date(st.To), st.Range)
	if !st.Term.ID.IsZero() {
		title += ": " + st.Term.Name
	}

	t.Line("%s), by %s, in %s", title, st.Granularity, st.Timezone)

	totals := st.Totals
	t.Line("totals:")
	t.Table("Figure", "Value")
	t.Row("lessons", strconv.Itoa(totals.Lessons))
	t.Row("canceled lessons", strconv.Itoa(totals.CanceledLessons))
	t.Row("places", strconv.Itoa(totals.Capacity))
	t.Row("bookings", strconv.Itoa(totals.Bookings))
	t.Row("utilization", percent(totals.Utilization()))
	t.Row("attended", strconv.Itoa(totals.Attended))
	t.Row("no-shows", strconv.Itoa(totals.NoShows))
	t.Row("no-show rate", noShowRate(totals.LessonCounts))
	t.Row("late cancellations", strconv.Itoa(totals.LateCancellations))
	t.Row("trials", strconv.Itoa(totals.Trials))
	t.Row("guests", strconv.Itoa(totals.Guests))
	t.Row("waiting-list entries", strconv.Itoa(totals.WaitlistEntries))
	t.Row("active members", strconv.Itoa(totals.ActiveMembers))
	t.Row("passes sold", strconv.Itoa(totals.PassesSold))
	t.Row("sales", money(totals.Gross, st.Currency))

	t.Line("by %s:", st.Granularity)
	t.Table(append(append([]string{"Period"}, figureHeader...), "Passes sold", "Sales")...)

	for _, p := range st.Periods {
		t.Row(append(append([]string{t.Date(p.Start) + " to " + t.Date(p.End)}, figures(p.LessonCounts)...),
			strconv.Itoa(p.PassesSold), money(p.Gross, st.Currency))...)
	}

	figureTable(t, "class", st.Classes, func(c service.ClassStatistics) (string, service.LessonCounts) {
		if c.Class.ID.IsZero() {
			return t.Time(c.Lesson.StartsAt) + " " + c.CourseType.Name + " (" + string(c.Lesson.Kind) + ")",
				c.LessonCounts
		}

		return c.Class.Weekday.String()[:3] + " " + c.Class.StartTime.String() + " " + c.CourseType.Name +
			" (" + string(c.Class.Level) + ")", c.LessonCounts
	})
	figureTable(t, "room", st.Rooms, func(r service.RoomStatistics) (string, service.LessonCounts) {
		return r.Room.Name + " (" + r.Location.Name + ")", r.LessonCounts
	})
	figureTable(t, "course type", st.CourseTypes, func(c service.CourseTypeStatistics) (string, service.LessonCounts) {
		return c.CourseType.Name, c.LessonCounts
	})
	figureTable(t, "instructor", st.Instructors, func(i service.InstructorStatistics) (string, service.LessonCounts) {
		return i.Staff.DisplayName, i.LessonCounts
	})

	switch {
	case len(st.TopParticipants) == 0:
	case !participants:
		t.Line("top participants: left out, as they name persons; --%s lists them", flagStatsParticipants)
	default:
		t.Line("top participants:")
		t.Table("Name", "Bookings", "Attended", "Late cancellations")

		for _, p := range st.TopParticipants {
			// The statistics leave anonymized persons out (ADR 0016).
			t.Row(p.Person.GivenName+" "+p.Person.FamilyName, strconv.Itoa(p.Bookings), strconv.Itoa(p.Attended),
				strconv.Itoa(p.LateCancellations))
		}
	}
}

// figureHeader names the columns of figures.
var figureHeader = []string{
	"Lessons", "Places", "Bookings", "Utilization", "Attended", "No-shows", "No-show rate", "Late", "Trials",
	"Guests", "Waiting list",
}

// figures are the cells of the lesson counts c (figureHeader).
func figures(c service.LessonCounts) []string {
	return []string{
		strconv.Itoa(c.Lessons), strconv.Itoa(c.Capacity), strconv.Itoa(c.Bookings), percent(c.Utilization()),
		strconv.Itoa(c.Attended), strconv.Itoa(c.NoShows), noShowRate(c), strconv.Itoa(c.LateCancellations),
		strconv.Itoa(c.Trials), strconv.Itoa(c.Guests), strconv.Itoa(c.WaitlistEntries),
	}
}

// figureTable writes the table "by <what>" of items, each named with its
// lesson counts by of.
func figureTable[T any](t *cli.Text, what string, items []T, of func(T) (string, service.LessonCounts)) {
	t.Line("by %s:", what)

	if len(items) == 0 {
		t.Line("none")

		return
	}

	t.Table(append([]string{strings.ToUpper(what[:1]) + what[1:]}, figureHeader...)...)

	for _, item := range items {
		name, c := of(item)
		t.Row(append([]string{name}, figures(c)...)...)
	}
}

// percent formats a ratio as a whole percentage.
func percent(r float64) string { return fmt.Sprintf("%.0f%%", r*100) }

// noShowRate is the no-show rate of c, empty where no attendance was
// taken.
func noShowRate(c service.LessonCounts) string {
	rate, ok := c.NoShowRate()
	if !ok {
		return ""
	}

	return percent(rate)
}

// money formats an amount in minor units of currency, e.g. "1234.50 EUR":
// the currency's decimals and no grouping, so a script can read it.
func money(m domain.Money, currency string) string {
	decimals, _ := domain.CurrencyExponent(currency) // the settings allow only known currencies

	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}

	digits := fmt.Sprintf("%0*d", decimals+1, int64(m))
	if decimals > 0 {
		digits = digits[:len(digits)-decimals] + "." + digits[len(digits)-decimals:]
	}

	return sign + digits + " " + currency
}
