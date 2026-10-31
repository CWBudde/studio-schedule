-- A manual "released through" date (ADR 0003, amendment 2026-10-31): the
-- office may release the schedule through a school date beyond the weekly
-- rule. It only extends the release window and never hides a released
-- lesson. Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE settings
    -- domain.Settings.ReleasedThrough; NULL: no date, only the weekly rule.
    ADD COLUMN released_through date;

-- +goose Down

ALTER TABLE settings DROP COLUMN released_through;
