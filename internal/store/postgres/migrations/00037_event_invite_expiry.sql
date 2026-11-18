-- +goose Up

CREATE INDEX event_registrations_invite_expiry_idx
    ON event_registrations (expires_at) WHERE status = 'invited';

-- +goose Down

DROP INDEX event_registrations_invite_expiry_idx;
