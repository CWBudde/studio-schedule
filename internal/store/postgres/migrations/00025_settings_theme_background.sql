-- Themes and layouts (ADR 0018): the school's theme and its background
-- image, which themes that show one put behind the app. The background is
-- stored like the logo (00017_receipts.sql, 00018_settings_accent.sql).
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE settings
    -- domain.Theme; NULL: the zero Theme, the app's own look (Programme).
    ADD COLUMN theme text CHECK (theme IN ('programme', 'werkstatt')),
    -- At most domain.MaxBackgroundBytes.
    ADD COLUMN background bytea CHECK (octet_length(background) BETWEEN 1 AND 1048576),
    ADD COLUMN background_type text CHECK (background_type IN ('image/jpeg', 'image/png', 'image/webp')),
    -- domain.Background.Digest, as logo_digest: hex SHA-256 of the content
    -- type, a line feed and the bytes. Written with the background, so a
    -- cache check need not read it.
    ADD COLUMN background_digest text CHECK (background_digest ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT settings_background_type CHECK ((background IS NULL) = (background_type IS NULL)),
    ADD CONSTRAINT settings_background_digest CHECK ((background IS NULL) = (background_digest IS NULL));

-- +goose Down

ALTER TABLE settings
    DROP CONSTRAINT settings_background_digest,
    DROP CONSTRAINT settings_background_type,
    DROP COLUMN background_digest,
    DROP COLUMN background_type,
    DROP COLUMN background,
    DROP COLUMN theme;
