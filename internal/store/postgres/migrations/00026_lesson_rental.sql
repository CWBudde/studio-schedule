-- Room rentals (ADR 0007, amendment 2026-10-31): the lesson kind 'rental'
-- blocks a room for an outside renter. A rental is a one-off lesson without
-- instructor, accompanist or capacity, visible to staff only, and names its
-- renter (domain.Lesson.Validate). Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE lessons
    DROP CONSTRAINT lessons_kind_check,
    DROP CONSTRAINT lessons_capacity_check,
    ALTER COLUMN instructor_id DROP NOT NULL,
    -- At most domain.MaxRenterLength characters.
    ADD COLUMN renter text CHECK (btrim(renter) <> '' AND char_length(renter) <= 100),
    ADD CONSTRAINT lessons_kind_check CHECK (kind IN ('regular', 'workshop', 'private', 'rental')),
    ADD CONSTRAINT lessons_capacity_check CHECK (capacity > 0 OR kind = 'rental'),
    ADD CONSTRAINT lessons_rental CHECK (
        CASE WHEN kind = 'rental'
            THEN instructor_id IS NULL AND accompanist_id IS NULL AND capacity = 0 AND visibility = 'hidden'
                AND class_id IS NULL AND renter IS NOT NULL
            ELSE instructor_id IS NOT NULL AND renter IS NULL
        END
    );

-- +goose Down

-- The rentals go: the older schema has no place for them. They have no
-- bookings, so nothing refers to them that would refuse it.
DELETE FROM lessons WHERE kind = 'rental';

ALTER TABLE lessons
    DROP CONSTRAINT lessons_rental,
    DROP CONSTRAINT lessons_capacity_check,
    DROP CONSTRAINT lessons_kind_check,
    DROP COLUMN renter,
    ALTER COLUMN instructor_id SET NOT NULL,
    ADD CONSTRAINT lessons_capacity_check CHECK (capacity > 0),
    ADD CONSTRAINT lessons_kind_check CHECK (kind IN ('regular', 'workshop', 'private'));
