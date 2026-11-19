-- Draft announcements have no publication time or mail progress.

-- +goose Up

ALTER TABLE announcements
    MODIFY COLUMN publish_at DATETIME(6),
    ADD CONSTRAINT announcements_draft_unmailed CHECK (
        publish_at IS NOT NULL OR (emailed_at IS NULL AND mail_cursor IS NULL AND email_recipients = 0)
    );

-- +goose Down

DELETE FROM announcements WHERE publish_at IS NULL;
ALTER TABLE announcements
    DROP CONSTRAINT announcements_draft_unmailed,
    MODIFY COLUMN publish_at DATETIME(6) NOT NULL;
