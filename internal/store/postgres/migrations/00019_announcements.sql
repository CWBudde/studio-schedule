-- Announcements (ADR 0014): what the office tells an audience, who
-- dismissed them, and the mailings the daily limit of emailed
-- announcements counts. Conventions as in 00001_core.sql, except that an
-- announcement goes with the lesson it is about, and a mailing outlives its
-- announcement: it keeps counting for the day's limit, without it.

-- +goose Up

CREATE TABLE announcements (
    id uuid PRIMARY KEY,
    author_id uuid NOT NULL REFERENCES accounts ON DELETE RESTRICT,
    title text NOT NULL CHECK (btrim(title) <> ''),
    body text NOT NULL CHECK (btrim(body) <> ''),
    audience text NOT NULL CHECK (audience IN ('everyone', 'role', 'class', 'course-type', 'lesson')),
    role text CHECK (role IN ('member', 'instructor', 'office', 'admin')),
    class_id uuid REFERENCES classes ON DELETE RESTRICT,
    course_type_id uuid REFERENCES course_types ON DELETE RESTRICT,
    -- A lesson's announcement goes with it: lessons are deleted by timetable
    -- edits (ADR 0007), which an announcement must not block.
    lesson_id uuid REFERENCES lessons ON DELETE CASCADE,
    publish_at timestamptz NOT NULL,
    expires_at timestamptz CHECK (expires_at > publish_at),
    email boolean NOT NULL,
    -- Set once, when it is emailed: an announcement is emailed once.
    emailed_at timestamptz CHECK (emailed_at IS NULL OR email),
    email_recipients integer NOT NULL CHECK (email_recipients >= 0),
    -- While it is being sent in batches: the last address mailed, lowercased.
    mail_cursor text CHECK (mail_cursor IS NULL OR (mail_cursor <> '' AND email AND emailed_at IS NULL)),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at),
    -- Exactly the reference the audience needs.
    CONSTRAINT announcements_audience_ref CHECK (
        (role IS NOT NULL) = (audience = 'role')
        AND (class_id IS NOT NULL) = (audience = 'class')
        AND (course_type_id IS NOT NULL) = (audience = 'course-type')
        AND (lesson_id IS NOT NULL) = (audience = 'lesson')
    )
);

-- The lists, newest first, and the ones waiting for their mail.
CREATE INDEX announcements_publish_idx ON announcements (publish_at DESC, id DESC);
CREATE INDEX announcements_mail_due_idx ON announcements (publish_at, id) WHERE email AND emailed_at IS NULL;
CREATE INDEX announcements_author_idx ON announcements (author_id);
CREATE INDEX announcements_class_idx ON announcements (class_id) WHERE class_id IS NOT NULL;
CREATE INDEX announcements_course_type_idx ON announcements (course_type_id) WHERE course_type_id IS NOT NULL;
CREATE INDEX announcements_lesson_idx ON announcements (lesson_id) WHERE lesson_id IS NOT NULL;

-- An account's dismissals go with the announcement and with the account.
CREATE TABLE announcement_dismissals (
    announcement_id uuid NOT NULL REFERENCES announcements ON DELETE CASCADE,
    account_id uuid NOT NULL REFERENCES accounts ON DELETE CASCADE,
    dismissed_at timestamptz NOT NULL,
    PRIMARY KEY (account_id, announcement_id)
);

CREATE INDEX announcement_dismissals_announcement_idx ON announcement_dismissals (announcement_id);

CREATE TABLE announcement_mailings (
    id uuid PRIMARY KEY,
    -- NULL once the announcement is deleted.
    announcement_id uuid REFERENCES announcements ON DELETE SET NULL,
    sender_id uuid NOT NULL REFERENCES accounts ON DELETE RESTRICT,
    sent_at timestamptz NOT NULL,
    recipients integer NOT NULL CHECK (recipients > 0)
);

-- The daily limit counts the mailings of a school day.
CREATE INDEX announcement_mailings_sent_idx ON announcement_mailings (sent_at, id);
CREATE INDEX announcement_mailings_announcement_idx ON announcement_mailings (announcement_id);
CREATE INDEX announcement_mailings_sender_idx ON announcement_mailings (sender_id);

-- +goose Down

DROP TABLE announcement_mailings;
DROP TABLE announcement_dismissals;
DROP TABLE announcements;
