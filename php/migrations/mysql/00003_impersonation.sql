-- Admin impersonation ("login as") and its audit trail (ADR 0005).
-- Conventions as in 00001_core.sql.

-- +goose Up

-- The admin who signed in as account_id; NULL for a normal session.
ALTER TABLE sessions ADD COLUMN impersonator_id BINARY(16),
    ADD INDEX sessions_impersonator_idx (impersonator_id),
    ADD CONSTRAINT sessions_impersonator_id_fkey FOREIGN KEY (impersonator_id) REFERENCES accounts(id) ON DELETE RESTRICT,
    ADD CONSTRAINT sessions_not_self_impersonated CHECK (impersonator_id <> account_id);

CREATE TABLE audit_events (
    id BINARY(16) PRIMARY KEY,
    occurred_at DATETIME(6) NOT NULL,
    actor_id BINARY(16) NOT NULL,
    action VARBINARY(32) NOT NULL,
    target_id BINARY(16) NOT NULL,
    CONSTRAINT audit_events_action_check CHECK (action IN ('impersonation-started', 'impersonation-ended')),
    KEY audit_events_occurred_idx (occurred_at DESC, id DESC),
    KEY audit_events_actor_idx (actor_id),
    KEY audit_events_target_idx (target_id),
    CONSTRAINT audit_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES accounts(id) ON DELETE RESTRICT,
    CONSTRAINT audit_events_target_id_fkey FOREIGN KEY (target_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE audit_events;
ALTER TABLE sessions
    DROP FOREIGN KEY sessions_impersonator_id_fkey,
    DROP CONSTRAINT sessions_not_self_impersonated,
    DROP INDEX sessions_impersonator_idx,
    DROP COLUMN impersonator_id;
