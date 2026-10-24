-- Staff actions (ADR 0009, amendment): bookings that the office or a lesson's
-- instructor or accompanist make (source 'staff'), guests whose email is
-- optional, and a note on trial bookings for the instructor.
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE bookings
    DROP CONSTRAINT bookings_source_check,
    ADD CONSTRAINT bookings_source_check CHECK (source IN ('self', 'member', 'staff'));

-- A booking has a person or a guest name; a guest's email is optional.
ALTER TABLE bookings
    DROP CONSTRAINT bookings_person_or_guest,
    ADD CONSTRAINT bookings_person_or_guest CHECK (
        (person_id IS NOT NULL AND guest_name IS NULL AND guest_email IS NULL)
        OR (person_id IS NULL AND guest_name IS NOT NULL)
    );

-- NULL: no note. Only a trial booking has one.
ALTER TABLE bookings
    ADD COLUMN trial_note text CHECK (trial_note <> ''),
    ADD CONSTRAINT bookings_trial_note_when_trial CHECK (trial_note IS NULL OR trial);

-- +goose Down

-- Guests without an email and staff bookings have no place in the old
-- schema: the rollback fails while they exist, rather than losing them.
ALTER TABLE bookings
    DROP CONSTRAINT bookings_trial_note_when_trial,
    DROP COLUMN trial_note;

ALTER TABLE bookings
    DROP CONSTRAINT bookings_person_or_guest,
    ADD CONSTRAINT bookings_person_or_guest CHECK (
        (person_id IS NOT NULL AND guest_name IS NULL AND guest_email IS NULL)
        OR (person_id IS NULL AND guest_name IS NOT NULL AND guest_email IS NOT NULL)
    );

ALTER TABLE bookings
    DROP CONSTRAINT bookings_source_check,
    ADD CONSTRAINT bookings_source_check CHECK (source IN ('self', 'member'));
