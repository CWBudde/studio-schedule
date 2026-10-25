-- name: GetCalendarFeed :one
SELECT * FROM calendar_feeds WHERE id = $1;

-- name: GetCalendarFeedByToken :one
SELECT * FROM calendar_feeds WHERE token_hash = $1;

-- name: CreateCalendarFeed :exec
INSERT INTO calendar_feeds (id, account_id, scope, location_id, token_hash, created_at, last_used_at, revoked_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListCalendarFeedsByAccount :many
SELECT * FROM calendar_feeds
WHERE account_id = $1 AND revoked_at IS NULL
ORDER BY created_at, id;

-- name: RevokeCalendarFeed :execrows
UPDATE calendar_feeds SET revoked_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeActiveCalendarFeed :execrows
UPDATE calendar_feeds SET revoked_at = sqlc.arg(revoked_at)
WHERE account_id = sqlc.arg(account_id) AND scope = sqlc.arg(scope)
    AND location_id IS NOT DISTINCT FROM sqlc.narg(location_id) AND revoked_at IS NULL;

-- name: TouchCalendarFeed :execrows
UPDATE calendar_feeds SET last_used_at = sqlc.arg(last_used_at) WHERE id = sqlc.arg(id);
