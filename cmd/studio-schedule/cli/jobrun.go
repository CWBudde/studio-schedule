package cli

import (
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// JobRun is a recorded run of a background job as `jobs run --output json`
// prints it and the MCP tool run_job returns it: the instants in UTC, the
// error only of a failed run.
type JobRun struct {
	Job       string    `json:"job"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Result    string    `json:"result"`
	Error     string    `json:"error,omitempty"`
}

// NewJobRun is the document of run.
func NewJobRun(run domain.JobRun) JobRun {
	return JobRun{
		Job: run.Job, StartedAt: run.StartedAt.UTC(), EndedAt: run.EndedAt.UTC(), Result: string(run.Result),
		Error: run.Error,
	}
}
