-- +goose Up

ALTER TABLE announcements
    DROP CONSTRAINT announcements_audience_check,
    DROP CONSTRAINT announcements_audience_ref;
ALTER TABLE announcements
    ADD COLUMN event_id BINARY(16),
    ADD INDEX announcements_event_idx (event_id),
    ADD CONSTRAINT announcements_event_id_fkey FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE RESTRICT,
    ADD CONSTRAINT announcements_audience_check CHECK (audience IN ('everyone', 'role', 'class', 'course-type', 'lesson', 'event')),
    ADD CONSTRAINT announcements_audience_ref CHECK (
        (role IS NOT NULL) = (audience = 'role')
        AND (class_id IS NOT NULL) = (audience = 'class')
        AND (course_type_id IS NOT NULL) = (audience = 'course-type')
        AND (event_id IS NOT NULL) = (audience = 'event')
        AND (lesson_id IS NOT NULL) = (audience = 'lesson')
    );

-- +goose Down

DELETE FROM announcements WHERE audience = 'event';
ALTER TABLE announcements
    DROP FOREIGN KEY announcements_event_id_fkey,
    DROP CONSTRAINT announcements_audience_check,
    DROP CONSTRAINT announcements_audience_ref;
ALTER TABLE announcements
    DROP COLUMN event_id,
    ADD CONSTRAINT announcements_audience_check CHECK (audience IN ('everyone', 'role', 'class', 'course-type', 'lesson')),
    ADD CONSTRAINT announcements_audience_ref CHECK (
        (role IS NOT NULL) = (audience = 'role')
        AND (class_id IS NOT NULL) = (audience = 'class')
        AND (course_type_id IS NOT NULL) = (audience = 'course-type')
        AND (lesson_id IS NOT NULL) = (audience = 'lesson')
    );
