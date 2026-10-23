-- The language of the mails an account receives (ADR 0007).
-- Conventions as in 00001_core.sql.

-- +goose Up

-- NULL means the school's default locale (domain zero value).
ALTER TABLE accounts ADD COLUMN locale text CHECK (locale <> '');

-- +goose Down

ALTER TABLE accounts DROP COLUMN locale;
