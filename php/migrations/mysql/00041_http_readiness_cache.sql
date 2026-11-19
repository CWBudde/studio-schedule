-- +goose Up

CREATE TABLE http_readiness_cache (
    id INT PRIMARY KEY,
    version_hash VARCHAR(64) NOT NULL,
    checked_at_us BIGINT NOT NULL,
    ready BOOLEAN NOT NULL,
    CONSTRAINT http_readiness_cache_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO http_readiness_cache (id, version_hash, checked_at_us, ready) VALUES (1, '', 0, FALSE);

-- +goose Down

DROP TABLE http_readiness_cache;
