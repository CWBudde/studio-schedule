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
SELECT * FROM outbox_mails ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(max_rows);
