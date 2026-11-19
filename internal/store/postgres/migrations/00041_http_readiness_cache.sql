-- +goose Up

-- PHP workers share one readiness result for this database.
CREATE TABLE http_readiness_cache (
    id INTEGER PRIMARY KEY,
    version_hash VARCHAR(64) NOT NULL,
    checked_at_us BIGINT NOT NULL,
    ready BOOLEAN NOT NULL,
    CONSTRAINT http_readiness_cache_singleton CHECK (id = 1)
);

INSERT INTO http_readiness_cache (id, version_hash, checked_at_us, ready) VALUES (1, '', 0, FALSE);

-- +goose Down

DROP TABLE http_readiness_cache;
