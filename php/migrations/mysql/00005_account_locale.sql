-- The language of the mails an account receives (ADR 0008).
-- Conventions as in 00001_core.sql.

-- +goose Up

-- NULL means the school's default locale (domain zero value).
ALTER TABLE accounts ADD COLUMN locale LONGTEXT,
    ADD CONSTRAINT accounts_locale_check CHECK (OCTET_LENGTH(locale) > 0);

-- +goose Down

ALTER TABLE accounts DROP CONSTRAINT accounts_locale_check, DROP COLUMN locale;
