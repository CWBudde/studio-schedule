-- +goose Up

ALTER TABLE lessons
    MODIFY COLUMN course_type_id BINARY(16) NULL,
    ADD CONSTRAINT lessons_course_type CHECK (course_type_id IS NOT NULL OR kind = 'rental');

-- +goose Down

-- The older schema has no place for rentals without a course type.
DELETE FROM lessons WHERE course_type_id IS NULL;

ALTER TABLE lessons
    DROP CONSTRAINT lessons_course_type,
    MODIFY COLUMN course_type_id BINARY(16) NOT NULL;
