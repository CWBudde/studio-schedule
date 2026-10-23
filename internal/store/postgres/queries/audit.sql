-- name: CreateAuditEvent :exec
INSERT INTO audit_events (id, occurred_at, actor_id, action, target_id)
VALUES ($1, $2, $3, $4, $5);

-- name: ListAuditEvents :many
SELECT * FROM audit_events ORDER BY occurred_at DESC, id DESC LIMIT sqlc.arg(max_rows);
