-- Lesson administration (ADR 0007): the office edits single lessons, which
-- detaches them from their class, and cancels them with a note. Conventions
-- as in 00001_core.sql.

-- +goose Up

-- An individually edited lesson of a class no longer follows class and term
-- edits. Every existing lesson starts undetached; the implicit rule (a lesson
-- that differs from its class does not follow it) still covers older edits.
ALTER TABLE lessons
    ADD COLUMN detached boolean NOT NULL DEFAULT false,
    ADD COLUMN cancel_note text CHECK (btrim(cancel_note) <> ''),
    ADD COLUMN canceled_at timestamptz,
    ADD COLUMN canceled_by uuid REFERENCES accounts ON DELETE RESTRICT,
    ADD CONSTRAINT lessons_detached_with_class CHECK (NOT detached OR class_id IS NOT NULL),
    ADD CONSTRAINT lessons_cancellation_when_canceled CHECK (
        status = 'canceled' OR (cancel_note IS NULL AND canceled_at IS NULL AND canceled_by IS NULL)
    );

CREATE INDEX lessons_canceled_by_idx ON lessons (canceled_by);

-- +goose Down

DROP INDEX lessons_canceled_by_idx;

ALTER TABLE lessons
    DROP CONSTRAINT lessons_cancellation_when_canceled,
    DROP CONSTRAINT lessons_detached_with_class,
    DROP COLUMN canceled_by,
    DROP COLUMN canceled_at,
    DROP COLUMN cancel_note,
    DROP COLUMN detached;
