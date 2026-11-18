-- Announcement drafts (ADR 0014, amendment; ADR 0022, amendment): an
-- announcement without a publish time is a draft, saved but neither shown
-- nor mailed until the office publishes it. NULL compares as unknown, so
-- the shown and mail-due queries leave drafts out by themselves, and
-- DESC sorts NULLs first, so the list (and its index) puts them first. A
-- draft has no mail state.

-- +goose Up
ALTER TABLE announcements
    ALTER COLUMN publish_at DROP NOT NULL,
    ADD CONSTRAINT announcements_draft_unmailed CHECK (
        publish_at IS NOT NULL OR (emailed_at IS NULL AND mail_cursor IS NULL AND email_recipients = 0)
    );

-- +goose Down
DELETE FROM announcements WHERE publish_at IS NULL;
ALTER TABLE announcements
    DROP CONSTRAINT announcements_draft_unmailed,
    ALTER COLUMN publish_at SET NOT NULL;
