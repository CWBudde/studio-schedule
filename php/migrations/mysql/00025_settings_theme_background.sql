-- +goose Up

ALTER TABLE settings
    ADD COLUMN theme VARBINARY(32),
    ADD COLUMN background LONGBLOB,
    ADD COLUMN background_type VARBINARY(32),
    ADD COLUMN background_digest VARCHAR(64),
    ADD CONSTRAINT settings_theme_check CHECK (theme IN ('programme', 'werkstatt')),
    ADD CONSTRAINT settings_background_check CHECK (OCTET_LENGTH(background) BETWEEN 1 AND 1048576),
    ADD CONSTRAINT settings_background_type_check CHECK (background_type IN ('image/jpeg', 'image/png', 'image/webp')),
    ADD CONSTRAINT settings_background_digest_check CHECK (
        CHAR_LENGTH(background_digest) = 64 AND background_digest REGEXP '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT settings_background_type CHECK ((background IS NULL) = (background_type IS NULL)),
    ADD CONSTRAINT settings_background_digest CHECK ((background IS NULL) = (background_digest IS NULL));

-- +goose Down

ALTER TABLE settings
    DROP CONSTRAINT settings_background_digest,
    DROP CONSTRAINT settings_background_type,
    DROP CONSTRAINT settings_background_digest_check,
    DROP CONSTRAINT settings_background_type_check,
    DROP CONSTRAINT settings_background_check,
    DROP CONSTRAINT settings_theme_check,
    DROP COLUMN background_digest,
    DROP COLUMN background_type,
    DROP COLUMN background,
    DROP COLUMN theme;
