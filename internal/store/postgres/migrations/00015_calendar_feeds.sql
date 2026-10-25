-- Calendar feeds (ADR 0011): an account's iCalendar subscription URLs, each
-- secured by a random token of which only the SHA-256 is stored. Conventions
-- as in 00001_core.sql, except that feeds go with their account and their
-- location: the subject of a feed that is gone leaves nothing to subscribe to.

-- +goose Up

CREATE TABLE calendar_feeds (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts ON DELETE CASCADE,
    scope text NOT NULL CHECK (scope IN ('bookings', 'teaching', 'schedule')),
    -- NULL: the whole school; only a schedule feed has a location.
    location_id uuid REFERENCES locations ON DELETE CASCADE,
    token_hash bytea NOT NULL CHECK (octet_length(token_hash) = 32),
    created_at timestamptz NOT NULL,
    -- Updated at most once a day by the feed's fetches.
    last_used_at timestamptz,
    revoked_at timestamptz,
    CONSTRAINT calendar_feeds_location_schedule CHECK (location_id IS NULL OR scope = 'schedule'),
    CONSTRAINT calendar_feeds_used_after_created CHECK (last_used_at >= created_at),
    CONSTRAINT calendar_feeds_revoked_after_created CHECK (revoked_at >= created_at)
);

CREATE UNIQUE INDEX calendar_feeds_token_key ON calendar_feeds (token_hash);
-- One active feed per subject: a new link revokes the old one first.
CREATE UNIQUE INDEX calendar_feeds_one_active_key ON calendar_feeds (account_id, scope, location_id)
    NULLS NOT DISTINCT WHERE revoked_at IS NULL;
CREATE INDEX calendar_feeds_account_idx ON calendar_feeds (account_id, created_at, id);
CREATE INDEX calendar_feeds_location_idx ON calendar_feeds (location_id);

-- +goose Down

DROP TABLE calendar_feeds;
