-- name: GetPushSubscription :one
SELECT * FROM push_subscriptions WHERE id = $1;

-- name: DeleteMovedPushSubscription :execrows
-- The endpoint moves to another account: its old row goes, with its
-- messages (ON DELETE CASCADE).
DELETE FROM push_subscriptions WHERE endpoint = sqlc.arg(endpoint) AND account_id <> sqlc.arg(account_id);

-- name: SavePushSubscription :exec
-- The same account's endpoint keeps its ID and creation time. The
-- conflict is another account's endpoint too if one committed it after
-- DeleteMovedPushSubscription ran: it becomes this account's, and its
-- queued messages, which were the other account's, go (moved).
WITH moved AS (
    DELETE FROM push_outbox
    WHERE subscription_id IN (
        SELECT s.id FROM push_subscriptions s
        WHERE s.endpoint = sqlc.arg(endpoint) AND s.account_id <> sqlc.arg(account_id)
    )
)
INSERT INTO push_subscriptions (id, account_id, endpoint, p256dh, auth, device_label, created_at, last_used_at)
VALUES (
    sqlc.arg(id), sqlc.arg(account_id), sqlc.arg(endpoint), sqlc.arg(p256dh), sqlc.arg(auth),
    sqlc.narg(device_label), sqlc.arg(created_at), sqlc.arg(last_used_at)
)
ON CONFLICT (endpoint) DO UPDATE
SET account_id = excluded.account_id, p256dh = excluded.p256dh, auth = excluded.auth,
    device_label = excluded.device_label, last_used_at = excluded.last_used_at;

-- name: ListPushSubscriptionsByAccount :many
SELECT * FROM push_subscriptions WHERE account_id = $1 ORDER BY created_at, id;

-- name: DeletePushSubscription :execrows
DELETE FROM push_subscriptions WHERE id = $1;

-- name: DeletePushSubscriptionsByAccount :execrows
DELETE FROM push_subscriptions WHERE account_id = $1;

-- name: DeletePushSubscriptionByEndpoint :execrows
DELETE FROM push_subscriptions WHERE account_id = sqlc.arg(account_id) AND endpoint = sqlc.arg(endpoint);

-- name: TouchPushSubscription :execrows
UPDATE push_subscriptions SET last_used_at = sqlc.arg(last_used_at) WHERE id = sqlc.arg(id);

-- name: DeleteUnusedPushSubscriptions :execrows
DELETE FROM push_subscriptions WHERE last_used_at < sqlc.arg(used_before);

-- name: GetPushMessage :one
SELECT * FROM push_outbox WHERE id = $1;

-- name: CreatePushMessage :exec
INSERT INTO push_outbox (
    id, subscription_id, payload, created_at, expires_at, attempts, next_attempt_at, sent_at, failed_at, last_error
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: UpdatePushMessage :execrows
UPDATE push_outbox
SET payload = $2, expires_at = $3, attempts = $4, next_attempt_at = $5, sent_at = $6, failed_at = $7,
    last_error = $8
WHERE id = $1;

-- name: ListDuePushMessages :many
-- SKIP LOCKED lets concurrent workers each take different messages.
SELECT * FROM push_outbox
WHERE next_attempt_at IS NOT NULL AND next_attempt_at <= sqlc.arg(now)
ORDER BY next_attempt_at, id
LIMIT sqlc.arg(max_rows)
FOR UPDATE SKIP LOCKED;

-- name: DeleteOldPushMessages :execrows
DELETE FROM push_outbox WHERE created_at < sqlc.arg(created_before);
