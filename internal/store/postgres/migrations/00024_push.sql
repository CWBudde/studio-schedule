-- Web push (ADR 0017): the push subscriptions of accounts, one per browser
-- and device, and the outbox of push messages. Conventions as in
-- 00001_core.sql, except that subscriptions go with their account and
-- messages with their subscription: a deleted account has no devices to
-- notify, and a subscription the push service no longer knows has nobody
-- to reach.
--
-- An endpoint is the push service's URL of one subscription, so it is
-- unique; when it moves to another account (the device changed hands) the
-- store deletes the old row with its messages and creates a new one.
-- last_used_at starts at created_at and moves when the device saves the
-- subscription again or a message reaches the push service; the retention
-- job drops subscriptions unused for six months.
--
-- A message is pending until it is sent (sent_at) or given up (failed_at):
-- only a pending one has a next attempt. Messages are deleted after a week,
-- sent or not.

-- +goose Up

CREATE TABLE push_subscriptions (
    id uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts ON DELETE CASCADE,
    endpoint text NOT NULL CHECK (endpoint LIKE 'https://%' AND length(endpoint) <= 2048),
    p256dh bytea NOT NULL CHECK (octet_length(p256dh) = 65),
    auth bytea NOT NULL CHECK (octet_length(auth) = 16),
    device_label text CHECK (char_length(device_label) <= 100),
    created_at timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL,
    CONSTRAINT push_subscriptions_used_after_created CHECK (last_used_at >= created_at)
);

CREATE UNIQUE INDEX push_subscriptions_endpoint_key ON push_subscriptions (endpoint);
CREATE INDEX push_subscriptions_account_idx ON push_subscriptions (account_id, created_at, id);
CREATE INDEX push_subscriptions_last_used_idx ON push_subscriptions (last_used_at);

CREATE TABLE push_outbox (
    id uuid PRIMARY KEY,
    subscription_id uuid NOT NULL REFERENCES push_subscriptions ON DELETE CASCADE,
    -- json, not jsonb: the payload comes back as it was written.
    payload json NOT NULL CHECK (json_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz,
    sent_at timestamptz,
    failed_at timestamptz,
    last_error text,
    CONSTRAINT push_outbox_expires_after_created CHECK (expires_at > created_at),
    CONSTRAINT push_outbox_sent_or_failed CHECK (sent_at IS NULL OR failed_at IS NULL),
    CONSTRAINT push_outbox_pending_due CHECK (
        (sent_at IS NULL AND failed_at IS NULL) = (next_attempt_at IS NOT NULL)
    )
);

CREATE INDEX push_outbox_due_idx ON push_outbox (next_attempt_at, id) WHERE next_attempt_at IS NOT NULL;
CREATE INDEX push_outbox_subscription_idx ON push_outbox (subscription_id);
CREATE INDEX push_outbox_created_idx ON push_outbox (created_at);

-- +goose Down

DROP TABLE push_outbox;
DROP TABLE push_subscriptions;
