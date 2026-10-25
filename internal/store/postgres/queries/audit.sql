-- name: CreateAuditEvent :exec
INSERT INTO audit_events (id, occurred_at, actor_id, action, target_kind, target_id, target_person_id, details)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListAuditEvents :many
SELECT * FROM audit_events ORDER BY occurred_at DESC, id DESC LIMIT sqlc.arg(max_rows);
