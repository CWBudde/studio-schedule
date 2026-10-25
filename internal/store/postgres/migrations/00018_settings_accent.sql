-- Settings and branding (ADR 0013): the school's accent color, the digest
-- of the logo (its ETag), and the default language as one of the app's.
-- Conventions as in 00001_core.sql.

-- +goose Up

ALTER TABLE settings
    -- NULL: the app's own color. Lower case, so comparisons need no folding.
    ADD COLUMN accent_color text CHECK (accent_color ~ '^#[0-9a-f]{6}$'),
    -- domain.Logo.Digest: hex SHA-256 of the content type, a line feed and
    -- the bytes. Written with the logo, so a cache check need not read it.
    ADD COLUMN logo_digest text CHECK (logo_digest ~ '^[0-9a-f]{64}$');

UPDATE settings SET logo_digest = encode(sha256(convert_to(logo_type || E'\n', 'UTF8') || logo), 'hex')
WHERE logo IS NOT NULL;

ALTER TABLE settings ADD CONSTRAINT settings_logo_digest CHECK ((logo IS NULL) = (logo_digest IS NULL));

-- The default language was any language tag; it is now one of the app's
-- (domain.Locales): a tag's base language when that is one, else English,
-- the app's fallback (domain.SchoolLocale).
UPDATE settings SET default_locale = CASE
    WHEN lower(split_part(replace(btrim(default_locale), '_', '-'), '-', 1)) IN ('en', 'de')
        THEN lower(split_part(replace(btrim(default_locale), '_', '-'), '-', 1))
    ELSE 'en'
END;

-- +goose Down

-- The default language keeps its normalized value.
ALTER TABLE settings
    DROP CONSTRAINT settings_logo_digest,
    DROP COLUMN logo_digest,
    DROP COLUMN accent_color;
