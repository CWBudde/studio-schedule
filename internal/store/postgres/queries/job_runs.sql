-- name: CreateJobRun :exec
INSERT INTO job_runs (id, job, started_at, ended_at, result, error) VALUES ($1, $2, $3, $4, $5, $6);

-- name: LatestJobRuns :many
SELECT DISTINCT ON (job) * FROM job_runs ORDER BY job, started_at DESC, id DESC;

-- name: DeleteJobRunsStartedBefore :execrows
DELETE FROM job_runs WHERE started_at < sqlc.arg(started_before);

-- name: TryLockJob :one
-- A session-level advisory lock in the two-key space; the class keeps the
-- job locks apart from the other advisory locks of the application.
SELECT pg_try_advisory_lock(sqlc.arg(class)::int4, hashtext(sqlc.arg(job)::text))::boolean AS locked;

-- name: UnlockJob :exec
SELECT pg_advisory_unlock(sqlc.arg(class)::int4, hashtext(sqlc.arg(job)::text));
