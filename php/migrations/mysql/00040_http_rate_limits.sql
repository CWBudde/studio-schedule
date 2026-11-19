-- +goose Up

CREATE TABLE http_rate_limits (
    bucket VARBINARY(64) NOT NULL,
    key_hash VARBINARY(32) NOT NULL,
    attempts INT NOT NULL,
    pending INT NOT NULL,
    expires_at_us BIGINT NOT NULL,
    generation VARBINARY(16) NOT NULL,
    PRIMARY KEY (bucket, key_hash),
    KEY http_rate_limits_expiry_idx (expires_at_us),
    CONSTRAINT http_rate_limits_key_hash_check CHECK (OCTET_LENGTH(key_hash) = 32),
    CONSTRAINT http_rate_limits_attempts_check CHECK (attempts >= 0),
    CONSTRAINT http_rate_limits_pending_check CHECK (pending >= 0),
    CONSTRAINT http_rate_limits_generation_check CHECK (OCTET_LENGTH(generation) = 16)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE http_rate_limits;
