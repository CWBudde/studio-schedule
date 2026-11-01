-- The runs of the background jobs (ADR 0020): one row per run, with its
-- start, end, result and, for a failed run, the error. A run that found the
-- job's lock taken (another server or a command ran the job) is recorded as
-- skipped, starting and ending at the same instant. Runs are only added;
-- the retention job deletes old ones. Conventions as in 00001_core.sql.
--
-- The checks mirror domain.JobRun.Validate: a job name of at most 100
-- bytes, and an error on a failed run only (NULL otherwise) of at most 1000
-- characters. job_runs_latest_idx serves the latest run per job (DISTINCT
-- ON job), job_runs_started_idx the retention job's deletion by start.

-- +goose Up

CREATE TABLE job_runs (
    id uuid PRIMARY KEY,
    job text COLLATE "C" NOT NULL CHECK (btrim(job) <> '' AND octet_length(job) <= 100),
    started_at timestamptz NOT NULL,
    ended_at timestamptz NOT NULL,
    result text NOT NULL CHECK (result IN ('ok', 'failed', 'skipped')),
    error text CHECK (error <> '' AND char_length(error) <= 1000),
    CONSTRAINT job_runs_ended_after_started CHECK (ended_at >= started_at),
    CONSTRAINT job_runs_error_iff_failed CHECK ((result = 'failed') = (error IS NOT NULL))
);

CREATE INDEX job_runs_latest_idx ON job_runs (job, started_at DESC, id DESC);
CREATE INDEX job_runs_started_idx ON job_runs (started_at);

-- +goose Down

DROP TABLE job_runs;
