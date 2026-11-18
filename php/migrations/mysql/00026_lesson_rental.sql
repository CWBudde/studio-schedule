-- +goose Up

ALTER TABLE lessons
    DROP CONSTRAINT lessons_kind_check,
    DROP CONSTRAINT lessons_capacity_check,
    MODIFY COLUMN instructor_id BINARY(16),
    ADD COLUMN renter LONGTEXT,
    ADD CONSTRAINT lessons_renter_check CHECK (OCTET_LENGTH(TRIM(renter)) > 0 AND CHAR_LENGTH(renter) <= 100),
    ADD CONSTRAINT lessons_kind_check CHECK (kind IN ('regular', 'workshop', 'private', 'rental')),
    ADD CONSTRAINT lessons_capacity_check CHECK (capacity > 0 OR kind = 'rental'),
    ADD CONSTRAINT lessons_rental CHECK (
        (kind = 'rental' AND instructor_id IS NULL AND accompanist_id IS NULL AND capacity = 0 AND visibility = 'hidden'
            AND class_id IS NULL AND renter IS NOT NULL)
        OR (kind <> 'rental' AND instructor_id IS NOT NULL AND renter IS NULL)
    );

-- +goose Down

DELETE FROM lessons WHERE kind = 'rental';

ALTER TABLE lessons DROP FOREIGN KEY lessons_instructor_id_fkey;

ALTER TABLE lessons
    DROP CONSTRAINT lessons_rental,
    DROP CONSTRAINT lessons_capacity_check,
    DROP CONSTRAINT lessons_kind_check,
    DROP CONSTRAINT lessons_renter_check,
    DROP COLUMN renter,
    MODIFY COLUMN instructor_id BINARY(16) NOT NULL,
    ADD CONSTRAINT lessons_instructor_id_fkey FOREIGN KEY (instructor_id) REFERENCES staff(id) ON DELETE RESTRICT,
    ADD CONSTRAINT lessons_capacity_check CHECK (capacity > 0),
    ADD CONSTRAINT lessons_kind_check CHECK (kind IN ('regular', 'workshop', 'private'));
