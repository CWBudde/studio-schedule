-- +goose Up

CREATE TABLE calendar_feeds (
    id BINARY(16) PRIMARY KEY,
    account_id BINARY(16) NOT NULL,
    scope VARBINARY(32) NOT NULL,
    location_id BINARY(16),
    token_hash VARBINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    last_used_at DATETIME(6),
    revoked_at DATETIME(6),
    -- NULL disables the unique key for revoked feeds. For active feeds, the
    -- one-byte sentinel is distinct from every 16-byte location, including zero.
    active_location_key VARBINARY(16) GENERATED ALWAYS AS (
        CASE WHEN revoked_at IS NULL THEN COALESCE(location_id, X'00') END
    ) VIRTUAL,
    CONSTRAINT calendar_feeds_scope_check CHECK (scope IN ('bookings', 'teaching', 'schedule')),
    CONSTRAINT calendar_feeds_token_hash_check CHECK (OCTET_LENGTH(token_hash) = 32),
    CONSTRAINT calendar_feeds_location_schedule CHECK (location_id IS NULL OR scope = 'schedule'),
    CONSTRAINT calendar_feeds_used_after_created CHECK (last_used_at >= created_at),
    CONSTRAINT calendar_feeds_revoked_after_created CHECK (revoked_at >= created_at),
    UNIQUE KEY calendar_feeds_token_key (token_hash),
    UNIQUE KEY calendar_feeds_one_active_key (account_id, scope, active_location_key),
    KEY calendar_feeds_account_idx (account_id, created_at, id),
    KEY calendar_feeds_location_idx (location_id),
    CONSTRAINT calendar_feeds_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    CONSTRAINT calendar_feeds_location_id_fkey FOREIGN KEY (location_id) REFERENCES locations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE calendar_feeds;
