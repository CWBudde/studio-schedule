-- Admin impersonation ("login as") and its audit trail (ADR 0005).
-- Conventions as in 00001_core.sql.

-- +goose Up

-- The admin who signed in as account_id; NULL for a normal session.
ALTER TABLE sessions ADD COLUMN impersonator_id uuid REFERENCES accounts ON DELETE RESTRICT,
    ADD CONSTRAINT sessions_not_self_impersonated CHECK (impersonator_id <> account_id);

CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    occurred_at timestamptz NOT NULL,
    actor_id uuid NOT NULL REFERENCES accounts ON DELETE RESTRICT,
    action text NOT NULL CHECK (action IN ('impersonation-started', 'impersonation-ended')),
    target_id uuid NOT NULL REFERENCES accounts ON DELETE RESTRICT
);

CREATE INDEX audit_events_occurred_idx ON audit_events (occurred_at DESC, id DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_id);
CREATE INDEX audit_events_target_idx ON audit_events (target_id);
CREATE INDEX sessions_impersonator_idx ON sessions (impersonator_id) WHERE impersonator_id IS NOT NULL;

-- +goose Down

DROP TABLE audit_events;
ALTER TABLE sessions DROP COLUMN impersonator_id;
