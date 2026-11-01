package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/jobs"
)

// What `jobs list` and `jobs run` print with --output json: they mirror no
// API operation, so docs/cli.md documents these schemas.
const (
	jobRunJSON = `{job, startedAt, endedAt, result, error}, where result is ok, failed or skipped and error ` +
		`is the error message of a failed run (absent otherwise); the instants are RFC 3339 in UTC`
	jobsListJSON = `an array of {name, everySeconds, lastRun} in the order the jobs run at startup, where ` +
		`everySeconds is the interval and lastRun, absent before the first run, is ` + jobRunJSON
)

// jobsAvailability is what the help says about the jobs a configuration
// has.
const jobsAvailability = "The jobs are those serve runs with the same configuration: deliver-mail only " +
	"with " + config.EnvSMTPHost + " set, deliver-push only with the VAPID_* keys."

func newJobsCmd(app *cli.App) *cobra.Command {
	return cli.Group("jobs", "List the background jobs and run one now", newJobsListCmd(app), newJobsRunCmd(app))
}

func newJobsListCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "list",
		Short: "List the background jobs with their interval and latest run",
		Long: "List the background jobs of the database from " + config.EnvDatabaseURL + " or --database-url " +
			"with their interval and their latest recorded run, of any process (a serve instance or jobs " +
			"run): its start in the school's time zone, its duration, its result (ok, failed, or skipped " +
			"because another process ran the job) and the error of a failed run. " + jobsAvailability +
			" Runs are kept for 30 days.",
		Example: "  studio-schedule jobs list\n  studio-schedule jobs list --output json",
		Store:   true,
		JSON:    jobsListJSON,
		Run: func(ctx context.Context, env *cli.Env, _ []string) error {
			runner, release, err := env.Jobs()
			if err != nil {
				return err
			}

			defer release()

			statuses, err := runner.Statuses(ctx)
			if err != nil {
				return err
			}

			body := make([]jobStatus, len(statuses))
			for i, s := range statuses {
				body[i] = newJobStatus(s)
			}

			return env.Out.Print(body, func(t *cli.Text) {
				t.Table("Job", "Every", "Last run", "Duration", "Result", "Error")

				for _, s := range statuses {
					t.Row(append([]string{s.Name, every(s.Every)}, runCells(t, s.Last)...)...)
				}
			})
		},
	})
}

func newJobsRunCmd(app *cli.App) *cobra.Command {
	return app.Command(cli.Spec{
		Use:   "run <name>",
		Short: "Run a background job now and wait for it",
		Long: "Run one background job now, in this process, under the job's lock, and wait for it: what " +
			"serve does on the job's interval, recorded the same way (jobs list shows the run). The job does " +
			"its real work: deliver-mail sends the due mails through the configured mail server, " +
			"deliver-push sends the due push messages, announcements queues the mails of the announcements " +
			"that are due, and purge-sessions, purge-mail and purge-personal-data delete what their " +
			"retention periods let go. " + jobsAvailability + " It prints the run and exits with status 1 " +
			"when the job failed, 3 for a job this configuration does not have, and 4 when another process " +
			"(a serve instance, another jobs run) holds the job's lock and runs it.",
		Example: "  studio-schedule jobs run generate-lessons\n  studio-schedule jobs run deliver-mail",
		Args:    cobra.ExactArgs(1),
		Store:   true,
		JSON:    jobRunJSON,
		Run: func(ctx context.Context, env *cli.Env, args []string) error {
			runner, release, err := env.Jobs()
			if err != nil {
				return err
			}

			defer release()

			return runJob(ctx, env, runner, args[0])
		},
	})
}

// errJobFailed reports a run that failed, after the run is printed: exit
// status 1, whatever the job's error wraps.
var errJobFailed = errors.New("the job failed")

// runJob runs the job name and prints the run.
func runJob(ctx context.Context, env *cli.Env, runner *jobs.Runner, name string) error {
	run, err := runner.RunNow(ctx, name)

	switch {
	case run.ID.IsZero():
		// No such job: nothing ran or was recorded.
		return fmt.Errorf("%w; the jobs are %s", err, jobNames(runner))
	case run.Result == domain.JobSkipped:
		return err // another holder has the lock: a conflict
	}

	err = env.Out.Print(newJobRun(run), func(t *cli.Text) {
		t.Table("Job", "Started", "Duration", "Result", "Error")
		t.Row(append([]string{run.Job}, runCells(t, run)...)...)
	})
	if err == nil && run.Result == domain.JobFailed {
		err = fmt.Errorf("%w: %s", errJobFailed, run.Error)
	}

	return err
}

// jobNames lists the names of runner's jobs.
func jobNames(runner *jobs.Runner) string {
	infos := runner.Jobs()

	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name
	}

	return strings.Join(names, ", ")
}

// runCells are the start, duration, result and error of run, empty
// before the first run.
func runCells(t *cli.Text, run domain.JobRun) []string {
	if run.ID.IsZero() {
		return []string{"", "", "", ""}
	}

	return []string{
		t.Time(run.StartedAt), run.EndedAt.Sub(run.StartedAt).Round(time.Millisecond).String(),
		t.Paint(runStyle[run.Result], string(run.Result)), run.Error,
	}
}

// runStyle colors a result on a color terminal.
var runStyle = map[domain.JobRunResult]cli.Style{
	domain.JobOK: cli.Green, domain.JobFailed: cli.Red, domain.JobSkipped: cli.Faint,
}

// every is an interval without its zero minutes and seconds: 1m, 1h, 24h.
func every(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}

	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}

	return s
}

// jobStatus is a job of `jobs list --output json` (jobsListJSON).
type jobStatus struct {
	Name         string  `json:"name"`
	EverySeconds int64   `json:"everySeconds"`
	LastRun      *jobRun `json:"lastRun,omitempty"`
}

func newJobStatus(s jobs.Status) jobStatus {
	out := jobStatus{Name: s.Name, EverySeconds: int64(s.Every / time.Second)}
	if !s.Last.ID.IsZero() {
		run := newJobRun(s.Last)
		out.LastRun = &run
	}

	return out
}

// jobRun is a recorded run (jobRunJSON).
type jobRun struct {
	Job       string    `json:"job"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Result    string    `json:"result"`
	Error     string    `json:"error,omitempty"`
}

func newJobRun(run domain.JobRun) jobRun {
	return jobRun{
		Job: run.Job, StartedAt: run.StartedAt.UTC(), EndedAt: run.EndedAt.UTC(), Result: string(run.Result),
		Error: run.Error,
	}
}
