-- The failed mails of the outbox: delivery was given up, so the mail is
-- neither sent nor due (domain.MailFailed). Conventions as in 00001_core.sql.
--
-- The metrics count the unsent mails on every scrape: the pending ones from
-- outbox_mails_due_idx, the failed ones from this index, instead of reading
-- the whole table (the mails of MailRetention). The mail log's "failed"
-- filter reads it in its order. It holds the failed mails only, so it stays
-- small.

-- +goose Up

CREATE INDEX outbox_mails_failed_idx ON outbox_mails (created_at DESC, id DESC)
    WHERE sent_at IS NULL AND next_attempt_at IS NULL;

-- +goose Down

DROP INDEX outbox_mails_failed_idx;
