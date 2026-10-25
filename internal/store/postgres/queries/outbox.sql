-- name: GetOutboxMail :one
SELECT * FROM outbox_mails WHERE id = $1;

-- name: CreateOutboxMail :exec
INSERT INTO outbox_mails (
    id, recipient, subject, body_text, body_html, template, created_at,
    attempts, next_attempt_at, sent_at, last_error, reply_to, receipt_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: UpdateOutboxMail :execrows
UPDATE outbox_mails
SET recipient = $2, subject = $3, body_text = $4, body_html = $5, template = $6, created_at = $7,
    attempts = $8, next_attempt_at = $9, sent_at = $10, last_error = $11, reply_to = $12, receipt_id = $13
WHERE id = $1;

-- name: ListDueOutboxMails :many
-- SKIP LOCKED lets concurrent workers each take different mails.
SELECT * FROM outbox_mails
WHERE sent_at IS NULL AND next_attempt_at IS NOT NULL AND next_attempt_at <= sqlc.arg(now)
ORDER BY next_attempt_at, id
LIMIT sqlc.arg(max_rows)
FOR UPDATE SKIP LOCKED;

-- name: ListOutboxMails :many
-- The status is derived as in domain.OutboxMail.Status. The recipient
-- matches ignoring ASCII case: lower() under collation "C" folds ASCII only.
-- The cursor, if given, keeps the mails after it in the order.
SELECT * FROM outbox_mails
WHERE (sqlc.arg(status)::text = ''
        OR (sqlc.arg(status) = 'sent' AND sent_at IS NOT NULL)
        OR (sqlc.arg(status) = 'failed' AND sent_at IS NULL AND next_attempt_at IS NULL)
        OR (sqlc.arg(status) = 'pending' AND sent_at IS NULL AND next_attempt_at IS NOT NULL))
    AND (sqlc.arg(template)::text = '' OR template = sqlc.arg(template))
    AND (sqlc.arg(recipient)::text = ''
        OR strpos(lower(recipient COLLATE "C"), lower(sqlc.arg(recipient)::text COLLATE "C")) > 0)
    AND (sqlc.narg(before_created_at)::timestamptz IS NULL
        OR (created_at, id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(max_rows);

-- name: PurgeOutboxBodies :execrows
UPDATE outbox_mails SET body_text = NULL, body_html = NULL
WHERE sent_at < sqlc.arg(sent_before) AND body_text IS NOT NULL;

-- name: DeleteOldOutboxMails :execrows
DELETE FROM outbox_mails WHERE created_at < sqlc.arg(created_before);
