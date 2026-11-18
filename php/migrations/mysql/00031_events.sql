-- +goose Up

CREATE TABLE events (
    id BINARY(16) PRIMARY KEY,
    slug VARBINARY(80) NOT NULL,
    title LONGTEXT NOT NULL CHECK (CHAR_LENGTH(TRIM(title)) > 0),
    description LONGTEXT CHECK (CHAR_LENGTH(description) > 0),
    course_type_id BINARY(16) NOT NULL,
    capacity INT NOT NULL CHECK (capacity BETWEEN 1 AND 10000),
    price BIGINT NOT NULL CHECK (price BETWEEN 0 AND 100000000),
    vat_rate_bp INT NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    status VARBINARY(32) NOT NULL CHECK (status IN ('draft', 'published', 'canceled')),
    signup_opens_at DATETIME(6),
    signup_closes_at DATETIME(6),
    cancel_until DATETIME(6),
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    created_by BINARY(16),
    CONSTRAINT events_slug_format CHECK (
        CONVERT(slug USING utf8mb4) COLLATE utf8mb4_bin REGEXP '^[a-z0-9]+(-[a-z0-9]+)*$'
        AND CONVERT(slug USING utf8mb4) COLLATE utf8mb4_bin NOT REGEXP '[^a-z0-9-]'
        AND OCTET_LENGTH(slug) <= 80
    ),
    CONSTRAINT events_slug_key UNIQUE (slug),
    CONSTRAINT events_signup_window CHECK (signup_closes_at > signup_opens_at),
    CONSTRAINT events_updated_after_created CHECK (updated_at >= created_at),
    INDEX events_course_type_idx (course_type_id),
    INDEX events_created_by_idx (created_by),
    CONSTRAINT events_course_type_id_fkey FOREIGN KEY (course_type_id) REFERENCES course_types (id) ON DELETE RESTRICT,
    CONSTRAINT events_created_by_fkey FOREIGN KEY (created_by) REFERENCES accounts (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE event_images (
    event_id BINARY(16) PRIMARY KEY,
    image LONGBLOB NOT NULL CHECK (OCTET_LENGTH(image) BETWEEN 1 AND 1048576),
    image_type VARBINARY(32) NOT NULL CHECK (image_type IN ('image/jpeg', 'image/png', 'image/webp')),
    image_digest VARBINARY(64) NOT NULL CHECK (
        OCTET_LENGTH(image_digest) = 64
        AND CONVERT(image_digest USING utf8mb4) COLLATE utf8mb4_bin REGEXP '^[0-9a-f]{64}$'
    ),
    CONSTRAINT event_images_event_id_fkey FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE lessons
    ADD COLUMN event_id BINARY(16),
    ADD INDEX lessons_event_idx (event_id, starts_at, id),
    ADD CONSTRAINT lessons_event_id_fkey FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE RESTRICT,
    ADD CONSTRAINT lessons_event_session CHECK (event_id IS NULL OR (kind = 'workshop' AND class_id IS NULL));

CREATE TABLE event_registrations (
    id BINARY(16) PRIMARY KEY,
    event_id BINARY(16) NOT NULL,
    person_id BINARY(16),
    guest_name LONGTEXT CHECK (CHAR_LENGTH(TRIM(guest_name)) > 0),
    guest_email LONGTEXT CHECK (CHAR_LENGTH(guest_email) > 0),
    status VARBINARY(32) NOT NULL CHECK (status IN ('waiting', 'invited', 'registered', 'canceled')),
    payment VARBINARY(32) NOT NULL CHECK (payment IN ('unpaid', 'paid', 'refunded')),
    price BIGINT NOT NULL CHECK (price BETWEEN 0 AND 100000000),
    vat_rate_bp INT NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    receipt_id BINARY(16),
    cancellation_receipt_id BINARY(16),
    invite_token_hash VARBINARY(32) CHECK (OCTET_LENGTH(invite_token_hash) = 32),
    invited_at DATETIME(6),
    expires_at DATETIME(6),
    registered_by BINARY(16),
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    canceled_at DATETIME(6),
    -- The unique index applies only to a person's open registrations.
    open_person_id BINARY(16) GENERATED ALWAYS AS (
        CASE WHEN status IN ('waiting', 'invited', 'registered') THEN person_id END
    ) VIRTUAL,
    CONSTRAINT event_registrations_person_or_guest CHECK (
        (person_id IS NOT NULL AND guest_name IS NULL AND guest_email IS NULL)
        OR (person_id IS NULL AND guest_name IS NOT NULL)
    ),
    CONSTRAINT event_registrations_invite CHECK (
        (status = 'invited' AND invite_token_hash IS NOT NULL AND invited_at IS NOT NULL
            AND expires_at IS NOT NULL AND expires_at > invited_at)
        OR (status <> 'invited' AND invite_token_hash IS NULL AND invited_at IS NULL AND expires_at IS NULL)
    ),
    CONSTRAINT event_registrations_canceled CHECK ((status = 'canceled') = (canceled_at IS NOT NULL)),
    CONSTRAINT event_registrations_unpaid_no_receipt CHECK (payment <> 'unpaid' OR receipt_id IS NULL),
    CONSTRAINT event_registrations_storno CHECK (
        (payment = 'refunded') = (cancellation_receipt_id IS NOT NULL)
        AND (payment <> 'refunded' OR receipt_id IS NOT NULL)
    ),
    CONSTRAINT event_registrations_storno_other CHECK (cancellation_receipt_id <> receipt_id),
    CONSTRAINT event_registrations_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT event_registrations_receipt_key UNIQUE (receipt_id),
    CONSTRAINT event_registrations_cancellation_receipt_key UNIQUE (cancellation_receipt_id),
    CONSTRAINT event_registrations_token_key UNIQUE (invite_token_hash),
    CONSTRAINT event_registrations_one_open_key UNIQUE (event_id, open_person_id),
    INDEX event_registrations_event_idx (event_id, created_at, id),
    INDEX event_registrations_person_idx (person_id, created_at, id),
    INDEX event_registrations_registered_by_idx (registered_by),
    -- MySQL and MariaDB index every row instead of just guest registrations.
    INDEX event_registrations_guest_idx (event_id),
    CONSTRAINT event_registrations_event_id_fkey FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE RESTRICT,
    CONSTRAINT event_registrations_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons (id) ON DELETE RESTRICT,
    CONSTRAINT event_registrations_receipt_id_fkey FOREIGN KEY (receipt_id) REFERENCES receipts (id) ON DELETE RESTRICT,
    CONSTRAINT event_registrations_cancellation_receipt_id_fkey FOREIGN KEY (cancellation_receipt_id)
        REFERENCES receipts (id) ON DELETE RESTRICT,
    CONSTRAINT event_registrations_registered_by_fkey FOREIGN KEY (registered_by) REFERENCES accounts (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE event_registrations;

ALTER TABLE lessons
    DROP FOREIGN KEY lessons_event_id_fkey,
    DROP CONSTRAINT lessons_event_session;
ALTER TABLE lessons DROP INDEX lessons_event_idx, DROP COLUMN event_id;

DROP TABLE event_images;
DROP TABLE events;
