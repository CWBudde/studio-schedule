-- +goose Up

-- The leading columns narrow the index to the failed mails for queries
-- that would use the partial index on PostgreSQL.
CREATE INDEX outbox_mails_failed_idx ON outbox_mails (sent_at, next_attempt_at, created_at DESC, id DESC);

-- +goose Down

DROP INDEX outbox_mails_failed_idx ON outbox_mails;
