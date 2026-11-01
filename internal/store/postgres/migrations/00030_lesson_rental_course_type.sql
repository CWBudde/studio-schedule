-- A room rental's course type is optional (ADR 0007, amendment 2026-11-01);
-- every other lesson keeps needing one (domain.Lesson.Validate).
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE lessons
    ALTER COLUMN course_type_id DROP NOT NULL,
    ADD CONSTRAINT lessons_course_type CHECK (course_type_id IS NOT NULL OR kind = 'rental');

-- +goose Down

-- The rentals without a course type go: the older schema has no place for
-- them. They have no bookings, so nothing refers to them that would refuse
-- it.
DELETE FROM lessons WHERE course_type_id IS NULL;

ALTER TABLE lessons
    DROP CONSTRAINT lessons_course_type,
    ALTER COLUMN course_type_id SET NOT NULL;
