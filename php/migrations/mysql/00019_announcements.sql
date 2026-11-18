-- +goose Up

CREATE TABLE announcements (
    id BINARY(16) PRIMARY KEY,
    author_id BINARY(16) NOT NULL,
    title LONGTEXT NOT NULL,
    body LONGTEXT NOT NULL,
    audience VARBINARY(32) NOT NULL,
    role VARBINARY(32),
    class_id BINARY(16),
    course_type_id BINARY(16),
    -- A lesson's announcement goes with it on timetable edits.
    lesson_id BINARY(16),
    publish_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6),
    email BOOLEAN NOT NULL,
    emailed_at DATETIME(6),
    email_recipients INT NOT NULL,
    -- While it is being sent in batches: the last address mailed, lowercased.
    mail_cursor LONGTEXT,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT announcements_title_check CHECK (OCTET_LENGTH(TRIM(title)) > 0),
    CONSTRAINT announcements_body_check CHECK (OCTET_LENGTH(TRIM(body)) > 0),
    CONSTRAINT announcements_audience_check CHECK (audience IN ('everyone', 'role', 'class', 'course-type', 'lesson')),
    CONSTRAINT announcements_role_check CHECK (role IN ('member', 'instructor', 'office', 'admin')),
    CONSTRAINT announcements_expires_at_check CHECK (expires_at > publish_at),
    CONSTRAINT announcements_emailed_at_check CHECK (emailed_at IS NULL OR email),
    CONSTRAINT announcements_email_recipients_check CHECK (email_recipients >= 0),
    CONSTRAINT announcements_mail_cursor_check CHECK (
        mail_cursor IS NULL OR (OCTET_LENGTH(mail_cursor) > 0 AND email AND emailed_at IS NULL)
    ),
    CONSTRAINT announcements_updated_at_check CHECK (updated_at >= created_at),
    CONSTRAINT announcements_audience_ref CHECK (
        (role IS NOT NULL) = (audience = 'role')
        AND (class_id IS NOT NULL) = (audience = 'class')
        AND (course_type_id IS NOT NULL) = (audience = 'course-type')
        AND (lesson_id IS NOT NULL) = (audience = 'lesson')
    ),
    KEY announcements_publish_idx (publish_at DESC, id DESC),
    KEY announcements_mail_due_idx (publish_at, id),
    KEY announcements_author_idx (author_id),
    KEY announcements_class_idx (class_id),
    KEY announcements_course_type_idx (course_type_id),
    KEY announcements_lesson_idx (lesson_id),
    CONSTRAINT announcements_author_id_fkey FOREIGN KEY (author_id) REFERENCES accounts(id) ON DELETE RESTRICT,
    CONSTRAINT announcements_class_id_fkey FOREIGN KEY (class_id) REFERENCES classes(id) ON DELETE RESTRICT,
    CONSTRAINT announcements_course_type_id_fkey
        FOREIGN KEY (course_type_id) REFERENCES course_types(id) ON DELETE RESTRICT,
    CONSTRAINT announcements_lesson_id_fkey FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE announcement_dismissals (
    announcement_id BINARY(16) NOT NULL,
    account_id BINARY(16) NOT NULL,
    dismissed_at DATETIME(6) NOT NULL,
    PRIMARY KEY (account_id, announcement_id),
    KEY announcement_dismissals_announcement_idx (announcement_id),
    CONSTRAINT announcement_dismissals_announcement_id_fkey
        FOREIGN KEY (announcement_id) REFERENCES announcements(id) ON DELETE CASCADE,
    CONSTRAINT announcement_dismissals_account_id_fkey
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE announcement_mailings (
    id BINARY(16) PRIMARY KEY,
    -- NULL once the announcement is deleted. The mailing still counts for the daily limit.
    announcement_id BINARY(16),
    sender_id BINARY(16) NOT NULL,
    sent_at DATETIME(6) NOT NULL,
    recipients INT NOT NULL,
    CONSTRAINT announcement_mailings_recipients_check CHECK (recipients > 0),
    KEY announcement_mailings_sent_idx (sent_at, id),
    KEY announcement_mailings_announcement_idx (announcement_id),
    KEY announcement_mailings_sender_idx (sender_id),
    CONSTRAINT announcement_mailings_announcement_id_fkey
        FOREIGN KEY (announcement_id) REFERENCES announcements(id) ON DELETE SET NULL,
    CONSTRAINT announcement_mailings_sender_id_fkey FOREIGN KEY (sender_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE announcement_mailings;
DROP TABLE announcement_dismissals;
DROP TABLE announcements;
