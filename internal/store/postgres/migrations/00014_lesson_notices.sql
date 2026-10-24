-- The instructor view (ADR 0009, amendment): the notices staff send to the
-- participants of a lesson, which the notices' rate limits count, the
-- Reply-To address of a mail, and the lessons of a staff member by start.
-- Conventions as in 00001_core.sql, except that a notice outlives its
-- lesson: it keeps counting for its sender's hourly limit and records what
-- was sent, without the lesson.

-- +goose Up

-- NULL: replies go to the sender address.
ALTER TABLE outbox_mails
    ADD COLUMN reply_to text CHECK (reply_to <> '');

CREATE TABLE lesson_notices (
    id uuid PRIMARY KEY,
    -- NULL once the lesson is deleted.
    lesson_id uuid REFERENCES lessons ON DELETE SET NULL,
    sender_id uuid NOT NULL REFERENCES accounts ON DELETE RESTRICT,
    sent_at timestamptz NOT NULL,
    recipients integer NOT NULL CHECK (recipients > 0)
);

-- The limits per sender and per lesson count the notices of a window.
CREATE INDEX lesson_notices_sender_idx ON lesson_notices (sender_id, sent_at, id);
CREATE INDEX lesson_notices_lesson_idx ON lesson_notices (lesson_id, sent_at, id);

-- The lessons of a staff member in a range (ListByStaff); the indexes also
-- serve the foreign keys the single-column ones served.
CREATE INDEX lessons_instructor_starts_idx ON lessons (instructor_id, starts_at, id);
CREATE INDEX lessons_accompanist_starts_idx ON lessons (accompanist_id, starts_at, id);
DROP INDEX lessons_instructor_idx;
DROP INDEX lessons_accompanist_idx;

-- +goose Down

CREATE INDEX lessons_accompanist_idx ON lessons (accompanist_id);
CREATE INDEX lessons_instructor_idx ON lessons (instructor_id);
DROP INDEX lessons_accompanist_starts_idx;
DROP INDEX lessons_instructor_starts_idx;

DROP TABLE lesson_notices;

ALTER TABLE outbox_mails DROP COLUMN reply_to;
