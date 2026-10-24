-- Class member bookings (ADR 0009): whether a cancellation released the
-- date (the family canceled it), which keeps a class member from being
-- booked into it again, as opposed to the office ending a membership or a
-- member's pause; and the pause itself, during which a member is not booked
-- automatically.
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE bookings
    ADD COLUMN released boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT bookings_released_when_canceled CHECK (NOT released OR status = 'canceled');

-- NULL: not paused. A member is paused for the lessons whose slot date is on
-- or before paused_until.
ALTER TABLE class_members ADD COLUMN paused_until date;

-- +goose Down

ALTER TABLE class_members DROP COLUMN paused_until;

ALTER TABLE bookings
    DROP CONSTRAINT bookings_released_when_canceled,
    DROP COLUMN released;
