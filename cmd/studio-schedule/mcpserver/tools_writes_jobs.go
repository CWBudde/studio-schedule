package mcpserver

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/httpapi"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// jobEffect is what a background job does beyond writing the school's own
// data: it mails (or pushes to) people, or it deletes. Either makes a run
// destructive: it cannot be taken back.
type jobEffect string

const (
	jobWrites  jobEffect = ""                                             // writes the school's data only
	jobMails   jobEffect = "mail or push to people, or queue their mails" // reaches people
	jobDeletes jobEffect = "delete what their retention periods let go"   // cannot be undone
)

// jobEffects are the effects of every job app.Jobs can build, the senders
// included (a test compares the names with app.Jobs, so a new job cannot
// be left out).
var jobEffects = map[string]jobEffect{
	"generate-lessons":    jobWrites,
	"waitlist-expiry":     jobMails,
	"announcements":       jobMails,
	"deliver-mail":        jobMails,
	"deliver-push":        jobMails,
	"purge-sessions":      jobDeletes,
	"purge-mail":          jobDeletes,
	"purge-personal-data": jobDeletes,
}

// jobsDescription names the jobs that mail and those that delete, from
// jobEffects: "a, b and c mail …; d and e delete …".
func jobsDescription() string {
	parts := make([]string, 0, 2)

	for _, effect := range []jobEffect{jobMails, jobDeletes} {
		var names []string

		for _, name := range slices.Sorted(maps.Keys(jobEffects)) {
			if jobEffects[name] == effect {
				names = append(names, name)
			}
		}

		last := len(names) - 1
		parts = append(parts, strings.Join(names[:last], ", ")+" and "+names[last]+" "+string(effect))
	}

	return strings.Join(parts, "; ")
}

// destructiveJobs reports whether a job of jobEffects mails or deletes:
// run_job's annotation, since it runs any of them.
func destructiveJobs() bool {
	return slices.ContainsFunc(slices.Collect(maps.Values(jobEffects)), func(e jobEffect) bool { return e != jobWrites })
}

// jobWriteTools run a background job now (ADR 0020).
func jobWriteTools() []tool {
	return []tool{{
		name: "run_job",
		description: "Run one background job now and wait for it, as `jobs run <name>` does (ADR 0020): what " +
			"the server does on the job's interval, under the job's lock, recorded the same way. The job does " +
			"its real work: " + jobsDescription() + "; generate-lessons only writes the school's " +
			"lessons. The jobs are those the server runs with the same configuration: deliver-mail " +
			"only with a mail server, deliver-push only with the VAPID keys. Returns the run: the job, its " +
			"start and end (RFC 3339, UTC), its result (ok or failed) and the error of a failed run. An " +
			"unknown job is not found; a job that another process runs now is a conflict.",
		access:      httpapi.AccessAdmin,
		write:       WriteJobs,
		annotations: jobAnnotations(),
		add:         handler(runJob),
	}}
}

// jobAnnotations are run_job's: destructive since some jobs mail or
// delete, never idempotent (each run does the work that is due then), and
// open to the world, since deliver-mail and deliver-push reach mail and
// push servers.
func jobAnnotations() mcp.ToolAnnotations {
	a := writeAnnotations("Run a background job", destructiveJobs(), false)
	open := true
	a.OpenWorldHint = &open

	return a
}

// runJobInput are the arguments of run_job.
type runJobInput struct {
	Name string `json:"name" jsonschema:"The job, e.g. generate-lessons or deliver-mail."`
}

// runJob is the run_job tool: Runner.RunNow on the jobs of Options.Jobs,
// and the run as `jobs run --output json` prints it. A failed run is the
// run with its error, not an error result: the job ran, and the run says
// how it ended.
func runJob(ctx context.Context, s *Server, _ service.Principal, in runJobInput) (any, error) {
	if in.Name == "" {
		return nil, &domain.FieldError{Field: "name", Msg: "name is required"}
	}

	runner, err := s.opts.Jobs()
	if err != nil {
		return nil, err
	}

	run, err := runner.RunNow(ctx, in.Name)

	switch {
	case run.ID.IsZero():
		// No such job (not found): nothing ran. A run whose record fails
		// keeps its ID; the runner only logs the failure, as for `jobs run`.
		names := make([]string, 0, len(runner.Jobs()))
		for _, info := range runner.Jobs() {
			names = append(names, info.Name)
		}

		return nil, fmt.Errorf("%w; the jobs are %s", err, strings.Join(names, ", "))
	case run.Result == domain.JobSkipped:
		return nil, err // another holder has the lock: a conflict
	}

	return cli.NewJobRun(run), nil
}
