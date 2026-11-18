-- The instructor view (ADR 0009, amendment): the notices staff send to the
-- participants of a lesson, which the notices' rate limits count, the
-- Reply-To address of a mail, and the lessons of a staff member by start.
-- Conventions as in 00001_core.sql, except that a notice outlives its
-- lesson: it keeps counting for its sender's hourly limit and records what
-- was sent, without the lesson.

-- +goose Up

-- NULL: replies go to the sender address.
ALTER TABLE outbox_mails
    ADD COLUMN reply_to LONGTEXT,
    ADD CONSTRAINT outbox_mails_reply_to_check CHECK (OCTET_LENGTH(reply_to) > 0);

CREATE TABLE lesson_notices (
    id BINARY(16) PRIMARY KEY,
    -- NULL once the lesson is deleted.
    lesson_id BINARY(16),
    sender_id BINARY(16) NOT NULL,
    sent_at DATETIME(6) NOT NULL,
    recipients INT NOT NULL,
    CONSTRAINT lesson_notices_recipients_check CHECK (recipients > 0),
    KEY lesson_notices_sender_idx (sender_id, sent_at, id),
    KEY lesson_notices_lesson_idx (lesson_id, sent_at, id),
    CONSTRAINT lesson_notices_lesson_id_fkey FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE SET NULL,
    CONSTRAINT lesson_notices_sender_id_fkey FOREIGN KEY (sender_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- The lessons of a staff member in a range (ListByStaff); the indexes also
-- serve the foreign keys the single-column ones served.
CREATE INDEX lessons_instructor_starts_idx ON lessons (instructor_id, starts_at, id);
CREATE INDEX lessons_accompanist_starts_idx ON lessons (accompanist_id, starts_at, id);
DROP INDEX lessons_instructor_idx ON lessons;
DROP INDEX lessons_accompanist_idx ON lessons;

-- +goose Down

CREATE INDEX lessons_accompanist_idx ON lessons (accompanist_id);
CREATE INDEX lessons_instructor_idx ON lessons (instructor_id);
DROP INDEX lessons_accompanist_starts_idx ON lessons;
DROP INDEX lessons_instructor_starts_idx ON lessons;

DROP TABLE lesson_notices;

ALTER TABLE outbox_mails DROP CONSTRAINT outbox_mails_reply_to_check, DROP COLUMN reply_to;
