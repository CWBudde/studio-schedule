-- Generated lessons remember their class slot, and classes can be retired
-- (ADR 0003, ADR 0007). Conventions as in 00001_core.sql.

-- +goose Up

-- The school date of the class slot a lesson was generated for; NULL for
-- one-off lessons. It stays when a lesson moves, so generation does not
-- create the slot again. Until now nothing moved lessons, so the slot is the
-- school date of the start (in UTC for a database without settings, which
-- cannot have generated lessons).
ALTER TABLE lessons ADD COLUMN slot_date DATE;

-- Named zones require the server time zone tables. Outside the engine's
-- supported conversion range, even a fixed one-hour offset leaves the
-- timestamp unchanged. The slot check below rejects either failure.
UPDATE lessons LEFT JOIN settings ON settings.id = TRUE
SET slot_date = CASE
    WHEN settings.timezone IS NULL OR settings.timezone = 'UTC' THEN DATE(starts_at)
    WHEN CONVERT_TZ(starts_at, '+00:00', '+01:00') <> starts_at
        THEN DATE(CONVERT_TZ(starts_at, '+00:00', settings.timezone))
END
WHERE class_id IS NOT NULL;

ALTER TABLE lessons
    ADD CONSTRAINT lessons_slot_with_class CHECK ((class_id IS NULL) = (slot_date IS NULL)),
    ADD CONSTRAINT lessons_class_slot_key UNIQUE (class_id, slot_date),
    DROP INDEX lessons_class_start_key;

-- Retired classes generate no more lessons and leave the public timetable.
ALTER TABLE classes ADD COLUMN retired BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down

-- Restore the old uniqueness rule before removing the retirement flag.
ALTER TABLE lessons
    ADD CONSTRAINT lessons_class_start_key UNIQUE (class_id, starts_at),
    DROP INDEX lessons_class_slot_key,
    DROP CONSTRAINT lessons_slot_with_class,
    DROP COLUMN slot_date;

ALTER TABLE classes DROP COLUMN retired;
