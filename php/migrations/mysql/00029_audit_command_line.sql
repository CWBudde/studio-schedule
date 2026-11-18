-- +goose Up

INSERT INTO audit_actions (action) VALUES ('password-reset-issued'), ('sessions-revoked'), ('setup-link-issued');

ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor;
ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_actor CHECK (
        actor_id IS NOT NULL OR action IN ('account-deleted', 'setup-link-issued')
    );

-- +goose Down

DELETE FROM audit_events WHERE action IN ('password-reset-issued', 'sessions-revoked', 'setup-link-issued');

ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor;
ALTER TABLE audit_events
    ADD CONSTRAINT audit_events_actor CHECK (actor_id IS NOT NULL OR action = 'account-deleted');

DELETE FROM audit_actions WHERE action IN ('password-reset-issued', 'sessions-revoked', 'setup-link-issued');
