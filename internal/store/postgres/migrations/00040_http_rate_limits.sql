-- +goose Up

-- PHP shares fixed-window rate limits across workers in this table.
CREATE TABLE http_rate_limits (
    bucket VARCHAR(64) NOT NULL,
    key_hash BYTEA NOT NULL,
    attempts INTEGER NOT NULL,
    pending INTEGER NOT NULL,
    expires_at_us BIGINT NOT NULL,
    generation BYTEA NOT NULL,
    PRIMARY KEY (bucket, key_hash),
    CONSTRAINT http_rate_limits_key_hash_check CHECK (OCTET_LENGTH(key_hash) = 32),
    CONSTRAINT http_rate_limits_attempts_check CHECK (attempts >= 0),
    CONSTRAINT http_rate_limits_pending_check CHECK (pending >= 0),
    CONSTRAINT http_rate_limits_generation_check CHECK (OCTET_LENGTH(generation) = 16)
);

CREATE INDEX http_rate_limits_expiry_idx ON http_rate_limits (expires_at_us);

-- +goose Down

DROP TABLE http_rate_limits;
