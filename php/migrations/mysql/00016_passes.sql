-- +goose Up

ALTER TABLE settings
    ADD COLUMN redemption_mode VARBINARY(32) NOT NULL DEFAULT 'attendance',
    ADD CONSTRAINT settings_redemption_mode_check CHECK (redemption_mode IN ('booking', 'attendance'));

ALTER TABLE bookings DROP COLUMN prepaid;

CREATE TABLE pass_types (
    id BINARY(16) PRIMARY KEY,
    name LONGTEXT NOT NULL,
    kind VARBINARY(32) NOT NULL,
    visits INT,
    validity_days INT NOT NULL,
    price BIGINT NOT NULL,
    vat_rate_bp INT NOT NULL,
    active BOOLEAN NOT NULL,
    created_at DATETIME(6) NOT NULL,
    -- PostgreSQL lower() with C collation folds ASCII only.
    name_key VARBINARY(3072) GENERATED ALWAYS AS (
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(
        REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(CAST(name AS BINARY),
            'A', 'a'), 'B', 'b'), 'C', 'c'), 'D', 'd'), 'E', 'e'), 'F', 'f'), 'G', 'g'),
            'H', 'h'), 'I', 'i'), 'J', 'j'), 'K', 'k'), 'L', 'l'), 'M', 'm'), 'N', 'n'),
            'O', 'o'), 'P', 'p'), 'Q', 'q'), 'R', 'r'), 'S', 's'), 'T', 't'), 'U', 'u'),
            'V', 'v'), 'W', 'w'), 'X', 'x'), 'Y', 'y'), 'Z', 'z')
    ) VIRTUAL,
    CONSTRAINT pass_types_name_check CHECK (OCTET_LENGTH(TRIM(name)) > 0),
    CONSTRAINT pass_types_name_index_length CHECK (OCTET_LENGTH(name) <= 3072),
    CONSTRAINT pass_types_kind_check CHECK (kind IN ('visits', 'time')),
    CONSTRAINT pass_types_visits_check CHECK (visits BETWEEN 1 AND 1000),
    CONSTRAINT pass_types_validity_days_check CHECK (validity_days BETWEEN 0 AND 3660),
    CONSTRAINT pass_types_price_check CHECK (price BETWEEN 0 AND 100000000),
    CONSTRAINT pass_types_vat_rate_bp_check CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    CONSTRAINT pass_types_visits_kind CHECK ((kind = 'visits') = (visits IS NOT NULL)),
    CONSTRAINT pass_types_time_days CHECK (kind = 'visits' OR validity_days > 0),
    UNIQUE KEY pass_types_name_key (name_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE passes (
    id BINARY(16) PRIMARY KEY,
    pass_type_id BINARY(16) NOT NULL,
    person_id BINARY(16) NOT NULL,
    kind VARBINARY(32) NOT NULL,
    visits_total INT,
    visits_remaining INT,
    valid_from DATE NOT NULL,
    valid_until DATE,
    status VARBINARY(32) NOT NULL,
    origin VARBINARY(32) NOT NULL,
    sold_at DATETIME(6) NOT NULL,
    sold_by BINARY(16),
    payer_person_id BINARY(16),
    receipt_id BINARY(16),
    price_paid BIGINT NOT NULL,
    note LONGTEXT,
    voided_at DATETIME(6),
    CONSTRAINT passes_kind_check CHECK (kind IN ('visits', 'time')),
    CONSTRAINT passes_visits_total_check CHECK (visits_total BETWEEN 1 AND 1000),
    CONSTRAINT passes_visits_remaining_check CHECK (visits_remaining >= 0),
    CONSTRAINT passes_status_check CHECK (status IN ('active', 'voided')),
    CONSTRAINT passes_origin_check CHECK (origin IN ('sale', 'assignment')),
    CONSTRAINT passes_price_paid_check CHECK (price_paid BETWEEN 0 AND 100000000),
    CONSTRAINT passes_note_check CHECK (OCTET_LENGTH(note) > 0),
    CONSTRAINT passes_visits_kind CHECK (
        (kind = 'visits') = (visits_total IS NOT NULL) AND (visits_total IS NULL) = (visits_remaining IS NULL)
    ),
    CONSTRAINT passes_visits_remaining CHECK (visits_remaining <= visits_total),
    CONSTRAINT passes_time_until CHECK (kind = 'visits' OR valid_until IS NOT NULL),
    CONSTRAINT passes_until_after_from CHECK (valid_until >= valid_from),
    CONSTRAINT passes_voided CHECK ((status = 'voided') = (voided_at IS NOT NULL)),
    CONSTRAINT passes_sale_only CHECK (
        origin = 'sale' OR (payer_person_id IS NULL AND receipt_id IS NULL AND price_paid = 0)
    ),
    KEY passes_person_idx (person_id, sold_at, id),
    KEY passes_pass_type_idx (pass_type_id),
    KEY passes_payer_idx (payer_person_id),
    KEY passes_sold_by_fkey (sold_by),
    CONSTRAINT passes_pass_type_id_fkey FOREIGN KEY (pass_type_id) REFERENCES pass_types(id) ON DELETE RESTRICT,
    CONSTRAINT passes_person_id_fkey FOREIGN KEY (person_id) REFERENCES persons(id) ON DELETE RESTRICT,
    CONSTRAINT passes_sold_by_fkey FOREIGN KEY (sold_by) REFERENCES accounts(id) ON DELETE RESTRICT,
    CONSTRAINT passes_payer_person_id_fkey FOREIGN KEY (payer_person_id) REFERENCES persons(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE pass_redemptions (
    pass_id BINARY(16) NOT NULL,
    booking_id BINARY(16) NOT NULL,
    at DATETIME(6) NOT NULL,
    PRIMARY KEY (pass_id, booking_id),
    UNIQUE KEY pass_redemptions_booking_key (booking_id),
    KEY pass_redemptions_pass_idx (pass_id, at, booking_id),
    CONSTRAINT pass_redemptions_pass_id_fkey FOREIGN KEY (pass_id) REFERENCES passes(id) ON DELETE RESTRICT,
    CONSTRAINT pass_redemptions_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES bookings(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down

DROP TABLE pass_redemptions;
DROP TABLE passes;
DROP TABLE pass_types;

ALTER TABLE bookings ADD COLUMN prepaid BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE bookings ALTER COLUMN prepaid DROP DEFAULT;

ALTER TABLE settings
    DROP CONSTRAINT settings_redemption_mode_check,
    DROP COLUMN redemption_mode;
