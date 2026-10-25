-- name: CreateAuditEvent :exec
INSERT INTO audit_events (id, occurred_at, actor_id, action, target_kind, target_id, target_person_id, details)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListAuditEvents :many
SELECT * FROM audit_events ORDER BY occurred_at DESC, id DESC LIMIT sqlc.arg(max_rows);

-- name: DeleteAuditEventsBefore :execrows
DELETE FROM audit_events WHERE occurred_at < $1;

-- name: RetargetAuditEventsToPerson :exec
-- Before an account is deleted, the events that target it target its person
-- instead (ADR 0016).
UPDATE audit_events
SET target_kind = 'person', target_id = NULL, target_person_id = sqlc.arg(person_id)
WHERE target_id = sqlc.arg(account_id);
