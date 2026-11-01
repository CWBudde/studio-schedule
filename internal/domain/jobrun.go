package domain

import (
	"slices"
	"time"
	"unicode/utf8"
)

// MaxJobRunErrorLen bounds the error message a job run records, in
// characters; a longer one is cut (JobRunError).
const MaxJobRunErrorLen = 1000

// MaxJobNameLen bounds the name of a background job, in bytes.
const MaxJobNameLen = 100

// JobRun records one run of a background job (ADR 0020): when it started
// and ended, its result and, for a failed run, the error. A run whose
// per-job lock another holder had is recorded as skipped, with its start
// and end at the same instant.
type JobRun struct {
	ID JobRunID
	// Job names the job, e.g. "generate-lessons".
	Job       string
	StartedAt time.Time
	EndedAt   time.Time
	Result    JobRunResult
	// Error is the error message of a failed run, at most
	// MaxJobRunErrorLen characters; empty for the other results.
	Error string
}

// JobRunResult is how a job run ended.
type JobRunResult string

// Known job run results.
const (
	// JobOK: the job ran and returned no error.
	JobOK JobRunResult = "ok"
	// JobFailed: the job returned an error or panicked.
	JobFailed JobRunResult = "failed"
	// JobSkipped: another holder (a second server, a command) had the
	// job's lock, so this run did not happen.
	JobSkipped JobRunResult = "skipped"
)

// JobRunResults returns all job run results.
func JobRunResults() []JobRunResult { return []JobRunResult{JobOK, JobFailed, JobSkipped} }

// Valid reports whether r is a known job run result.
func (r JobRunResult) Valid() bool { return slices.Contains(JobRunResults(), r) }

// Validate checks the run for consistency. All errors wrap ErrValidation.
func (r JobRun) Validate() error {
	return validate(
		rule{!r.ID.IsZero(), "job run id must be set"},
		rule{!blank(r.Job) && len(r.Job) <= MaxJobNameLen, "job run needs a job name of at most 100 bytes"},
		rule{!r.StartedAt.IsZero() && !r.EndedAt.IsZero(), "job run start and end must be set"},
		rule{notBefore(r.EndedAt, r.StartedAt), "job run cannot end before it started"},
		rule{r.Result.Valid(), "invalid job run result"},
		rule{(r.Result == JobFailed) == (r.Error != ""), "a failed job run has an error, the others none"},
		rule{
			utf8.ValidString(r.Error) && utf8.RuneCountInString(r.Error) <= MaxJobRunErrorLen,
			"job run error is too long",
		},
	)
}

// JobRunError returns msg as the error of a failed job run: invalid UTF-8
// replaced, cut to MaxJobRunErrorLen characters, and "unknown error" for a
// blank one, so a failed run always has an error.
func JobRunError(msg string) string {
	r := []rune(msg) // invalid bytes become U+FFFD
	if blank(string(r)) {
		return "unknown error"
	}

	if len(r) > MaxJobRunErrorLen {
		return string(r[:MaxJobRunErrorLen-1]) + "…"
	}

	return string(r)
}
