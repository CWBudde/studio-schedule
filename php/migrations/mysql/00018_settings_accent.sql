-- +goose Up

ALTER TABLE settings
    ADD COLUMN accent_color VARCHAR(7),
    ADD COLUMN logo_digest VARCHAR(64),
    ADD CONSTRAINT settings_accent_color_check CHECK (
        CHAR_LENGTH(accent_color) = 7 AND accent_color REGEXP '^#[0-9a-f]{6}$'
    ),
    ADD CONSTRAINT settings_logo_digest_check CHECK (
        CHAR_LENGTH(logo_digest) = 64 AND logo_digest REGEXP '^[0-9a-f]{64}$'
    );

UPDATE settings SET logo_digest = LOWER(SHA2(CONCAT(logo_type, X'0A', logo), 256))
WHERE logo IS NOT NULL;

ALTER TABLE settings ADD CONSTRAINT settings_logo_digest CHECK ((logo IS NULL) = (logo_digest IS NULL));

-- Normalize to the base language when supported, and otherwise to English.
UPDATE settings SET default_locale = CASE
    WHEN CAST(LOWER(SUBSTRING_INDEX(REPLACE(TRIM(CONVERT(default_locale USING utf8mb4)), '_', '-'), '-', 1))
        AS BINARY) = 'de' THEN 'de'
    ELSE 'en'
END;

-- +goose Down

-- The default language keeps its normalized value.
ALTER TABLE settings
    DROP CONSTRAINT settings_logo_digest,
    DROP CONSTRAINT settings_logo_digest_check,
    DROP CONSTRAINT settings_accent_color_check,
    DROP COLUMN logo_digest,
    DROP COLUMN accent_color;
