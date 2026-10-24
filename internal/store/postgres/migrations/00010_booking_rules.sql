-- Booking rules (ADR 0009): how a booking came about, late cancellations,
-- and bookings beyond capacity. Conventions as in 00001_core.sql.

-- +goose Up

-- Every existing booking was made by a member; a late flag only marks a
-- canceled booking. The staff actions add their own source later.
ALTER TABLE bookings
    ADD COLUMN source text NOT NULL DEFAULT 'self' CHECK (source IN ('self', 'member')),
    ADD COLUMN late boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT bookings_late_when_canceled CHECK (NOT late OR status = 'canceled');

-- An event records whether its cancellation came late and whether its
-- booking went beyond the lesson's capacity.
ALTER TABLE booking_events
    ADD COLUMN late boolean NOT NULL DEFAULT false,
    ADD COLUMN override boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT booking_events_late_when_canceled CHECK (NOT late OR status = 'canceled');

-- +goose Down

ALTER TABLE booking_events
    DROP CONSTRAINT booking_events_late_when_canceled,
    DROP COLUMN override,
    DROP COLUMN late;

ALTER TABLE bookings
    DROP CONSTRAINT bookings_late_when_canceled,
    DROP COLUMN late,
    DROP COLUMN source;
