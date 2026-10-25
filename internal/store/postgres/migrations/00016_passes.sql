-- Passes (ADR 0012): the pass catalog, the passes of persons and which
-- bookings they paid for, and when a booking uses a pass (a school setting).
-- bookings.prepaid, unused since Phase 1, gives way to the redemptions: a
-- booking is paid when a pass paid for it.
-- Conventions as in 00001_core.sql. Passes and redemptions are never
-- deleted with what they refer to: a person with passes, a pass type that
-- was sold and a booking a pass paid for stay.

-- +goose Up

ALTER TABLE settings
    ADD COLUMN redemption_mode text NOT NULL DEFAULT 'attendance'
        CHECK (redemption_mode IN ('booking', 'attendance'));

ALTER TABLE bookings DROP COLUMN prepaid;

CREATE TABLE pass_types (
    id uuid PRIMARY KEY,
    name text COLLATE "C" NOT NULL CHECK (btrim(name) <> ''),
    kind text NOT NULL CHECK (kind IN ('visits', 'time')),
    -- NULL for a time pass.
    visits integer CHECK (visits BETWEEN 1 AND 1000),
    -- A visit pass: days valid from its first day, 0 for no expiry; a time
    -- pass: its length in days.
    validity_days integer NOT NULL CHECK (validity_days BETWEEN 0 AND 3660),
    -- Gross, in minor units of settings.currency.
    price bigint NOT NULL CHECK (price BETWEEN 0 AND 100000000),
    -- Basis points: 1900 is 19 %.
    vat_rate_bp integer NOT NULL CHECK (vat_rate_bp BETWEEN 0 AND 10000),
    active boolean NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT pass_types_visits_kind CHECK ((kind = 'visits') = (visits IS NOT NULL)),
    CONSTRAINT pass_types_time_days CHECK (kind = 'visits' OR validity_days > 0)
);

-- Unique ignoring ASCII case, like the catalog names (00006).
CREATE UNIQUE INDEX pass_types_name_key ON pass_types (lower(name));

CREATE TABLE passes (
    id uuid PRIMARY KEY,
    pass_type_id uuid NOT NULL REFERENCES pass_types ON DELETE RESTRICT,
    person_id uuid NOT NULL REFERENCES persons ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN ('visits', 'time')),
    -- NULL for a time pass.
    visits_total integer CHECK (visits_total BETWEEN 1 AND 1000),
    visits_remaining integer CHECK (visits_remaining >= 0),
    valid_from date NOT NULL,
    -- NULL: a visit pass without expiry.
    valid_until date,
    status text NOT NULL CHECK (status IN ('active', 'voided')),
    origin text NOT NULL CHECK (origin IN ('sale', 'assignment')),
    sold_at timestamptz NOT NULL,
    sold_by uuid REFERENCES accounts ON DELETE RESTRICT,
    payer_person_id uuid REFERENCES persons ON DELETE RESTRICT,
    -- The receipt of the sale; its foreign key comes with the receipts
    -- (Phase 6 B).
    receipt_id uuid,
    price_paid bigint NOT NULL CHECK (price_paid BETWEEN 0 AND 100000000),
    note text CHECK (note <> ''),
    voided_at timestamptz,
    CONSTRAINT passes_visits_kind CHECK (
        (kind = 'visits') = (visits_total IS NOT NULL) AND (visits_total IS NULL) = (visits_remaining IS NULL)
    ),
    CONSTRAINT passes_visits_remaining CHECK (visits_remaining <= visits_total),
    CONSTRAINT passes_time_until CHECK (kind = 'visits' OR valid_until IS NOT NULL),
    CONSTRAINT passes_until_after_from CHECK (valid_until >= valid_from),
    CONSTRAINT passes_voided CHECK ((status = 'voided') = (voided_at IS NOT NULL)),
    CONSTRAINT passes_sale_only CHECK (
        origin = 'sale' OR (payer_person_id IS NULL AND receipt_id IS NULL AND price_paid = 0)
    )
);

CREATE INDEX passes_person_idx ON passes (person_id, sold_at, id);
CREATE INDEX passes_pass_type_idx ON passes (pass_type_id);
CREATE INDEX passes_payer_idx ON passes (payer_person_id);

CREATE TABLE pass_redemptions (
    pass_id uuid NOT NULL REFERENCES passes ON DELETE RESTRICT,
    booking_id uuid NOT NULL REFERENCES bookings ON DELETE RESTRICT,
    at timestamptz NOT NULL,
    PRIMARY KEY (pass_id, booking_id),
    -- A booking uses one pass at most.
    CONSTRAINT pass_redemptions_booking_key UNIQUE (booking_id)
);

CREATE INDEX pass_redemptions_pass_idx ON pass_redemptions (pass_id, at, booking_id);

-- +goose Down

DROP TABLE pass_redemptions;
DROP TABLE passes;
DROP TABLE pass_types;

ALTER TABLE bookings ADD COLUMN prepaid boolean NOT NULL DEFAULT false;
ALTER TABLE bookings ALTER COLUMN prepaid DROP DEFAULT;

ALTER TABLE settings DROP COLUMN redemption_mode;
