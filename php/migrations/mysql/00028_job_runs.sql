-- +goose Up

CREATE TABLE job_runs (
    id BINARY(16) PRIMARY KEY,
    job VARBINARY(100) NOT NULL CHECK (OCTET_LENGTH(TRIM(job)) > 0 AND OCTET_LENGTH(job) <= 100),
    started_at DATETIME(6) NOT NULL,
    ended_at DATETIME(6) NOT NULL,
    result VARBINARY(32) NOT NULL CHECK (result IN ('ok', 'failed', 'skipped')),
    error LONGTEXT CHECK (CHAR_LENGTH(error) BETWEEN 1 AND 1000),
    CONSTRAINT job_runs_ended_after_started CHECK (ended_at >= started_at),
    CONSTRAINT job_runs_error_iff_failed CHECK ((result = 'failed') = (error IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX job_runs_latest_idx ON job_runs (job, started_at DESC, id DESC);
CREATE INDEX job_runs_started_idx ON job_runs (started_at);

-- +goose Down

DROP TABLE job_runs;
