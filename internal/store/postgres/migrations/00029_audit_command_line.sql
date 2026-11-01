-- The audit actions of the command line's account recovery (ADR 0020):
-- a password-reset link issued, sessions ended, and a setup link an
-- operator printed while no admin could sign in. Conventions as in
-- 00001_core.sql.
--
-- audit_events.actor_id: NULL only for an account its holder deleted
-- (account-deleted) and for a setup link, which an operator issues without
-- an account (setup-link-issued; the details name the OS user).

-- +goose Up

INSERT INTO audit_actions (action) VALUES ('password-reset-issued'), ('sessions-revoked'), ('setup-link-issued');

ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor,
    ADD CONSTRAINT audit_events_actor CHECK (
        actor_id IS NOT NULL OR action IN ('account-deleted', 'setup-link-issued')
    );

-- +goose Down

DELETE FROM audit_events WHERE action IN ('password-reset-issued', 'sessions-revoked', 'setup-link-issued');

ALTER TABLE audit_events DROP CONSTRAINT audit_events_actor,
    ADD CONSTRAINT audit_events_actor CHECK (actor_id IS NOT NULL OR action = 'account-deleted');

DELETE FROM audit_actions WHERE action IN ('password-reset-issued', 'sessions-revoked', 'setup-link-issued');
