-- +goose Up

-- MySQL and MariaDB index every row. Only invited rows have an expiry.
CREATE INDEX event_registrations_invite_expiry_idx ON event_registrations (expires_at);

-- +goose Down

DROP INDEX event_registrations_invite_expiry_idx ON event_registrations;
