-- The audit actions of the office's event administration (ADR 0019): a
-- registration added, canceled (with or without a refund) or marked paid,
-- and an event canceled. Conventions as in 00001_core.sql.

-- +goose Up

INSERT INTO audit_actions (action) VALUES
    ('event-registration-added'), ('event-registration-canceled'), ('event-registration-paid'), ('event-canceled');

-- +goose Down

DELETE FROM audit_events
WHERE action IN ('event-registration-added', 'event-registration-canceled', 'event-registration-paid', 'event-canceled');

DELETE FROM audit_actions
WHERE action IN ('event-registration-added', 'event-registration-canceled', 'event-registration-paid', 'event-canceled');
