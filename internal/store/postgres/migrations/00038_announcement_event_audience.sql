-- +goose Up
ALTER TABLE announcements
    DROP CONSTRAINT announcements_audience_check,
    DROP CONSTRAINT announcements_audience_ref,
    ADD COLUMN event_id uuid REFERENCES events ON DELETE RESTRICT,
    ADD CONSTRAINT announcements_audience_check CHECK (audience IN ('everyone', 'role', 'class', 'course-type', 'lesson', 'event')),
    ADD CONSTRAINT announcements_audience_ref CHECK (
        (role IS NOT NULL) = (audience = 'role')
        AND (class_id IS NOT NULL) = (audience = 'class')
        AND (course_type_id IS NOT NULL) = (audience = 'course-type')
        AND (event_id IS NOT NULL) = (audience = 'event')
        AND (lesson_id IS NOT NULL) = (audience = 'lesson')
    );

CREATE INDEX announcements_event_idx ON announcements (event_id) WHERE event_id IS NOT NULL;

-- +goose Down
DELETE FROM announcements WHERE audience = 'event';
ALTER TABLE announcements
    DROP CONSTRAINT announcements_audience_check,
    DROP CONSTRAINT announcements_audience_ref,
    DROP COLUMN event_id,
    ADD CONSTRAINT announcements_audience_check CHECK (audience IN ('everyone', 'role', 'class', 'course-type', 'lesson')),
    ADD CONSTRAINT announcements_audience_ref CHECK (
        (role IS NOT NULL) = (audience = 'role')
        AND (class_id IS NOT NULL) = (audience = 'class')
        AND (course_type_id IS NOT NULL) = (audience = 'course-type')
        AND (lesson_id IS NOT NULL) = (audience = 'lesson')
    );
